package jqgo

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type tokenKind int

const (
	tokEOF    tokenKind = iota
	tokIdent            // foo, keywords too
	tokField            // .foo
	tokVar              // $foo
	tokFormat           // @base64
	tokNumber           // 1.5
	tokString           // "..." (possibly with interpolation)
	tokOp               // punctuation and operators
)

type token struct {
	kind  tokenKind
	text  string // identifier, field or variable name, operator
	num   any    // tokNumber
	parts []strPart
	pos   int
}

// strPart is either literal text or the tokens of an interpolated \( ... ).
type strPart struct {
	lit    string
	interp []token
	isExpr bool
}

// ParseError describes a syntax error in a query.
type ParseError struct {
	Query  string
	Offset int
	Msg    string
}

func (e *ParseError) Error() string {
	line, col := lineCol(e.Query, e.Offset)
	return fmt.Sprintf("%s at line %d, column %d", e.Msg, line, col)
}

func lineCol(src string, off int) (int, int) {
	if off > len(src) {
		off = len(src)
	}
	line := 1 + strings.Count(src[:off], "\n")
	col := off - strings.LastIndex(src[:off], "\n")
	return line, col
}

type lexer struct {
	src string
	pos int
}

var operators = []string{
	"?//=", "?//", "//=", "|=", "+=", "-=", "*=", "/=", "%=", "==", "!=", "<=", ">=", "//", "..",
	".", "[", "]", "(", ")", "{", "}", "|", ",", ":", ";", "=", "<", ">", "+", "-", "*", "/", "%", "?",
}

func lex(src string) ([]token, error) {
	l := &lexer{src: src}
	var toks []token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.kind == tokEOF {
			return toks, nil
		}
	}
}

func (l *lexer) errorf(pos int, format string, args ...any) error {
	return &ParseError{Query: l.src, Offset: pos, Msg: fmt.Sprintf(format, args...)}
}

func (l *lexer) skipSpace() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			l.pos++
		case c == '#':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				// A backslash before the newline continues the comment (jq 1.7).
				if l.src[l.pos] == '\\' && l.pos+1 < len(l.src) {
					l.pos++
				}
				l.pos++
			}
		default:
			return
		}
	}
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (l *lexer) ident() string {
	start := l.pos
	for l.pos < len(l.src) {
		if isIdentChar(l.src[l.pos]) {
			l.pos++
			continue
		}
		// module separator, e.g. foo::bar
		if l.src[l.pos] == ':' && l.pos+2 < len(l.src) && l.src[l.pos+1] == ':' && isIdentStart(l.src[l.pos+2]) {
			l.pos += 2
			continue
		}
		break
	}
	return l.src[start:l.pos]
}

func (l *lexer) next() (token, error) {
	l.skipSpace()
	start := l.pos
	if l.pos >= len(l.src) {
		return token{kind: tokEOF, pos: start}, nil
	}
	c := l.src[l.pos]
	switch {
	case c == '"':
		l.pos++
		parts, err := l.stringParts(start)
		if err != nil {
			return token{}, err
		}
		return token{kind: tokString, parts: parts, pos: start}, nil
	case isIdentStart(c):
		return token{kind: tokIdent, text: l.ident(), pos: start}, nil
	case c == '$':
		l.pos++
		if l.pos < len(l.src) && isIdentStart(l.src[l.pos]) {
			return token{kind: tokVar, text: l.ident(), pos: start}, nil
		}
		return token{}, l.errorf(start, "syntax error: unexpected '$'")
	case c == '@':
		l.pos++
		if l.pos < len(l.src) && isIdentStart(l.src[l.pos]) {
			return token{kind: tokFormat, text: l.ident(), pos: start}, nil
		}
		return token{}, l.errorf(start, "syntax error: unexpected '@'")
	case isDigit(c) || (c == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1])):
		return l.number()
	case c == '.' && l.pos+1 < len(l.src) && isIdentStart(l.src[l.pos+1]):
		l.pos++
		name := l.ident()
		return token{kind: tokField, text: name, pos: start}, nil
	}
	for _, op := range operators {
		if strings.HasPrefix(l.src[l.pos:], op) {
			l.pos += len(op)
			return token{kind: tokOp, text: op, pos: start}, nil
		}
	}
	r, _ := utf8.DecodeRuneInString(l.src[l.pos:])
	return token{}, l.errorf(start, "syntax error: unexpected %q", r)
}

func (l *lexer) number() (token, error) {
	start := l.pos
	for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
		l.pos++
	}
	isInt := true
	if l.pos < len(l.src) && l.src[l.pos] == '.' {
		isInt = false
		l.pos++
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
		}
	}
	if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
		p := l.pos + 1
		if p < len(l.src) && (l.src[p] == '+' || l.src[p] == '-') {
			p++
		}
		if p < len(l.src) && isDigit(l.src[p]) {
			isInt = false
			l.pos = p
			for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
				l.pos++
			}
		}
	}
	text := l.src[start:l.pos]
	if isInt {
		if i, err := strconv.ParseInt(text, 10, 64); err == nil && int64(int(i)) == i {
			return token{kind: tokNumber, num: int(i), pos: start}, nil
		}
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); !ok || ne.Err != strconv.ErrRange {
			return token{}, l.errorf(start, "invalid number %q", text)
		}
	}
	return token{kind: tokNumber, num: f, pos: start}, nil
}

// stringParts lexes the body of a string literal whose opening quote has
// already been consumed.
func (l *lexer) stringParts(start int) ([]strPart, error) {
	var parts []strPart
	var sb strings.Builder
	for {
		if l.pos >= len(l.src) {
			return nil, l.errorf(start, "unterminated string literal")
		}
		c := l.src[l.pos]
		switch c {
		case '"':
			l.pos++
			if sb.Len() > 0 || len(parts) == 0 {
				parts = append(parts, strPart{lit: sb.String()})
			}
			return parts, nil
		case '\\':
			if l.pos+1 >= len(l.src) {
				return nil, l.errorf(start, "unterminated string literal")
			}
			e := l.src[l.pos+1]
			l.pos += 2
			switch e {
			case '"', '\\', '/':
				sb.WriteByte(e)
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'u':
				r, err := l.hex4()
				if err != nil {
					return nil, err
				}
				if utf16.IsSurrogate(r) && strings.HasPrefix(l.src[l.pos:], `\u`) {
					save := l.pos
					l.pos += 2
					r2, err := l.hex4()
					if err == nil && utf16.DecodeRune(r, r2) != utf8.RuneError {
						r = utf16.DecodeRune(r, r2)
					} else {
						l.pos = save
					}
				}
				sb.WriteRune(r)
			case '(':
				if sb.Len() > 0 {
					parts = append(parts, strPart{lit: sb.String()})
					sb.Reset()
				}
				toks, err := l.interpolation()
				if err != nil {
					return nil, err
				}
				parts = append(parts, strPart{interp: toks, isExpr: true})
			default:
				return nil, l.errorf(l.pos-2, "invalid escape \\%c", e)
			}
		default:
			sb.WriteByte(c)
			l.pos++
		}
	}
}

func (l *lexer) hex4() (rune, error) {
	if l.pos+4 > len(l.src) {
		return 0, l.errorf(l.pos, "invalid \\u escape")
	}
	v, err := strconv.ParseUint(l.src[l.pos:l.pos+4], 16, 32)
	if err != nil {
		return 0, l.errorf(l.pos, "invalid \\u escape")
	}
	l.pos += 4
	return rune(v), nil
}

// interpolation collects the tokens of \( ... ) up to the matching paren.
func (l *lexer) interpolation() ([]token, error) {
	start := l.pos
	depth := 0
	var toks []token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		if t.kind == tokEOF {
			return nil, l.errorf(start, "unterminated string interpolation")
		}
		if t.kind == tokOp {
			switch t.text {
			case "(":
				depth++
			case ")":
				if depth == 0 {
					return append(toks, token{kind: tokEOF, pos: t.pos}), nil
				}
				depth--
			}
		}
		toks = append(toks, t)
	}
}
