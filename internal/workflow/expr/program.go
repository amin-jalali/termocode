package expr

import (
	"fmt"
	"sync"
)

// Program is a parsed expression. It is safe to use from many
// goroutines: evaluation never changes it.
type Program struct {
	src  string
	root node
}

// Compile parses src once; call Program.Eval many times.
func Compile(src string) (*Program, error) {
	n, err := parseCached(src)
	if err != nil {
		return nil, err
	}
	return &Program{src: src, root: n}, nil
}

// Source returns the expression text.
func (p *Program) Source() string { return p.src }

// Eval evaluates the program against scope. It never panics.
func (p *Program) Eval(scope map[string]any, env *Env) (v Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			v, err = Value{}, fmt.Errorf("expr: internal error: %v", r)
		}
	}()
	return evalNode(p.root, scope, env)
}

func evalNode(n node, scope map[string]any, env *Env) (Value, error) {
	x, err := (&evaluator{scope: scope, env: env}).eval(n)
	if err != nil {
		return Value{}, err
	}
	return Value{x}, nil
}

// The parse cache keeps the trees of recent expressions (workflow files
// use the same few over and over). It is cleared when full.
const maxCachedExprs = 512

var (
	exprMu    sync.Mutex
	exprCache = map[string]node{}
)

func parseCached(src string) (node, error) {
	exprMu.Lock()
	n, ok := exprCache[src]
	exprMu.Unlock()
	if ok {
		return n, nil
	}
	n, err := parse(src)
	if err != nil {
		return nil, err
	}
	exprMu.Lock()
	if len(exprCache) >= maxCachedExprs {
		clear(exprCache)
	}
	exprCache[src] = n
	exprMu.Unlock()
	return n, nil
}
