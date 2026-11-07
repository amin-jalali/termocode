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
-- CTE and FROM-subquery columns (DBX-3): read from the select list of
-- the group; WITH name [(a,b)] AS (...) and (SELECT ...) [AS] alias.
local KWSET={}
for _,k in ipairs(KW) do if not k:find(' ') then KWSET[k]=1 end end
local ID='[%w_\34\96]+'
local function top_split(s)
 local parts,depth,cur={},0,1
 for i=1,#s do
  local ch=s:sub(i,i)
  if ch=='(' then depth=depth+1 elseif ch==')' then depth=depth-1
  elseif ch==',' and depth==0 then parts[#parts+1]=s:sub(cur,i-1);cur=i+1 end
 end
 parts[#parts+1]=s:sub(cur)
 return parts
end
local function trim(s) return (s:gsub('^%s+',''):gsub('%s+$','')) end
local function select_cols(inner)
 local body=inner:match('^%s*[Ss][Ee][Ll][Ee][Cc][Tt]%s+(.*)$')
 if not body then return nil end
 body=body:gsub('^[Dd][Ii][Ss][Tt][Ii][Nn][Cc][Tt]%s+','')
 local depth,stop=0,#body+1
 for i=1,#body do
  local ch=body:sub(i,i)
  if ch=='(' then depth=depth+1 elseif ch==')' then depth=depth-1
  elseif depth==0 and i>1 and body:sub(i-1,i-1):match('%s') and body:sub(i,i+3):lower()=='from' and body:sub(i+4,i+4):match('[%s(]') then stop=i;break end
 end
 local cols={}
 for _,item in ipairs(top_split(body:sub(1,stop-1))) do
  item=trim(item)
  local name=item:match('%s[Aa][Ss]%s+('..ID..')$') or item:match('%)%s*('..ID..')$')
   or item:match('^[%w_.\34\96]+%s+('..ID..')$') or item:match('^'..ID..'%.('..ID..')$') or item:match('^('..ID..')$')
  if name and not name:match('^%d+$') and not KWSET[name:lower()] then cols[#cols+1]=unq(name) end
 end
 return cols
end
local function derived(text)
 local map,ctes={},{}
 local i=1
 while true do
  local s,e=text:find('%b()',i)
  if not s then break end
  local cols=select_cols(text:sub(s+1,e-1))
  if cols then
   local pre=text:sub(1,s-1)
   local name,list=pre:match('('..ID..')%s*(%b())%s+[Aa][Ss]%s*$')
   if not name then name=pre:match('('..ID..')%s+[Aa][Ss]%s*$') end
   if name and not KWSET[name:lower()] then
    if list then
     cols={}
     for _,c in ipairs(top_split(list:sub(2,-2))) do c=trim(c);if c~='' then cols[#cols+1]=unq(c) end end
    end
    name=unq(name):lower()
    map[name]=cols;ctes[#ctes+1]=name
   else
    local post=text:sub(e+1)
    local alias=post:match('^%s+[Aa][Ss]%s+([%w_]+)') or post:match('^%s+([%w_]+)')
    if alias then
     alias=alias:lower()
     if not NOALIAS[alias] and not KWSET[alias] then map[alias]=cols end
    end
   end
  end
  i=s+1
 end
 return map,ctes
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
  local text=statement_text()
  local tbl=aliases(text)[q] or q
  local der=derived(text)
  local dcols=der[q] or der[tbl]
  if dcols then
   for _,c in ipairs(dcols) do
    if c:lower():sub(1,#lb)==lb then add(quoted(S1,c),'column') end
   end
   return out
  end
  local cols=S1.cols and S1.cols[tbl]
  if cols then
   -- Type-aware (DBX-3): after "x.col = tbl." the columns of the same
   -- type family come first; the menu shows each column's type.
   local types=(S1.types and S1.types[tbl]) or {}
   local want=nil
   local lq,lc=before:sub(1,col-#word-#qual-1):match('([%w_$\34\96]+)%.([%w_$\34\96]+)%s*[=<>!]+%s*$')
   if lq then
    local al=aliases(text)
    local lt=al[unq(lq):lower()] or unq(lq):lower()
    want=S1.types and S1.types[lt] and S1.types[lt][unq(lc)]
   end
   local first,rest={},{}
   for _,c in ipairs(cols) do
    if c:lower():sub(1,#lb)==lb then
     local ty=types[c]
     local it={word=quoted(S1,c),menu=ty and ('column '..ty) or 'column'}
     if want and ty==want then first[#first+1]=it else rest[#rest+1]=it end
    end
   end
   for _,it in ipairs(first) do out[#out+1]=it end
   for _,it in ipairs(rest) do out[#out+1]=it end
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
 -- JOIN … ON (DBX-3): whole conditions from the foreign keys between
 -- the table just joined and the tables already in the statement.
 do
  local pre=before:sub(1,col-#word):lower()
  local jt,ja=pre:match('join%s+([%w_.\34\96]+)%s+as%s+([%w_]+)%s+on%s+$')
  if not jt then jt,ja=pre:match('join%s+([%w_.\34\96]+)%s+([%w_]+)%s+on%s+$') end
  if not jt then jt=pre:match('join%s+([%w_.\34\96]+)%s+on%s+$') end
  if jt and S1.fks then
   jt=unq(jt:match('([^.]+)$') or jt)
   ja=ja or jt
   local others={}
   for a,tn in pairs(aliases(statement_text())) do
    if a~=ja and not (tn==jt and a==jt) then
     if not others[tn] or others[tn]==tn then others[tn]=a end
    end
   end
   local conds={}
   for _,f in ipairs(S1.fks) do
    if f.t==jt and others[f.rt] then conds[#conds+1]=ja..'.'..f.c..' = '..others[f.rt]..'.'..f.rc end
    if f.rt==jt and others[f.t] then conds[#conds+1]=ja..'.'..f.rc..' = '..others[f.t]..'.'..f.c end
   end
   table.sort(conds)
   for _,c in ipairs(conds) do
    if c:lower():sub(1,#lb)==lb then add(c,'join') end
   end
  end
 end
 -- Live templates (DBX-6): "sel" offers SELECT * FROM ; with the cursor
 -- put where the table goes (CompleteDone below).
 if #lb>0 then
  for _,t in ipairs(S1.templates or {}) do
   if t.a:sub(1,#lb)==lb then out[#out+1]={word=t.t,abbr=t.a,menu='template',user_data={termocode_tpl=t.c}} end
  end
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
 local _,ctes=derived(statement_text())
 for _,c in ipairs(ctes) do
  if c:sub(1,#lb)==lb then add(quoted(S1,c),'cte') end
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
 api.nvim_create_autocmd('CompleteDone',{group=g,buffer=buf,callback=function()
  local it=vim.v.completed_item
  local ud=type(it)=='table' and it.user_data
  if type(ud)~='table' or not ud.termocode_tpl or type(it.word)~='string' then return end
  local pos=api.nvim_win_get_cursor(0)
  local start=pos[2]-#it.word
  if start<0 or api.nvim_get_current_line():sub(start+1,pos[2])~=it.word then return end
  api.nvim_win_set_cursor(0,{pos[1],start+ud.termocode_tpl})
 end})
 vim.keymap.set('n','<leader>x',function() if _G.termocode_notify then _G.termocode_notify('termocode_db_explain','plan') end end,{buffer=buf,silent=true,desc='Explain the statement'})
 vim.keymap.set('n','<leader>.',function() if _G.termocode_notify then _G.termocode_notify('termocode_db_quickfix') end end,{buffer=buf,silent=true,desc='Apply the SQL quick fix'})
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
	var inspectConn *dbclient.Conn
	if c, ok := m.databases.connection(id); ok {
		inspectConn = &c
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
	m.databaseInspectBuffer(buf, lines, inspectConn)
}

func lastExt(p string) string {
	if i := strings.LastIndexByte(p, '.'); i >= 0 && !strings.ContainsAny(p[i:], `/\`) {
		return p[i:]
	}
	return ""
}

func init() {
	registerNotifyHandler("termocode_db_buf", func(m *Model, args []any) tea.Cmd {
		buf := notifyArgInt(args, 0)
		m.databaseBufferLens(buf)
		return m.databaseBackgroundSchema(buf)
	})
	registerNotifyHandler("termocode_db_cancel", func(m *Model, _ []any) tea.Cmd { return m.databaseCancelRun() })
}
