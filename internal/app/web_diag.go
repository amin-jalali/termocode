package app

import (
	"reflect"
	"sort"
	"unicode/utf8"
	"unsafe"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The diagnostics topic (desktop Problems, squiggles, status bar counts):
// what vim.diagnostic holds for the workspace's files, the same set the
// laptop's Problems panel lists. The laptop refreshes its panel on every
// DiagnosticChanged (debounced, problems_panel.go); when that list moves
// and a browser listens, one fixed Lua chunk reads the diagnostics again
// with their whole ranges in UTF-16 units of their rows (the panel keeps
// only the start, in bytes). No timer and no polling: an Update without
// a new list costs one comparison.

func init() {
	webTopics["diagnostics"] = webTopicDef{snapshot: webDiagSnapshot, apply: webApplyReplace[webproto.DiagSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishDiag)
}

// webDiagLuaMax caps the diagnostics one read takes from nvim.
const webDiagLuaMax = 10 * webproto.MaxDiagItems

// webLuaDiag returns every diagnostic of a named file buffer: path,
// severity, the range (rows zero-based, columns in UTF-16 units of the
// row as nvim has it now), message, source and code.
const webLuaDiag = `
local max = ...
local api = vim.api
local out = {}
if not (vim.diagnostic and vim.diagnostic.get) then return out end
local function u16(s, b)
  local k, i = 0, 1
  while i <= #s and i <= b do
    local x = s:byte(i)
    local n = (x < 0x80 and 1) or (x < 0xE0 and 2) or (x < 0xF0 and 3) or 4
    if i + n - 1 > b then break end
    k = k + ((n == 4) and 2 or 1)
    i = i + n
  end
  return k
end
local names, rows = {}, {}
local function row(b, r)
  local key = b .. ':' .. r
  local s = rows[key]
  if s == nil then
    s = ''
    if api.nvim_buf_is_loaded(b) then s = api.nvim_buf_get_lines(b, r, r + 1, false)[1] or '' end
    rows[key] = s
  end
  return s
end
for _, d in ipairs(vim.diagnostic.get(nil)) do
  if #out >= max then break end
  local b = d.bufnr
  local name = b and names[b]
  if b and name == nil then
    name = ''
    if api.nvim_buf_is_valid(b) and vim.bo[b].buftype == '' then name = api.nvim_buf_get_name(b) end
    names[b] = name
  end
  if name and name ~= '' then
    local l0, c0 = d.lnum or 0, d.col or 0
    local l1, c1 = d.end_lnum or l0, d.end_col or c0
    if l1 < l0 or (l1 == l0 and c1 < c0) then l1, c1 = l0, c0 end
    local code = d.code
    if code ~= nil then code = tostring(code) end
    out[#out + 1] = { p = name, s = d.severity or 1, l = l0, c = u16(row(b, l0), c0),
      el = l1, ec = u16(row(b, l1), c1), m = d.message or '', src = d.source or '', code = code or '' }
  end
end
return out`

// webLuaDiagRow is one diagnostic as webLuaDiag returns it.
type webLuaDiagRow struct {
	Path    string `msgpack:"p"`
	Sev     int    `msgpack:"s"`
	Line    int    `msgpack:"l"`
	Col     int    `msgpack:"c"`
	EndLine int    `msgpack:"el"`
	EndCol  int    `msgpack:"ec"`
	Msg     string `msgpack:"m"`
	Source  string `msgpack:"src"`
	Code    string `msgpack:"code"`
}

// webDiagPub is what the diagnostics topic sent last (Update only).
type webDiagPub struct {
	key     webDiagKey
	read    bool               // key was read (or is being read)
	reading bool               // a read is on its way
	last    *webproto.DiagSnap // what the browsers have; nil: nothing yet
}

// webDiagKey identifies the laptop's Problems list: setItems replaces
// the slice as a whole, so its address says "new".
type webDiagKey struct {
	items unsafe.Pointer
	n     int
}

func (m *Model) webDiagKey() webDiagKey {
	if m.problems == nil {
		return webDiagKey{}
	}
	return webDiagKey{items: unsafe.Pointer(unsafe.SliceData(m.problems.items)), n: len(m.problems.items)}
}

// webDiagDelta is a diagnostics delta: the whole snapshot; a newer one
// replaces a queued one.
type webDiagDelta webproto.DiagSnap

func (webDiagDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webDiagDelta)
	return n, ok
}

func webDiagSnapshot(m *Model, _ string) (any, error) {
	if m.web != nil && m.web.diag.last != nil {
		return *m.web.diag.last, nil
	}
	return webproto.DiagSnap{Files: []webproto.DiagFile{}}, nil
}

// webPublishDiag reads the diagnostics again when the laptop's list
// moved (or a browser started listening) and sends them when they
// changed. One read at a time; a change during a read reads again after.
func (m *Model) webPublishDiag(tea.Msg) tea.Cmd {
	p := &m.web.diag
	if !m.web.webListens("diagnostics") {
		*p = webDiagPub{}
		return nil
	}
	key := m.webDiagKey()
	if p.reading || (p.read && key == p.key) || m.nvim == nil {
		return nil
	}
	p.key, p.read, p.reading = key, true, true
	client := m.nvim
	return func() tea.Msg {
		var rows []webLuaDiagRow
		err := client.ExecLuaResult(webLuaDiag, &rows, webDiagLuaMax)
		return applyMsg(func(m *Model) tea.Cmd {
			if m.web == nil {
				return nil
			}
			p := &m.web.diag
			p.reading = false
			if err != nil {
				recordError("[web] diagnostics: " + err.Error())
				return nil
			}
			if !m.web.webListens("diagnostics") {
				return nil
			}
			snap := webDiagBuild(rows, m.webRel)
			if p.last != nil && reflect.DeepEqual(*p.last, snap) {
				return nil
			}
			p.last = &snap
			m.web.publish("diagnostics", webDiagDelta(snap))
			return nil
		})
	}
}

// webDiagBuild makes the topic from nvim's rows: files outside the
// workspace and files that may hold secrets are left out (the page
// cannot open them), the rest sorted and capped. Pure for tests.
func webDiagBuild(rows []webLuaDiagRow, rel func(string) (string, bool)) webproto.DiagSnap {
	snap := webproto.DiagSnap{Files: []webproto.DiagFile{}}
	by := map[string][]webproto.Diag{}
	worst := map[string]int{}
	paths := map[string]string{} // abs -> rel
	for _, r := range rows {
		p, ok := paths[r.Path]
		if !ok {
			if q, in := rel(r.Path); in && q != "" && !ai.IsSecretFileName(r.Path) {
				p = q
			}
			paths[r.Path] = p
		}
		if p == "" {
			continue
		}
		sev := r.Sev
		if sev < 1 || sev > 4 {
			sev = 1
		}
		switch sev {
		case 1:
			snap.Errors++
		case 2:
			snap.Warnings++
		case 3:
			snap.Infos++
		default:
			snap.Hints++
		}
		d := webproto.Diag{Sev: sev, Line: max(r.Line, 0), Col: max(r.Col, 0), EndLine: max(r.EndLine, 0), EndCol: max(r.EndCol, 0),
			Message: webDiagCut(r.Msg, webproto.MaxDiagMessage), Source: webDiagCut(r.Source, 64), Code: webDiagCut(r.Code, 64)}
		if d.EndLine < d.Line || (d.EndLine == d.Line && d.EndCol < d.Col) {
			d.EndLine, d.EndCol = d.Line, d.Col
		}
		by[p] = append(by[p], d)
		if w, ok := worst[p]; !ok || sev < w {
			worst[p] = sev
		}
	}
	files := make([]string, 0, len(by))
	for p := range by {
		files = append(files, p)
	}
	sort.Slice(files, func(i, j int) bool {
		if worst[files[i]] != worst[files[j]] {
			return worst[files[i]] < worst[files[j]]
		}
		return files[i] < files[j]
	})
	total := 0
	for _, p := range files {
		items := by[p]
		sort.SliceStable(items, func(i, j int) bool {
			a, b := items[i], items[j]
			if a.Sev != b.Sev {
				return a.Sev < b.Sev
			}
			if a.Line != b.Line {
				return a.Line < b.Line
			}
			return a.Col < b.Col
		})
		keep := min(len(items), webproto.MaxDiagPerFile, webproto.MaxDiagItems-total)
		snap.More += len(items) - keep
		if keep == 0 {
			continue
		}
		total += keep
		snap.Files = append(snap.Files, webproto.DiagFile{Path: p, Items: items[:keep]})
	}
	return snap
}

// webDiagCut cuts s at n bytes on a rune edge.
func webDiagCut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
