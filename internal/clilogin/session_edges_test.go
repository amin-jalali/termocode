//go:build unix

package clilogin

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// A code pasted after the CLI ended is refused with a plain reason.
func TestSessionWriteAfterExit(t *testing.T) {
	env, _ := fakeEnv(t, "FAKE_LOGIN=fail")
	s, err := Start(context.Background(), Options{CLI: ai.CLICodex, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, EventExit)
	<-s.Done()
	if err := s.Write("CODE-1234"); err == nil || err.Error() != "the sign-in has ended" {
		t.Errorf("write after exit: %v", err)
	}
	s.Cancel() // after the end: no effect, no panic
}

func TestStartUnknownCLIAndEmptyPathEntries(t *testing.T) {
	if _, err := Start(context.Background(), Options{CLI: "aider"}); err == nil || !strings.Contains(err.Error(), `unknown CLI "aider"`) {
		t.Errorf("unknown cli: %v", err)
	}
	env, _ := fakeEnv(t, "FAKE_LOGIN=ok")
	// "::" in PATH (empty entries) is skipped, the fake is still found.
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=::" + strings.TrimPrefix(kv, "PATH=")
		}
	}
	s, err := Start(context.Background(), Options{CLI: ai.CLICodex, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, EventExit)
	if loginArgs("aider", false) != nil {
		t.Error("unknown cli has login args")
	}
}

func TestParseHelpersWithoutText(t *testing.T) {
	if deviceCode("Enter the code ABCD-12345") != "" {
		t.Error("a code without a link before it is no device code")
	}
	if lastLine(" \n\t\n") != "" {
		t.Error("blank text has no last line")
	}
}
