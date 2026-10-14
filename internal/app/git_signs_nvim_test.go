package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/nvim"
)

func gitSignsNvim(t *testing.T) *nvim.Client {
	t.Helper()
	c, err := nvim.New()
	if err != nil {
		t.Skipf("nvim not available: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := c.ExecLua(gitSignsLua); err != nil {
		t.Fatal(err)
	}
	return c
}

// The git signs run git without blocking nvim. A blocking system() froze
// the editor while git ran, and nvim never finished an exit that started
// during one (it outlived termocode; Quit waited for the kill).
func TestGitSignsDoNotBlockNvim(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexec sleep 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := gitSignsNvim(t)
	f := filepath.Join(t.TempDir(), "a.txt")
	_ = os.WriteFile(f, []byte("x\n"), 0o644)
	_ = c.Command("edit " + f)
	start := time.Now()
	_ = c.ExecLua(`_G.termocode_git_signs_refresh()`)
	if _, err := c.EvalLuaString(`return 'ok'`); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("nvim was blocked %v while git ran", d.Round(time.Millisecond))
	}
}

// The signs still show what changed against HEAD.
func TestGitSignsMarkChangedLines(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(f, []byte("one\ntwo\nthree\n"), 0o644)
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "a.txt"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	_ = os.WriteFile(f, []byte("one\nTWO\nthree\nfour\n"), 0o644)
	c := gitSignsNvim(t)
	_ = c.Command("edit " + f)
	_ = c.ExecLua(`_G.termocode_git_signs_refresh()`)
	want := "2=TermocodeGitChange 4=TermocodeGitAdd"
	var got string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ = c.EvalLuaString(`local m = _G._tc_git_marks[vim.api.nvim_get_current_buf()] or {}
local out = {}
for l = 1, 10 do if m[l] then out[#out + 1] = l .. '=' .. m[l] end end
return table.concat(out, ' ')`)
		if got == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("git marks %q, want %q", got, want)
}
