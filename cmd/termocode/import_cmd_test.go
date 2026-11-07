package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (CMP-86): `termocode import <export>` shows every file it would
// write, asks [y/N] (No is the default) and writes only on yes; --yes
// skips the question; a missing export or a bad flag fails.
func TestRunImport(t *testing.T) {
	export := filepath.Join("..", "..", "internal", "movein", "testdata", "export")
	run := func(stdin, cwd string, args ...string) (int, string, string) {
		var out, errw bytes.Buffer
		code := runImportIn(args, strings.NewReader(stdin), &out, &errw, cwd)
		return code, out.String(), errw.String()
	}
	ws := t.TempDir()
	code, out, _ := run("\n", ws, export)
	if code != 1 || !strings.Contains(out, "AGENTS.md") || !strings.Contains(out, "[y/N]") || !strings.Contains(out, "Nothing was written") {
		t.Errorf("no answer must write nothing: %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(ws, "AGENTS.md")); err == nil {
		t.Error("wrote without a yes")
	}
	if code, out, errs := run("y\n", ws, export); code != 0 {
		t.Errorf("yes: %d %q %q", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(ws, ".termocode", "workflows", "nightly-deps-bump.jsonc")); err != nil {
		t.Errorf("workflow not written: %v", err)
	}
	ws2 := t.TempDir()
	if code, _, _ := run("", t.TempDir(), "--dir", ws2, "--yes", "--agent", "claude-code", export); code != 0 {
		t.Errorf("--yes --dir: %d", code)
	}
	if b, err := os.ReadFile(filepath.Join(ws2, ".termocode", "workflows", "weekly-report.jsonc")); err != nil || !strings.Contains(string(b), "claude-code") {
		t.Errorf("--agent: %v %q", err, b)
	}
	if code, _, _ := run("", ws, "--agent", "other", export); code != 2 {
		t.Errorf("bad agent %d", code)
	}
	if code, _, _ := run("", ws); code != 2 {
		t.Errorf("no export %d", code)
	}
	if code, _, errs := run("", ws, filepath.Join(ws, "nope")); code != 1 || errs == "" {
		t.Errorf("missing export %d %q", code, errs)
	}
	if code, out, _ := run("", ws, "--help"); code != 0 || !strings.Contains(out, "termocode import") {
		t.Errorf("help %d", code)
	}
	if tuiCommand([]string{"import"}) {
		t.Error("import must not start the UI")
	}
}
