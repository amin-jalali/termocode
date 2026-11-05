package expr

import (
	"strconv"
	"strings"
)

// MaxExprLen is the size cap of one expression and of one pattern.
const MaxExprLen = 1024

// maxDepth caps nesting (parentheses, lists, calls, not), so the parser
// and the evaluator never go deep on the stack.
const maxDepth = 64

// roots are the only names a path may start with (plan §3.5).
var roots = map[string]bool{"item": true, "items": true, "steps": true, "vars": true, "run": true, "workflow": true}

const rootList = "item, items, steps, vars, run or workflow"

type node interface{}

type litNode struct{ v any }

type seg struct {
	key   string
	index int
	isIdx bool
}

type pathNode struct {
	root string
	segs []seg
}

type listNode struct{ elems []node }

type parser struct {
	toks  []token
	i     int
	depth int
}

func parseNumber(s string) (float64, error) { return strconv.ParseFloat(s, 64) }

func parse(src string) (node, error) {
	if len(src) > MaxExprLen {
		return nil, errAt(0, "expression is too long (%d bytes, the limit is %d)", len(src), MaxExprLen)
	}
	if strings.TrimSpace(src) == "" {
		return nil, errAt(0, "expression is empty")
	}
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	n, err := p.expr()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tEOF {
		return nil, errAt(t.pos, "unexpected %s", describe(t))
	}
	return n, nil
}

func describe(t token) string {
	switch t.kind {
	case tEOF:
		return "end of expression"
	case tNum:
		return "number " + t.text
	case tStr:
		return "string"
	}
	return strconv.Quote(t.text)
}

func (p *parser) peek() token { return p.toks[p.i] }

func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tEOF {
		p.i++
	}
	return t
}

func (p *parser) isOp(s string) bool {
	t := p.peek()
	return t.kind == tOp && t.text == s
}

func (p *parser) expect(s string) error {
	t := p.peek()
	if t.kind != tOp || t.text != s {
		return errAt(t.pos, "want %q, got %s", s, describe(t))
	}
	p.i++
	return nil
}

func (p *parser) enter(pos int) error {
	p.depth++
	if p.depth > maxDepth {
		return errAt(pos, "expression is nested too deep (limit %d)", maxDepth)
	}
	return nil
}

func (p *parser) leave() { p.depth-- }

func (p *parser) expr() (node, error) { return p.atom() }

func (p *parser) atom() (node, error) {
	t := p.next()
	switch t.kind {
	case tNum:
		return litNode{t.num}, nil
	case tStr:
		return litNode{t.text}, nil
	case tOp:
		switch t.text {
		case "-":
			n := p.peek()
			if n.kind == tNum {
				p.next()
				return litNode{-n.num}, nil
			}
			return nil, errAt(t.pos, "\"-\" must come before a number")
		case "(":
			if err := p.enter(t.pos); err != nil {
				return nil, err
			}
			defer p.leave()
			n, err := p.expr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			return n, nil
		case "[":
			if err := p.enter(t.pos); err != nil {
				return nil, err
			}
			defer p.leave()
			return p.list()
		}
	case tName:
		switch t.text {
		case "true":
			return litNode{true}, nil
		case "false":
			return litNode{false}, nil
		case "null":
			return litNode{nil}, nil
		}
		if roots[t.text] {
			return p.path(t.text)
		}
		return nil, errAt(t.pos, "unknown name %q (a path starts with %s)", t.text, rootList)
	}
	return nil, errAt(t.pos, "unexpected %s", describe(t))
}

func (p *parser) list() (node, error) {
	l := listNode{elems: []node{}}
	if p.isOp("]") {
		p.next()
		return l, nil
	}
	for {
		n, err := p.expr()
		if err != nil {
			return nil, err
		}
		l.elems = append(l.elems, n)
		if p.isOp(",") {
			p.next()
			continue
		}
		if err := p.expect("]"); err != nil {
			return nil, err
		}
		return l, nil
	}
}

func (p *parser) path(root string) (node, error) {
	pn := pathNode{root: root}
	for {
		switch {
		case p.isOp("."):
			p.next()
			t := p.next()
			if t.kind != tName {
				return nil, errAt(t.pos, "want a name after \".\", got %s", describe(t))
			}
			pn.segs = append(pn.segs, seg{key: t.text})
		case p.isOp("["):
			p.next()
			t := p.next()
			switch {
			case t.kind == tStr:
				pn.segs = append(pn.segs, seg{key: t.text})
			case t.kind == tNum && t.num == float64(int(t.num)) && t.num >= 0 && t.num < 1e9:
				pn.segs = append(pn.segs, seg{index: int(t.num), isIdx: true})
			default:
				return nil, errAt(t.pos, "want a whole number of 0 or more, or a string, in [ ]")
			}
			if err := p.expect("]"); err != nil {
				return nil, err
			}
		default:
			return pn, nil
		}
	}
}
