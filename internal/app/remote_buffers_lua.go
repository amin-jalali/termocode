package app

// remoteBuffersLua turns a buffer named by a path under the container
// workspace root into a remote buffer (remote_buffers.go). Arg: the
// root. BufReadCmd asks Go for the text (termocode_fs_read) and
// BufWriteCmd sends it back (termocode_fs_write); nvim itself never
// touches a local file of that name. The buffer stays a normal file
// buffer (empty buftype), so the editor treats it like any other file.
//
// Safety: a buffer whose read failed is locked (not modifiable) and is
// never saved, so an empty buffer cannot overwrite the real file. A
// failed save raises an error (the Save command reports it) and the
// buffer keeps its text and stays modified.
const remoteBuffersLua = `
local root = ...
local pat = (root == '/' and '' or root) .. '/*'
local g = vim.api.nvim_create_augroup('termocode_remote_buffers', { clear = true })

local function req(method, ...)
  local ch = vim.g.termocode_channel
  if not ch or ch <= 0 then return { ok = false, err = 'termocode is not connected.' } end
  local ok, r = pcall(vim.rpcrequest, ch, method, ...)
  if not ok then return { ok = false, err = tostring(r) } end
  return r
end

local function tail(p) return vim.fn.fnamemodify(p, ':t') end

vim.api.nvim_create_autocmd('BufReadCmd', { group = g, pattern = pat, callback = function(ev)
  local b = ev.buf
  local name = vim.api.nvim_buf_get_name(b)
  vim.b[b].termocode_remote = true
  vim.bo[b].swapfile = false
  local r = req('termocode_fs_read', name)
  if not r.ok then
    vim.b[b].termocode_remote_failed = true
    vim.bo[b].modifiable = false
    vim.bo[b].modified = false
    vim.notify('termocode: cannot open ' .. tail(name) .. ': ' .. r.err, vim.log.levels.ERROR)
    return
  end
  vim.b[b].termocode_remote_failed = nil
  local text = r.text
  local lines = vim.split(text, '\n', { plain = true })
  local eol = #lines > 1 and lines[#lines] == ''
  if eol then table.remove(lines) end
  local dos = eol
  for _, l in ipairs(lines) do
    if l:sub(-1) ~= '\r' then dos = false break end
  end
  if dos then
    for i, l in ipairs(lines) do lines[i] = l:sub(1, -2) end
  end
  vim.bo[b].modifiable = true
  local ul = vim.bo[b].undolevels
  vim.bo[b].undolevels = -1 -- loading is not an edit to undo
  vim.api.nvim_buf_set_lines(b, 0, -1, false, lines)
  vim.bo[b].undolevels = ul
  vim.bo[b].fileformat = dos and 'dos' or 'unix'
  vim.bo[b].eol = eol
  vim.b[b].termocode_remote_empty = text == ''
  vim.b[b].termocode_remote_new = r.new or nil
  vim.bo[b].modified = false
  vim.api.nvim_exec_autocmds('BufReadPost', { buffer = b, modeline = false })
end })

vim.api.nvim_create_autocmd('BufWriteCmd', { group = g, pattern = pat, callback = function(ev)
  local b = ev.buf
  local name = vim.api.nvim_buf_get_name(b)
  local target = vim.fn.fnamemodify(ev.match, ':p')
  if vim.b[b].termocode_remote_failed then
    error('termocode: ' .. tail(name) .. ' could not be read, so it is not saved. Open it again first.', 0)
  end
  local lines = vim.api.nvim_buf_get_lines(b, 0, -1, false)
  local nl = vim.bo[b].fileformat == 'dos' and '\r\n' or '\n'
  local text = table.concat(lines, nl)
  if #lines == 1 and lines[1] == '' and vim.b[b].termocode_remote_empty then
    text = ''
  elseif vim.bo[b].eol or vim.bo[b].fixeol then
    text = text .. nl
  end
  local r = req('termocode_fs_write', target, text)
  if not r.ok then
    error('termocode: cannot save ' .. tail(target) .. ': ' .. r.err .. ' Your text is kept.', 0)
  end
  if target == name then
    vim.bo[b].modified = false
    vim.b[b].termocode_remote_new = nil
  end
  if r.warn and r.warn ~= '' then
    vim.notify('termocode: ' .. r.warn, vim.log.levels.WARN)
  end
  vim.api.nvim_exec_autocmds('BufWritePost', { buffer = b, modeline = false })
end })
`
