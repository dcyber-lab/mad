package jqgo

import (
	"fmt"
)

type parser struct {
	src  string
	toks []token
	pos  int
}

func parse(src string) (node, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{src: src, toks: toks}
	n, err := p.parseProgram()
	if err != nil {
		return nil, err
	}
	return n, nil
}

// parseProgram parses a whole query; a program consisting only of
// definitions behaves as ".".
func (p *parser) parseProgram() (n node, err error) {
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(*ParseError); ok {
				err = pe
				return
			}
			panic(r)
		}
	}()
	n = p.parsePipe(false)
	if t := p.peek(); t.kind != tokEOF {
		p.fail(t, "")
	}
	return n, nil
}

// parseDefs parses a sequence of definitions (the builtin library).
func parseDefs(src string) (defs []*funcDef, err error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{src: src, toks: toks}
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(*ParseError); ok {
				err = pe
				return
			}
			panic(r)
		}
	}()
	for p.peek().kind != tokEOF {
		defs = append(defs, p.parseFuncDef())
	}
	return defs, nil
}

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) peekAt(i int) token {
	if p.pos+i < len(p.toks) {
		return p.toks[p.pos+i]
	}
	return p.toks[len(p.toks)-1]
}

func (p *parser) advance() token {
	t := p.toks[p.pos]
	if t.kind != tokEOF {
		p.pos++
	}
	return t
}

func (p *parser) isOp(s string) bool {
	t := p.peek()
	return t.kind == tokOp && t.text == s
}

func (p *parser) isKeyword(s string) bool {
	t := p.peek()
	return t.kind == tokIdent && t.text == s
}

func (p *parser) fail(t token, want string) {
	var got string
	switch t.kind {
	case tokEOF:
		got = "end of query"
	case tokString:
		got = "string"
	case tokNumber:
		got = "number"
	case tokVar:
		got = "$" + t.text
	case tokField:
		got = "." + t.text
	case tokFormat:
		got = "@" + t.text
	default:
		got = fmt.Sprintf("%q", t.text)
	}
	msg := "syntax error: unexpected " + got
	if want != "" {
		msg += ", expecting " + want
	}
	panic(&ParseError{Query: p.src, Offset: t.pos, Msg: msg})
}

func (p *parser) expectOp(s string) token {
	if !p.isOp(s) {
		p.fail(p.peek(), fmt.Sprintf("%q", s))
	}
	return p.advance()
}

func (p *parser) expectKeyword(s string) {
	if !p.isKeyword(s) {
		p.fail(p.peek(), fmt.Sprintf("%q", s))
	}
	p.advance()
}

var keywords = map[string]bool{
	"def": true, "if": true, "then": true, "elif": true, "else": true, "end": true,
	"as": true, "reduce": true, "foreach": true, "try": true, "catch": true,
	"label": true, "import": true, "include": true, "and": true, "or": true,
	"__loc__": true,
}

// parsePipe: lowest precedence. noComma is used for object values, where
// a comma separates entries.
func (p *parser) parsePipe(noComma bool) node {
	if p.isKeyword("def") {
		def := p.parseFuncDef()
		return &funcDefNode{def: def, rest: p.parsePipe(noComma)}
	}
	var left node
	if noComma {
		left = p.parseAlt()
	} else {
		left = p.parseComma()
	}
	if p.isOp("|") {
		p.advance()
		return &pipeNode{left: left, right: p.parsePipe(noComma)}
	}
	return left
}

func (p *parser) parseComma() node {
	left := p.parseAlt()
	for p.isOp(",") {
		p.advance()
		left = &commaNode{left: left, right: p.parseAlt()}
	}
	return left
}

// parseAlt: '//' is right associative and binds looser than assignment.
func (p *parser) parseAlt() node {
	left := p.parseAssign()
	if p.isOp("//") {
		p.advance()
		return &altNode{left: left, right: p.parseAlt()}
	}
	return left
}

var assignOps = map[string]bool{"=": true, "|=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true, "//=": true}

func (p *parser) parseAssign() node {
	left := p.parseOr()
	if t := p.peek(); t.kind == tokOp && assignOps[t.text] {
		p.advance()
		// nonassoc in jq; the right side may itself contain '//'
		right := p.parseAlt()
		return &assignNode{op: t.text, left: left, right: right}
	}
	return left
}

func (p *parser) parseOr() node {
	left := p.parseAnd()
	for p.isKeyword("or") {
		p.advance()
		left = &orNode{left: left, right: p.parseAnd()}
	}
	return left
}

func (p *parser) parseAnd() node {
	left := p.parseCompare()
	for p.isKeyword("and") {
		p.advance()
		left = &andNode{left: left, right: p.parseCompare()}
	}
	return left
}

var compareOps = map[string]bool{"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true}

func (p *parser) parseCompare() node {
	left := p.parseAdditive()
	if t := p.peek(); t.kind == tokOp && compareOps[t.text] {
		p.advance()
		right := p.parseAdditive()
		if t2 := p.peek(); t2.kind == tokOp && compareOps[t2.text] {
			p.fail(t2, "")
		}
		return &binopNode{op: t.text, left: left, right: right}
	}
	return left
}

func (p *parser) parseAdditive() node {
	left := p.parseMultiplicative()
	for {
		t := p.peek()
		if t.kind != tokOp || (t.text != "+" && t.text != "-") {
			return left
		}
		p.advance()
		left = &binopNode{op: t.text, left: left, right: p.parseMultiplicative()}
	}
}

func (p *parser) parseMultiplicative() node {
	left := p.parseUnary()
	for {
		t := p.peek()
		if t.kind != tokOp || (t.text != "*" && t.text != "/" && t.text != "%") {
			return left
		}
		p.advance()
		left = &binopNode{op: t.text, left: left, right: p.parseUnary()}
	}
}

func (p *parser) parseUnary() node {
	if p.isOp("-") {
		p.advance()
		x := p.parseUnary()
		if lit, ok := x.(literalNode); ok {
			switch v := lit.v.(type) {
			case int:
				if v != 0 {
					return literalNode{v: -v}
				}
			case float64:
				return literalNode{v: -v}
			}
		}
		return &negNode{x: x}
	}
	return p.parsePostfix(true)
}

// parsePostfix parses a term with its suffixes. With allowAs, a trailing
// "as $x | body" turns it into a binding whose body extends to the end.
func (p *parser) parsePostfix(allowAs bool) node {
	term := p.parsePrimary()
	for {
		t := p.peek()
		switch {
		case t.kind == tokField:
			p.advance()
			term = &indexNode{term: term, name: t.text}
			continue
		case t.kind == tokOp && t.text == "." && p.peekAt(1).kind == tokString:
			p.advance()
			key := p.parseString("")
			term = &indexNode{term: term, key: key}
			continue
		case t.kind == tokOp && t.text == "." && p.peekAt(1).kind == tokOp && p.peekAt(1).text == "[":
			p.advance()
			continue
		case t.kind == tokOp && t.text == "[":
			term = p.parseBracketSuffix(term)
			continue
		case t.kind == tokOp && t.text == "?":
			p.advance()
			term = &tryNode{body: term}
			continue
		case t.kind == tokOp && t.text == "?//":
			// ".a?//1" lexes as ?// but means .a? // 1 outside patterns.
			p.toks[p.pos] = token{kind: tokOp, text: "//", pos: t.pos + 1}
			term = &tryNode{body: term}
			continue
		case t.kind == tokIdent && t.text == "as" && allowAs:
			p.advance()
			pats := []pattern{p.parsePattern()}
			for p.isOp("?//") {
				p.advance()
				pats = append(pats, p.parsePattern())
			}
			p.expectOp("|")
			body := p.parsePipe(false)
			return &bindNode{src: term, pats: pats, body: body}
		}
		return term
	}
}

func (p *parser) parseBracketSuffix(term node) node {
	p.expectOp("[")
	if p.isOp("]") {
		p.advance()
		return &iterateNode{term: term}
	}
	if p.isOp(":") {
		p.advance()
		to := p.parsePipe(false)
		p.expectOp("]")
		return &sliceNode{term: term, to: to}
	}
	key := p.parsePipe(false)
	if p.isOp(":") {
		p.advance()
		var to node
		if !p.isOp("]") {
			to = p.parsePipe(false)
		}
		p.expectOp("]")
		return &sliceNode{term: term, from: key, to: to}
	}
	p.expectOp("]")
	return &indexNode{term: term, key: key}
}

func (p *parser) parsePrimary() node {
	t := p.peek()
	switch t.kind {
	case tokNumber:
		p.advance()
		return literalNode{v: t.num}
	case tokString:
		return p.parseString("")
	case tokFormat:
		p.advance()
		if p.peek().kind == tokString {
			return p.parseString(t.text)
		}
		return &formatNode{name: t.text}
	case tokField:
		p.advance()
		return &indexNode{term: identityNode{}, name: t.text}
	case tokVar:
		p.advance()
		if t.text == "__loc__" {
			line, _ := lineCol(p.src, t.pos)
			return literalNode{v: map[string]any{"file": "<top-level>", "line": line}}
		}
		return &varNode{name: t.text}
	case tokOp:
		switch t.text {
		case ".":
			p.advance()
			if p.peek().kind == tokString {
				return &indexNode{term: identityNode{}, key: p.parseString("")}
			}
			return identityNode{}
		case "..":
			p.advance()
			return recurseAllNode{}
		case "(":
			p.advance()
			x := p.parsePipe(false)
			p.expectOp(")")
			return x
		case "[":
			p.advance()
			if p.isOp("]") {
				p.advance()
				return &arrayNode{}
			}
			x := p.parsePipe(false)
			p.expectOp("]")
			return &arrayNode{x: x}
		case "{":
			return p.parseObject()
		}
	case tokIdent:
		switch t.text {
		case "if":
			return p.parseIf()
		case "try":
			p.advance()
			body := p.parsePostfixNoAs()
			var catch node
			if p.isKeyword("catch") {
				p.advance()
				catch = p.parsePostfixNoAs()
			}
			return &tryNode{body: body, catch: catch}
		case "reduce":
			p.advance()
			src := p.parsePostfix(false)
			p.expectKeyword("as")
			pat := p.parsePattern()
			p.expectOp("(")
			init := p.parsePipe(false)
			p.expectOp(";")
			update := p.parsePipe(false)
			p.expectOp(")")
			return &reduceNode{src: src, pat: pat, init: init, update: update}
		case "foreach":
			p.advance()
			src := p.parsePostfix(false)
			p.expectKeyword("as")
			pat := p.parsePattern()
			p.expectOp("(")
			init := p.parsePipe(false)
			p.expectOp(";")
			update := p.parsePipe(false)
			var extract node
			if p.isOp(";") {
				p.advance()
				extract = p.parsePipe(false)
			}
			p.expectOp(")")
			return &foreachNode{src: src, pat: pat, init: init, update: update, extract: extract}
		case "label":
			p.advance()
			v := p.advance()
			if v.kind != tokVar {
				p.fail(v, "$name")
			}
			p.expectOp("|")
			return &labelNode{name: v.text, body: p.parsePipe(false)}
		case "def":
			def := p.parseFuncDef()
			return &funcDefNode{def: def, rest: p.parsePipe(false)}
		case "break":
			p.advance()
			v := p.advance()
			if v.kind != tokVar {
				p.fail(v, "$name")
			}
			return &breakNode{name: v.text}
		case "import", "include":
			panic(&ParseError{Query: p.src, Offset: t.pos, Msg: "modules (import/include) are not supported"})
		}
		if keywords[t.text] {
			p.fail(t, "")
		}
		p.advance()
		if !p.isOp("(") {
			switch t.text {
			case "null":
				return literalNode{v: nil}
			case "true":
				return literalNode{v: true}
			case "false":
				return literalNode{v: false}
			}
		}
		return p.parseCall(t.text)
	}
	p.fail(t, "")
	return nil
}

// parsePostfixNoAs parses the body of try/catch: a postfix term.
func (p *parser) parsePostfixNoAs() node {
	if p.isOp("-") {
		return p.parseUnary()
	}
	return p.parsePostfix(false)
}

func (p *parser) parseCall(name string) node {
	call := &callNode{name: name}
	if p.isOp("(") {
		p.advance()
		for {
			call.args = append(call.args, p.parsePipe(false))
			if p.isOp(";") {
				p.advance()
				continue
			}
			p.expectOp(")")
			break
		}
	}
	for _, a := range call.args {
		call.argDefs = append(call.argDefs, &funcDef{body: a})
	}
	return call
}

func (p *parser) parseIf() node {
	p.expectKeyword("if")
	cond := p.parsePipe(false)
	p.expectKeyword("then")
	then := p.parsePipe(false)
	n := &ifNode{cond: cond, then: then}
	switch {
	case p.isKeyword("elif"):
		// elif is sugar for else if ... end, sharing our "end"
		p.toks[p.pos].text = "if"
		n.els = p.parseIf()
		return n
	case p.isKeyword("else"):
		p.advance()
		n.els = p.parsePipe(false)
	}
	p.expectKeyword("end")
	return n
}

func (p *parser) parseFuncDef() *funcDef {
	p.expectKeyword("def")
	t := p.advance()
	if t.kind != tokIdent || keywords[t.text] {
		p.fail(t, "function name")
	}
	def := &funcDef{name: t.text}
	if p.isOp("(") {
		p.advance()
		for {
			a := p.advance()
			switch {
			case a.kind == tokVar:
				def.params = append(def.params, param{name: a.text, isVar: true})
			case a.kind == tokIdent && !keywords[a.text]:
				def.params = append(def.params, param{name: a.text})
			default:
				p.fail(a, "parameter name")
			}
			if p.isOp(";") {
				p.advance()
				continue
			}
			p.expectOp(")")
			break
		}
	}
	p.expectOp(":")
	def.body = p.parsePipe(false)
	p.expectOp(";")
	return def
}

func (p *parser) parseString(format string) node {
	t := p.advance()
	if t.kind != tokString {
		p.fail(t, "string")
	}
	n := &stringNode{format: format}
	for _, part := range t.parts {
		if !part.isExpr {
			n.parts = append(n.parts, textNode{s: part.lit})
			continue
		}
		sub := &parser{src: p.src, toks: part.interp}
		x := sub.parsePipe(false)
		if tt := sub.peek(); tt.kind != tokEOF {
			sub.fail(tt, "\")\"")
		}
		n.parts = append(n.parts, x)
	}
	if format == "" && len(n.parts) == 1 {
		if t, ok := n.parts[0].(textNode); ok {
			return literalNode{v: t.s}
		}
	}
	return n
}

func (p *parser) parseObject() node {
	p.expectOp("{")
	obj := &objectNode{}
	for !p.isOp("}") {
		t := p.peek()
		var e objEntry
		switch {
		case t.kind == tokVar:
			p.advance()
			if t.text == "__loc__" {
				line, _ := lineCol(p.src, t.pos)
				e = objEntry{key: literalNode{v: "__loc__"}, value: literalNode{v: map[string]any{"file": "<top-level>", "line": line}}}
			} else if p.isOp(":") {
				e.key = &varNode{name: t.text} // {$k: v} uses $k's value as the key
			} else {
				e = objEntry{key: literalNode{v: t.text}, value: &varNode{name: t.text}}
			}
		case t.kind == tokIdent:
			p.advance()
			e.key = literalNode{v: t.text}
		case t.kind == tokNumber:
			p.advance()
			e.key = literalNode{v: t.num} // rejected at runtime, like jq
		case t.kind == tokString:
			e.key = p.parseString("")
		case t.kind == tokFormat && p.peekAt(1).kind == tokString:
			p.advance()
			e.key = p.parseString(t.text)
		case t.kind == tokOp && t.text == "(":
			p.advance()
			e.key = p.parsePipe(false)
			p.expectOp(")")
			if !p.isOp(":") {
				p.fail(p.peek(), "\":\"")
			}
		default:
			p.fail(t, "object key")
		}
		if e.value == nil {
			if p.isOp(":") {
				p.advance()
				e.value = p.parseObjectValue()
			} else {
				// {a} is {a: .a}; {"a\(1)"} is {"a1": .["a1"]}
				e.value = &indexNode{term: identityNode{}, key: e.key}
			}
		}
		obj.entries = append(obj.entries, e)
		if p.isOp(",") {
			p.advance()
			continue
		}
		if !p.isOp("}") {
			p.fail(p.peek(), "\"}\"")
		}
	}
	p.advance()
	return obj
}

// parseObjectValue parses an object value: a pipeline without top-level commas.
func (p *parser) parseObjectValue() node {
	return p.parsePipe(true)
}

func (p *parser) parsePattern() pattern {
	t := p.peek()
	switch {
	case t.kind == tokVar:
		p.advance()
		return pattern{kind: 'v', name: t.text}
	case t.kind == tokOp && t.text == "[":
		p.advance()
		pat := pattern{kind: 'a'}
		if p.isOp("]") {
			p.fail(p.peek(), "pattern")
		}
		for {
			pat.elems = append(pat.elems, p.parsePattern())
			if p.isOp(",") {
				p.advance()
				continue
			}
			p.expectOp("]")
			return pat
		}
	case t.kind == tokOp && t.text == "{":
		p.advance()
		pat := pattern{kind: 'o'}
		for {
			var op objPattern
			k := p.peek()
			switch {
			case k.kind == tokVar:
				p.advance()
				op.keyVar = k.text
				op.key = literalNode{v: k.text}
			case k.kind == tokIdent:
				p.advance()
				op.key = literalNode{v: k.text}
			case k.kind == tokString:
				op.key = p.parseString("")
			case k.kind == tokOp && k.text == "(":
				p.advance()
				op.key = p.parsePipe(false)
				p.expectOp(")")
			default:
				p.fail(k, "object pattern")
			}
			if p.isOp(":") {
				p.advance()
				sub := p.parsePattern()
				op.val = &sub
			} else if op.keyVar == "" {
				p.fail(p.peek(), "\":\"")
			}
			pat.obj = append(pat.obj, op)
			if p.isOp(",") {
				p.advance()
				continue
			}
			p.expectOp("}")
			return pat
		}
	}
	p.fail(t, "pattern")
	return pattern{}
}
