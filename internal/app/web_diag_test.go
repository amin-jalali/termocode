package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (desktop Problems): the topic lists the workspace's files, worst
// first, then by path; a file's items by severity, row and column. Files
// outside the workspace and files that may hold secrets are left out of
// the list and the counts; the caps cut per file and in all, and More
// says how many were not listed.
func TestDiagBuildSortsFiltersAndCaps(t *testing.T) {
	root := "/ws"
	rel := func(abs string) (string, bool) {
		if !strings.HasPrefix(abs, root+"/") {
			return "", false
		}
		return strings.TrimPrefix(abs, root+"/"), true
	}
	rows := []webLuaDiagRow{
		{Path: "/ws/b.go", Sev: 2, Line: 3, Col: 1, EndLine: 3, EndCol: 4, Msg: "unused"},
		{Path: "/ws/a.go", Sev: 3, Line: 0, Col: 0, EndLine: 0, EndCol: 1, Msg: "info"},
		{Path: "/ws/b.go", Sev: 1, Line: 9, Col: 2, EndLine: 9, EndCol: 5, Msg: "bad", Source: "gopls", Code: "E1"},
		{Path: "/ws/b.go", Sev: 1, Line: 1, Col: 7, EndLine: 0, EndCol: 0, Msg: "reversed"},
		{Path: "/elsewhere/c.go", Sev: 1, Line: 0, Msg: "outside"},
		{Path: "/ws/.env", Sev: 1, Line: 0, Msg: "secret"},
		{Path: "/ws/a.go", Sev: 9, Line: 5, Msg: "odd severity"},
		{Path: "/ws/a.go", Sev: 4, Line: 6, Msg: strings.Repeat("é", webproto.MaxDiagMessage)},
	}
	s := webDiagBuild(rows, rel)
	if s.Errors != 3 || s.Warnings != 1 || s.Infos != 1 || s.Hints != 1 || s.More != 0 {
		t.Errorf("counts %+v", s)
	}
	var got []string
	for _, f := range s.Files {
		for _, d := range f.Items {
			got = append(got, fmt.Sprintf("%s:%d:%d-%d:%d %d", f.Path, d.Line, d.Col, d.EndLine, d.EndCol, d.Sev))
		}
	}
	want := []string{
		"a.go:5:0-5:0 1", "a.go:0:0-0:1 3", "a.go:6:0-6:0 4",
		"b.go:1:7-1:7 1", "b.go:9:2-9:5 1", "b.go:3:1-3:4 2",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("order:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if m := s.Files[0].Items[2].Message; len(m) > webproto.MaxDiagMessage || !strings.HasPrefix(m, "é") || strings.ContainsRune(m, '�') {
		t.Errorf("long message cut badly: %d bytes", len(m))
	}
	if d := s.Files[1].Items[1]; d.Source != "gopls" || d.Code != "E1" {
		t.Errorf("source and code %+v", d)
	}

	// Caps: per file, then in all.
	rows = rows[:0]
	for i := 0; i < webproto.MaxDiagPerFile+10; i++ {
		rows = append(rows, webLuaDiagRow{Path: "/ws/x.go", Sev: 1, Line: i})
	}
	for f := 0; f < 5; f++ {
		for i := 0; i < webproto.MaxDiagPerFile; i++ {
			rows = append(rows, webLuaDiagRow{Path: fmt.Sprintf("/ws/y%d.go", f), Sev: 2, Line: i})
		}
	}
	s = webDiagBuild(rows, rel)
	n := 0
	for _, f := range s.Files {
		n += len(f.Items)
	}
	all := webproto.MaxDiagPerFile + 10 + 5*webproto.MaxDiagPerFile
	if n != webproto.MaxDiagItems || s.More != all-n || s.Errors+s.Warnings != all {
		t.Errorf("caps: listed %d more %d counts %d+%d", n, s.More, s.Errors, s.Warnings)
	}
	if len(s.Files[0].Items) != webproto.MaxDiagPerFile || s.Files[0].Path != "x.go" {
		t.Errorf("per-file cap: %s has %d", s.Files[0].Path, len(s.Files[0].Items))
	}
}

// Spec: with a browser listening, the laptop's diagnostics reach the
// topic with their whole range in UTF-16 units of the row (a Persian
// word and an emoji before the mark count as JavaScript counts them),
// and a change of the laptop's Problems list sends them again.
func TestDiagTopicFollowsTheLaptop(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	r.m.problems = newProblemsPanel() // as the app has it
	line := `x := "سلام 😀"; bad()`
	r.write("a.go", "package a\n"+line+"\n")
	b := webBufOpen(t, r, r.c, "a.go")
	if snap := r.sub("diagnostics"); len(snap["files"].([]any)) != 0 {
		t.Fatalf("first snap %v", snap)
	}
	at := strings.Index(line, "bad")
	r.lua(`local b, c0, c1 = ...
vim.diagnostic.set(vim.api.nvim_create_namespace('t'), b, {
  { lnum = 1, col = c0, end_lnum = 1, end_col = c1, severity = 1, message = 'undefined: bad', source = 'gopls', code = 'UndeclaredName' },
  { lnum = 0, col = 0, severity = 2, message = 'package doc' },
})`, nil, b.Buf, at, at+3)
	webDrain(t, r.m, r.m.refreshProblemsCmd())
	webDrain(t, r.m, r.m.webPublishDiag(nil))
	ds := r.deltas("diagnostics")
	if len(ds) != 1 {
		t.Fatalf("deltas %v", ds)
	}
	var snap webproto.DiagSnap
	webResData(t, map[string]any{"t": "res", "data": ds[0]}, &snap)
	if snap.Errors != 1 || snap.Warnings != 1 || len(snap.Files) != 1 || snap.Files[0].Path != "a.go" {
		t.Fatalf("snap %+v", snap)
	}
	d := snap.Files[0].Items[0]
	c := u16(line[:at])
	if d.Line != 1 || d.Col != c || d.EndLine != 1 || d.EndCol != c+3 || d.Message != "undefined: bad" || d.Source != "gopls" || d.Code != "UndeclaredName" {
		t.Errorf("error %+v, want cols %d-%d", d, c, c+3)
	}
	// Nothing new on the laptop: nothing sent.
	webDrain(t, r.m, r.m.webPublishDiag(nil))
	if ds := r.deltas("diagnostics"); len(ds) != 0 {
		t.Errorf("sent again without a change: %v", ds)
	}
	// The laptop's list changes: sent again.
	r.lua(`vim.diagnostic.reset(vim.api.nvim_create_namespace('t'))`, nil)
	webDrain(t, r.m, r.m.refreshProblemsCmd())
	webDrain(t, r.m, r.m.webPublishDiag(nil))
	ds = r.deltas("diagnostics")
	if len(ds) != 1 || len(ds[0]["files"].([]any)) != 0 || ds[0]["errors"].(float64) != 0 {
		t.Errorf("after reset %v", ds)
	}
}

// Spec (WEB-7): the diagnostics topic replays: an error and a warning
// appear, then the error is fixed; queued whole snapshots, the newest
// replacing the older.
func TestDiagReplayGolden(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	r.m.problems = newProblemsPanel()
	r.write("main.go", "package main\n\nfunc main() { x }\n")
	b := webBufOpen(t, r, r.c, "main.go")
	set := func(code string) {
		r.lua(code, nil, b.Buf)
		webDrain(t, r.m, r.m.refreshProblemsCmd())
		webDrain(t, r.m, r.m.webPublishDiag(nil))
	}
	c := webReplayRecord(t, r.m, r.h, "diagnostics", func() {
		webDrain(t, r.m, r.m.webPublishDiag(nil))
		set(`local b = ...
vim.diagnostic.set(vim.api.nvim_create_namespace('r'), b, {
  { lnum = 2, col = 14, end_lnum = 2, end_col = 15, severity = 1, message = 'undefined: x', source = 'compiler' },
  { lnum = 0, col = 0, end_lnum = 0, end_col = 12, severity = 2, message = 'missing package doc', source = 'lint' },
})`)
		set(`local b = ...
vim.diagnostic.set(vim.api.nvim_create_namespace('r'), b, {
  { lnum = 0, col = 0, end_lnum = 0, end_col = 12, severity = 2, message = 'missing package doc', source = 'lint' },
})`)
	})
	webReplayCheck(t, r.h.topics["diagnostics"], c)
	webReplayGolden(t, webReplayPath("diagnostics", "error_then_fixed"), c)
}
