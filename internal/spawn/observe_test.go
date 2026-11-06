package spawn

import (
	"context"
	"strings"
	"testing"
)

// GITX-33: SetObserver sees the program and args of every command made
// with Command / CommandContext (an exec.Cmd has no start hook, so it is
// told when the command is made).
func TestObserver(t *testing.T) {
	var got []string
	SetObserver(func(argv []string) { got = append(got, strings.Join(argv, " ")) })
	_ = Command("git", "status", "-z")
	_ = CommandContext(context.Background(), "git", "log", "-1")
	SetObserver(nil)
	_ = Command("git", "fetch")
	if strings.Join(got, ",") != "git status -z,git log -1" {
		t.Fatalf("seen %q", got)
	}
}
