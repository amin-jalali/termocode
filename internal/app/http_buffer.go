package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The .http buffer side (plan 2.2): Go parses the buffer text (unsaved
// edits included), builds the lens + undercurl row table and hands it
// to the decoration provider in httpBufferLua.

func init() {
	// The buffer changed (150 ms debounce) or was entered.
	registerNotifyHandler("termocode_http_buf", func(m *Model, args []any) tea.Cmd {
		if name := m.httpRefreshBuf(notifyArgInt(args, 0)); name != "" {
			return m.httpCheckPrivateEnv(name)
		}
		return nil
	})
	// A click on a request's lens: send that request.
	registerNotifyHandler("termocode_http_lens", func(m *Model, args []any) tea.Cmd {
		return m.httpSendBufferLine(notifyArgInt(args, 0), notifyArgInt(args, 1))
	})
	registerNotifyHandler("termocode_http_cancel", func(m *Model, _ []any) tea.Cmd {
		return m.httpCancel()
	})
}

// isHTTPPath says whether a file is an .http / .rest file.
func isHTTPPath(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return ext == ".http" || ext == ".rest"
}

// ensureHTTPLua loads the .http buffer and page Lua when a session
// attached without it (tests, older sessions).
func (m *Model) ensureHTTPLua() {
	if m.nvim == nil {
		return
	}
	if ok, err := m.nvim.EvalLuaString(`return (_G._termocode_http_buf_set and _G._termocode_http_open) and 'y' or 'n'`); err == nil && ok != "y" {
		_ = m.nvim.ExecLua(httpBufferLua)
		_ = m.nvim.ExecLua(httpPageLua)
	}
}

// httpBufText reads a buffer's name and lines (0 = current buffer).
func (m *Model) httpBufText(buf int) (name string, lines []string, ok bool) {
	if m.nvim == nil {
		return "", nil, false
	}
	var raw string
	err := m.nvim.ExecLuaResult(`local b = ...
if b == 0 then b = vim.api.nvim_get_current_buf() end
if not vim.api.nvim_buf_is_valid(b) then return '' end
return vim.json.encode({ name = vim.api.nvim_buf_get_name(b), lines = vim.api.nvim_buf_get_lines(b, 0, -1, false) })`, &raw, buf)
	if err != nil || raw == "" {
		return "", nil, false
	}
	var res struct {
		Name  string   `json:"name"`
		Lines []string `json:"lines"`
	}
	if json.Unmarshal([]byte(raw), &res) != nil {
		return "", nil, false
	}
	return res.Name, res.Lines, true
}

// httpBufCache is the last .http buffer read and parsed, by its
// changedtick: a send and the lens refreshes around it read the buffer
// once, not three times. nvim never reuses a buffer number, and any
// change bumps the tick, so a stale entry is never used.
type httpBufCache struct {
	buf   int
	tick  int64
	name  string
	lines []string
	f     httpclient.File
}

// httpBufParsed reads and parses a buffer (0 = current), from the cache
// when its changedtick did not move. cursor is the current window's
// cursor line.
func (m *Model) httpBufParsed(buf int) (c *httpBufCache, cursor int, ok bool) {
	if m.nvim == nil {
		return nil, 0, false
	}
	s := m.httpS()
	kb, kt := -1, int64(-1)
	if s.bufCache != nil {
		kb, kt = s.bufCache.buf, s.bufCache.tick
	}
	var raw string
	err := m.nvim.ExecLuaResult(`local b, kb, kt = ...
if b == 0 then b = vim.api.nvim_get_current_buf() end
if not vim.api.nvim_buf_is_valid(b) then return '' end
local t = vim.api.nvim_buf_get_changedtick(b)
local out = { buf = b, tick = t, name = vim.api.nvim_buf_get_name(b), cursor = vim.api.nvim_win_get_cursor(0)[1] }
if not (b == kb and t == kt) then out.lines = vim.api.nvim_buf_get_lines(b, 0, -1, false) end
return vim.json.encode(out)`, &raw, buf, kb, kt)
	if err != nil || raw == "" {
		return nil, 0, false
	}
	var res struct {
		Buf    int       `json:"buf"`
		Tick   int64     `json:"tick"`
		Name   string    `json:"name"`
		Cursor int       `json:"cursor"`
		Lines  *[]string `json:"lines"`
	}
	if json.Unmarshal([]byte(raw), &res) != nil {
		return nil, 0, false
	}
	if res.Lines == nil {
		if s.bufCache == nil || s.bufCache.buf != res.Buf || s.bufCache.tick != res.Tick {
			return nil, 0, false
		}
		return s.bufCache, res.Cursor, true
	}
	c = &httpBufCache{buf: res.Buf, tick: res.Tick, name: res.Name, lines: *res.Lines}
	if isHTTPPath(c.name) {
		c.f = httpclient.Parse(strings.Join(c.lines, "\n"))
		s.bufCache = c
	}
	return c, res.Cursor, true
}

// httpSendKeyLabel is the lens' key ("Ctrl+Alt+R", or the user's own).
func (m *Model) httpSendKeyLabel() string {
	keys := m.keys.KeysFor(keymap.ActionHTTPSend)
	if len(keys) == 0 {
		return ""
	}
	return theme.KeyLabel(keys[0])
}

// httpLensChunks is the lens of one request (virt_text chunks).
func (m *Model) httpLensChunks(file, label, env string, sending bool) [][]string {
	if sending {
		return [][]string{{" " + httpGlyphFlight.String() + " sending…  Esc cancel ", "TermocodeHttpLensBusy"}}
	}
	text := " " + httpGlyphSend.String() + " send"
	if k := m.httpSendKeyLabel(); k != "" {
		text += " " + k
	}
	out := [][]string{{text, "TermocodeHttpLens"}}
	if l, ok := m.httpS().lens[file+"\x00"+label]; ok {
		out = append(out, []string{" · ", "TermocodeHttpLens"}, []string{l.text[:l.n], l.hl})
		if rest := l.text[l.n:]; rest != "" {
			out = append(out, []string{rest, "TermocodeHttpLens"})
		}
	}
	if env != "" {
		out = append(out, []string{" · env " + env, "TermocodeHttpLens"})
	}
	out[len(out)-1][0] += " "
	return out
}

// httpBufRows builds the decoration row table for a parsed buffer.
func httpBufRows(f httpclient.File, lines []string, known func(string) bool, lens func(r *httpclient.Request) [][]string) []map[string]any {
	byLine := map[int]map[string]any{}
	row := func(l int) map[string]any {
		r := byLine[l]
		if r == nil {
			r = map[string]any{"l": l}
			byLine[l] = r
		}
		return r
	}
	for i := range f.Requests {
		r := &f.Requests[i]
		row(r.Line)["lens"] = lens(r)
	}
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") || !strings.Contains(line, "{{") {
			continue
		}
		var curls [][]int
		for _, loc := range httpPlaceholderLocs(line) {
			if !known(line[loc[2]:loc[3]]) {
				curls = append(curls, []int{loc[0], loc[1]})
			}
		}
		if len(curls) > 0 {
			row(i + 1)["curls"] = curls
		}
	}
	out := make([]map[string]any, 0, len(byLine))
	for l := 1; l <= len(lines)+1; l++ {
		if r, ok := byLine[l]; ok {
			out = append(out, r)
		}
	}
	return out
}

// httpPlaceholderLocs finds {{…}} in s: [start, end, innerStart, innerEnd].
func httpPlaceholderLocs(s string) [][4]int {
	var out [][4]int
	for i := 0; i < len(s); {
		j := strings.Index(s[i:], "{{")
		if j < 0 {
			break
		}
		start := i + j
		k := strings.Index(s[start+2:], "}}")
		if k < 0 {
			break
		}
		end := start + 2 + k + 2
		inner := s[start+2 : end-2]
		if strings.ContainsAny(inner, "{}") {
			i = start + 2
			continue
		}
		a, b := start+2, end-2
		for a < b && s[a] == ' ' {
			a++
		}
		for b > a && s[b-1] == ' ' {
			b--
		}
		out = append(out, [4]int{start, end, a, b})
		i = end
	}
	return out
}

// httpRefreshBuf re-parses an .http buffer and redraws its lens. It
// returns the file name ("" when buf is not an .http buffer).
func (m *Model) httpRefreshBuf(buf int) string {
	if m.nvim == nil {
		return ""
	}
	c, _, ok := m.httpBufParsed(buf)
	if !ok || !isHTTPPath(c.name) {
		return ""
	}
	name, lines, f, buf := c.name, c.lines, c.f, c.buf
	m.ensureHTTPLua()
	rv := httpResolver(name, f.Vars, "")
	s := m.httpS()
	rows := httpBufRows(f, lines, rv.Known, func(r *httpclient.Request) [][]string {
		label := r.Label()
		sending := s.running != nil && s.running.file == name && s.running.label == label
		return m.httpLensChunks(name, label, rv.Env, sending)
	})
	// Completion names: file variables, then the environment's (names
	// only; values never reach the menu).
	seen := map[string]bool{}
	vars := []string{}
	for _, v := range f.Vars {
		if !seen[v.Name] {
			seen[v.Name] = true
			vars = append(vars, v.Name)
		}
	}
	for _, n := range rv.Envs.VarNames(rv.Env) {
		if !seen[n] {
			seen[n] = true
			vars = append(vars, n)
		}
	}
	if err := m.nvim.ExecLuaArgs(`local b, r, run, v = ...
if _G._termocode_http_buf_set then _G._termocode_http_buf_set(b, r, run, v) end`, buf, rows, s.running != nil, vars); err != nil {
		recordError(fmt.Sprintf("http lens: %v", err))
	}
	return name
}

// httpRefreshLens redraws the lens of every buffer showing file.
func (m *Model) httpRefreshLens(file string) {
	if m.nvim == nil || file == "" {
		return
	}
	var raw string
	if err := m.nvim.ExecLuaResult(`local name = ...
local out = {}
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_is_loaded(b) and vim.api.nvim_buf_get_name(b) == name then out[#out + 1] = tostring(b) end
end
return table.concat(out, ' ')`, &raw, file); err != nil {
		return
	}
	for _, f := range strings.Fields(raw) {
		var b int
		if _, err := fmt.Sscan(f, &b); err == nil {
			m.httpRefreshBuf(b)
		}
	}
}

// httpRequestFromBuffer parses a buffer and returns the request at the
// 1-based line (0 = the cursor line of the current window).
func (m *Model) httpRequestFromBuffer(buf, line int) (file string, f httpclient.File, req *httpclient.Request, err error) {
	c, cursor, ok := m.httpBufParsed(buf)
	if !ok {
		return "", f, nil, fmt.Errorf("no editor")
	}
	name := c.name
	if !isHTTPPath(name) {
		return name, f, nil, fmt.Errorf("not an .http file")
	}
	if line <= 0 {
		line = cursor
	}
	f = c.f
	req = f.AtLine(line)
	if req == nil {
		return name, f, nil, fmt.Errorf("no request in %s", filepath.Base(name))
	}
	return name, f, req, nil
}

// httpSendBufferLine sends the request at line of buffer buf (0 = the
// current buffer and cursor).
func (m *Model) httpSendBufferLine(buf, line int) tea.Cmd {
	file, f, req, err := m.httpRequestFromBuffer(buf, line)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "HTTP: "+err.Error(), "")
	}
	return m.httpSendRequest(file, f.Vars, *req, "")
}
