package app

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/migrate"
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
