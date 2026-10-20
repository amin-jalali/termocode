package app

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/dbclient"
	tea "github.com/charmbracelet/bubbletea"
)

// dbSelectionLua returns the cursor line and the visual selection (if any)
// of the current buffer as JSON.
const dbSelectionLua = `
local a=vim.api
local mode=vim.fn.mode()
local selection=''
local first=a.nvim_win_get_cursor(0)[1]
if mode=='v' or mode=='V' or mode=='\22' then
 local p=vim.fn.getpos('v');local q=a.nvim_win_get_cursor(0)
 local lo,hi=p[2],q[1];local lc,hc=p[3]-1,q[2]
 if lo>hi or (lo==hi and lc>hc) then lo,hi=hi,lo;lc,hc=hc,lc end
 local lines=a.nvim_buf_get_lines(0,lo-1,hi,false)
 if mode=='v' then
  if #lines==1 then lines[1]=lines[1]:sub(lc+1,hc+1)
  else lines[#lines]=lines[#lines]:sub(1,hc+1);lines[1]=lines[1]:sub(lc+1) end
 end
 selection=table.concat(lines,'\n');first=lo
 vim.api.nvim_feedkeys(vim.api.nvim_replace_termcodes('<Esc>',true,false,true),'n',false)
end
return vim.json.encode({line=first,selection=selection})
`

// dbConsoleLua wires up SQL buffers: a decoration provider draws the lens
// at the end of the first line of each statement ("▸ run Ctrl+Alt+R ·
// local-pg"), with ephemeral extmarks for the visible rows only. Go owns
// the row table (_G._termocode_sql.rows[buf] = {[lnum] = text}); a change
// in the buffer (150 ms debounce) or entering it notifies termocode_db_buf.
// Esc in a SQL buffer cancels a running query. Nothing runs while idle.
const dbConsoleLua = `
if _G._termocode_sql then return end
local api=vim.api
local S={rows={},running=false}
_G._termocode_sql=S
local ns=api.nvim_create_namespace('termocode_sql_lens')
api.nvim_set_decoration_provider(ns,{
 on_win=function(_,_,b) return S.rows[b]~=nil end,
 on_line=function(_,_,b,row)
  local t=S.rows[b] and S.rows[b][row+1]
  if not t then return end
  api.nvim_buf_set_extmark(b,ns,row,0,{virt_text={{' '..t,S.running and 'TermocodeHttpLensBusy' or 'TermocodeHttpLens'}},virt_text_pos='eol',ephemeral=true})
 end,
})
local timers={}
local function changed(buf)
 if timers[buf] then timers[buf]:stop() end
 timers[buf]=vim.defer_fn(function()
  timers[buf]=nil
  if api.nvim_buf_is_valid(buf) and _G.termocode_notify then _G.termocode_notify('termocode_db_buf',buf) end
 end,150)
end
local g=api.nvim_create_augroup('termocode_sql',{clear=true})
api.nvim_create_autocmd('FileType',{group=g,pattern='sql',callback=function(args)
 local buf=args.buf
 api.nvim_create_autocmd('BufEnter',{group=g,buffer=buf,callback=function() changed(buf) end})
 api.nvim_create_autocmd('BufWipeout',{group=g,buffer=buf,callback=function() S.rows[buf]=nil end})
 if not vim.b[buf].termocode_sql_attached then
  vim.b[buf].termocode_sql_attached=true
  api.nvim_buf_attach(buf,false,{on_lines=function() changed(buf) end,on_detach=function() pcall(function() vim.b[buf].termocode_sql_attached=nil end) end})
 end
 vim.keymap.set('n','<Esc>',function()
  if S.running and _G.termocode_notify then _G.termocode_notify('termocode_db_cancel');return '<Ignore>' end
  return '<Esc>'
 end,{buffer=buf,expr=true,silent=true})
 changed(buf)
end})
`

func (m *Model) ensureDBLua() {
	if m.nvim == nil {
		return
	}
	_ = m.nvim.ExecLua(dbConsoleLua)
	_ = m.nvim.ExecLua(dbPageLua)
}

// databaseRefreshLens records the status of the statement that ran (shown
// in its lens) and redraws the lenses of the file's visible buffers.
func (m *Model) databaseRefreshLens(file, sql, text string) {
	s := m.dbConsole()
	if file != "" && sql != "" {
		if text == "" {
			delete(s.lens, file+"\x00"+sql)
		} else {
			s.lens[file+"\x00"+sql] = text
		}
	}
	if m.nvim == nil || file == "" {
		return
	}
	var bufs []int
	_ = m.nvim.ExecLuaResult(`local f,run=...;_G._termocode_sql.running=run;local out={};for _,b in ipairs(vim.api.nvim_list_bufs()) do if vim.api.nvim_buf_get_name(b)==f and #vim.fn.win_findbuf(b)>0 then out[#out+1]=b end end;return out`, &bufs, file, s.running != nil)
	for _, b := range bufs {
		m.databaseBufferLens(b)
	}
}

// dbConsoleConn is the connection a SQL buffer runs on: its "-- @conn <id>"
// header, else the choice made once for the file in this session.
func (s *dbConsoleState) consoleConn(file string, lines []string) string {
	for _, line := range lines {
		if id, ok := strings.CutPrefix(strings.TrimSpace(line), "-- @conn "); ok {
			return strings.TrimSpace(id)
		}
	}
	return s.attached[file]
}

// dbStatementSpans maps each statement of text to the 1-based line where
// its SQL starts (comments before it skipped) and its byte range.
type dbStatementSpan struct {
	sql        string
	line       int
	start, end int
}

func dbStatementSpans(text, driver string) []dbStatementSpan {
	sqls, err := dbclient.Split(text, driver)
	if err != nil {
		return nil
	}
	var out []dbStatementSpan
	offset := 0
	for _, sql := range sqls {
		at := strings.Index(text[offset:], sql)
		if at < 0 {
			continue
		}
		at += offset
		offset = at + len(sql)
		// The first line that is not a comment or blank carries the lens.
		first := at
		for _, l := range strings.SplitAfter(sql, "\n") {
			t := strings.TrimSpace(l)
			if t != "" && !strings.HasPrefix(t, "--") {
				break
			}
			first += len(l)
		}
		if first >= offset {
			first = at
		}
		out = append(out, dbStatementSpan{sql: sql, line: strings.Count(text[:first], "\n") + 1, start: at, end: offset})
	}
	return out
}

func (m *Model) databaseBufferLens(buf int) {
	if m.nvim == nil {
		return
	}
	file, lines, ok := m.httpBufText(buf)
	if !ok || !strings.EqualFold(lastExt(file), ".sql") {
		return
	}
	m.ensureDatabasesLoaded()
	s := m.dbConsole()
	id := s.consoleConn(file, lines)
	rows := []map[string]any{}
	if c, ok := m.databases.connection(id); ok {
		label := dbGlyphRun() + " run"
		if k := m.httpSendKeyLabel(); k != "" {
			label += " " + k
		}
		label += " · " + id
		for _, sp := range dbStatementSpans(strings.Join(lines, "\n"), c.Driver) {
			text := label
			if status := s.lens[file+"\x00"+sp.sql]; status != "" {
				text += " · " + status
			}
			rows = append(rows, map[string]any{"line": sp.line, "text": sanitizeDBText(dbRedact(text))})
		}
	}
	_ = m.nvim.ExecLuaArgs(`local b,rows=...;local out={};for _,r in ipairs(rows) do out[r.line]=r.text end;_G._termocode_sql.rows[b]=out;vim.cmd('redraw')`, buf, rows)
}

func lastExt(p string) string {
	if i := strings.LastIndexByte(p, '.'); i >= 0 && !strings.ContainsAny(p[i:], `/\`) {
		return p[i:]
	}
	return ""
}

func init() {
	registerNotifyHandler("termocode_db_buf", func(m *Model, args []any) tea.Cmd { m.databaseBufferLens(notifyArgInt(args, 0)); return nil })
	registerNotifyHandler("termocode_db_cancel", func(m *Model, _ []any) tea.Cmd { return m.databaseCancelRun() })
}
