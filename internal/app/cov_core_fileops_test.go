package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/search"
)

func coreSubmit(m Model, kind promptKindEnum, path, value string) Model {
	m.promptOpen, m.promptKind, m.promptPath = true, kind, path
	m, _ = m.update(prompt.SubmitMsg{Value: value})
	return m
}

// Rename, New File and New Folder report disk errors and change nothing.
func TestCovCoreFileOpsErrors(t *testing.T) {
	withConfigDir(t)
	m := covNew(t)
	m.w, m.h = 100, 30
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	_ = os.WriteFile(blocker, nil, 0o644)

	m = coreSubmit(m, promptKindRename, filepath.Join(dir, "gone.txt"), "new.txt")
	if m.err == "" {
		t.Error("renaming a missing file gave no error")
	}
	m = coreSubmit(m, promptKindNewFile, blocker, "x.go")
	if m.err == "" {
		t.Error("a new file inside a file gave no error")
	}
	m = coreSubmit(m, promptKindNewFile, dir, "sub/deeper/x.go")
	if m.err != "" {
		t.Errorf("new file in new folders: %q", m.err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "deeper", "x.go")); err != nil {
		t.Error("new file not made")
	}
	m = coreSubmit(m, promptKindNewFile, dir, "sub/deeper/x.go")
	if m.err == "" {
		t.Error("a new file over an existing one gave no error")
	}
	m = coreSubmit(m, promptKindNewFolder, blocker, "dir")
	if m.err == "" {
		t.Error("a new folder inside a file gave no error")
	}
	// Go to Line ignores what is not a line number.
	for _, v := range []string{"", "x", "0", "-3"} {
		if m.gotoLineFromPrompt(v) != nil {
			t.Errorf("goto %q did something", v)
		}
	}
}

// Deleting many items lists five and counts the rest.
func TestCovCoreBulkDeleteQuestion(t *testing.T) {
	withConfigDir(t)
	m := covNew(t)
	m.w, m.h = 100, 30
	m.openBulkDeleteConfirm(nil)
	if m.confirmOpen {
		t.Fatal("an empty selection asked")
	}
	var paths []string
	for _, n := range []string{"g", "f", "e", "d", "c", "b", "a"} {
		paths = append(paths, "/w/"+n+".txt")
	}
	m.openBulkDeleteConfirm(paths)
	v := ansiPlain(m.confirm.View())
	if !strings.Contains(v, "Delete 7 items?") || !strings.Contains(v, "a.txt") || !strings.Contains(v, "…and 2 more") || strings.Contains(v, "g.txt") {
		t.Errorf("question:\n%s", v)
	}
}

// The Find Results page: the Aa / ab / .* chips search again with the
// option flipped; an unknown chip and an empty query do nothing.
func TestCovCoreFindToggles(t *testing.T) {
	withConfigDir(t)
	m := covNew(t)
	m.w, m.h = 100, 30
	m.findRun.query = "needle"
	toggle := notifyHandlers["termocode_find_toggle"]
	for _, chip := range []string{"Aa", "ab", ".*"} {
		b := m.findRun.opts
		before := [3]bool{b.CaseSensitive, b.WholeWord, b.Regex}
		if toggle(&m, []any{chip}) == nil {
			t.Errorf("%s: no search", chip)
		}
		a := m.findRun.opts
		if [3]bool{a.CaseSensitive, a.WholeWord, a.Regex} == before || !m.findRun.reuse {
			t.Errorf("%s: options %+v unchanged", chip, m.findRun.opts)
		}
	}
	if toggle(&m, []any{"zz"}) != nil {
		t.Error("an unknown chip searched")
	}
	if m.runFindInFilesWith("   ", search.Options{}) != nil {
		t.Error("an empty query searched")
	}
	if notifyHandlers["termocode_find_close"](&m, nil) != nil {
		t.Error("close without nvim")
	}
	m.findRun.cancel()
}

// Streamed Find in Files batches: the first opens the page, the next
// appends, the end with an error says so; a closed stream ends the run.
func TestCovCoreFindBatches(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	dir := t.TempDir()
	ch := make(chan search.Batch)
	close(ch)
	m.findRun = findResultsRun{gen: 7, query: "needle", roots: []string{dir},
		started: time.Now().Add(-2 * time.Second), st: findState{searching: true, engine: "basic"}}
	res := func(path string) search.Batch {
		return search.Batch{Results: []search.Result{{Path: path, Line: 1, Col: 1, Preview: "a needle"}}}
	}
	m, _ = m.update(findBatchMsg{gen: 7, ch: ch, batch: res("a.txt")})
	if !m.findRun.opened {
		t.Fatal("the first batch did not open the page")
	}
	covWaitLua(t, m, `return vim.g.termocode_find_results_buf and '1' or '0'`, "1")
	m, _ = m.update(findBatchMsg{gen: 7, ch: ch, batch: res("b.txt")})
	if m.findRun.st.matches != 2 || m.findRun.st.files != 2 {
		t.Errorf("after two batches: %+v", m.findRun.st)
	}
	m, _ = m.update(findBatchMsg{gen: 7, ch: ch, batch: search.Batch{Done: true, Err: errors.New("rg: broken pattern")}})
	if !strings.Contains(toastText(m), "Find failed") || m.findRun.st.searching {
		t.Errorf("end with an error:\n%s", toastText(m))
	}
	if h := m.findHead(); !strings.Contains(fmt.Sprint(h), " s") {
		t.Errorf("a long search shows its time: %v", h)
	}
	// A batch of an older run is dropped.
	if cmd := m.handleFindBatch(findBatchMsg{gen: 3}); cmd != nil {
		t.Error("an old batch was used")
	}
	// A closed stream reads as the end.
	if msg := waitFindBatch(7, ch)(); !msg.(findBatchMsg).batch.Done {
		t.Error("a closed stream is not the end")
	}
}
