package app

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop editor's language features (hover, completion, go to
// definition, find references) from the laptop's own language servers:
// the browser runs none. One fixed Lua chunk checks that the buffer is
// still at the changedtick the page asked about, turns the page's
// UTF-16 column into each server's own position encoding and sends the
// request without waiting (vim.lsp's async client: the laptop's editor
// never stops for a slow server). When every server answered, the chunk
// sends the result back as one termocode_web_lsp notification; the hub
// cleans it (paths made workspace-relative, files outside the workspace
// and secret files without their text, every string capped) and answers
// the call. A server that does not answer in webLspWait ends the call.

// webLspWait is how long a call waits for the servers (tests lower it).
var webLspWait = 8 * time.Second

// webLspMaxWaiting caps the calls waiting for servers at once.
const webLspMaxWaiting = 64

func init() {
	for op, kind := range webLspKinds {
		webOps[op] = func(m *Model, call *webCall) tea.Cmd { return m.webOpLsp(call, kind) }
	}
	registerNotifyHandler("termocode_web_lsp", func(m *Model, a []any) tea.Cmd {
		m.webLspDone(notifyArgInt(a, 0), notifyArgString(a, 1))
		return nil
	})
}

// webLspKinds names the chunk's request kind of each op.
var webLspKinds = map[string]string{
	webproto.OpLspHover:      "hover",
	webproto.OpLspComplete:   "complete",
	webproto.OpLspDefinition: "definition",
	webproto.OpLspReferences: "references",
}

// webLspState holds the calls waiting for the servers (Update only).
type webLspState struct {
	next    int
	waiting map[int]*webLspCall
}

type webLspCall struct {
	call *webCall
	kind string
	done chan struct{}
}

// webLuaLsp sends one request to every server of buffer b that can
// answer it and returns at once: {ok}, or {err = gone | stale (with the
// buffer's tick) | nolsp | nocap}.
const webLuaLsp = `
local id, kind, b, tick, line, col16, maxn, maxtext = ...
local api = vim.api
if not api.nvim_buf_is_valid(b) or not api.nvim_buf_is_loaded(b) then return { err = 'gone' } end
local cur = api.nvim_buf_get_changedtick(b)
if cur ~= tick then return { err = 'stale', tick = cur } end
local caps = { hover = 'hoverProvider', complete = 'completionProvider', definition = 'definitionProvider', references = 'referencesProvider' }
local methods = { hover = 'textDocument/hover', complete = 'textDocument/completion', definition = 'textDocument/definition', references = 'textDocument/references' }
local get = vim.lsp.get_clients or vim.lsp.get_active_clients
local all = get({ bufnr = b })
if #all == 0 then return { err = 'nolsp' } end
local cs = {}
for _, c in ipairs(all) do
  if (c.server_capabilities or {})[caps[kind]] then cs[#cs + 1] = c end
end
if #cs == 0 then return { err = 'nocap' } end

-- Columns: byte offsets of a row <-> units of an encoding.
local function step(x)
  return (x < 0x80 and 1) or (x < 0xE0 and 2) or (x < 0xF0 and 3) or 4
end
local function units(s, b0, enc)
  if enc == 'utf-8' then return math.min(b0, #s) end
  local k, i = 0, 1
  while i <= #s and i <= b0 do
    local n = step(s:byte(i))
    if i + n - 1 > b0 then break end
    k = k + ((enc == 'utf-16' and n == 4) and 2 or 1)
    i = i + n
  end
  return k
end
local function bytes(s, u, enc)
  if enc == 'utf-8' then return math.min(math.max(u, 0), #s) end
  local k, i = 0, 1
  while i <= #s and k < u do
    local n = step(s:byte(i))
    k = k + ((enc == 'utf-16' and n == 4) and 2 or 1)
    i = i + n
  end
  return math.min(i - 1, #s)
end
local rows = {}
local function row(r)
  local s = rows[r]
  if s == nil then
    s = api.nvim_buf_get_lines(b, r, r + 1, false)[1] or ''
    rows[r] = s
  end
  return s
end
-- A position of this buffer in a server's encoding as [row, UTF-16 col].
local function here(p, enc)
  local s = row(p.line)
  return p.line, units(s, bytes(s, p.character, enc), 'utf-16')
end

local text = row(line)
local bcol = bytes(text, col16, 'utf-16')
local uri = vim.uri_from_bufnr(b)

local function hovertext(c)
  if type(c) == 'string' then return c end
  if type(c) ~= 'table' then return '' end
  if c.kind == 'plaintext' then return '` + "```" + `text\n' .. (c.value or '') .. '\n` + "```" + `' end
  if c.value then
    if c.language then return '` + "```" + `' .. c.language .. '\n' .. c.value .. '\n` + "```" + `' end
    return c.value
  end
  local parts = {}
  for _, x in ipairs(c) do parts[#parts + 1] = hovertext(x) end
  return table.concat(parts, '\n\n')
end

local function doc(d)
  if type(d) == 'string' then return d end
  if type(d) == 'table' and type(d.value) == 'string' then return d.value end
  return nil
end

local function cut(s)
  if type(s) ~= 'string' then return nil end
  if #s > maxtext then return s:sub(1, maxtext) end
  return s
end

local results = {}
local function finish()
  local out = {}
  if kind == 'hover' then
    local texts = {}
    for _, r in ipairs(results) do
      if r.res and r.res.contents then
        local t = vim.trim(hovertext(r.res.contents))
        if t ~= '' then texts[#texts + 1] = t end
      end
    end
    local s = table.concat(texts, '\n\n---\n\n')
    if #s > maxtext then s = s:sub(1, maxtext) end
    out.md = s
  elseif kind == 'complete' then
    local items = {}
    for _, r in ipairs(results) do
      local res = r.res
      if type(res) == 'table' then
        local list = res.items or res
        if res.isIncomplete then out.incomplete = true end
        local defrange = res.itemDefaults and res.itemDefaults.editRange
        for _, it in ipairs(list) do
          if #items >= maxn then out.cut = true break end
          local te = it.textEdit
          local range = te and (te.range or te.replace) or nil
          if not range and defrange then range = defrange.range or defrange.replace or defrange end
          local x = { label = cut(it.label) or '', kind = it.kind, detail = cut(it.detail), doc = cut(doc(it.documentation)),
            insert = cut((te and te.newText) or it.textEditText or it.insertText or it.label) or '',
            snippet = it.insertTextFormat == 2, sort = cut(it.sortText), filter = cut(it.filterText), from = -1, to = -1 }
          if range and range.start and range['end'] and range.start.line == line and range['end'].line == line then
            local _, a = here(range.start, r.enc)
            local _, z = here(range['end'], r.enc)
            x.from, x.to = a, z
          end
          if type(it.additionalTextEdits) == 'table' and #it.additionalTextEdits > 0 then
            local eds = {}
            for _, e in ipairs(it.additionalTextEdits) do
              if #eds >= 16 then break end
              if e.range and type(e.newText) == 'string' then
                local l0, c0 = here(e.range.start, r.enc)
                local l1, c1 = here(e.range['end'], r.enc)
                eds[#eds + 1] = { line = l0, col = c0, endLine = l1, endCol = c1, text = cut(e.newText) }
              end
            end
            if #eds > 0 then x.edits = eds end
          end
          items[#items + 1] = x
        end
      end
    end
    out.items = items
  else
    local seen, locs = {}, {}
    for _, r in ipairs(results) do
      local res = r.res
      if type(res) == 'table' then
        if res.uri or res.targetUri then res = { res } end
        local list = {}
        for _, l in ipairs(res) do
          if #list >= maxn then out.cut = true break end
          list[#list + 1] = l
        end
        local ok, items = pcall(vim.lsp.util.locations_to_items, list, r.enc)
        if ok and type(items) == 'table' then
          for _, it in ipairs(items) do
            local s = it.text or ''
            local c0 = units(s, (it.col or 1) - 1, 'utf-16')
            local c1 = units(s, #s, 'utf-16')
            if (it.end_lnum or it.lnum) == it.lnum and it.end_col then c1 = units(s, it.end_col - 1, 'utf-16') end
            local key = (it.filename or '') .. ':' .. it.lnum .. ':' .. c0
            if not seen[key] and #locs < maxn then
              seen[key] = true
              locs[#locs + 1] = { file = it.filename or '', line = it.lnum - 1, col = c0, endLine = it.lnum - 1, endCol = math.max(c0, c1), text = s }
            elseif not seen[key] then
              out.cut = true
            end
          end
        end
      end
    end
    out.locs = locs
  end
  if _G.termocode_notify then _G.termocode_notify('termocode_web_lsp', id, vim.json.encode(out)) end
end

local pending = #cs
local function back(c, enc)
  return function(err, res)
    results[#results + 1] = { enc = enc, res = (err == nil) and res or nil }
    pending = pending - 1
    if pending == 0 then finish() end
  end
end
local new = vim.fn.has('nvim-0.11') == 1
local sent = 0
for _, c in ipairs(cs) do
  local enc = c.offset_encoding or 'utf-16'
  local params = { textDocument = { uri = uri }, position = { line = line, character = units(text, bcol, enc) } }
  if kind == 'references' then params.context = { includeDeclaration = true } end
  if kind == 'complete' then params.context = { triggerKind = 1 } end
  local ok, r
  if new then ok, r = pcall(c.request, c, methods[kind], params, back(c, enc), b)
  else ok, r = pcall(c.request, methods[kind], params, back(c, enc), b) end
  if ok and r then sent = sent + 1 else pending = pending - 1 end
end
if sent == 0 then return { err = 'nocap' } end
return { ok = true }`

// webLuaLspRes is what webLuaLsp returns.
type webLuaLspRes struct {
	Err  string `msgpack:"err"`
	Tick int    `msgpack:"tick"`
	OK   bool   `msgpack:"ok"`
}

// webLspOut is the notification's JSON.
type webLspOut struct {
	MD         string          `json:"md"`
	Items      []webLspOutItem `json:"items"`
	Incomplete bool            `json:"incomplete"`
	Locs       []webLspOutLoc  `json:"locs"`
	Cut        bool            `json:"cut"`
}

type webLspOutItem struct {
	Label   string             `json:"label"`
	Kind    int                `json:"kind"`
	Detail  string             `json:"detail"`
	Doc     string             `json:"doc"`
	Insert  string             `json:"insert"`
	Snippet bool               `json:"snippet"`
	Sort    string             `json:"sort"`
	Filter  string             `json:"filter"`
	From    int                `json:"from"`
	To      int                `json:"to"`
	Edits   []webproto.LspEdit `json:"edits"`
}

type webLspOutLoc struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	EndLine int    `json:"endLine"`
	EndCol  int    `json:"endCol"`
	Text    string `json:"text"`
}

// webOpLsp sends the request; the answer comes with the notification.
func (m *Model) webOpLsp(call *webCall, kind string) tea.Cmd {
	a := call.args.(*webproto.LspPosArgs)
	if m.web == nil || m.nvim == nil {
		call.fail(webproto.CodeGone, "The editor is not running.")
		return nil
	}
	b := m.web.filesState().bufs[a.Buf]
	if b == nil || b.term {
		call.fail(webproto.CodeNotFound, "Open the file first.")
		return nil
	}
	st := &m.web.lsp
	if len(st.waiting) >= webLspMaxWaiting {
		call.fail(webproto.CodeBusy, "The language server is busy. Try again.")
		return nil
	}
	st.next++
	id := st.next
	maxn, maxText := webproto.MaxLocations, webproto.MaxHoverText
	if kind == "complete" {
		maxn, maxText = webproto.MaxCompletions, webproto.MaxCompleteDoc
	}
	var res webLuaLspRes
	if err := m.nvim.ExecLuaResult(webLuaLsp, &res, id, kind, b.buf, a.Tick, a.Line, a.Col, maxn, maxText); err != nil {
		webFail(call, err)
		return nil
	}
	switch res.Err {
	case "":
	case "stale":
		webFailData(call, webproto.CodeConflict, "The file changed. Ask again.", webproto.ConflictData{Tick: res.Tick})
		return nil
	case "gone":
		call.fail(webproto.CodeGone, "This file was closed on the laptop.")
		return nil
	case "nolsp":
		call.fail(webproto.CodeNotFound, "No language server runs for this file.")
		return nil
	default:
		call.fail(webproto.CodeNotFound, "The language server cannot do that.")
		return nil
	}
	if st.waiting == nil {
		st.waiting = map[int]*webLspCall{}
	}
	w := &webLspCall{call: call, kind: kind, done: make(chan struct{})}
	st.waiting[id] = w
	call.later()
	wait := webLspWait
	return func() tea.Msg {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-w.done:
			return nil
		case <-call.ctx.Done():
		case <-t.C:
			call.fail(webproto.CodeBusy, "The language server did not answer in time.")
		}
		return applyMsg(func(m *Model) tea.Cmd {
			if m.web != nil && m.web.lsp.waiting[id] == w {
				delete(m.web.lsp.waiting, id)
			}
			return nil
		})
	}
}

// webLspDone answers the call the notification belongs to.
func (m *Model) webLspDone(id int, raw string) {
	if m.web == nil {
		return
	}
	w := m.web.lsp.waiting[id]
	if w == nil {
		return // timed out, or not ours
	}
	delete(m.web.lsp.waiting, id)
	defer close(w.done)
	var out webLspOut
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		recordError("[web] lsp: " + err.Error())
		w.call.fail(webproto.CodeInternal, "The language server's answer could not be read.")
		return
	}
	switch w.kind {
	case "hover":
		w.call.reply(webLspHover(out.MD))
	case "complete":
		w.call.reply(webLspCompletion(out))
	default:
		w.call.reply(webLspLocations(out, m.webRel, w.kind == "references"))
	}
}

// webLspHover makes the hover's markdown an AST.
func webLspHover(md string) webproto.LspHover {
	md = webDiagCut(md, webproto.MaxHoverText)
	if md == "" {
		return webproto.LspHover{None: true}
	}
	return webproto.LspHover{MD: webMarkdown(md)}
}

// webLspCompletion caps and checks the items.
func webLspCompletion(out webLspOut) webproto.LspCompletion {
	res := webproto.LspCompletion{Items: []webproto.LspItem{}, Incomplete: out.Incomplete, Cut: out.Cut}
	for _, it := range out.Items {
		if len(res.Items) == webproto.MaxCompletions {
			res.Cut = true
			break
		}
		if it.Label == "" {
			continue
		}
		x := webproto.LspItem{Label: webDiagCut(it.Label, webproto.MaxCompleteText), Detail: webDiagCut(it.Detail, webproto.MaxCompleteText),
			Doc: webDiagCut(it.Doc, webproto.MaxCompleteDoc), Insert: webDiagCut(it.Insert, webproto.MaxCompleteText), Snippet: it.Snippet,
			Sort: webDiagCut(it.Sort, 64), Filter: webDiagCut(it.Filter, webproto.MaxCompleteText), From: -1, To: -1}
		if it.Kind >= 1 && it.Kind <= 25 {
			x.Kind = it.Kind
		}
		if it.From >= 0 && it.To >= it.From {
			x.From, x.To = it.From, it.To
		}
		for _, e := range it.Edits {
			if len(x.Edits) == webproto.MaxCompleteEdit {
				break
			}
			if e.Line < 0 || e.Col < 0 || e.EndLine < e.Line || (e.EndLine == e.Line && e.EndCol < e.Col) {
				continue
			}
			e.Text = webDiagCut(e.Text, webproto.MaxCompleteText)
			x.Edits = append(x.Edits, e)
		}
		res.Items = append(res.Items, x)
	}
	return res
}

// webLspLocations makes the paths workspace-relative: a place outside
// the workspace keeps only its file's name, a secret file no text.
// References are sorted by path and position.
func webLspLocations(out webLspOut, rel func(string) (string, bool), sorted bool) webproto.LspLocations {
	res := webproto.LspLocations{Locs: []webproto.LspLoc{}, Cut: out.Cut}
	for _, l := range out.Locs {
		if len(res.Locs) == webproto.MaxLocations {
			res.Cut = true
			break
		}
		if l.File == "" || l.Line < 0 || l.Col < 0 {
			continue
		}
		loc := webproto.LspLoc{Line: l.Line, Col: l.Col, EndLine: max(l.EndLine, l.Line), EndCol: l.EndCol}
		if loc.EndLine == loc.Line && loc.EndCol < loc.Col {
			loc.EndCol = loc.Col
		}
		p, in := rel(l.File)
		switch {
		case !in || p == "":
			loc.Outside, loc.Name = true, webDiagCut(filepath.Base(l.File), webproto.MaxName)
		case ai.IsSecretFileName(l.File):
			loc.Path = p
		default:
			loc.Path, loc.Text = p, webDiagCut(l.Text, webproto.MaxLocationText)
		}
		res.Locs = append(res.Locs, loc)
	}
	if !sorted { // definitions: the servers' order, the best first
		return res
	}
	sort.SliceStable(res.Locs, func(i, j int) bool {
		a, b := res.Locs[i], res.Locs[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	return res
}
