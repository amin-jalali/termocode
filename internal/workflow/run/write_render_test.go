package run

import (
	"context"
	"testing"
)

// WFX-13: a write gets Render, which fills templates in the write's own
// scope (a PR body_file sees {{item.*}} and steps.*).
type renderSpy struct {
	*fakes
	got map[string]string
}

func (r *renderSpy) Write(ctx context.Context, c WriteCall) (Output, error) {
	if c.Render == nil {
		r.got[c.Item.Key] = "<no render>"
	} else {
		s, err := c.Render("{{item.title}} wt={{steps.fix.worktree}}")
		if err != nil {
			return Output{}, err
		}
		r.got[c.Item.Key] = s
	}
	return r.fakes.Write(ctx, c)
}

func TestWriteGetsRenderInItsScope(t *testing.T) {
	f := newFakes(items("a")...)
	spy := &renderSpy{fakes: f, got: map[string]string{}}
	opts := f.options()
	opts.Write = spy
	rf := wait(t, Start(context.Background(), mustParse(t, parFile(t, "1", "")), opts))
	if rf.State != RunOK {
		t.Fatalf("%s %s", rf.State, rf.Error)
	}
	if got := spy.got["a"]; got != "T a wt=/wt/a" {
		t.Errorf("rendered %q", got)
	}
}
