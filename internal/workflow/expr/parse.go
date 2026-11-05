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

type notNode struct{ x node }

type callNode struct {
	fn   string
	args []node
	pos  int
}

// funcs are the only functions, with their argument counts.
var funcs = map[string]int{
	"len": 1, "lower": 1, "upper": 1, "trim": 1, "has": 1, "seen": 1, "days_since": 1, "json": 1,
}

// binNode is and, or, a comparison, + or -.
type binNode struct {
	op   string
	l, r node
	pos  int
}

var cmpOps = map[string]bool{
	"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true,
	"has": true, "in": true, "matches": true, "startswith": true, "endswith": true,
}

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

func (p *parser) expr() (node, error) { return p.or() }

func (p *parser) or() (node, error) {
	l, err := p.and()
	if err != nil {
		return nil, err
	}
	for p.isName("or") {
		pos := p.next().pos
		r, err := p.and()
		if err != nil {
			return nil, err
		}
		l = binNode{op: "or", l: l, r: r, pos: pos}
	}
	return l, nil
}

func (p *parser) and() (node, error) {
	l, err := p.not()
	if err != nil {
		return nil, err
	}
	for p.isName("and") {
		pos := p.next().pos
		r, err := p.not()
		if err != nil {
			return nil, err
		}
		l = binNode{op: "and", l: l, r: r, pos: pos}
	}
	return l, nil
}

func (p *parser) not() (node, error) {
	if p.isName("not") {
		t := p.next()
		if err := p.enter(t.pos); err != nil {
			return nil, err
		}
		defer p.leave()
		x, err := p.not()
		if err != nil {
			return nil, err
		}
		return notNode{x}, nil
	}
	return p.cmp()
}

func (p *parser) cmp() (node, error) {
	l, err := p.sum()
	if err != nil {
		return nil, err
	}
	t := p.peek()
	if (t.kind == tOp || t.kind == tName) && cmpOps[t.text] {
		p.next()
		r, err := p.sum()
		if err != nil {
			return nil, err
		}
		return binNode{op: t.text, l: l, r: r, pos: t.pos}, nil
	}
	return l, nil
}

func (p *parser) sum() (node, error) {
	l, err := p.atom()
	if err != nil {
		return nil, err
	}
	for p.isOp("+") || p.isOp("-") {
		t := p.next()
		r, err := p.atom()
		if err != nil {
			return nil, err
		}
		l = binNode{op: t.text, l: l, r: r, pos: t.pos}
	}
	return l, nil
}

func (p *parser) isName(s string) bool {
	t := p.peek()
	return t.kind == tName && t.text == s
}

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
		if _, ok := funcs[t.text]; ok {
			return p.call(t)
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

func (p *parser) call(name token) (node, error) {
	if err := p.expect("("); err != nil {
		return nil, err
	}
	if err := p.enter(name.pos); err != nil {
		return nil, err
	}
	defer p.leave()
	c := callNode{fn: name.text, pos: name.pos}
	if !p.isOp(")") {
		for {
			n, err := p.expr()
			if err != nil {
				return nil, err
			}
			c.args = append(c.args, n)
			if !p.isOp(",") {
				break
			}
			p.next()
		}
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	if want := funcs[c.fn]; len(c.args) != want {
		return nil, errAt(c.pos, "%s takes %d argument (got %d)", c.fn, want, len(c.args))
	}
	if c.fn == "has" {
		if _, ok := c.args[0].(pathNode); !ok {
			return nil, errAt(c.pos, "has( ) needs a path, like has(item.labels)")
		}
	}
	return c, nil
}
