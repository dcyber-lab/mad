package jqgo

import "fmt"

// The checker resolves names statically, the way jq's compiler does: a
// call either refers to an enclosing definition or parameter (resolved at
// run time through the environment) or to a global function (resolved
// here, once). Undefined functions, variables and labels are compile
// errors.

type scope struct {
	parent *scope
	kind   byte // 'v', 'f', 'l'
	name   string
	arity  int
}

type checker struct {
	lookup func(name string, arity int) *globalFunc
	vars   map[string]bool // variables supplied at run time ($ENV, WithVariables)
}

func (s *scope) find(kind byte, name string, arity int) bool {
	for x := s; x != nil; x = x.parent {
		if x.kind == kind && x.name == name && (kind != 'f' || x.arity == arity) {
			return true
		}
	}
	return false
}

func (c *checker) checkDef(d *funcDef, s *scope) error {
	return c.checkDefIn(d, &scope{parent: s, kind: 'f', name: d.name, arity: len(d.params)})
}

// checkDefIn checks d's body in s2, which may or may not already bind d
// itself (top-level builtins are reached through globals instead).
func (c *checker) checkDefIn(d *funcDef, s2 *scope) error {
	for _, p := range d.params {
		s2 = &scope{parent: s2, kind: 'f', name: p.name}
		if p.isVar {
			s2 = &scope{parent: s2, kind: 'v', name: p.name}
		}
	}
	return c.check(d.body, s2)
}

func (c *checker) withPattern(pat *pattern, s *scope) *scope {
	for _, name := range patternVars(pat, nil) {
		s = &scope{parent: s, kind: 'v', name: name}
	}
	return s
}

func (c *checker) checkPatternKeys(pat *pattern, s *scope) error {
	switch pat.kind {
	case 'a':
		for i := range pat.elems {
			if err := c.checkPatternKeys(&pat.elems[i], s); err != nil {
				return err
			}
		}
	case 'o':
		for _, op := range pat.obj {
			if err := c.check(op.key, s); err != nil {
				return err
			}
			if op.val != nil {
				if err := c.checkPatternKeys(op.val, s); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *checker) check(n node, s *scope) error {
	switch n := n.(type) {
	case nil, identityNode, recurseAllNode, literalNode, textNode, *formatNode:
		return nil
	case *indexNode:
		return c.check2(n.term, n.key, s)
	case *sliceNode:
		if err := c.check2(n.term, n.from, s); err != nil {
			return err
		}
		return c.check(n.to, s)
	case *iterateNode:
		return c.check(n.term, s)
	case *pipeNode:
		return c.check2(n.left, n.right, s)
	case *commaNode:
		return c.check2(n.left, n.right, s)
	case *negNode:
		return c.check(n.x, s)
	case *binopNode:
		return c.check2(n.left, n.right, s)
	case *andNode:
		return c.check2(n.left, n.right, s)
	case *orNode:
		return c.check2(n.left, n.right, s)
	case *altNode:
		return c.check2(n.left, n.right, s)
	case *assignNode:
		return c.check2(n.left, n.right, s)
	case *ifNode:
		if err := c.check2(n.cond, n.then, s); err != nil {
			return err
		}
		return c.check(n.els, s)
	case *tryNode:
		return c.check2(n.body, n.catch, s)
	case *reduceNode:
		if err := c.check2(n.src, n.init, s); err != nil {
			return err
		}
		if err := c.checkPatternKeys(&n.pat, s); err != nil {
			return err
		}
		n.inPlace = canUpdateInPlace(n.update)
		return c.check(n.update, c.withPattern(&n.pat, s))
	case *foreachNode:
		if err := c.check2(n.src, n.init, s); err != nil {
			return err
		}
		if err := c.checkPatternKeys(&n.pat, s); err != nil {
			return err
		}
		return c.check2(n.update, n.extract, c.withPattern(&n.pat, s))
	case *funcDefNode:
		if err := c.checkDef(n.def, s); err != nil {
			return err
		}
		return c.check(n.rest, &scope{parent: s, kind: 'f', name: n.def.name, arity: len(n.def.params)})
	case *callNode:
		for _, a := range n.args {
			if err := c.check(a, s); err != nil {
				return err
			}
		}
		if s.find('f', n.name, len(n.args)) {
			n.lexical = true
			return nil
		}
		if g := c.lookup(n.name, len(n.args)); g != nil {
			n.global = g
			return nil
		}
		return fmt.Errorf("%s/%d is not defined", n.name, len(n.args))
	case *varNode:
		if s.find('v', n.name, 0) || c.vars[n.name] {
			return nil
		}
		return fmt.Errorf("$%s is not defined", n.name)
	case *bindNode:
		if err := c.check(n.src, s); err != nil {
			return err
		}
		inner := s
		for i := range n.pats {
			inner = c.withPattern(&n.pats[i], inner)
		}
		for i := range n.pats {
			if err := c.checkPatternKeys(&n.pats[i], inner); err != nil {
				return err
			}
		}
		return c.check(n.body, inner)
	case *labelNode:
		return c.check(n.body, &scope{parent: s, kind: 'l', name: n.name})
	case *breakNode:
		if s.find('l', n.name, 0) {
			return nil
		}
		return fmt.Errorf("$*label-%s is not defined", n.name)
	case *arrayNode:
		return c.check(n.x, s)
	case *objectNode:
		for _, ent := range n.entries {
			if lit, ok := ent.key.(literalNode); ok {
				if _, isStr := lit.v.(string); !isStr {
					return fmt.Errorf("Cannot use %s as object key", typeDump(lit.v))
				}
			}
			if err := c.check2(ent.key, ent.value, s); err != nil {
				return err
			}
		}
		return nil
	case *stringNode:
		for _, part := range n.parts {
			if err := c.check(part, s); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("jqgo: unknown node %T", n)
}

func (c *checker) check2(a, b node, s *scope) error {
	if err := c.check(a, s); err != nil {
		return err
	}
	return c.check(b, s)
}
