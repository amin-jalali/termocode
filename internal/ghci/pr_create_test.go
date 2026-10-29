package ghci

import (
	"context"
	"strings"
	"testing"
)

func TestCompose(t *testing.T) {
	title, body := Compose([]string{"feat: parser", " fix: edge ", "", "docs: notes"})
	if title != "feat: parser" || body != "- feat: parser\n- fix: edge\n- docs: notes" {
		t.Errorf("title %q body %q", title, body)
	}
	if title, body = Compose([]string{"only one"}); title != "only one" || body != "" {
		t.Errorf("single: %q %q", title, body)
	}
	if title, body = Compose(nil); title != "" || body != "" {
		t.Errorf("none: %q %q", title, body)
	}
}

func TestDefaultBranch(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"repo view": {out: `{"defaultBranchRef":{"name":"trunk"}}`}}}
	if b, err := DefaultBranch(context.Background(), f.run, "/r"); err != nil || b != "trunk" {
		t.Errorf("%q %v", b, err)
	}
	f = &fakeGH{answers: map[string]fakeAnswer{"repo view": {out: `{}`}}}
	if _, err := DefaultBranch(context.Background(), f.run, "/r"); err == nil {
		t.Error("empty default accepted")
	}
}

// Create passes the text as separate arguments (no shell) and returns the
// URL gh prints last.
func TestCreate(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"pr create": {out: "Creating pull request for feat/x into main\n\nhttps://github.com/o/r/pull/15\n"}}}
	url, err := Create(context.Background(), f.run, "/r", NewPR{Title: "Add; rm -rf", Body: "- a\n- b", Base: "main", Branch: "feat/x", Draft: true})
	if err != nil || url != "https://github.com/o/r/pull/15" {
		t.Fatalf("%q %v", url, err)
	}
	want := []string{"pr", "create", "--title", "Add; rm -rf", "--body", "- a\n- b", "--head", "feat/x", "--base", "main", "--draft"}
	if strings.Join(f.calls[0], "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args %q", f.calls[0])
	}
}

func TestCreateRefuses(t *testing.T) {
	f := &fakeGH{}
	for _, p := range []NewPR{{Title: " ", Branch: "x"}, {Title: "t"}, {Title: "t", Branch: "-x"}} {
		if _, err := Create(context.Background(), f.run, "/r", p); err == nil {
			t.Errorf("%+v accepted", p)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("gh called %d times", len(f.calls))
	}
}
