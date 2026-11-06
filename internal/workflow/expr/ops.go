package expr

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// MaxStringLen caps any string an expression builds (with +, json(),
// upper(), …), so a few operators cannot blow up memory.
const MaxStringLen = 1 << 20

func (e *evaluator) bin(b binNode) (any, error) {
	l, err := e.eval(b.l)
	if err != nil {
		return nil, err
	}
	switch b.op {
	case "and":
		if !truthy(l) {
			return false, nil
		}
		r, err := e.eval(b.r)
		if err != nil {
			return nil, err
		}
		return truthy(r), nil
	case "or":
		if truthy(l) {
			return true, nil
		}
		r, err := e.eval(b.r)
		if err != nil {
			return nil, err
		}
		return truthy(r), nil
	}
	r, err := e.eval(b.r)
	if err != nil {
		return nil, err
	}
	switch b.op {
	case "==":
		return equal(l, r), nil
	case "!=":
		return !equal(l, r), nil
	case "<", "<=", ">", ">=":
		return order(b, l, r)
	case "has":
		return has(b, l, r)
	case "in":
		return has(b, r, l)
	case "matches":
		return matches(b, l, r)
	case "startswith", "endswith":
		if l == nil {
			return false, nil
		}
		ls, ok1 := l.(string)
		rs, ok2 := r.(string)
		if !ok1 || !ok2 {
			return nil, errAt(b.pos, "%q needs strings (got %s and %s)", b.op, kindOf(l), kindOf(r))
		}
		if b.op == "startswith" {
			return strings.HasPrefix(ls, rs), nil
		}
		return strings.HasSuffix(ls, rs), nil
	case "+":
		return add(b, l, r)
	case "-":
		lf, ok1 := l.(float64)
		rf, ok2 := r.(float64)
		if !ok1 || !ok2 {
			return nil, errAt(b.pos, "\"-\" needs numbers (got %s and %s)", kindOf(l), kindOf(r))
		}
		return lf - rf, nil
	}
	return nil, errAt(b.pos, "internal error: unknown operator %q", b.op)
}

// maxEqualDepth stops equal on very deep or cyclic values.
const maxEqualDepth = 10000

// equal is deep JSON equality; numbers compare by value.
func equal(a, b any) bool { return equalAt(a, b, 0) }

func equalAt(a, b any, depth int) bool {
	if depth > maxEqualDepth {
		return false
	}
	a, b = norm(a), norm(b)
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equalAt(x[i], y[i], depth+1) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, xv := range x {
			yv, ok := y[k]
			if !ok || !equalAt(xv, yv, depth+1) {
				return false
			}
		}
		return true
	}
	return false
}

func order(b binNode, l, r any) (any, error) {
	if l == nil || r == nil {
		return false, nil
	}
	var c int
	switch x := l.(type) {
	case float64:
		y, ok := r.(float64)
		if !ok {
			return nil, errAt(b.pos, "cannot compare %s and %s with %q", kindOf(l), kindOf(r), b.op)
		}
		switch {
		case x < y:
			c = -1
		case x > y:
			c = 1
		case x != y: // NaN
			return false, nil
		}
	case string:
		y, ok := r.(string)
		if !ok {
			return nil, errAt(b.pos, "cannot compare %s and %s with %q", kindOf(l), kindOf(r), b.op)
		}
		c = strings.Compare(x, y)
	default:
		return nil, errAt(b.pos, "cannot compare %s and %s with %q", kindOf(l), kindOf(r), b.op)
	}
	switch b.op {
	case "<":
		return c < 0, nil
	case "<=":
		return c <= 0, nil
	case ">":
		return c > 0, nil
	}
	return c >= 0, nil
}

// has: a list holds an element, a map holds a key, a string holds a
// substring. null holds nothing.
func has(b binNode, coll, x any) (any, error) {
	switch c := coll.(type) {
	case nil:
		return false, nil
	case []any:
		for _, el := range c {
			if equal(el, x) {
				return true, nil
			}
		}
		return false, nil
	case map[string]any:
		k, ok := x.(string)
		if !ok {
			return nil, errAt(b.pos, "%q: a map key must be a string (got %s)", b.op, kindOf(x))
		}
		_, found := c[k]
		return found, nil
	case string:
		s, ok := x.(string)
		if !ok {
			return nil, errAt(b.pos, "%q on a string needs a string on the right (got %s)", b.op, kindOf(x))
		}
		return strings.Contains(c, s), nil
	}
	if b.op == "in" {
		return nil, errAt(b.pos, "\"in\" needs a list, map or string on the right (got %s)", kindOf(coll))
	}
	return nil, errAt(b.pos, "\"has\" needs a list, map or string on the left (got %s)", kindOf(coll))
}

func matches(b binNode, l, r any) (any, error) {
	pat, ok := r.(string)
	if !ok {
		return nil, errAt(b.pos, "\"matches\": the pattern must be a string (got %s)", kindOf(r))
	}
	re, err := compileRE(pat)
	if err != nil {
		return nil, errAt(b.pos, "%v", err)
	}
	if l == nil {
		return false, nil
	}
	s, ok := l.(string)
	if !ok {
		return nil, errAt(b.pos, "\"matches\" needs a string on the left (got %s)", kindOf(l))
	}
	return re.MatchString(s), nil
}

func add(b binNode, l, r any) (any, error) {
	lf, lnum := l.(float64)
	rf, rnum := r.(float64)
	if lnum && rnum {
		return lf + rf, nil
	}
	_, lstr := l.(string)
	_, rstr := r.(string)
	if lstr || rstr {
		okL := lstr || lnum || l == nil
		okR := rstr || rnum || r == nil
		if okL && okR {
			ls, rs := text(l), text(r)
			if len(ls)+len(rs) > MaxStringLen {
				return nil, errAt(b.pos, "string is too long (limit %d bytes)", MaxStringLen)
			}
			return ls + rs, nil
		}
	}
	ll, lok := l.([]any)
	rl, rok := r.([]any)
	if lok && rok {
		if len(ll)+len(rl) > MaxStringLen {
			return nil, errAt(b.pos, "list is too long")
		}
		out := make([]any, 0, len(ll)+len(rl))
		return append(append(out, ll...), rl...), nil
	}
	return nil, errAt(b.pos, "cannot add %s and %s", kindOf(l), kindOf(r))
}

// The pattern cache keeps compiled RE2 patterns; it is cleared when full.
const maxCachedPatterns = 256

var (
	reMu    sync.Mutex
	reCache = map[string]*regexp.Regexp{}
)

func compileRE(pat string) (*regexp.Regexp, error) {
	if len(pat) > MaxExprLen {
		return nil, fmt.Errorf("pattern is too long (%d bytes, the limit is %d)", len(pat), MaxExprLen)
	}
	reMu.Lock()
	re, ok := reCache[pat]
	reMu.Unlock()
	if ok {
		return re, nil
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		msg := err.Error()
		if len(msg) > 120 {
			msg = msg[:120] + "…"
		}
		return nil, fmt.Errorf("bad pattern: %s", strings.TrimPrefix(msg, "error parsing regexp: "))
	}
	reMu.Lock()
	if len(reCache) >= maxCachedPatterns {
		clear(reCache)
	}
	reCache[pat] = re
	reMu.Unlock()
	return re, nil
}
