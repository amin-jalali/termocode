package ghci

import (
	"context"
	"strings"
	"testing"
)

func TestSetDraft(t *testing.T) {
	for draft, want := range map[bool]string{false: "pr ready 5", true: "pr ready 5 --undo"} {
		f := &fakeGH{answers: map[string]fakeAnswer{"pr ready": {}}}
		if err := SetDraft(context.Background(), f.run, "/r", 5, draft); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(f.calls[0], " "); got != want {
			t.Errorf("draft=%v args %q want %q", draft, got, want)
		}
	}
	f := &fakeGH{answers: map[string]fakeAnswer{"pr ready": {errOut: "Pull request #5 is already marked as ready for review", err: errString("exit 1")}}}
	if err := SetDraft(context.Background(), f.run, "/r", 5, false); err == nil {
		t.Error("error swallowed")
	}
}
