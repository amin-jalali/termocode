package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/git"
)

// covOpenCommit opens the commit card on hash and loads its first file.
func covOpenCommit(t *testing.T, m *Model, hash string) {
	t.Helper()
	ready := covMsg[commitViewReadyMsg](t, m.gitOpenCommitViewCmd(hash))
	if ready.err != nil {
		t.Fatalf("commit %s: %v", hash, ready.err)
	}
	if cmd := m.onCommitViewReady(ready); cmd != nil {
		if txt, ok := cmd().(commitFileTextMsg); ok {
			if syn := m.onCommitFileText(txt); syn != nil {
				if s, ok := syn().(commitFileSyntaxMsg); ok {
					m.onCommitFileSyntax(s)
				}
			}
		}
	}
	if !m.commitViewShown() {
		t.Fatal("commit card not open")
	}
}

// covRenameCommit adds a commit that renames b.go, deletes c.txt and has
// a long subject.
func covRenameCommit(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	covWrite(t, dir, "b.go", "package b\n\nfunc B() {}\n")
	covWrite(t, dir, "c.txt", "c\n")
	covCommit(t, dir, "add b and c")
	covGit(t, dir, "mv", "b.go", "r.go")
	covGit(t, dir, "rm", "-q", "c.txt")
	covCommit(t, dir, "rename b.go to r.go and delete c.txt so the subject is long")
	covRefresh(m)
	return m, dir
}

// The commit card: a renamed file is tagged; its text loads (and comes
// from the cache the second time); the long subject is cut in messages;
// Copy SHA, Raw, a parent and the ⋯ menu work; a deleted file can not be
// opened, neither in the work tree nor at the commit.
func TestCovGitCommitCard(t *testing.T) {
	m, dir := covRenameCommit(t)
	head := covGit(t, dir, "rev-parse", "HEAD")
	covOpenCommit(t, m, head)
	cv := m.commitView
	if m.gitOpenCommitViewCmd(head) != nil || m.gitOpenCommitViewCmd("") != nil {
		t.Error("the shown commit (or none) was loaded again")
	}
	var renamed, deleted int = -1, -1
	for i, f := range cv.info.Files {
		switch f.Status {
		case 'R':
			renamed = i
		case 'D':
			deleted = i
		}
	}
	if renamed < 0 || deleted < 0 {
		t.Fatalf("files %+v", cv.info.Files)
	}
	if got := commitFiles(cv.info)[renamed].Tag; got != "from b.go" {
		t.Errorf("rename tag %q", got)
	}
	if got := commitViewTitle(cv.info); !strings.HasSuffix(got, "…\"") || len([]rune(got)) > 52 {
		t.Errorf("title %q", got)
	}

	// Load the renamed file twice: the second time comes from the cache.
	load := func() {
		cmd := m.commitLoadFile(renamed)
		if cmd == nil {
			return
		}
		switch msg := cmd().(type) {
		case commitFileTextMsg:
			if syn := m.onCommitFileText(msg); syn != nil {
				m.onCommitFileSyntax(syn().(commitFileSyntaxMsg))
			}
		case commitFileSyntaxMsg:
			m.onCommitFileSyntax(msg)
		}
	}
	load()
	if f := cv.card.File(renamed); f.Pending || !strings.Contains(f.New, "func B") {
		t.Fatalf("renamed file not loaded: %+v", f)
	}
	key := cv.oldRev + ".." + cv.newRev + ":r.go"
	if _, ok := cv.blobs[key]; !ok {
		t.Fatal("file text not cached")
	}
	cv.card = diffview.NewCommit(commitHeader(cv.info), commitFiles(cv.info), commitButtons())
	load() // from the cache, with its syntax
	if f := cv.card.File(renamed); f.Pending {
		t.Error("cached file not shown")
	}
	delete(cv.syn, key)
	cv.card = diffview.NewCommit(commitHeader(cv.info), commitFiles(cv.info), commitButtons())
	load() // text cached, syntax not

	m.commitAction("copy-sha", 0)
	if !toastHas(m, "Copied "+cv.info.Hash) {
		t.Error("no copy toast")
	}
	m.commitAction("open-file", deleted)
	if !toastHas(m, "c.txt is not in the working tree") {
		t.Error("deleted file opened")
	}
	m.commitAction("open-at-commit", deleted)
	if !toastHas(m, "c.txt was deleted in this commit") {
		t.Error("deleted file opened at the commit")
	}
	at := covMsg[commitFileAtMsg](t, m.commitAction("open-at-commit", renamed))
	if at.path != "r.go" || !strings.Contains(at.text, "func B") {
		t.Errorf("file at commit %+v", at)
	}
	if m.commitAction("open-file", 99) != nil {
		t.Error("file 99 did something")
	}
	parent := covMsg[commitViewReadyMsg](t, m.commitAction("parent:"+cv.info.Parents[0], 0))
	if parent.err != nil || !strings.HasPrefix(parent.info.Subject, "add b and c") {
		t.Errorf("parent %+v", parent)
	}
	m.onCommitViewReady(parent)
	if m.commitView.info.Subject != "add b and c" {
		t.Error("parent not shown")
	}
	covOpenCommit(t, m, head)
	m.commitAction("open-file", renamed)
	if m.commitViewShown() || m.focus != FocusEditor {
		t.Error("Open file did not close the card")
	}
	m.openFileAtCommit(at) // no editor: just closes

	covOpenCommit(t, m, head)
	if covMsg[gitCommitDiffReadyMsg](t, m.commitAction("raw", 0)).path == "" || m.commitViewShown() {
		t.Error("Raw did not open the commit diff")
	}
	// The card's own messages: Close, an unknown message passes on.
	covOpenCommit(t, m, head)
	if m.runCommitCardCmd(func() tea.Msg { return nil }) != nil {
		t.Error("an empty message did something")
	}
	if got := m.runCommitCardCmd(func() tea.Msg { return tea.FocusMsg{} }); got == nil {
		t.Error("an unknown message was dropped")
	}
	m.runCommitCardCmd(func() tea.Msg { return diffview.HideMsg{} })
	if m.commitViewShown() || m.commitLoadFile(0) != nil {
		t.Error("Close did not close")
	}
	if m.onCommitFileText(commitFileTextMsg{}) != nil {
		t.Error("text for a closed card was taken")
	}
}

// A commit that does not exist and a stale answer.
func TestCovGitCommitCardErrors(t *testing.T) {
	m, _ := covGitRepo(t)
	ready := covMsg[commitViewReadyMsg](t, m.gitOpenCommitViewCmd("0123456789abcdef"))
	m.onCommitViewReady(ready)
	if !toastHas(m, "Could not read the commit") || m.commitViewShown() {
		t.Error("bad commit: no error")
	}
	m.commitView.want = "other"
	if m.onCommitViewReady(ready) != nil {
		t.Error("a stale answer was taken")
	}
	var empty Model
	if empty.commitAction("copy-sha", 0) != nil {
		t.Error("action with no card")
	}
}

// Compare two GRAPH commits: the card copies the range and refuses the
// commit-only actions; a mark on a commit that left the graph is dropped.
func TestCovGitCompareCard(t *testing.T) {
	m, dir := covRenameCommit(t)
	if m.gitCompareMark(-1) != nil || m.gitCompareMark(99) != nil {
		t.Error("mark out of range")
	}
	m.gitCompareMark(0)
	cmd := m.gitCompareMark(1)
	ready := covMsg[commitViewReadyMsg](t, cmd)
	if txt, ok := m.onCommitViewReady(ready)().(commitFileTextMsg); ok {
		m.onCommitFileText(txt)
	}
	cv := m.commitView
	if cv.compare == nil || !strings.Contains(compareHeader(cv.info, *cv.compare).Subject, "1 commit from") {
		t.Fatalf("compare %+v", cv.compare)
	}
	m.commitAction("copy-range", 0)
	if !toastHas(m, "Copied "+cv.compare.sa+".."+cv.compare.sb) {
		t.Error("no copy-range toast")
	}
	for _, id := range []string{"raw", "revert", "cherry-pick", "open-at-commit"} {
		if m.commitAction(id, 0) != nil || m.confirmOpen {
			t.Errorf("%s ran on a compare", id)
		}
	}
	m.commitAction("more", 0)
	if !strings.Contains(compareHeader(git.CommitInfo{Commits: 3}, commitRange{sa: "a", sb: "b"}).Subject, "3 commits") {
		t.Error("plural")
	}

	cv.mark = "gone"
	if m.gitCompareMark(0) != nil || cv.mark != "" {
		t.Error("a mark on a lost commit was kept")
	}
	_ = dir
}

// Revert and cherry-pick: Cherry-pick of HEAD is refused; Revert runs; a
// cherry-pick that conflicts stops with the count; a revert git refuses
// is an error.
func TestCovGitCommitPick(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "one\nTWO\nthree\n")
	covCommit(t, dir, "two upper")
	covRefresh(m)
	head := covGit(t, dir, "rev-parse", "HEAD")
	covOpenCommit(t, m, head)
	m.commitAction("cherry-pick", 0)
	if !toastHas(m, "This commit is HEAD already") || m.confirmOpen {
		t.Fatal("cherry-pick of HEAD not refused")
	}
	m.commitAction("revert", 0)
	if !m.confirmOpen || m.confirmKind != confirmKindCommitRevert {
		t.Fatal("no revert card")
	}
	m.confirmOpen = false
	done := covMsg[commitPickDoneMsg](t, m.handleConfirmSelect("revert"))
	m.onCommitPickDone(done)
	if done.err != nil || !toastHas(m, "Reverted ") || covGit(t, dir, "log", "-1", "--format=%s") != `Revert "two upper"` {
		t.Fatalf("revert: %v", done.err)
	}

	// Cherry-pick "two upper" on a branch that changed the same line.
	covGit(t, dir, "checkout", "-q", "-b", "other", head+"~1")
	covWrite(t, dir, "a.txt", "one\nDOS\nthree\n")
	covCommit(t, dir, "two spanish")
	covRefresh(m)
	covOpenCommit(t, m, head)
	m.commitAction("cherry-pick", 0)
	m.confirmOpen = false
	done = covMsg[commitPickDoneMsg](t, m.handleConfirmSelect("cherry-pick"))
	m.onCommitPickDone(done)
	if len(done.conflicts) != 1 || !toastHas(m, "Cherry-pick stopped: 1 conflict") {
		t.Fatalf("cherry-pick: %+v", done)
	}
	m.onCommitPickDone(commitPickDoneMsg{verb: "revert", conflicts: []string{"a", "b"}})
	if !toastHas(m, "Revert stopped: 2 conflicts") {
		t.Error("plural conflicts")
	}

	// git refuses: local changes are in the way.
	covGit(t, dir, "cherry-pick", "--abort")
	covWrite(t, dir, "a.txt", "local\n")
	covOpenCommit(t, m, head)
	m.commitAction("revert", 0)
	m.confirmOpen = false
	done = covMsg[commitPickDoneMsg](t, m.handleConfirmSelect("revert"))
	m.onCommitPickDone(done)
	if done.err == nil || !toastHas(m, "Revert failed") {
		t.Fatalf("revert during a cherry-pick: %+v", done)
	}
	if m.runCommitPick("revert") != nil {
		t.Error("a second answer ran again")
	}
}

// Files over the size limit are not diffed; the cache keeps a bounded
// number of files.
func TestCovGitCommitBigFilesAndCache(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "big.txt", strings.Repeat("x\n", commitMaxLines+1))
	for i := 0; i < commitBlobCache+2; i++ {
		covWrite(t, dir, fmt.Sprintf("f%02d.txt", i), fmt.Sprintf("%d\n", i))
	}
	covCommit(t, dir, "many files")
	covRefresh(m)
	covOpenCommit(t, m, covGit(t, dir, "rev-parse", "HEAD"))
	cv := m.commitView
	for i := range cv.info.Files {
		if cmd := m.commitLoadFile(i); cmd != nil {
			if txt, ok := cmd().(commitFileTextMsg); ok {
				m.onCommitFileText(txt)
				if cv.info.Files[i].Path == "big.txt" && txt.note != commitTooBig {
					t.Errorf("big file note %q", txt.note)
				}
			}
		}
	}
	if len(cv.order) != commitBlobCache || len(cv.blobs) != commitBlobCache {
		t.Errorf("cache holds %d / %d", len(cv.order), len(cv.blobs))
	}
	if _, err := os.Stat(filepath.Join(dir, "big.txt")); err != nil {
		t.Fatal(err)
	}
	m.zenMode = true
	if _, _, _, _, ok := m.commitDocked(); ok {
		t.Error("docked in zen mode")
	}
}
