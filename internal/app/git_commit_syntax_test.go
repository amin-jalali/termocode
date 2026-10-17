package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/git"
)

// Spec (commit view §3.4): a loaded file shows plain at once; its syntax
// tokens are computed off the loop (a Cmd) and colour it when they
// arrive; the tokens are kept with the file's text, so opening it again
// from the cache is coloured at once. A file chroma does not know gets
// no token work at all.
func TestCommitViewSyntaxAfterText(t *testing.T) {
	m := &Model{}
	cv := m.commitViewS()
	cv.want = "abc"
	info := git.CommitInfo{FullHash: "abcfull", Parents: []string{"p"},
		Files: []git.CommitFile{{Path: "a.go", Status: 'M'}, {Path: "notes.zzzz", Status: 'M'}}}
	info.Hash = "abc"
	m.onCommitViewReady(commitViewReadyMsg{want: "abc", info: info})
	if !cv.open {
		t.Fatal("card not open")
	}
	sides := cv.oldRev + ".." + cv.newRev
	cmd := m.onCommitFileText(commitFileTextMsg{sides: sides, file: 0, path: "a.go",
		old: "package a\n", new: "package a\n\nfunc f() {}\n"})
	if cmd == nil {
		t.Fatal("no token Cmd after the text")
	}
	if cv.card.HasSyntax(0) {
		t.Fatal("coloured before the tokens arrived")
	}
	msg, ok := cmd().(commitFileSyntaxMsg)
	if !ok {
		t.Fatalf("token Cmd gave %T", cmd())
	}
	stale := msg
	stale.sides = "x..y"
	m.onCommitFileSyntax(stale)
	if cv.card.HasSyntax(0) {
		t.Error("tokens for other sides coloured the file")
	}
	m.onCommitFileSyntax(msg)
	if !cv.card.HasSyntax(0) {
		t.Fatal("tokens did not colour the file")
	}

	// From the cache: coloured at once, no Cmd.
	if cmd := m.onCommitViewReady(commitViewReadyMsg{want: "abc", info: info}); cmd != nil {
		t.Error("a cached file runs a Cmd")
	}
	if !cv.card.HasSyntax(0) {
		t.Error("a cached file is not coloured at once")
	}

	if cmd := m.onCommitFileText(commitFileTextMsg{sides: sides, file: 1, path: "notes.zzzz", old: "a\n", new: "b\n"}); cmd != nil {
		t.Error("an unknown file type runs a token Cmd")
	}
}
