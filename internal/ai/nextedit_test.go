package ai

import (
	"strings"
	"testing"
)

func TestDiffHunk(t *testing.T) {
	old := []string{"a", "func f(x int) {", "\treturn x", "}"}
	cur := []string{"a", "func f(n int) {", "\treturn x", "}"}
	h, ok := DiffHunk(old, cur)
	if !ok || h.Row != 1 || len(h.Old) != 1 || h.Old[0] != "func f(x int) {" || h.New[0] != "func f(n int) {" {
		t.Fatalf("hunk %+v ok=%v", h, ok)
	}
	if _, ok := DiffHunk(old, old); ok {
		t.Fatal("no change must give no hunk")
	}
	// Inserted line.
	h, ok = DiffHunk([]string{"a", "b"}, []string{"a", "x", "b"})
	if !ok || h.Row != 1 || len(h.Old) != 0 || len(h.New) != 1 || h.New[0] != "x" {
		t.Fatalf("insert hunk %+v", h)
	}
	// Same lines repeated: prefix and suffix must not overlap.
	h, ok = DiffHunk([]string{"x", "x"}, []string{"x", "x", "x"})
	if !ok || len(h.Old) != 0 || len(h.New) != 1 {
		t.Fatalf("repeat hunk %+v", h)
	}
}

func TestEditHistoryKeepsLast(t *testing.T) {
	var h EditHistory
	for i := 0; i < 5; i++ {
		h.Add(EditHunk{Row: i, New: []string{"x"}})
	}
	e := h.Edits()
	if len(e) != NextEditHistory || e[0].Row != 2 || e[len(e)-1].Row != 4 {
		t.Fatalf("history %+v", e)
	}
	last, ok := h.Last()
	if !ok || last.Row != 4 {
		t.Fatalf("last %+v", last)
	}
	h.Reset()
	if _, ok := h.Last(); ok {
		t.Fatal("reset")
	}
}

func nesInput() NextEditInput {
	lines := []string{
		"package main",
		"",
		"func area(width, h int) int {",
		"\treturn w * h",
		"}",
	}
	return NextEditInput{Filetype: "go", Path: "/p/main.go", Lines: lines, CursorRow: 2,
		Edits: []EditHunk{{Row: 2, Old: []string{"func area(w, h int) int {"}, New: []string{lines[2]}}}}
}

func TestBuildNextEditPrompt(t *testing.T) {
	msgs := BuildNextEditPrompt(nesInput())
	if len(msgs) != 2 || msgs[0].Role != RoleSystem {
		t.Fatalf("msgs %+v", msgs)
	}
	u := msgs[1].Content
	for _, want := range []string{"main.go", "language: go", "-func area(w, h int) int {", "+func area(width, h int) int {", "4|\treturn w * h", "line 3"} {
		if !strings.Contains(u, want) {
			t.Fatalf("prompt misses %q:\n%s", want, u)
		}
	}
}

func TestNextEditWindowIsLimited(t *testing.T) {
	in := nesInput()
	in.Lines = make([]string, 1000)
	for i := range in.Lines {
		in.Lines[i] = "line"
	}
	in.CursorRow = 500
	first, last := NextEditWindowRange(len(in.Lines), in.CursorRow)
	if first != 500-NextEditWindow || last != 500+NextEditWindow+1 {
		t.Fatalf("window %d..%d", first, last)
	}
	u := BuildNextEditPrompt(in)[1].Content
	if strings.Contains(u, "\n1|") || strings.Count(u, "|line") != last-first {
		t.Fatalf("prompt not windowed (%d lines)", strings.Count(u, "|line"))
	}
}

func TestParseNextEdit(t *testing.T) {
	in := nesInput()
	ne, ok := ParseNextEdit(`{"start":4,"end":4,"lines":["\treturn width * h"]}`, in)
	if !ok || ne.Start != 3 || ne.End != 3 || len(ne.Lines) != 1 || ne.Lines[0] != "\treturn width * h" {
		t.Fatalf("parse %+v ok=%v", ne, ok)
	}
	// A fenced answer is fine.
	if _, ok := ParseNextEdit("```json\n{\"start\":4,\"end\":4,\"lines\":[\"\\treturn width * h\"]}\n```", in); !ok {
		t.Fatal("fenced answer")
	}
	bad := map[string]string{
		"none":         `{"none":true}`,
		"prose":        `Sure! {"start":4,"end":4,"lines":["x"]}`,
		"two objects":  `{"start":4,"end":4,"lines":["x"]}{"start":4,"end":4,"lines":["y"]}`,
		"unknown key":  `{"start":4,"end":4,"lines":["x"],"why":"because"}`,
		"no lines":     `{"start":4,"end":4}`,
		"edited line":  `{"start":3,"end":3,"lines":["func area(width, height int) int {"]}`,
		"out of file":  `{"start":9,"end":9,"lines":["x"]}`,
		"zero":         `{"start":0,"end":0,"lines":["x"]}`,
		"reversed":     `{"start":5,"end":4,"lines":["x"]}`,
		"same text":    `{"start":4,"end":4,"lines":["\treturn w * h"]}`,
		"newline":      `{"start":4,"end":4,"lines":["a\nb"]}`,
		"cursor mark":  `{"start":4,"end":4,"lines":["<CURSOR>x"]}`,
		"string start": `{"start":"4","end":4,"lines":["x"]}`,
		"empty":        ``,
	}
	for name, raw := range bad {
		if ne, ok := ParseNextEdit(raw, in); ok {
			t.Errorf("%s: want no suggestion, got %+v", name, ne)
		}
	}
	long := `{"start":4,"end":4,"lines":[` + strings.Repeat(`"x",`, NextEditMaxLines) + `"x"]}`
	if _, ok := ParseNextEdit(long, in); ok {
		t.Error("too many lines")
	}
}

func TestParseNextEditOutsideWindow(t *testing.T) {
	in := nesInput()
	in.Lines = make([]string, 600)
	for i := range in.Lines {
		in.Lines[i] = "x"
	}
	in.CursorRow = 10
	in.Edits = []EditHunk{{Row: 10, New: []string{"x"}}}
	if _, ok := ParseNextEdit(`{"start":300,"end":300,"lines":["y"]}`, in); ok {
		t.Fatal("a line the model never saw must be refused")
	}
	if _, ok := ParseNextEdit(`{"start":20,"end":20,"lines":["y"]}`, in); !ok {
		t.Fatal("a line in the window is fine")
	}
}

func TestNextEditAllowed(t *testing.T) {
	for _, p := range []string{"/p/.env", "/p/.env.local", "/p/server.pem", "/p/tls.key", "/home/u/.ssh/config",
		"/p/id_rsa", "/p/credentials.json", "/p/my_secrets.yaml", "/p/.netrc", "/p/token.txt", "/p/.npmrc"} {
		if ok, _ := NextEditAllowed(p, 10, 100); ok {
			t.Errorf("%s must be blocked", p)
		}
	}
	for _, p := range []string{"/p/main.go", "/p/env.go", "/p/keyboard.ts", "/p/README.md"} {
		if ok, why := NextEditAllowed(p, 10, 100); !ok {
			t.Errorf("%s blocked: %s", p, why)
		}
	}
	if ok, _ := NextEditAllowed("", 10, 100); ok {
		t.Error("a buffer with no file is not sent")
	}
	if ok, _ := NextEditAllowed("/p/main.go", NextEditMaxBufLines+1, 100); ok {
		t.Error("too many lines")
	}
	if ok, _ := NextEditAllowed("/p/main.go", 10, NextEditMaxBufBytes+1); ok {
		t.Error("too many bytes")
	}
}

// IsSecretFileName is the one rule for "this file may hold secrets"; the
// web UI reuses it to refuse sending such a file's text.
func TestIsSecretFileName(t *testing.T) {
	for _, p := range []string{"/w/.env", "/w/.env.local", "/w/prod.env", "/w/deploy/key.pem", "/home/u/.ssh/config",
		"/w/.npmrc", "/w/id_ed25519", "/w/db_password.txt", `C:\w\Secrets.json`} {
		if !IsSecretFileName(p) {
			t.Errorf("%s: not a secret file", p)
		}
	}
	for _, p := range []string{"/w/main.go", "/w/README.md", "/w/env.go", "/w/environment/x.go"} {
		if IsSecretFileName(p) {
			t.Errorf("%s: called a secret file", p)
		}
	}
}
