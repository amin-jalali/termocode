package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agents"
)

// ── WORKTREES: live agent preview (CMP-8) ───────────────────────────────

// wtPreviewFixture is wtFixture with three more agent tabs: a second
// one in wt-sidebar, one in the main checkout and one in the agent lane.
func wtPreviewFixture(t *testing.T) (*wtState, []wtTabAgent) {
	t.Helper()
	st, tabs := wtFixture(t)
	tabs[0].buf = 11
	tabs[1].buf = 12
	tabs = append(tabs,
		wtTabAgent{name: "Codex", cwd: "/src/app.worktrees/wt-sidebar/web", state: agents.Working, buf: 13},
		wtTabAgent{name: "Aider", cwd: "/src/app", state: agents.Done, buf: 14},
		wtTabAgent{name: "Gemini", cwd: "/src/app/.git/termocode/worktrees/r9-a", state: agents.Working, buf: 15, asleep: true},
	)
	return st, tabs
}

func wtPreviewCursor(t *testing.T, st *wtState, tabs []wtTabAgent, path string) ([]wtItem, []wtRow, int) {
	t.Helper()
	st.cursorPath = path
	items := wtItems(st.entries, tabs, st.others, st.root)
	rows := wtRows(items, wtKindAll, "", st.dirty)
	return items, rows, st.cursorIndex(items, rows)
}

func wtPreviewNames(tiles []wtPreviewTile) string {
	var out []string
	for _, tl := range tiles {
		out = append(out, tl.name)
	}
	return strings.Join(out, ",")
}

func TestWTPreviewPickSingleShowsTheCursorAgent(t *testing.T) {
	st, tabs := wtPreviewFixture(t)
	items, rows, cur := wtPreviewCursor(t, st, tabs, "/src/app.worktrees/wt-sidebar")
	tiles := wtPreviewPick(items, rows, cur, tabs, st.entries, false)
	// The waiting agent goes first, as on the card.
	if len(tiles) != 1 || tiles[0].name != "Claude Code" || tiles[0].tab != 0 || tiles[0].buf != 11 || tiles[0].state != wtStateWaiting {
		t.Fatalf("single preview: %+v", tiles)
	}
	if tiles[0].worktree != "feat/wt-sidebar" {
		t.Errorf("tile worktree %q", tiles[0].worktree)
	}
	// The agent of toast runs in another window: nothing to show here.
	items, rows, cur = wtPreviewCursor(t, st, tabs, "/src/app.worktrees/toast")
	if tiles := wtPreviewPick(items, rows, cur, tabs, st.entries, false); len(tiles) != 0 {
		t.Errorf("other window's agent got a tile: %+v", tiles)
	}
	// A plain shell is not an agent.
	shell := []wtTabAgent{{cwd: "/src/app", buf: 3}}
	items, rows, cur = wtPreviewCursor(t, st, shell, "/src/app")
	if tiles := wtPreviewPick(items, rows, cur, shell, st.entries, false); len(tiles) != 0 {
		t.Errorf("a shell got a tile: %+v", tiles)
	}
}

func TestWTPreviewPickGridCursorFirstAtMostFour(t *testing.T) {
	st, tabs := wtPreviewFixture(t)
	items, rows, cur := wtPreviewCursor(t, st, tabs, "/src/app")
	tiles := wtPreviewPick(items, rows, cur, tabs, st.entries, true)
	if got, want := wtPreviewNames(tiles), "Aider,Claude Code,Codex,Gemini"; got != want {
		t.Errorf("grid order: got %s, want %s", got, want)
	}
	if !tiles[3].asleep {
		t.Error("the hibernated tab is not marked asleep")
	}
	more := append([]wtTabAgent{}, tabs...)
	more = append(more, wtTabAgent{name: "Amp", cwd: "/src/app", state: agents.Working, buf: 16})
	items, rows, cur = wtPreviewCursor(t, st, more, "/src/app")
	if tiles := wtPreviewPick(items, rows, cur, more, st.entries, true); len(tiles) != wtPreviewMax {
		t.Errorf("grid holds %d tiles, want %d", len(tiles), wtPreviewMax)
	}
}

func TestWTPreviewTailTrimsBlankRows(t *testing.T) {
	got := wtPreviewTail([]string{"a", "b\tc", "d   ", "", "   ", ""}, 2)
	if strings.Join(got, "|") != "b    c|d" {
		t.Errorf("tail: %q", got)
	}
	if got := wtPreviewTail([]string{"", " "}, 5); len(got) != 0 {
		t.Errorf("blank screen: %q", got)
	}
}

func TestWTPreviewLayout(t *testing.T) {
	for _, c := range []struct {
		n, w, h    int
		cols, rows int
	}{
		{1, 120, 20, 1, 1},
		{2, 120, 20, 2, 1},
		{2, 60, 20, 1, 2},
		{3, 120, 20, 2, 2},
		{4, 120, 20, 2, 2},
		{4, 120, 6, 4, 1},
	} {
		cols, rows := wtPreviewGrid(c.n, c.w, c.h)
		if cols != c.cols || rows != c.rows {
			t.Errorf("%d tiles in %dx%d: %dx%d, want %dx%d", c.n, c.w, c.h, cols, rows, c.cols, c.rows)
		}
	}
}

func TestWTPreviewComposeDrawsEveryTile(t *testing.T) {
	tiles := []wtPreviewTile{
		{tab: 0, buf: 11, name: "Claude Code", worktree: "feat/wt-sidebar", state: wtStateWaiting},
		{tab: 2, buf: 13, name: "Codex", worktree: "feat/wt-sidebar", state: wtStateWorking},
	}
	screens := map[int][]string{
		11: {"old line", "Do you want to proceed?", "❯ 1. Yes"},
		13: {"running tests", ""},
	}
	out := wtPreviewCompose(tiles, screens, 13, 100, 6)
	if len(out) != 6 {
		t.Fatalf("rows: %d", len(out))
	}
	plain := make([]string, len(out))
	for i, l := range out {
		plain[i] = ansi.Strip(l)
		if w := ansi.StringWidth(l); w != 100 {
			t.Errorf("row %d is %d wide: %q", i, w, plain[i])
		}
	}
	all := strings.Join(plain, "\n")
	for _, want := range []string{"Claude Code", "Codex", "waiting", "Do you want to proceed?", "❯ 1. Yes", "running tests", "│"} {
		if !strings.Contains(all, want) {
			t.Errorf("preview misses %q:\n%s", want, all)
		}
	}
	// The selected tile (Codex) carries the mark.
	if !strings.Contains(plain[0], "▸ Codex") && !strings.Contains(plain[0], "> Codex") {
		t.Errorf("selected tile not marked: %q", plain[0])
	}
	// A screen not read yet says so.
	out = wtPreviewCompose(tiles[:1], map[int][]string{}, 0, 60, 4)
	if !strings.Contains(ansi.Strip(strings.Join(out, "\n")), "Reading the screen") {
		t.Errorf("unread screen: %q", out)
	}
}

func TestWTPreviewTileAt(t *testing.T) {
	// 4 tiles in 101x10: 2 cols of 50 (+1 rule), 2 rows of 5.
	if i := wtPreviewTileAt(4, 101, 10, 0, 0); i != 0 {
		t.Errorf("top left: %d", i)
	}
	if i := wtPreviewTileAt(4, 101, 10, 0, 60); i != 1 {
		t.Errorf("top right: %d", i)
	}
	if i := wtPreviewTileAt(4, 101, 10, 7, 10); i != 2 {
		t.Errorf("bottom left: %d", i)
	}
	if i := wtPreviewTileAt(3, 101, 10, 7, 60); i != -1 {
		t.Errorf("empty slot: %d", i)
	}
}

func TestWTPreviewPaletteAndKeys(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range worktreePaletteItems() {
		ids[it.ID] = true
	}
	for _, id := range []string{"worktree-preview", "worktree-preview-grid"} {
		if !ids[id] {
			t.Errorf("palette misses %s", id)
		}
		if p, ok := aiActionPolicies[id]; !ok || p.Gate != aiGateAuto {
			t.Errorf("%s: AI policy %+v", id, p)
		}
		m := Model{wt: newWTState()}
		if _, handled := m.dispatchWorktreePalette(id); !handled {
			t.Errorf("%s is not dispatched", id)
		}
	}
	keys := map[string]bool{}
	for _, r := range worktreeKeyRows() {
		keys[r.Key] = true
	}
	if !keys["v"] || !keys["V"] {
		t.Errorf("key card misses v / V: %v", keys)
	}
}

func TestWTPreviewNoAgentHint(t *testing.T) {
	st, tabs := wtPreviewFixture(t)
	items, rows, cur := wtPreviewCursor(t, st, tabs, "/src/app.worktrees/toast")
	if h := wtPreviewEmptyHint(items, rows, cur, false); !strings.Contains(h, "another window") {
		t.Errorf("toast hint: %q", h)
	}
	items, rows, cur = wtPreviewCursor(t, st, tabs, "/elsewhere/api-v2")
	if h := wtPreviewEmptyHint(items, rows, cur, false); !strings.Contains(h, "No agent runs in api-v2") {
		t.Errorf("idle hint: %q", h)
	}
	if h := wtPreviewEmptyHint(items, rows, cur, true); !strings.Contains(h, "No agent tab") {
		t.Errorf("grid hint: %q", h)
	}
}
