package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// AI parity: ide_op runs the web page's own handler of an IDE op. It
// asks exactly where the page asks: an op with a laptop card asks every
// time with no Always allow; the others run at once. Pairing and the
// gateway's ops are never offered.
func TestAIIDEOpGateFollowsTheLaptopCard(t *testing.T) {
	m := aiTestModel(t)
	tl := aiToolOf(t, &m, "ide_op")
	for _, op := range aiIDEOpList() {
		p, ok := webproto.Lookup(op)
		if !ok {
			t.Errorf("%s is no web op", op)
			continue
		}
		if p.Gateway || p.Role == webproto.RoleCtl {
			t.Errorf("%s is a gateway or control op", op)
		}
		if ask, class := tl.Gate(map[string]any{"op": op}); ask != p.LaptopCard || class != "" {
			t.Errorf("%s: ask=%v class=%q, want ask=%v (the page's laptop card) and no Always allow", op, ask, class, p.LaptopCard)
		}
	}
	for _, never := range []string{"web.pair_ask", "web.grant_ask", "cards.answer", "db.secret", "web.open", "control.ask"} {
		if _, ok := aiIDEOps[never]; ok {
			t.Errorf("%s is offered to the AI", never)
		}
		if err := tl.Validate(map[string]any{"op": never}); err == nil {
			t.Errorf("%s passed Validate", never)
		}
	}
	if err := tl.Validate(map[string]any{"op": "snippets.save", "args": map[string]any{"nope": 1}}); err == nil || !strings.Contains(err.Error(), "trigger") {
		t.Errorf("bad args: %v (the error should name the fields)", err)
	}
}

// The op's handler runs as on the page: a snippet saved through ide_op
// is listed and deleted like one saved on the page; a refusal of the
// handler reaches the model as an error.
func TestAIIDEOpRunsThePageHandler(t *testing.T) {
	m := aiTestModel(t)
	tl := aiToolOf(t, &m, "ide_op")
	if _, err := aiToolDo(t, tl, map[string]any{"op": "snippets.save", "args": map[string]any{"scope": "go", "trigger": "iferr", "body": "if err != nil {\n\treturn err\n}"}}); err != nil {
		t.Fatal(err)
	}
	out, err := aiToolDo(t, tl, map[string]any{"op": "snippets.user"})
	if err != nil || !strings.Contains(out, "iferr") {
		t.Fatalf("snippets.user: %q %v", out, err)
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "snippets.delete", "args": map[string]any{"scope": "go", "trigger": "iferr"}}); err != nil {
		t.Fatal(err)
	}
	if out, _ := aiToolDo(t, tl, map[string]any{"op": "snippets.user"}); strings.Contains(out, "iferr") {
		t.Errorf("the snippet is still there: %s", out)
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "tasks.terminate", "args": map[string]any{"label": "no-such-task"}}); err == nil {
		t.Error("stopping a task that does not run gave no error")
	}
}
