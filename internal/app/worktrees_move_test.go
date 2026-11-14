package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/lanemove"
)

// wtApply runs cmd (of a batch: its first command, the work; the rest
// are toasts that would wait) and applies its applyMsg; it returns the
// follow-up, not run.
func wtApply(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok && len(b) > 0 && b[0] != nil {
		msg = b[0]()
	}
	a, ok := msg.(applyMsg)
	if !ok {
		t.Fatalf("%T is not an applyMsg", msg)
	}
	return a(m)
}

// wtFlatBox is the card's text on one line: no borders, no wrapping.
func wtFlatBox(m *Model) string {
	var words []string
	for _, w := range strings.Fields(wtBox(m)) {
		if w = strings.Trim(w, "│╭╮╰╯─┌┐└┘"); w != "" {
			words = append(words, w)
		}
	}
	return strings.Join(words, " ")
}

// wtMoveEnv points the lanes folder, the home and the settings at temp
// folders.
func wtMoveEnv(t *testing.T) (lanes, home string) {
	t.Helper()
	data, home := t.TempDir(), t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	old := wtLaneHome
	wtLaneHome = func() (string, error) { return home, nil }
	t.Cleanup(func() { wtLaneHome = old })
	return filepath.Join(data, "termocode", "lanes"), home
}

// CMP-6: M asks with Cancel first (Esc and Cancel pack nothing), then
// packs the lane into the lanes folder; U lists the pack and unpacks it
// as a NEW worktree on a free branch, after a card of its own.
func TestWorktreeMovePackThenUnpack(t *testing.T) {
	lanes, _ := wtMoveEnv(t)
	m, root, x, _ := wtRealModel(t)
	if err := os.WriteFile(filepath.Join(x, "draft.txt"), []byte("work in progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.wt.cursorPath = x
	if c := wtKey(t, m, "M"); c != nil {
		t.Fatal("M ran before the card")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindWTMove || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no safe-first card: open %v kind %d safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	for _, want := range []string{"Pack lane", "Pack and download", "Nothing here changes", "archive it here (x)", "termocode lane pull"} {
		if !strings.Contains(wtFlatBox(m), want) {
			t.Errorf("card lacks %q:\n%s", want, wtFlatBox(m))
		}
	}
	m.confirmOpen = false
	if c := m.onAIConfirmClosed(); c != nil {
		t.Error("Esc started work")
	}
	wtKey(t, m, "M")
	m.confirmOpen = false
	if c, _ := m.handleAIConfirm("cancel"); c != nil {
		t.Error("Cancel started work")
	}
	if got := lanemove.Packs(lanes); len(got) != 0 {
		t.Fatalf("packed without a yes: %v", got)
	}

	wtKey(t, m, "M")
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm("pack")
	wtApply(t, m, cmd)
	packs := lanemove.Packs(lanes)
	if len(packs) != 1 {
		t.Fatalf("packs %v", packs)
	}
	man, err := lanemove.InspectFile(packs[0])
	if err != nil || man.Branch != "feat/x" || len(man.Files) != 1 || man.Files[0] != "draft.txt" {
		t.Fatalf("pack %+v %v", man, err)
	}
	if _, err := os.Stat(filepath.Join(x, "draft.txt")); err != nil {
		t.Fatal("pack touched the worktree")
	}

	// U: the pack is listed under the pull row.
	if c := wtKey(t, m, "U"); c != nil || !m.pickerOpen || m.pickerKind != pickerKindWTUnpack {
		t.Fatalf("no unpack picker: %v %d", m.pickerOpen, m.pickerKind)
	}
	m.pickerOpen = false
	wtApply(t, m, m.onWTUnpackPicked(wtPackPfx+packs[0]))
	if !m.confirmOpen || m.confirmKind != confirmKindWTUnpack || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no unpack card: %v %d", m.confirmOpen, m.confirmKind)
	}
	// The card's whole text: a long host and path (macOS temp folders)
	// push the end of a drawn card into its "…and N more lines" row.
	unpack := strings.Join(strings.Fields(m.confirm.Message()), " ") + " " + wtFlatBox(m)
	for _, want := range []string{"branch feat/x-moved", "Nothing here is written over", "1 new file", "Unpack as a new worktree"} {
		if !strings.Contains(unpack, want) {
			t.Errorf("unpack card lacks %q:\n%s", want, unpack)
		}
	}
	m.confirmOpen = false
	if c, _ := m.handleAIConfirm("cancel"); c != nil {
		t.Error("Cancel unpacked")
	}
	wtApply(t, m, m.onWTUnpackPicked(wtPackPfx+packs[0]))
	m.confirmOpen = false
	cmd, _ = m.handleAIConfirm("unpack")
	wtApply(t, m, cmd)
	base := filepath.Dir(root)
	if r, err := filepath.EvalSymlinks(base); err == nil {
		base = r
	}
	folder := filepath.Join(base, "app.worktrees", "feat-x-moved")
	if b, err := os.ReadFile(filepath.Join(folder, "draft.txt")); err != nil || string(b) != "work in progress\n" {
		t.Fatalf("unpacked draft %q %v", b, err)
	}
	if m.wt.cursorPath != folder {
		t.Errorf("cursor on %s, want the new worktree", m.wt.cursorPath)
	}
}

// Pull asks for host:folder, refuses anything else, asks with Cancel
// first, then fetches the pack over ssh into the lanes folder and opens
// the unpack card.
func TestWorktreePullAsksThenFetches(t *testing.T) {
	lanes, _ := wtMoveEnv(t)
	m, _, x, _ := wtRealModel(t)
	if c := m.worktreePull(); c != nil || !m.promptOpen || m.promptKind != promptKindWTPull {
		t.Fatal("no host:folder prompt")
	}
	m.promptOpen = false
	m.onWTPullEntered("-oProxyCommand=evil:/x")
	if m.confirmOpen {
		t.Fatal("a bad host got a card")
	}
	m.onWTPullEntered("srv:~/src/app.worktrees/x")
	if !m.confirmOpen || m.confirmKind != confirmKindWTPull || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no pull card: %v %d", m.confirmOpen, m.confirmKind)
	}
	m.confirmOpen = false
	if c := m.onAIConfirmClosed(); c != nil {
		t.Error("Esc pulled")
	}

	var pack strings.Builder
	if _, err := lanemove.Pack(x, lanemove.PackOptions{Host: "srv"}, &pack); err != nil {
		t.Fatal(err)
	}
	old := wtLaneFetch
	t.Cleanup(func() { wtLaneFetch = old })
	var gotHost, gotDir string
	var batch bool
	wtLaneFetch = func(_ context.Context, host, dir string, _ bool, _ []string, b bool, out io.Writer) error {
		gotHost, gotDir, batch = host, dir, b
		_, err := io.WriteString(out, pack.String())
		return err
	}
	m.onWTPullEntered("srv:~/src/app.worktrees/x")
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm("pull")
	next := wtApply(t, m, cmd)
	if gotHost != "srv" || gotDir != "~/src/app.worktrees/x" || !batch {
		t.Errorf("ssh to %q %q batch %v", gotHost, gotDir, batch)
	}
	if got := lanemove.Packs(lanes); len(got) != 1 {
		t.Fatalf("the pulled pack is not kept: %v", got)
	}
	// The follow-up reads the pack off the UI thread, then the unpack
	// card opens.
	wtApply(t, m, next)
	if !m.confirmOpen || m.confirmKind != confirmKindWTUnpack || !strings.Contains(wtFlatBox(m), "From srv:") {
		t.Fatalf("no unpack card after the pull: %v %d", m.confirmOpen, m.confirmKind)
	}
}

// The AI never moves, unpacks or pulls a lane.
func TestWorktreeMoveAIPolicy(t *testing.T) {
	for _, id := range []string{wtMovePaletteID, wtUnpackPaletteID, wtPullPaletteID} {
		p, ok := aiActionPolicyFor(id)
		if !ok || p.Gate != aiGateNever {
			t.Errorf("%s: %+v %v", id, p, ok)
		}
	}
	found := map[string]bool{}
	for _, it := range worktreePaletteItems() {
		found[it.ID] = true
	}
	for _, id := range []string{wtMovePaletteID, wtUnpackPaletteID, wtPullPaletteID} {
		if !found[id] {
			t.Errorf("%s is not in the palette", id)
		}
	}
}
