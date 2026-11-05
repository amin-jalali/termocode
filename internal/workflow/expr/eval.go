package expr

import "fmt"

// Eval parses and evaluates src against scope. Missing paths are null.
// It never panics: an internal fault comes back as an error.
func Eval(src string, scope map[string]any) (Value, error) {
	return EvalEnv(src, scope, nil)
}

// Env holds the hooks some functions need. A nil Env is fine.
type Env struct{}

// EvalEnv is Eval with hooks.
func EvalEnv(src string, scope map[string]any, env *Env) (v Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			v, err = Value{}, fmt.Errorf("expr: internal error: %v", r)
		}
	}()
	n, err := parse(src)
	if err != nil {
		return Value{}, err
	}
	x, err := (&evaluator{scope: scope, env: env}).eval(n)
	if err != nil {
		return Value{}, err
	}
	return Value{x}, nil
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
