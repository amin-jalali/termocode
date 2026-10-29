package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Compare Two Files: pick the left file, then the right one; the diff
// card shows both. A file that went away or is too big is an error; an
// AI review still waiting keeps its card.
func TestCovGitCompareTwoFiles(t *testing.T) {
	m, dir := covGitRepo(t)
	left := filepath.Join(dir, "a.txt")
	right := covWrite(t, dir, "b.txt", "one\nTWO\nthree\n")

	m.dispatchPaletteAction("diff-files")
	if !m.pickerOpen || m.pickerKind != pickerKindDiffLeft || !strings.Contains(m.picker.View(), "b.txt") {
		t.Fatal("no left picker")
	}
	m.handlePickerSelect(picker.SelectMsg{ID: left})
	if !m.pickerOpen || m.pickerKind != pickerKindDiffRight || m.diffLeftPath != left {
		t.Fatal("no right picker")
	}
	ready := covMsg[compareReadyMsg](t, m.handlePickerSelect(picker.SelectMsg{ID: right}))
	if ready.file.Name != "a.txt ↔ b.txt" || ready.file.New != "one\nTWO\nthree\n" {
		t.Errorf("compare %+v", ready.file)
	}

	pending := &aiReview{card: diffview.New("AI", []diffview.File{{Name: "x.go", Old: "a\n", New: "b\n"}}, true)}
	m.aiS().review = pending
	m.openCompareCard(ready)
	if m.aiS().review != pending || !toastHas(m, "Finish the AI review first") {
		t.Fatal("the waiting AI review was replaced")
	}
	m.aiS().review = nil
	m.openCompareCard(ready)
	if !m.reviewOpen || m.aiS().review == nil {
		t.Fatal("compare card not open")
	}

	if e := covMsg[ErrMsg](t, m.onDiffRightPicked(filepath.Join(dir, "missing"))); !strings.Contains(e.Err.Error(), "compare:") {
		t.Errorf("missing file: %v", e.Err)
	}
	big := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(big, make([]byte, maxCompareBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if e := covMsg[ErrMsg](t, m.onDiffRightPicked(big)); !strings.Contains(e.Err.Error(), "too big") {
		t.Errorf("big file: %v", e.Err)
	}
	if !strings.Contains(covMsg[ErrMsg](t, m.onDiffRightPicked(dir)).Err.Error(), "compare:") {
		t.Error("a folder was read as a file")
	}
	m.diffLeftPath = ""
	if m.onDiffRightPicked(right) != nil {
		t.Error("compare with no left file")
	}

	// An empty workspace has nothing to compare.
	covGitEnv(t)
	m2 := covModelAt(t, t.TempDir())
	m2.dispatchPaletteAction("diff-files")
	if !toastHas(m2, "No files in workspace") || m2.pickerOpen {
		t.Error("empty workspace: no note")
	}
}
