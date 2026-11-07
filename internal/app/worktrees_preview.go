package app

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── WORKTREES view: live agent preview and grid (CMP-8) ─────────────────
//
// v in the WORKTREES view opens a PREVIEW tab in the bottom panel: the
// live screen of the agent that works in the selected worktree, read
// only. The list keeps the focus, so j / k move and the preview follows.
// V shows a grid instead: up to 4 agent screens of this window side by
// side, the selected worktree's first. In the panel, Tab / arrows pick a
// tile, Enter (or a click) jumps into that agent's terminal tab, g
// switches between one screen and the grid. v or V again closes it.
//
// The screens are the agents' terminal buffers, read from nvim twice a
// second while the tab is on show (never while it is hidden). Nothing is
// sent to the agents and nothing leaves the window.

const (
	wtPreviewPaletteID     = "worktree-preview"
	wtPreviewGridPaletteID = "worktree-preview-grid"

	// panelKindAgentPreview: its own number, far from the iota list in
	// panel.go, so other kinds can be added there freely.
	panelKindAgentPreview panelKind = 30

	wtPreviewMax  = 4                      // tiles in the grid
	wtPreviewPoll = 500 * time.Millisecond // how often the screens are read
	wtPreviewRows = 120                    // screen rows kept per agent
)

// wtPreviewState is the open preview (nil: closed).
type wtPreviewState struct {
	gen     int
	grid    bool
	sel     int              // the selected tile's buffer (0: the first tile)
	screens map[int][]string // the last screen read, by buffer
	err     string           // why the screens could not be read
}

var wtPreviewGen int

// wtPreviewTile is one agent screen of the preview.
type wtPreviewTile struct {
	tab      int    // index into this window's terminal tabs
	buf      int    // its nvim buffer
	name     string // the agent
	worktree string // the title of the worktree it works in
	state    string // working | waiting | done
	asleep   bool   // the tab is hibernated
}

// wtTabState is an agent tab's state as a card word ("" when no agent runs).
func wtTabState(s agents.State) string {
	switch s {
	case agents.Working:
		return wtStateWorking
	case agents.Waiting:
		return wtStateWaiting
	case agents.Done:
		return wtStateDone
	}
	return ""
}

// wtPreviewPick is what the preview shows: the cursor worktree's agent
// tab (the most urgent one, as on its card) or, in the grid, up to
// wtPreviewMax agent tabs of this window: the cursor's first, then in
// list order. Agents of other windows have no screen here.
func wtPreviewPick(items []wtItem, rows []wtRow, cur int, tabs []wtTabAgent, entries []worktrees.Entry, grid bool) []wtPreviewTile {
	byPath := map[string][]wtPreviewTile{}
	for i, t := range tabs {
		state := wtTabState(t.state)
		if t.name == "" || state == "" {
			continue
		}
		if p := wtOwner(entries, t.cwd); p != "" {
			byPath[p] = append(byPath[p], wtPreviewTile{tab: i, buf: t.buf, name: t.name, state: state, asleep: t.asleep})
		}
	}
	of := func(row int) []wtPreviewTile {
		if row < 0 || row >= len(rows) || rows[row].item < 0 {
			return nil
		}
		it := items[rows[row].item]
		ts := append([]wtPreviewTile(nil), byPath[it.e.Path]...)
		sort.SliceStable(ts, func(i, j int) bool { return wtStateRank(ts[i].state) < wtStateRank(ts[j].state) })
		for i := range ts {
			ts[i].worktree = wtTitle(it)
		}
		return ts
	}
	out := of(cur)
	if !grid {
		if len(out) > 1 {
			out = out[:1]
		}
		return out
	}
	for i := range rows {
		if i != cur {
			out = append(out, of(i)...)
		}
	}
	if len(out) > wtPreviewMax {
		out = out[:wtPreviewMax]
	}
	return out
}

// wtPreviewEmptyHint says why there is no screen to show.
func wtPreviewEmptyHint(items []wtItem, rows []wtRow, cur int, grid bool) string {
	if grid {
		return "No agent tab runs in this window. n in WORKTREES makes a worktree with an agent."
	}
	if cur < 0 || cur >= len(rows) || rows[cur].item < 0 {
		return "No worktree is selected."
	}
	it := items[rows[cur].item]
	if it.agents > 0 {
		return "The agent of " + wtTitle(it) + " runs in another window · Ctrl+Shift+J jumps to it"
	}
	return "No agent runs in " + wtTitle(it) + " · V shows every agent of this window"
}

// wtPreviewTail is the last n rows of a screen: tabs as spaces, control
// characters dropped, the blank rows under the prompt cut.
func wtPreviewTail(lines []string, n int) []string {
	clean := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.ReplaceAll(l, "\t", "    ")
		l = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, l)
		clean = append(clean, strings.TrimRight(l, " "))
	}
	for len(clean) > 0 && clean[len(clean)-1] == "" {
		clean = clean[:len(clean)-1]
	}
	if len(clean) > n {
		clean = clean[len(clean)-n:]
	}
	return clean
}

// wtPreviewGrid is how n tiles sit in w×h: side by side when there is
// room, 2×2 for 3 or 4, one row when the panel is short.
func wtPreviewGrid(n, w, h int) (cols, rows int) {
	switch {
	case n <= 1:
		return 1, 1
	case n == 2 && w >= 80:
		return 2, 1
	case n == 2:
		return 1, 2
	case h < 8:
		return n, 1
	}
	return 2, 2
}

// wtPreviewCells are the tile sizes: the last column and row take the rest.
func wtPreviewCells(n, w, h int) (cols, rows, colW, rowH int) {
	cols, rows = wtPreviewGrid(n, w, h)
	colW = max((w-(cols-1))/cols, 1)
	rowH = max(h/rows, 1)
	return cols, rows, colW, rowH
}

// wtPreviewTileAt is the tile under a panel cell (-1: none).
func wtPreviewTileAt(n, w, h, row, col int) int {
	if n <= 0 || row < 0 || col < 0 || row >= h || col >= w {
		return -1
	}
	n = min(n, wtPreviewMax)
	cols, rows, colW, rowH := wtPreviewCells(n, w, h)
	c := min(col/(colW+1), cols-1)
	r := min(row/rowH, rows-1)
	if i := r*cols + c; i < n {
		return i
	}
	return -1
}

// wtPreviewSelIndex is the selected tile (the first when sel is gone).
func wtPreviewSelIndex(tiles []wtPreviewTile, sel int) int {
	for i, t := range tiles {
		if t.buf == sel && sel != 0 {
			return i
		}
	}
	return 0
}

// wtPreviewHeader is a tile's top row: mark, agent · worktree, state.
func wtPreviewHeader(t wtPreviewTile, sel bool, w int) string {
	lead := "  "
	if sel {
		lead = "▸ "
		if theme.CurrentIconMode == theme.IconModeASCII {
			lead = "> "
		}
	}
	g, gc := wtStateGlyph(t.state)
	right := g.String() + " " + t.state
	if t.asleep {
		right, gc = theme.GlyphAsleep.String()+" asleep", theme.TextDim
	}
	left := lead + t.name + theme.ASCIIText(" · ") + t.worktree
	room := w - runewidth.StringWidth(right) - 2
	if room < 8 {
		right, room = "", w-1
	}
	left = runewidth.Truncate(left, max(room, 1), "…")
	pad := max(w-runewidth.StringWidth(left)-runewidth.StringWidth(right)-1, 1)
	fg := theme.TextSecondary
	if sel {
		fg = theme.AIAccent
	}
	bg := theme.Bg(theme.BgPanel)
	row := theme.FgBg(fg, theme.BgPanel).Bold(sel).Render(left) + bg.Render(strings.Repeat(" ", pad))
	if right != "" {
		row += theme.FgBg(gc, theme.BgPanel).Render(right)
	}
	return padTabBarToWidth(row, w, bg)
}

// wtPreviewTileLines is one tile: the header, then the end of the screen.
func wtPreviewTileLines(t wtPreviewTile, screen []string, read, sel bool, w, h int) []string {
	lines := []string{wtPreviewHeader(t, sel, w)}
	body := h - 1
	text := theme.FgBg(theme.TextPrimary, theme.BgEditor)
	muted := theme.FgBg(theme.TextMuted, theme.BgEditor)
	switch {
	case body <= 0:
	case t.asleep:
		lines = append(lines, muted.Render(" asleep · Enter wakes it"))
	case !read:
		lines = append(lines, muted.Render(" Reading the screen…"))
	case len(screen) == 0:
		lines = append(lines, muted.Render(" (blank screen)"))
	default:
		if len(screen) > body {
			screen = screen[len(screen)-body:]
		}
		for _, l := range screen {
			lines = append(lines, text.Render(" "+runewidth.Truncate(l, max(w-1, 1), "…")))
		}
	}
	return lines
}

// wtPreviewCompose draws the tiles into exactly h rows of w cells.
func wtPreviewCompose(tiles []wtPreviewTile, screens map[int][]string, selBuf, w, h int) []string {
	if len(tiles) == 0 || w <= 0 || h <= 0 {
		return nil
	}
	n := min(len(tiles), wtPreviewMax)
	cols, rows, colW, rowH := wtPreviewCells(n, w, h)
	sel := wtPreviewSelIndex(tiles, selBuf)
	bodyBg := theme.Bg(theme.BgEditor)
	sep := theme.FgBg(theme.TextDim, theme.BgEditor).Render("│")
	out := make([]string, 0, h)
	for r := 0; r < rows; r++ {
		th := rowH
		if r == rows-1 {
			th = h - rowH*(rows-1)
		}
		cells := make([][]string, cols)
		widths := make([]int, cols)
		for c := 0; c < cols; c++ {
			tw := colW
			if c == cols-1 {
				tw = max(w-colW*(cols-1)-(cols-1), 1)
			}
			widths[c] = tw
			if i := r*cols + c; i < n {
				scr, read := screens[tiles[i].buf]
				cells[c] = wtPreviewTileLines(tiles[i], scr, read, i == sel, tw, th)
			}
		}
		for y := 0; y < th; y++ {
			var b strings.Builder
			for c := 0; c < cols; c++ {
				if c > 0 {
					b.WriteString(sep)
				}
				line := ""
				if y < len(cells[c]) {
					line = cells[c][y]
				}
				b.WriteString(padTabBarToWidth(line, widths[c], bodyBg))
			}
			out = append(out, b.String())
		}
	}
	return out
}

// ── Model side ──────────────────────────────────────────────────────────

func (m *Model) wtPreviewOn() *wtPreviewState {
	if m.wt == nil {
		return nil
	}
	return m.wt.preview
}

// wtPreviewTiles are the tiles to show now, or why there are none.
func (m *Model) wtPreviewTiles() ([]wtPreviewTile, string) {
	st := m.wt
	p := st.preview
	if !st.loaded {
		return nil, "Reading the worktrees…"
	}
	items, rows := m.wtList()
	cur := st.cursorIndex(items, rows)
	grid := p != nil && p.grid
	tiles := wtPreviewPick(items, rows, cur, m.wtTabs(), st.entries, grid)
	if len(tiles) == 0 {
		return nil, wtPreviewEmptyHint(items, rows, cur, grid)
	}
	return tiles, ""
}

// worktreePreview opens the preview (grid: the agent grid), or closes it
// when it already shows that way (v / V, Worktree: Preview Agent …).
func (m *Model) worktreePreview(grid bool) tea.Cmd {
	st := m.wt
	if st == nil {
		return nil
	}
	if p := st.preview; p != nil && p.grid == grid && m.termOpen && m.panelActive == panelKindAgentPreview {
		m.closePanelTab(panelKindAgentPreview)
		return nil
	}
	var cmd tea.Cmd
	if st.preview == nil {
		wtPreviewGen++
		st.preview = &wtPreviewState{gen: wtPreviewGen, screens: map[int][]string{}}
		cmd = wtPreviewTick(wtPreviewGen)
	}
	st.preview.grid, st.preview.sel = grid, 0
	focus := m.focus
	m.showPanelTab(panelKindAgentPreview)
	if m.panelActive != panelKindAgentPreview { // no panel (the editor is not running)
		st.preview = nil
		return nil
	}
	m.focus = focus // the list keeps the keys: j / k move, the preview follows
	m.wtPreviewRead()
	return cmd
}

// wtPreviewLua reads the end of several terminal buffers.
const wtPreviewLua = `local bufs, n = ...
local out = {}
for _, buf in ipairs(bufs) do
  if vim.api.nvim_buf_is_valid(buf) then
    local count = vim.api.nvim_buf_line_count(buf)
    -- The terminal pads the screen with blank rows below the prompt.
    local from = math.max(count - n - 300, 0)
    out[#out + 1] = { buf = buf, text = table.concat(vim.api.nvim_buf_get_lines(buf, from, -1, false), '\n') }
  end
end
if #out == 0 then return '' end
return vim.json.encode(out)`

// wtPreviewRead reads the screens of the tiles on show (on Update).
func (m *Model) wtPreviewRead() {
	p := m.wtPreviewOn()
	if p == nil || m.wt.off {
		return
	}
	if m.nvim == nil {
		p.err = "the editor is not running"
		return
	}
	tiles, _ := m.wtPreviewTiles()
	var bufs []int
	for _, t := range tiles {
		if !t.asleep && t.buf > 0 {
			bufs = append(bufs, t.buf)
		}
	}
	if len(bufs) == 0 {
		return
	}
	var raw string
	if err := m.nvim.ExecLuaResult(wtPreviewLua, &raw, bufs, wtPreviewRows); err != nil {
		p.err = err.Error()
		return
	}
	var got []struct {
		Buf  int    `json:"buf"`
		Text string `json:"text"`
	}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			p.err = "the screens could not be read: " + err.Error()
			return
		}
	}
	p.err = ""
	screens := make(map[int][]string, len(got))
	for _, g := range got {
		screens[g.Buf] = wtPreviewTail(strings.Split(g.Text, "\n"), wtPreviewRows)
	}
	p.screens = screens
}

// wtPreviewTick reads the screens again while the tab is on show.
func wtPreviewTick(gen int) tea.Cmd {
	return tea.Tick(wtPreviewPoll, func(time.Time) tea.Msg {
		return applyMsg(func(m *Model) tea.Cmd {
			p := m.wtPreviewOn()
			if p == nil || p.gen != gen {
				return nil
			}
			if m.termOpen && m.panelActive == panelKindAgentPreview {
				m.wtPreviewRead()
			}
			return wtPreviewTick(gen)
		})
	})
}

// wtPreviewMoved reads at once after the list cursor moved.
func (m *Model) wtPreviewMoved() {
	if p := m.wtPreviewOn(); p != nil && m.termOpen && m.panelActive == panelKindAgentPreview {
		m.wtPreviewRead()
	}
}

// wtPreviewJump shows the selected tile's terminal tab.
func (m *Model) wtPreviewJump(tiles []wtPreviewTile) tea.Cmd {
	p := m.wtPreviewOn()
	if p == nil || len(tiles) == 0 || m.wt.off {
		return nil
	}
	t := tiles[wtPreviewSelIndex(tiles, p.sel)]
	if t.tab < 0 || t.tab >= len(m.terminalTabs) || m.terminalTabs[t.tab].BufID != t.buf {
		return nil // the tab moved or ended; the next read redraws
	}
	if !m.termOpen && len(m.hiddenTerminalTabs) > 0 {
		m.toggleTerminalPanel()
	}
	m.showPanelTab(panelKindTerminal)
	m.switchTerminalTab(t.tab)
	return nil
}

func (m *Model) wtPreviewRender(w, h int) []string {
	p := m.wtPreviewOn()
	if p == nil {
		return nil
	}
	tiles, hint := m.wtPreviewTiles()
	if len(tiles) == 0 {
		muted := theme.FgBg(theme.TextMuted, theme.BgEditor)
		return []string{"", muted.Render("  " + theme.ASCIIText(hint)), "",
			muted.Render(theme.ASCIIText("  v or V in WORKTREES closes this · g switches one screen / grid"))}
	}
	return wtPreviewCompose(tiles, p.screens, p.sel, w, h)
}

// wtPreviewKey handles keys while the preview has the focus.
func wtPreviewKey(m *Model, k tea.KeyMsg) (tea.Cmd, bool) {
	p := m.wtPreviewOn()
	if p == nil {
		return nil, false
	}
	tiles, _ := m.wtPreviewTiles()
	move := func(d int) {
		if len(tiles) == 0 {
			return
		}
		i := cycleIndex(wtPreviewSelIndex(tiles, p.sel), d, len(tiles))
		p.sel = tiles[i].buf
	}
	switch k.String() {
	case "g":
		p.grid, p.sel = !p.grid, 0
		m.wtPreviewRead()
	case "left", "h", "up", "k", "shift+tab":
		move(-1)
	case "right", "l", "down", "j", "tab":
		move(1)
	case "enter":
		return m.wtPreviewJump(tiles), true
	case "r":
		m.wtPreviewRead()
	case "esc":
		m.panelFocused = false
	default:
		return nil, false
	}
	return nil, true
}

func init() {
	registerPanelKind(panelKindAgentPreview, panelKindSpec{
		Title: "PREVIEW",
		Icon:  "▦",
		TitleFn: func(m *Model) string {
			p := m.wtPreviewOn()
			if p == nil {
				return ""
			}
			t := "PREVIEW"
			if p.grid {
				t = "AGENT GRID"
			}
			if p.err != "" {
				t += " · " + p.err
			}
			return t
		},
		Render: func(m *Model, w, h int) []string { return m.wtPreviewRender(w, h) },
		Key:    wtPreviewKey,
		Click: func(m *Model, row, col int) tea.Cmd {
			p := m.wtPreviewOn()
			_, _, w, h, ok := m.panelContentRect()
			if p == nil || !ok {
				return nil
			}
			tiles, _ := m.wtPreviewTiles()
			i := wtPreviewTileAt(len(tiles), w, h, row, col)
			if i < 0 {
				return nil
			}
			p.sel = tiles[i].buf
			return m.wtPreviewJump(tiles)
		},
		OnClose: func(m *Model) {
			if m.wt != nil {
				m.wt.preview = nil
			}
		},
	})
	// It only shows the user the agents' screens; the AI never reads them
	// through it (terminal_read is the AI's own, audited path).
	aiActionPolicies[wtPreviewPaletteID] = aiAuto()
	aiActionPolicies[wtPreviewGridPaletteID] = aiAuto()
}
