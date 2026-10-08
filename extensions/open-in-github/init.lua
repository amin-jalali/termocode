-- open-in-github — opens the current file (and line, or selected lines)
-- on GitHub / GitLab / Bitbucket / Codeberg in the browser, or copies
-- the link. Uses the "origin" remote and the current commit, so the link
-- stays valid after new pushes.
local tc = termocode

local function git(dir, args)
  local cmd = { 'git', '-C', dir }
  vim.list_extend(cmd, args)
  local out = vim.fn.systemlist(cmd)
  if vim.v.shell_error ~= 0 then return nil end
  return out[1]
end

-- web_base turns a remote URL into https://host/owner/repo.
local function web_base(remote)
  if not remote or remote == '' then return nil end
  remote = remote:gsub('%.git$', ''):gsub('/$', '')
  -- git@host:owner/repo
  local host, path = remote:match('^[%w._-]+@([^:/]+):(.+)$')
  if not host then
    -- ssh://git@host[:port]/owner/repo, https://[user@]host/owner/repo
    host, path = remote:match('^%a[%w+.-]*://[^@/]*@?([^/:]+)[:%d]*/(.+)$')
    if not host then host, path = remote:match('^%a[%w+.-]*://([^/:]+)[:%d]*/(.+)$') end
  end
  if not host then return nil end
  return 'https://' .. host .. '/' .. path, host
end

local function encode_path(p)
  return (p:gsub('[^%w%-%._~/]', function(c) return string.format('%%%02X', c:byte()) end))
end

local function build_link()
  local file = vim.api.nvim_buf_get_name(0)
  if file == '' or vim.bo.buftype ~= '' then return nil, 'No file in the editor' end
  local dir = vim.fn.fnamemodify(file, ':h')
  local root = git(dir, { 'rev-parse', '--show-toplevel' })
  if not root then return nil, 'Not a git repository' end
  local base, host = web_base(git(root, { 'remote', 'get-url', 'origin' }))
  if not base then return nil, 'No usable "origin" remote' end
  local rev = git(root, { 'rev-parse', 'HEAD' }) or 'HEAD'
  local rel = vim.fn.fnamemodify(file, ':p'):sub(#root + 2)

  local l1 = vim.fn.line('.')
  local l2 = l1
  local mode = vim.fn.mode()
  if mode == 'v' or mode == 'V' or mode == '\22' then
    l1, l2 = vim.fn.line('v'), vim.fn.line('.')
    if l1 > l2 then l1, l2 = l2, l1 end
  end

  local url
  if host:find('gitlab', 1, true) then
    url = string.format('%s/-/blob/%s/%s#L%d', base, rev, encode_path(rel), l1)
    if l2 ~= l1 then url = url .. '-' .. l2 end
  elseif host:find('bitbucket', 1, true) then
    url = string.format('%s/src/%s/%s#lines-%d', base, rev, encode_path(rel), l1)
    if l2 ~= l1 then url = url .. ':' .. l2 end
  elseif host:find('codeberg', 1, true) then
    url = string.format('%s/src/commit/%s/%s#L%d', base, rev, encode_path(rel), l1)
    if l2 ~= l1 then url = url .. '-L' .. l2 end
  else
    url = string.format('%s/blob/%s/%s#L%d', base, rev, encode_path(rel), l1)
    if l2 ~= l1 then url = url .. '-L' .. l2 end
  end
  return url
end

local function open_url(url)
  if vim.ui and vim.ui.open then
    local ok = pcall(vim.ui.open, url)
    if ok then return true end
  end
  for _, opener in ipairs({ 'xdg-open', 'open', 'wslview' }) do
    if vim.fn.executable(opener) == 1 then
      vim.fn.jobstart({ opener, url }, { detach = true })
      return true
    end
  end
  return false
end

tc.register_command({
  id = 'open',
  title = 'Open in GitHub',
  run = function()
    local url, err = build_link()
    if not url then return tc.notify('Open in GitHub', 'warn', err) end
    if open_url(url) then
      tc.notify('Opened in browser', 'info', url)
    else
      pcall(vim.fn.setreg, '+', url)
      tc.notify('No browser opener found — link copied', 'warn', url)
    end
  end,
})

tc.register_command({
  id = 'copy',
  title = 'Copy GitHub Link',
  run = function()
    local url, err = build_link()
    if not url then return tc.notify('Copy GitHub Link', 'warn', err) end
    pcall(vim.fn.setreg, '+', url)
    tc.notify('Link copied', 'info', url)
  end,
})
