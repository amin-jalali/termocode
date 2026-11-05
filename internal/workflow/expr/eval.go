package expr

import (
	"fmt"
	"time"
)

// Eval parses and evaluates src against scope. Missing paths are null.
// It never panics: an internal fault comes back as an error.
func Eval(src string, scope map[string]any) (Value, error) {
	return EvalEnv(src, scope, nil)
}

// Env holds the hooks some functions need. A nil Env is fine: seen()
// then fails and days_since() uses the real clock.
type Env struct {
	// Seen asks the workflow's seen ledger whether key was handled.
	Seen func(key string) (bool, error)
	// Now is the clock for days_since; nil means time.Now.
	Now func() time.Time
}

// EvalEnv is Eval with hooks.
func EvalEnv(src string, scope map[string]any, env *Env) (v Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			v, err = Value{}, fmt.Errorf("expr: internal error: %v", r)
		}
	}()
	return evalRaw(src, scope, env)
}

// evalRaw is EvalEnv without the recover guard (the fuzz test calls it,
// so a panic shows up as a failure instead of an error).
func evalRaw(src string, scope map[string]any, env *Env) (Value, error) {
	n, err := parseCached(src)
	if err != nil {
		return Value{}, err
	}
	return evalNode(n, scope, env)
}

type evaluator struct {
	scope map[string]any
	env   *Env
}

func (e *evaluator) eval(n node) (any, error) {
	switch t := n.(type) {
	case litNode:
		return t.v, nil
	case pathNode:
		v, _ := e.resolve(t)
		return v, nil
	case listNode:
		out := make([]any, len(t.elems))
		for i, el := range t.elems {
			v, err := e.eval(el)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case notNode:
		v, err := e.eval(t.x)
		if err != nil {
			return nil, err
		}
		return !truthy(v), nil
	case binNode:
		return e.bin(t)
	case callNode:
		return e.call(t)
	}
	return nil, fmt.Errorf("expr: internal error: unknown node %T", n)
}

// resolve walks a path. found is false when any step is missing.
func (e *evaluator) resolve(p pathNode) (v any, found bool) {
	cur, ok := e.scope[p.root]
	if !ok {
		return nil, false
	}
	cur = norm(cur)
	for _, s := range p.segs {
		switch c := cur.(type) {
		case map[string]any:
			if s.isIdx {
				return nil, false
			}
			cur, ok = c[s.key]
			if !ok {
				return nil, false
			}
		case []any:
			if !s.isIdx || s.index >= len(c) {
				return nil, false
			}
			cur = c[s.index]
		default:
			return nil, false
		}
		cur = norm(cur)
	}
	return cur, true
}
