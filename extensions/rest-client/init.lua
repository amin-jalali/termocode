-- rest-client — send HTTP requests written in .http / .rest files
-- (VS Code REST Client / JetBrains HTTP Client syntax). Needs curl.
--
--   * Panel "REST": the requests of the current (or last) .http file.
--     Click / Enter sends one; the last response is summarised on top.
--   * "Ext: REST: Send Request at Cursor"  — bind it in keymap.json:
--       { "ctrl+alt+r": "ext:rest-client.send" }
--   * "Ext: REST: Send Request..."         — pick from the file
--   * "Ext: REST: Show Last Response"
local tc = termocode
local parser = require('rest_client.parser')
local fmt = require('rest_client.format')

local MAX_BODY = 200 * 1024
local MAX_PRETTY = 100 * 1024

local state = {
  buf = nil,        -- last .http buffer seen
  requests = {},
  vars = {},
  rows = {},        -- panel row -> request
  running = nil,    -- request being sent
  last = nil,       -- { req, code, ms, size, headers, body, error }
}

local function is_http(path)
  return path:match('%.http$') or path:match('%.rest$')
end

local function reparse()
  local buf = state.buf
  if not (buf and vim.api.nvim_buf_is_valid(buf)) then
    state.requests, state.vars = {}, {}
    return
  end
  local src = table.concat(vim.api.nvim_buf_get_lines(buf, 0, -1, false), '\n')
  state.requests, state.vars = parser.parse(src)
end

local function track(ev)
  if ev.path ~= '' and is_http(ev.path) then
    state.buf = ev.buf
    reparse()
    tc.refresh('requests')
  end
end
tc.on('open', track)
tc.on('save', track)
tc.on('changed', function(ev)
  if ev.buf == state.buf then reparse(); tc.refresh('requests') end
end)

local function label(r)
  if r.name and r.name ~= '' then return r.name end
  return r.method .. ' ' .. r.url
end

local function response_text(last)
  local r = last.req
  local out = { r.method .. ' ' .. r.url }
  if last.error then
    out[#out + 1] = ''
    out[#out + 1] = 'ERROR: ' .. last.error
    return table.concat(out, '\n')
  end
  out[#out + 1] = string.format('→ %d %s · %d ms · %s', last.code, fmt.reason(last.code), last.ms, fmt.human_size(last.size))
  out[#out + 1] = ''
  for _, h in ipairs(last.headers) do out[#out + 1] = h end
  out[#out + 1] = ''
  local body = last.body or ''
  local ctype = ''
  for _, h in ipairs(last.headers) do
    local k, v = h:match('^([^:]+):%s*(.*)$')
    if k and k:lower() == 'content-type' then ctype = v:lower() end
  end
  if #body <= MAX_PRETTY and (ctype:find('json', 1, true) or fmt.looks_like_json(body)) then
    body = fmt.pretty_json(body)
  end
  out[#out + 1] = body
  return table.concat(out, '\n')
end

local function show_last()
  if not state.last then return tc.notify('No response yet', 'info') end
  local l = state.last
  local title = 'REST · ' .. label(l.req)
  if l.code then title = title .. ' · ' .. l.code .. ' ' .. fmt.reason(l.code) end
  tc.preview(title, response_text(l))
end

local function read_file(path, limit)
  local f = io.open(path, 'rb')
  if not f then return '' end
  local s = f:read(limit or '*a') or ''
  f:close()
  return s
end

local function send(req)
  if vim.fn.executable('curl') ~= 1 then
    return tc.notify('REST client needs curl', 'error', 'Install curl and try again.')
  end
  if state.running then
    return tc.notify('A request is still running', 'warn', label(state.running))
  end
  local r = parser.resolve(req, state.vars)
  local hdr_file, body_file = vim.fn.tempname(), vim.fn.tempname()
  local args = {
    'curl', '-sS', '--max-time', '30', '-X', r.method,
    '-D', hdr_file, '-o', body_file,
    '-w', '%{http_code} %{time_total} %{size_download}',
  }
  for _, k in ipairs(r.header_order) do
    vim.list_extend(args, { '-H', k .. ': ' .. r.headers[k] })
  end
  if r.body then vim.list_extend(args, { '--data-binary', '@-' }) end
  args[#args + 1] = r.url

  state.running = r
  tc.refresh('requests')
  local stdout, stderr = {}, {}
  local job = vim.fn.jobstart(args, {
    stdout_buffered = true,
    stderr_buffered = true,
    on_stdout = function(_, d) stdout = d or {} end,
    on_stderr = function(_, d) stderr = d or {} end,
    on_exit = tc.wrap(function(_, code)
      state.running = nil
      local last = { req = r, headers = {} }
      if code ~= 0 then
        last.error = vim.trim(table.concat(stderr, '\n'))
        if last.error == '' then last.error = 'curl exited with ' .. code end
      else
        local http, secs, size = table.concat(stdout, ''):match('(%d+) ([%d.]+) (%d+)')
        last.code = tonumber(http) or 0
        last.ms = math.floor((tonumber(secs) or 0) * 1000 + 0.5)
        last.size = tonumber(size) or 0
        -- keep only the final header block (after any 100 Continue)
        for line in (read_file(hdr_file) .. '\n'):gmatch('(.-)\r?\n') do
          if line:match('^HTTP/') then last.headers = { line }
          elseif line ~= '' then last.headers[#last.headers + 1] = line end
        end
        last.body = read_file(body_file, MAX_BODY)
        if last.size > MAX_BODY then
          last.body = last.body .. '\n… (truncated, ' .. fmt.human_size(last.size) .. ' total)'
        end
      end
      os.remove(hdr_file)
      os.remove(body_file)
      state.last = last
      tc.refresh('requests')
      show_last()
    end, 'response'),
  })
  if job <= 0 then
    state.running = nil
    tc.refresh('requests')
    return tc.notify('Could not start curl', 'error')
  end
  if r.body then
    vim.fn.chansend(job, r.body)
  end
  vim.fn.chanclose(job, 'stdin')
end

local function current_request()
  local buf = vim.api.nvim_get_current_buf()
  if is_http(vim.api.nvim_buf_get_name(buf)) then
    state.buf = buf
    reparse()
    return parser.at_line(state.requests, vim.api.nvim_win_get_cursor(0)[1])
  end
  if #state.requests == 1 then return state.requests[1] end
  return nil
end

tc.register_command({
  id = 'send',
  title = 'REST: Send Request at Cursor',
  run = function()
    local req = current_request()
    if req then return send(req) end
    if #state.requests == 0 then
      return tc.notify('No .http request found', 'warn', 'Open a .http or .rest file first.')
    end
    tc.run('pick')
  end,
})

tc.register_command({
  id = 'pick',
  title = 'REST: Send Request...',
  run = function()
    reparse()
    if #state.requests == 0 then
      return tc.notify('No .http request found', 'warn', 'Open a .http or .rest file first.')
    end
    local items = {}
    for _, r in ipairs(state.requests) do
      items[#items + 1] = { title = label(r), hint = r.method, req = r }
    end
    tc.pick({ title = 'Send Request', items = items }, function(choice)
      if choice then send(choice.req) end
    end)
  end,
})

tc.register_command({ id = 'last', title = 'REST: Show Last Response', run = show_last })

tc.register_panel({
  id = 'requests',
  title = 'REST',
  icon = '⇄',
  render = function(width)
    local lines, rows = {}, {}
    state.rows = rows
    local function add(text, row)
      lines[#lines + 1] = text
      rows[#lines] = row
    end
    if state.running then
      add('{{info}}⟳ sending{{/}} ' .. tc.escape(label(state.running)))
    elseif state.last then
      local l = state.last
      if l.error then
        add('{{error}}✘ ' .. tc.escape(l.error) .. '{{/}}', 'last')
      else
        add(string.format('{{%s bold}}%d{{/}} {{muted}}%s · %d ms · %s{{/}}',
          fmt.status_token(l.code), l.code, fmt.reason(l.code), l.ms, fmt.human_size(l.size)), 'last')
      end
      add('{{dim}}Enter here = show response{{/}}', 'last')
      add('')
    end
    if not (state.buf and vim.api.nvim_buf_is_valid(state.buf)) then
      add('{{muted}}Open a .http or .rest file.{{/}}')
      add('')
      add('{{dim}}GET https://httpbin.org/get{{/}}')
      add('{{dim}}Accept: application/json{{/}}')
      return lines
    end
    local name = vim.fn.fnamemodify(vim.api.nvim_buf_get_name(state.buf), ':t')
    add('{{bold secondary}}' .. tc.escape(name) .. '{{/}} {{dim}}' .. #state.requests .. ' requests{{/}}')
    for _, r in ipairs(state.requests) do
      local text = r.name and r.name ~= '' and r.name or r.url
      local room = math.max(width - #r.method - 4, 8)
      if vim.fn.strdisplaywidth(text) > room then text = vim.fn.strcharpart(text, 0, room - 1) .. '…' end
      add(string.format(' {{keyword}}%s{{/}} %s', r.method, tc.escape(text)), r)
    end
    return lines
  end,
  on_select = function(row)
    local target = state.rows[row]
    if target == 'last' then return show_last() end
    if type(target) == 'table' then send(target) end
  end,
})
