package app

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/migrate"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The setup card's Import row (EM-5): the probe that finds another
// editor runs once, off the UI goroutine, and drawing never reads files.

// setupImportModel is a Model with the setup card on and no real font
// probe. The caller made the sandbox (importSandbox) first.
func setupImportModel(t *testing.T) Model {
	t.Helper()
	old := setupCardOff
	setupCardOff = false
	t.Cleanup(func() { setupCardOff = old })
	t.Cleanup(SetSetupProbe(func(bool) SetupProbe { return SetupProbe{} }))
	return Model{w: 120, h: 40, toast: toast.New(), themeID: "vscode-dark-plus", setup: newSetupState()}
}

// setupRunCmd runs cmd (and the commands of a batch) and returns the messages.
func setupRunCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, setupRunCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// feedAll hands every message to m.Update.
func feedAll(m Model, msgs []tea.Msg) Model {
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

// countDetect replaces the editor detection with a counter around the
// real one.
func countDetect(t *testing.T, fn func() []migrate.Source) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	setupSeams.Lock()
	old := setupSeams.detect
	setupSeams.detect = func() []migrate.Source { n.Add(1); return fn() }
	setupSeams.Unlock()
	t.Cleanup(func() { setupSeams.Lock(); setupSeams.detect = old; setupSeams.Unlock() })
	return &n
}

func TestSetupImportProbeFindsEditorOnce(t *testing.T) {
	importSandbox(t, "vscode")
	n := countDetect(t, importDetect)
	m := setupImportModel(t)
	m = feedAll(m, setupRunCmd(m.setupInitCmd()))
	if n.Load() != 1 {
		t.Fatalf("probe ran %d times, want 1", n.Load())
	}
	if len(m.setup.importFound) != 1 || m.setup.importFound[0].title != "VS Code" || m.setup.importFound[0].parts == "" {
		t.Fatalf("probe result %+v probed=%v", m.setup.importFound, m.setup.importProbed)
	}
	// The cache answers the next time the card is shown and when Init runs again.
	m = feedAll(m, setupRunCmd(m.showSetupCard()))
	setupRunCmd(m.setupInitCmd())
	if n.Load() != 1 {
		t.Fatalf("probe ran again: %d", n.Load())
	}
}

func TestSetupImportProbeNothingFound(t *testing.T) {
	importSandbox(t)
	m := setupImportModel(t)
	m = feedAll(m, setupRunCmd(m.setupInitCmd()))
	if !m.setup.importProbed || len(m.setup.importFound) != 0 {
		t.Fatalf("probed=%v found=%+v", m.setup.importProbed, m.setup.importFound)
	}
}

func TestSetupImportProbeTimeoutMeansNone(t *testing.T) {
	importSandbox(t, "vscode")
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	countDetect(t, func() []migrate.Source { <-release; return importDetect() })
	old := setupImportTimeout
	setupImportTimeout = 30 * time.Millisecond
	t.Cleanup(func() { setupImportTimeout = old })
	m := setupImportModel(t)
	start := time.Now()
	m = feedAll(m, setupRunCmd(m.setupInitCmd()))
	if time.Since(start) > 5*time.Second {
		t.Fatal("the probe waited for a stuck detection")
	}
	if !m.setup.importProbed || len(m.setup.importFound) != 0 {
		t.Fatalf("probed=%v found=%+v", m.setup.importProbed, m.setup.importFound)
	}
}

func TestSetupImportNotProbedWhileDrawing(t *testing.T) {
	importSandbox(t, "vscode")
	n := countDetect(t, importDetect)
	m := setupImportModel(t)
	for i := 0; i < 3; i++ {
		m.publishSetupCard()
		m.setupCardView()
		_ = m.View()
	}
	if n.Load() != 0 {
		t.Fatalf("drawing ran the editor probe %d times", n.Load())
	}
	if strings.Contains(strings.Join(setupStepIDs(m.setupCardView()), ","), "import") {
		t.Fatal("import row before the probe finished")
	}
}

func setupStepIDs(v setupCardView) []string {
	var ids []string
	for _, s := range v.steps {
		ids = append(ids, s.id)
	}
	return ids
}

// ─── The row ───────────────────────────────────────────────────────────

func importFacts(found ...setupEditor) setupFacts {
	f := baseFacts(kittyInfo)
	f.importFound = found
	return f
}

var vsCode = setupEditor{title: "VS Code", parts: "settings, 23 keys, theme"}

func TestSetupImportRowHiddenWithoutEditor(t *testing.T) {
	steps := buildSetupSteps(importFacts())
	for _, s := range steps {
		if s.id == "import" {
			t.Fatalf("import row with no editor: %+v", s)
		}
	}
	for _, line := range setupText(t, importFacts(), 68) {
		if strings.Contains(line, "Import") {
			t.Errorf("card mentions Import with no editor: %q", line)
		}
	}
}

func TestSetupImportRowShown(t *testing.T) {
	steps := buildSetupSteps(importFacts(vsCode))
	if steps[0].id != "icons" || steps[1].id != "import" {
		t.Fatalf("order: %v", setupStepIDs(setupCardFromFacts(importFacts(vsCode))))
	}
	s := steps[1]
	if !s.optional || s.done || s.action != "Import…" {
		t.Fatalf("row %+v", s)
	}
	if s.status != "Settings found in VS Code · settings, 23 keys, theme" {
		t.Errorf("status %q", s.status)
	}
	if len(s.buttons) != 1 || s.buttons[0].id != "import-open" {
		t.Errorf("buttons %+v", s.buttons)
	}
	got := setupText(t, importFacts(vsCode), 68)
	row := importRowAt(got)
	if row < 0 || !strings.Contains(got[row], "VS Code") || !strings.HasSuffix(got[row], "Import…") {
		t.Errorf("drawn row:\n%s", strings.Join(got, "\n"))
	}
	// The statuses of all rows stay in one column.
	if i, j := strings.Index(got[row], "Settings"), strings.Index(got[row+1], "Not set up"); i != j {
		t.Errorf("status columns differ (%d, %d):\n%s", i, j, strings.Join(got, "\n"))
	}
	// Optional: it never blocks "SET UP · done".
	f := importFacts(vsCode)
	f.settled["ai"], f.iconMode = true, theme.IconModeNerdFont
	if !setupCardFromFacts(f).allDone {
		t.Error("the Import row holds back done")
	}
}

// importRowAt is the line of the Import row, -1 if none.
func importRowAt(lines []string) int {
	for i, l := range lines {
		if strings.HasPrefix(l, "   ○ Import") {
			return i
		}
	}
	return -1
}

func TestSetupImportRowSeveralEditors(t *testing.T) {
	s := stepByID(t, buildSetupSteps(importFacts(vsCode, setupEditor{title: "Zed", parts: "keys"}, setupEditor{title: "Helix", parts: "keys"})), "import")
	if s.status != "Settings found in VS Code and 2 more" {
		t.Errorf("status %q", s.status)
	}
}

func TestSetupImportRowNarrow(t *testing.T) {
	for _, w := range []int{30, 40, 50, 60, 68, 100} {
		got := setupText(t, importFacts(vsCode), w) // fails if a line is not w cells wide
		joined := strings.Join(got, "\n")
		if !strings.Contains(joined, "Import…") || (w >= 40 && !strings.Contains(joined, "VS Code")) {
			t.Errorf("w=%d: row lost its text:\n%s", w, joined)
		}
	}
	got := setupText(t, importFacts(vsCode), 40)
	row := importRowAt(got)
	if row < 0 || !strings.HasSuffix(got[row], "Import…") || !strings.HasPrefix(got[row+1], "     Settings found in VS Code") {
		t.Errorf("narrow row:\n%s", strings.Join(got, "\n"))
	}
}

func TestSetupImportRowNamesNoOtherProduct(t *testing.T) {
	// Only the detected editor's own name is data; the card text adds none.
	f := importFacts(setupEditor{title: "Zed", parts: "keys"})
	all := strings.Join(setupText(t, f, 100), "\n")
	for _, p := range []string{"VS Code", "Vim", "JetBrains", "Sublime", "Helix", "Cursor"} {
		if strings.Contains(all, p) {
			t.Errorf("card names %q for a Zed source", p)
		}
	}
}

func TestSetupImportButtonOpensThePicker(t *testing.T) {
	importSandbox(t, "vscode", "jetbrains")
	m := setupImportModel(t)
	m = feedAll(m, setupRunCmd(m.setupInitCmd()))
	var ids []string
	for _, b := range m.SetupCardButtons() {
		ids = append(ids, b.ID)
	}
	if !strings.Contains(strings.Join(ids, ","), "import-open") {
		t.Fatalf("no import button: %v", ids)
	}
	// The row itself (one button) runs it.
	m.pressSetupRow("import")
	if !m.pickerOpen || m.pickerKind != pickerKindImportSource {
		t.Fatalf("picker open=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
	var titles []string
	for _, it := range pickerItems(t, &m) {
		titles = append(titles, it.Title)
	}
	if got := strings.Join(titles, ","); !strings.Contains(got, "VS Code") || !strings.Contains(got, "GoLand 2025.1") {
		t.Errorf("picker rows %v", titles)
	}
}

func TestSetupImportRowHideSetup(t *testing.T) {
	importSandbox(t, "vscode")
	m := setupImportModel(t)
	m = feedAll(m, setupRunCmd(m.setupInitCmd()))
	if !m.setupCardView().show || len(m.setupCardView().steps) < 3 {
		t.Fatal("card/row not shown")
	}
	m.pressSetupRow("hide")
	if m.setupCardView().show {
		t.Fatal("Hide setup left the card (and the row) visible")
	}
}

// ─── Done and undo ─────────────────────────────────────────────────────

func TestSetupImportRowDoneAndUndoOffer(t *testing.T) {
	f := importFacts(vsCode)
	f.importedFrom = "VS Code"
	s := stepByID(t, buildSetupSteps(f), "import")
	if !s.done || s.status != "Imported from VS Code" || s.action != "Undo…" {
		t.Fatalf("done row %+v", s)
	}
	if len(s.buttons) != 1 || s.buttons[0].id != "import-undo" {
		t.Errorf("buttons %+v", s.buttons)
	}
	got := setupText(t, f, 68)
	if row := strings.Index(strings.Join(got, "\n"), "✓ Import  Imported from VS Code"); row < 0 || !strings.HasSuffix(got[2], "Undo…") {
		t.Errorf("drawn:\n%s", strings.Join(got, "\n"))
	}
	for _, w := range []int{30, 40, 50} {
		setupText(t, f, w) // every line exactly w cells
	}
}

func TestSetupImportRowFollowsImportAndUndo(t *testing.T) {
	importSandbox(t, "vscode")
	t.Cleanup(func() { theme.ApplyTheme("vscode-dark-plus") })
	old := setupCardOff
	setupCardOff = false
	t.Cleanup(func() { setupCardOff = old })
	t.Cleanup(SetSetupProbe(func(bool) SetupProbe { return SetupProbe{} }))

	m := importReviewAndConfirm(t)
	m.setup = newSetupState()
	m = feedAll(m, setupRunCmd(m.setupInitCmd()))
	if s := stepByID(t, m.setupCardView().steps, "import"); s.done || s.action != "Import…" {
		t.Fatalf("before the import: %+v", s)
	}

	m = importConfirm(t, m, "import")
	s := stepByID(t, m.setupCardView().steps, "import")
	if !s.done || s.status != "Imported from VS Code" || s.action != "Undo…" {
		t.Fatalf("after the import: %+v\n%s", s, importToast(m))
	}

	// Cancel keeps it done; Undo asks first.
	m.pressSetupRow("import")
	if !m.confirmOpen || m.confirmKind != confirmKindImportUndo {
		t.Fatal("the row's Undo… did not ask")
	}
	m = importConfirm(t, m, "cancel")
	if s := stepByID(t, m.setupCardView().steps, "import"); !s.done {
		t.Fatal("cancelled undo changed the row")
	}
	m.pressSetupRow("import")
	m = importConfirm(t, m, "undo")
	s = stepByID(t, m.setupCardView().steps, "import")
	if s.done || s.action != "Import…" || !strings.HasPrefix(s.status, "Settings found in VS Code") {
		t.Fatalf("after the undo: %+v", s)
	}
}

func TestSetupImportFailedImportKeepsTheOffer(t *testing.T) {
	importSandbox(t, "vscode")
	old := setupCardOff
	setupCardOff = false
	t.Cleanup(func() { setupCardOff = old })
	t.Cleanup(SetSetupProbe(func(bool) SetupProbe { return SetupProbe{} }))
	m := importModel(150, 42)
	m.setup = newSetupState()
	m.setup.importProbed, m.setup.importFound = true, []setupEditor{vsCode}
	m.importOpen = false
	m.applyImport() // no panel: nothing happens
	if s := stepByID(t, m.setupCardView().steps, "import"); s.done {
		t.Fatal("row done without an import")
	}
}

// The screenshot hook (import_demo.go) writes the sample editors and makes
// the card look again, so its Import row shows.
func TestImportDemoSetupShowsTheImportRow(t *testing.T) {
	importSandbox(t)
	m := setupImportModel(t)
	m.setupS().importProbed = true // the first probe found nothing
	cmd := m.importDemo("setup")
	m = feedAll(m, setupRunCmd(cmd))
	var row *setupStep
	for _, s := range buildSetupSteps(m.setupFacts()) {
		if s.id == "import" {
			s := s
			row = &s
		}
	}
	if row == nil || !strings.Contains(row.status, "Settings found in VS Code") {
		t.Fatalf("no Import row after the demo hook: %+v", row)
	}
}
