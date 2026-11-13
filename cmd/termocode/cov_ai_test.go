package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

func TestAIExecOptionErrors(t *testing.T) {
	withAIExecEnv(t, &scriptProvider{}, nil)
	for _, args := range [][]string{
		{"-x"}, {"-x", "a", "--max-steps", "0"}, {"-x", "a", "--max-steps=abc"}, {"-x", "a", "--timeout", "never"},
		{"-x", "a", "--timeout=-5s"}, {"-x", "a", "--tools", "everything"}, {"-x", "a", "--tools", "read", "--allow", ai.ClassGit},
		{"-x", "a", "--bogus"}, {"-x", "a", "--model"},
	} {
		if code, _, _ := runAIExec(t, "", args...); code != aiExitUsage {
			t.Errorf("%v: %d", args, code)
		}
	}
	if o, err := parseAIExec([]string{"exec", "do", "it", "--allow", " ,", "--rules", "--stdin", "--tools=none"}); err != nil || !o.rules || !o.stdin || o.tools != "none" || o.prompt != "do it" {
		t.Errorf("parse: %+v %v", o, err)
	}
}

func TestAIExecStdinAndFolderErrors(t *testing.T) {
	withAIExecEnv(t, &scriptProvider{}, nil)
	if code, _, errs := runAIExec(t, "   \n", "-x", "-", "--dir", t.TempDir()); code != aiExitUsage || !strings.Contains(errs, "no prompt on stdin") {
		t.Errorf("empty stdin prompt: %d %q", code, errs)
	}
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := runAIExec(t, "", "-x", "a", "--dir", file); code != aiExitUsage || !strings.Contains(errs, "not a folder") {
		t.Errorf("file as dir: %d %q", code, errs)
	}
	if code, _, _ := runAIExec(t, "", "-x", "a", "--dir", filepath.Join(t.TempDir(), "gone")); code != aiExitUsage {
		t.Errorf("missing dir: %d", code)
	}
	if code, out, _ := runAIExec(t, "", "--help"); code != aiExitOK || !strings.Contains(out, "termocode ai") {
		t.Errorf("help: %d", code)
	}
}

// --rules adds the project's rules to the system prompt; an interrupt ends
// the turn with the interrupted code.
func TestAIExecRulesAndInterrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Rules\nAlways say please.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &scriptProvider{replies: []ai.Chunk{{Delta: "ok"}}}
	withAIExecEnv(t, p, nil)
	if code, _, _ := runAIExec(t, "", "-x", "a", "--rules", "--dir", dir, "--tools", "all", "--allow", ai.ClassGit); code != aiExitOK {
		t.Fatalf("rules run: %d", code)
	}
	if sys := p.reqs[0].Messages[0].Content; !strings.Contains(sys, "Always say please") || !strings.Contains(sys, "Only these mutating tool classes") {
		t.Errorf("system prompt lacks the rules or the allow list: %q", sys)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errw bytes.Buffer
	if code := runAIIn(ctx, []string{"-x", "a", "--dir", dir}, strings.NewReader(""), &out, &errw, func(string) string { return "" }); code != aiExitInterrupted {
		t.Errorf("interrupted: %d", code)
	}
}

func TestAIExecCapKeepsRunesWhole(t *testing.T) {
	if aiExecCap("short") != "short" {
		t.Fatal("short text changed")
	}
	long := strings.Repeat("é", aiExecDetailMax)
	got := aiExecCap(long)
	if !strings.Contains(got, "more bytes]") || !strings.HasPrefix(got, "é") {
		t.Fatal("not cut")
	}
	head := got[:strings.Index(got, "…")]
	if strings.ToValidUTF8(head, "") != head {
		t.Fatal("cut in the middle of a rune")
	}
}

// The real setup reads the environment: with a key it builds a provider, with
// none it says the AI is not set up.
func TestAIExecSetupFromEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	if err := os.MkdirAll(filepath.Join(home, "cfg", "termocode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "cfg", "termocode", "config.json"), []byte(`{"secret_store":"plain"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ai.SetSecretStore(nil)
	t.Cleanup(func() { ai.SetSecretStore(nil) })
	none := func(string) string { return "" }
	if _, err := aiExecSetup(none, ""); err == nil {
		t.Fatal("no key, no provider")
	}
	withKey := func(k string) string {
		if k == "ANTHROPIC_API_KEY" {
			return "sk-ant-test0123456789abcdef"
		}
		return ""
	}
	env, err := aiExecSetup(withKey, "my-model")
	if err != nil || env.Provider == nil || env.Model != "my-model" {
		t.Fatalf("%+v %v", env, err)
	}
}
