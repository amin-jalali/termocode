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
local S={rows={},running=false,schema={},want=nil}
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

-- Completion (omnifunc, <C-x><C-o> and the auto-trigger of the other
-- omnifunc buffers): keywords, tables, views, routines, sequences and
-- schemas; after "alias." or "table." the columns. Pure Lua over
-- S.schema[buf], the table Go pushes from the tree (no round trip). Unknown
-- columns are requested once (termocode_db_cols) and the popup reopens
-- when Go has pushed them.
local KW={'select','from','where','group by','order by','having','limit','offset','insert into','values','update','set','delete from','join','inner join','left join','right join','full join','cross join','on','using','as','and','or','not','in','is','null','like','ilike','between','exists','distinct','union','union all','intersect','except','case','when','then','else','end','create table','alter table','drop table','create index','create view','add column','primary key','foreign key','references','default','unique','asc','desc','count','sum','avg','min','max','coalesce','cast','returning','with','explain','begin','commit','rollback','truncate','true','false','all','any','by','into','if exists','if not exists','cascade','nulls first','nulls last','current_date','current_timestamp','now','lower','upper','length','substr','replace','round','extract'}
local NOALIAS={where=1,on=1,set=1,join=1,left=1,right=1,inner=1,full=1,cross=1,group=1,order=1,limit=1,union=1,using=1,values=1,having=1,offset=1,returning=1,select=1,from=1,natural=1,window=1,fetch=1,['for']=1}
local function unq(s) return (s:gsub('^[\34\96]',''):gsub('[\34\96]$','')) end
local function quoted(S1,name)
 if name:match('^[a-z_][a-z0-9_$]*$') then return name end
 local q=S1.quote or '\34'
 return q..name:gsub(q,q..q)..q
end
local function statement_text()
 local row=api.nvim_win_get_cursor(0)[1]
 local lines=api.nvim_buf_get_lines(0,0,-1,false)
 local first,last=1,#lines
 for i=row-1,1,-1 do if lines[i]:match(';%s*$') then first=i+1;break end end
 for i=row,#lines do if lines[i]:match(';') and i>row then last=i;break end end
 return table.concat(lines,' ',first,last)
end
local function aliases(text)
 local map={}
 text=text:lower()
 for _,kw in ipairs({'from','join','update','into'}) do
  for tbl,after in text:gmatch(kw..'%s+([%w_.\34\96]+)()') do
   local name=unq(tbl:match('([^.]+)$') or tbl)
   map[name]=name
   local alias=text:match('^%s+as%s+([%w_]+)',after) or text:match('^%s+([%w_]+)',after)
   if alias and not NOALIAS[alias] then map[alias]=name end
  end
 end
 return map
end
function _G._termocode_sql_omni(findstart,base)
 local line=api.nvim_get_current_line()
 local col=api.nvim_win_get_cursor(0)[2]
 local before=line:sub(1,col)
 local word=before:match('[%w_$\34\96]*$')
 if findstart==1 then return col-#word end
 local buf=api.nvim_get_current_buf()
 local S1=S.schema[buf] or {tables={},cols={},schemas={}}
 local lb=unq(base):lower()
 local out={}
 local function add(w,menu,max)
  out[#out+1]={word=w,menu=menu}
 end
 local qual=before:sub(1,col-#word):match('([%w_$\34\96]+)%.$')
 if qual then
  local q=unq(qual):lower()
  local tbl=aliases(statement_text())[q] or q
  local cols=S1.cols and S1.cols[tbl]
  if cols then
   for _,c in ipairs(cols) do
    if c:lower():sub(1,#lb)==lb then add(quoted(S1,c),'column') end
   end
   return out
  end
  for _,t in ipairs(S1.tables or {}) do
   if t.n:lower()==tbl and (t.k=='table' or t.k=='view') then
    local w=api.nvim_win_get_cursor(0)
    S.want={buf=buf,table=tbl,row=w[1],col=w[2]}
    if _G.termocode_notify then _G.termocode_notify('termocode_db_cols',buf,tbl) end
    return out
   end
  end
  for _,sc in ipairs(S1.schemas or {}) do
   if sc:lower()==q then
    for _,t in ipairs(S1.tables or {}) do
     if (t.k=='table' or t.k=='view') and t.n:lower():sub(1,#lb)==lb then add(quoted(S1,t.n),t.k) end
     if #out>=300 then break end
    end
    return out
   end
  end
  return out
 end
 local upper=base:match('%u') and not base:match('%l')
 for _,k in ipairs(KW) do
  if k:sub(1,#lb)==lb then add(upper and k:upper() or k,'keyword') end
 end
 local n=0
 for _,t in ipairs(S1.tables or {}) do
  if t.n:lower():sub(1,#lb)==lb then
   add(quoted(S1,t.n),t.k=='func' and 'routine' or t.k)
   n=n+1
   if n>=300 then break end
  end
 end
 for _,sc in ipairs(S1.schemas or {}) do
  if n<300 and sc:lower():sub(1,#lb)==lb then add(quoted(S1,sc),'schema');n=n+1 end
 end
 return out
end
-- Go pushed new columns: open the popup again where the user waited.
function S.retrigger(buf,table_)
 local w=S.want
 if not w or w.buf~=buf or w.table~=table_ then return end
 S.want=nil
 if api.nvim_get_current_buf()~=buf or vim.fn.mode()~='i' or vim.fn.pumvisible()==1 then return end
 local pos=api.nvim_win_get_cursor(0)
 if pos[1]~=w.row or pos[2]~=w.col then return end
 api.nvim_feedkeys(api.nvim_replace_termcodes('<C-x><C-o>',true,false,true),'n',false)
end
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
 vim.bo[buf].omnifunc='v:lua._termocode_sql_omni'
 api.nvim_create_autocmd('BufEnter',{group=g,buffer=buf,callback=function() changed(buf) end})
 api.nvim_create_autocmd('BufWipeout',{group=g,buffer=buf,callback=function() S.rows[buf]=nil end})
 if not vim.b[buf].termocode_sql_attached then
  vim.b[buf].termocode_sql_attached=true
  api.nvim_buf_attach(buf,false,{on_lines=function() changed(buf) end,on_detach=function() pcall(function() vim.b[buf].termocode_sql_attached=nil end) end})
 end
 vim.keymap.set('n','<leader>x',function() if _G.termocode_notify then _G.termocode_notify('termocode_db_explain','plan') end end,{buffer=buf,silent=true,desc='Explain the statement'})
 vim.keymap.set('n','<leader>X',function() if _G.termocode_notify then _G.termocode_notify('termocode_db_explain','analyze') end end,{buffer=buf,silent=true,desc='Explain Analyze the statement'})
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
		if id, ok := strings.CutPrefix(strings.TrimSpace(line), "# @conn "); ok {
			return strings.TrimSpace(id) // a .redis console
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
		m.databasePushSchema(buf, c)
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
