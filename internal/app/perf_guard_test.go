package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Performance guards (Group P3). They count allocations per operation
// (testing.AllocsPerRun), not time, so they do not depend on the
// machine: a change that makes a hot path allocate much more fails
// `go test ./...`. The limits are ~30% above the counts measured when
// they were set; lower them when a change makes a path cheaper.
//
// The matching benchmarks (go test -bench Frame -benchmem ./internal/app)
// show time and bytes per operation.

const (
	maxAllocsEditorFrame   = 440 // a cursor move: nvim scrolls + redraws a line, then View (measured 333)
	maxAllocsAIPanelFrame  = 560 // the same with the AI chat showing a long answer (measured 428)
	maxAllocsExplorerView  = 2   // explorer View with nothing changed: a cache hit (measured 0)
	maxAllocsNormalizeRows = 4   // normalizeFrameLines on a full, already-normal frame (measured 2)
)

// perfWorkspace is a small project to show in the explorer.
func perfWorkspace(t testing.TB) string {
	dir := t.TempDir()
	for i := 0; i < 12; i++ {
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%02d.go", i)), []byte("package x\n"), 0o644)
	}
	_ = os.MkdirAll(filepath.Join(dir, "pkg", "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "pkg", "a.go"), []byte("package pkg\n"), 0o644)
	return dir
}

// perfModel is a 240×62 window with an open Go file in the editor (a
// grid filled by redraw events, as nvim sends them) and the explorer.
func perfModel(t testing.TB, chat bool) Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY", "TERMOCODE_AI_PROVIDER", "TERMOCODE_AI_API_KEY", "TERMOCODE_AI_MODEL", "TERMOCODE_AI_BASE_URL", "OLLAMA_HOST"} {
		t.Setenv(k, "")
	}
	t.Chdir(perfWorkspace(t))
	m0, _ := newTestModel(t).Update(tea.WindowSizeMsg{Width: 240, Height: 62})
	m := m0.(Model)
	if chat {
		m.ai = newAIState()
		m.ai.provider = fakeAIProvider{}
		answer := strings.Repeat("## Part\n\nSome **bold** text with `code` and a list:\n\n1. first item that is long enough to wrap in the panel\n2. second\n\n```go\nfunc x() {\n\treturn y(1, 2)\n}\n```\n\n", 12)
		m.ai.conv.Messages = []ai.StoredMessage{
			{Role: ai.StoredUser, Text: "Explain this file"},
			{Role: ai.StoredAssistant, Text: answer},
		}
		m.actionsOpen, m.actionsPinned = true, true
		m.applyLayout()
	}
	path, _ := filepath.Abs("file00.go")
	m.editor.SetMeta(path, false, "go")
	m.bufs = []nvim.BufferInfo{{ID: 1, Path: path}, {ID: 2, Path: path + ".2"}}
	m.tabs.SetBuffers(m.bufs, 1)
	pane := m.editorPaneLayout()
	events := [][]any{
		{"grid_resize", []any{1, pane.W, pane.H}},
		{"default_colors_set", []any{0xd4d4d4, 0x1e1e1e, 0xff0000, 0, 0}},
		{"hl_attr_define", []any{1, map[string]any{"foreground": 0x569cd6}, map[string]any{}, []any{}}},
		{"hl_attr_define", []any{2, map[string]any{"foreground": 0xce9178}, map[string]any{}, []any{}}},
	}
	for y := 0; y < pane.H; y++ {
		events = append(events, []any{"grid_line", []any{1, y, 0, perfLine(y)}})
	}
	events = append(events, []any{"flush"})
	m, _, _ = m.handleNvimMsg(nvim.RedrawMsg{Events: events})
	return m
}

// perfLine is one grid_line's cells (one per character, as nvim sends
// them): a code-looking line in three colours.
func perfLine(y int) []any {
	var cells []any
	add := func(text string, hl int) {
		for i, r := range text {
			if i == 0 {
				cells = append(cells, []any{string(r), hl})
			} else {
				cells = append(cells, []any{string(r)})
			}
		}
	}
	add(fmt.Sprintf("%4d ", y+1), 0)
	add("func", 1)
	add(" ", 0)
	add(fmt.Sprintf("name%d(x int) string { return \"value %d\" }", y, y), 2)
	return cells
}

// cursorMove is what nvim sends for one ↓ in a scrolling buffer: the
// grid scrolls one row, the new last line is drawn, the cursor moves.
func cursorMove(m Model, i int) Model {
	pane := m.editorPaneLayout()
	m, _, _ = m.handleNvimMsg(nvim.RedrawMsg{Events: [][]any{
		{"grid_scroll", []any{1, 0, pane.H, 0, pane.W, 1, 0}},
		{"grid_line", []any{1, pane.H - 1, 0, perfLine(i)}},
		{"grid_cursor_goto", []any{1, pane.H / 2, 6}},
		{"flush"},
	}})
	return m
}

func perfColour(t testing.TB) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev); theme.ApplyTheme("vscode-dark-plus") })
}

func frameAllocs(t *testing.T, chat bool) float64 {
	perfColour(t)
	m := perfModel(t, chat)
	if !strings.Contains(m.View(), "name") {
		t.Fatal("editor grid not shown")
	}
	i := 0
	return testing.AllocsPerRun(50, func() {
		i++
		m = cursorMove(m, i)
		_ = m.View()
	})
}

func TestPerfGuardEditorFrame(t *testing.T) {
	if n := frameAllocs(t, false); n > maxAllocsEditorFrame {
		t.Errorf("editor frame: %.0f allocs per cursor move, limit %d", n, maxAllocsEditorFrame)
	} else {
		t.Logf("editor frame: %.0f allocs (limit %d)", n, maxAllocsEditorFrame)
	}
}

func TestPerfGuardAIPanelFrame(t *testing.T) {
	if n := frameAllocs(t, true); n > maxAllocsAIPanelFrame {
		t.Errorf("AI panel frame: %.0f allocs per cursor move, limit %d", n, maxAllocsAIPanelFrame)
	} else {
		t.Logf("AI panel frame: %.0f allocs (limit %d)", n, maxAllocsAIPanelFrame)
	}
}

func TestPerfGuardExplorerViewUnchanged(t *testing.T) {
	perfColour(t)
	dir := perfWorkspace(t)
	t.Chdir(dir)
	ex := explorer.New()
	ex.SetSize(40, 60)
	gs := explorer.GitStatus{filepath.Join(dir, "file01.go"): "M"}
	first := ex.View(true, "", false, gs)
	n := testing.AllocsPerRun(100, func() {
		if ex.View(true, "", false, gs) != first {
			t.Fatal("explorer View changed")
		}
	})
	if n > maxAllocsExplorerView {
		t.Errorf("explorer View (unchanged): %.0f allocs, limit %d", n, maxAllocsExplorerView)
	} else {
		t.Logf("explorer View (unchanged): %.0f allocs (limit %d)", n, maxAllocsExplorerView)
	}
}

func TestPerfGuardHiddenContainersDoNotScheduleEngineWork(t *testing.T) {
	st := newServicesState()
	ctx, cancel := context.WithCancel(context.Background())
	st.visible = true
	st.containerCancel = cancel
	st.containerCancels["containers"] = cancel
	m := Model{services: st, showExp: true}
	m.activity.SetActive(activity.ViewFiles)
	if cmd := m.servicesSyncWatch(); cmd != nil {
		t.Fatal("hiding Containers view scheduled work")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("hiding the view did not cancel its engine work")
	}
	if cmd := m.containersDetect(); cmd != nil {
		t.Fatal("hidden Containers view scheduled engine detection")
	}
	if cmd := m.containersRefresh("containers"); cmd != nil {
		t.Fatal("hidden Containers view scheduled a section query")
	}
	if st.containerDetecting || len(st.containerCancels) != 0 {
		t.Fatalf("hidden view retained work: detecting=%v cancels=%d", st.containerDetecting, len(st.containerCancels))
	}
}

func TestPerfGuardNormalizeFrame(t *testing.T) {
	perfColour(t)
	m := perfModel(t, false)
	frame := strings.Split(m.renderBase(), "\n")
	n := testing.AllocsPerRun(100, func() {
		lines := append([]string(nil), frame...)
		_ = m.normalizeFrameLines(lines)
	})
	if n > maxAllocsNormalizeRows {
		t.Errorf("normalizeFrameLines: %.0f allocs, limit %d", n, maxAllocsNormalizeRows)
	} else {
		t.Logf("normalizeFrameLines: %.0f allocs (limit %d)", n, maxAllocsNormalizeRows)
	}
}

// maxRetainedHeap bounds what a session keeps on the heap after a
// scripted scenario (frames, a long chat, chats switched, the panel
// closed): it catches a cache that grows without bound or keeps closed
// chats. Generous on purpose (measured 0.2 MB): only leaks fail it.
const maxRetainedHeap = 16 << 20

func liveHeap() uint64 {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

func TestPerfGuardLiveHeap(t *testing.T) {
	perfColour(t)
	before := liveHeap()
	m := perfModel(t, true)
	answer := m.ai.conv.Messages[1].Text
	for i := 0; i < 400; i++ {
		m = cursorMove(m, i)
		if i%40 == 0 {
			// A new chat with a different long answer (the old one must
			// not stay cached).
			m.ai.conv.Messages = []ai.StoredMessage{
				{Role: ai.StoredUser, Text: fmt.Sprintf("question %d", i)},
				{Role: ai.StoredAssistant, Text: fmt.Sprintf("answer %d\n\n", i) + answer},
			}
		}
		if i%7 == 0 {
			m.hoverX, m.hoverY = i%240, i%62
		}
		_ = m.View()
	}
	m.actionsOpen = false
	m.applyLayout()
	_ = m.View()
	after := liveHeap()
	runtime.KeepAlive(m)
	grew := int64(after) - int64(before)
	if grew > maxRetainedHeap {
		t.Errorf("live heap grew by %.1f MB over the scenario, limit %d MB", float64(grew)/(1<<20), maxRetainedHeap>>20)
	} else {
		t.Logf("live heap grew by %.1f MB (limit %d MB)", float64(grew)/(1<<20), maxRetainedHeap>>20)
	}
	if m.ai.transcript.ok || len(m.ai.mdCache) != 0 {
		t.Error("a closed chat panel still holds its drawn transcript")
	}
}

func benchFrame(b *testing.B, chat bool) {
	perfColour(b)
	m := perfModel(b, chat)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m = cursorMove(m, i)
		_ = m.View()
	}
}

func BenchmarkEditorFrame(b *testing.B)  { benchFrame(b, false) }
func BenchmarkAIPanelFrame(b *testing.B) { benchFrame(b, true) }
