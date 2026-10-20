package app

// dbPageLua wires up the "SQL Result" page (plan F5), like the HTTP
// Response page: the buffer holds the real text (so / and yank work), a
// decoration provider colours it with ephemeral extmarks for the visible
// rows only (idle costs 0), and keys notify termocode_db_key with the
// result row and column under the cursor; Go does the work.
//
// Loaded once during attach (model.go::attachCmd) and on first use.
const dbPageLua = `
if _G._termocode_db_open then return end
local api=vim.api
local D={hl={},first=0,bounds={}}
_G._termocode_db=D
local ns=api.nvim_create_namespace('termocode_db_page')
local function the_buf()
 local b=vim.g.termocode_db_buf
 if b and api.nvim_buf_is_valid(b) then return b end
end
api.nvim_set_decoration_provider(ns,{
 on_win=function(_,_,b) return b==vim.g.termocode_db_buf end,
 on_line=function(_,_,b,row)
  local spans=D.hl[row+1]
  if not spans then return end
  for _,h in ipairs(spans) do
   pcall(api.nvim_buf_set_extmark,b,ns,row,h.s,{end_col=h.e,hl_group=h.g,ephemeral=true})
  end
 end,
})
local function notify(k)
 local pos=api.nvim_win_get_cursor(0)
 local row=-1
 if D.first>0 and pos[1]>=D.first then row=pos[1]-D.first end
 local x=vim.fn.virtcol('.')-1
 local col=-1
 for i,b in ipairs(D.bounds) do if x>=b-1 then col=i-1 end end
 if _G.termocode_notify then _G.termocode_notify('termocode_db_key',k,row,col) end
end
local function set_win(win)
 local o=vim.wo[win]
 o.number,o.relativenumber=false,false
 o.signcolumn,o.foldcolumn='no','0'
 pcall(function() o.statuscolumn='' end)
 o.cursorline,o.wrap,o.list=true,false,false
 o.winhighlight='CursorLine:TermocodeFindCursor'
end
local function put(b,s,e,lines)
 vim.bo[b].modifiable=true
 api.nvim_buf_set_lines(b,s,e,false,lines)
 vim.bo[b].modifiable=false
end
function _G._termocode_db_append(lines)
 local b=the_buf()
 if not b then return false end
 put(b,-1,-1,lines)
 return true
end
function _G._termocode_db_open(split,lines,hls,first,bounds,cursor,wrap)
 local b=the_buf()
 if not b then
  b=api.nvim_create_buf(true,true);vim.g.termocode_db_buf=b
  api.nvim_buf_set_name(b,'SQL Result')
  vim.bo[b].bufhidden='hide';vim.bo[b].swapfile=false
  for _,k in ipairs({']','[','r','h','f','o','c','C','v','y','q'}) do vim.keymap.set('n',k,function() notify(k) end,{buffer=b,silent=true,nowait=true}) end
  vim.keymap.set('n','gd',function() notify('gd') end,{buffer=b,silent=true})
  vim.keymap.set('n','<Tab>',function() notify('tab') end,{buffer=b,silent=true})
  vim.keymap.set('n','<Esc>',function() notify('esc') end,{buffer=b,silent=true})
  vim.keymap.set('n','<CR>',function() notify('v') end,{buffer=b,silent=true})
 end
 D.hl={}
 for _,h in ipairs(hls) do
  D.hl[h.l]=D.hl[h.l] or {}
  table.insert(D.hl[h.l],{s=h.s,e=h.e,g=h.g})
 end
 D.first=first;D.bounds=bounds
 put(b,0,-1,lines)
 local wins=vim.fn.win_findbuf(b)
 if #wins>0 then api.nvim_set_current_win(wins[1]) else
  if split then vim.cmd('rightbelow vsplit') end
  api.nvim_set_current_buf(b)
 end
 set_win(api.nvim_get_current_win())
 vim.wo.wrap=wrap and true or false
 pcall(api.nvim_win_set_cursor,0,{math.max(1,math.min(cursor,api.nvim_buf_line_count(b))),0})
 vim.cmd('stopinsert')
end
function _G._termocode_db_close()
 local b=the_buf()
 if not b then return end
 local wins=vim.fn.win_findbuf(b)
 for _,w in ipairs(wins) do
  if #api.nvim_tabpage_list_wins(0)>1 then pcall(api.nvim_win_close,w,false)
  else api.nvim_set_current_win(w);pcall(vim.cmd,'buffer #') end
 end
end
function _G._termocode_db_value(lines)
 local b=vim.g.termocode_db_value_buf
 if not (b and api.nvim_buf_is_valid(b)) then
  b=api.nvim_create_buf(true,true);vim.g.termocode_db_value_buf=b
  api.nvim_buf_set_name(b,'SQL Value')
  vim.bo[b].bufhidden='hide';vim.bo[b].swapfile=false
  vim.keymap.set('n','q',function() local r=the_buf(); if r then api.nvim_set_current_buf(r) end end,{buffer=b,silent=true,nowait=true})
 end
 put(b,0,-1,lines)
 api.nvim_set_current_buf(b)
 set_win(api.nvim_get_current_win())
 vim.cmd('stopinsert')
end
`
