package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func importRun(stdin, cwd string, args ...string) (int, string, string) {
	var out, errw bytes.Buffer
	code := runImportIn(args, strings.NewReader(stdin), &out, &errw, cwd)
	return code, out.String(), errw.String()
}

func TestImportArgumentVariants(t *testing.T) {
	export := filepath.Join("..", "..", "internal", "movein", "testdata", "export")
	ws := t.TempDir()
	if code, _, _ := importRun("", t.TempDir(), "--dir="+ws, "--agent=codex", "--yes", export); code != 0 {
		t.Errorf("--dir= --agent=: %d", code)
	}
	cwd := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := importRun("", cwd, "--dir", "sub", "--yes", export); code != 0 {
		t.Errorf("relative --dir: %d", code)
	}
	if _, err := os.Stat(filepath.Join(cwd, "sub", "AGENTS.md")); err != nil {
		t.Errorf("relative dir not used: %v", err)
	}
	for _, c := range []struct {
		args []string
		code int
		err  string
	}{
		{[]string{"--bogus", export}, 2, "unknown argument"},
		{[]string{export, "other"}, 2, "only one export"},
		{nil, 2, "Usage"},
		{[]string{"--agent", "nope", export}, 2, "--agent must be"},
		{[]string{"--dir", "/definitely/not/here", export}, 1, "not a folder"},
		{[]string{filepath.Join(t.TempDir(), "missing")}, 1, "termocode import:"},
	} {
		code, _, errw := importRun("", t.TempDir(), c.args...)
		if code != c.code || !strings.Contains(errw, c.err) {
			t.Errorf("%v: %d %q", c.args, code, errw)
		}
	}
	// An export with nothing in it.
	empty := t.TempDir()
	if code, out, _ := importRun("", t.TempDir(), empty); code != 1 || !strings.Contains(out, "Nothing to bring over") {
		t.Errorf("empty export: %d %q", code, out)
	}
	// Answering no.
	if code, out, _ := importRun("n\n", t.TempDir(), export); code != 1 || !strings.Contains(out, "Nothing was written") {
		t.Errorf("no: %d %q", code, out)
	}
	// Everything already there: nothing to write.
	again := t.TempDir()
	if code, _, _ := importRun("", t.TempDir(), "--dir", again, "--yes", export); code != 0 {
		t.Fatal("first import")
	}
	code, out, _ := importRun("y\n", t.TempDir(), "--dir", again, export)
	if code != 0 && code != 1 {
		t.Errorf("second import: %d %q", code, out)
	}
}

