package ghci

import (
	"context"
	"strings"
	"testing"
)

func TestCheckout(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"pr checkout": {}}}
	if err := Checkout(context.Background(), f.run, "/repo", 14); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.calls[0], " "); got != "pr checkout 14" || f.dirs[0] != "/repo" {
		t.Errorf("args %q dir %q", got, f.dirs[0])
	}
	f = &fakeGH{answers: map[string]fakeAnswer{"pr checkout": {errOut: "error: Your local changes would be overwritten", err: errString("exit 1")}}}
	if err := Checkout(context.Background(), f.run, "/repo", 14); err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Errorf("err %v", err)
	}
}
