package expr

import (
	"fmt"
	"sync"
	"testing"
)

func TestCompileOnceEvalMany(t *testing.T) {
	p, err := Compile(`item.prio + n`)
	if err == nil {
		t.Fatalf("Compile(unknown root) = %v, want an error", p)
	}
	p, err = Compile(`item.prio * 2`)
	if err == nil {
		t.Fatal("Compile(bad) = no error")
	}
	p, err = Compile(`item.prio + 1`)
	if err != nil {
		t.Fatal(err)
	}
	for _, prio := range []float64{1, 5} {
		v, err := p.Eval(map[string]any{"item": map[string]any{"prio": prio}}, nil)
		if err != nil || v.Any() != prio+1 {
			t.Fatalf("Eval = %v, %v; want %v", v.Any(), err, prio+1)
		}
	}
	if p.Source() != `item.prio + 1` {
		t.Fatalf("Source = %q", p.Source())
	}
}

func TestParseCacheIsBoundedAndSafe(t *testing.T) {
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Go(func() {
			for i := range maxCachedExprs * 2 {
				src := fmt.Sprintf("%d + %d", g, i)
				v, err := Eval(src, nil)
				if err != nil || v.Any() != float64(g+i) {
					t.Errorf("Eval(%q) = %v, %v", src, v.Any(), err)
					return
				}
			}
		})
	}
	wg.Wait()
	exprMu.Lock()
	n := len(exprCache)
	exprMu.Unlock()
	if n > maxCachedExprs {
		t.Fatalf("cache holds %d, cap %d", n, maxCachedExprs)
	}
}
