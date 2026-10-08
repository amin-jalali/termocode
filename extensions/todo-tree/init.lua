-- todo-tree — lists TODO / FIXME / HACK / XXX / NOTE comments of the
-- workspace in a sidebar panel. Click or Enter on a line opens it.
-- Uses ripgrep when installed, else grep. The scan runs as a background
-- job, so a big repo never blocks the editor.
local tc = termocode

local TAGS = { 'TODO', 'FIXME', 'HACK', 'XXX', 'NOTE' }
local TAG_STYLE = {
  TODO = 'info', FIXME = 'error', HACK = 'warning', XXX = 'warning', NOTE = 'muted',
}
local PATTERN = '\\b(' .. table.concat(TAGS, '|') .. ')\\b[:( ]'
local MAX_ITEMS = 2000

local state = {
  items = {},       -- { file, line, tag, text }
  rows = {},        -- panel row -> item (nil for headers)
  scanning = false,
  scanned_at = nil,
  error = nil,
}

local function scan_cmd()
  if vim.fn.executable('rg') == 1 then
    return { 'rg', '--no-heading', '--with-filename', '--line-number', '--color=never',
      '--max-columns=300', '-e', PATTERN, '.' }
  end
  return { 'grep', '-rInE', '--exclude-dir=.git', '--exclude-dir=node_modules',
    '--exclude-dir=vendor', '(' .. table.concat(TAGS, '|') .. ')[:( ]', '.' }
end

local function parse(lines)
  local items = {}
  for _, l in ipairs(lines) do
    local file, lnum, text = l:match('^(.-):(%d+):(.*)$')
    if file and text then
      local tag = nil
      for _, t in ipairs(TAGS) do
        if text:find(t, 1, true) then tag = t break end
      end
      if tag then
        file = file:gsub('^%./', '')
        local body = text:match(tag .. '[:(%s]+(.*)$') or text
        body = vim.trim(body):gsub('%*/$', ''):gsub('%-%->$', '')
        items[#items + 1] = { file = file, line = tonumber(lnum), tag = tag, text = vim.trim(body) }
        if #items >= MAX_ITEMS then break end
      end
    end
  end
  table.sort(items, function(a, b)
    if a.file ~= b.file then return a.file < b.file end
    return a.line < b.line
  end)
  return items
end

local function scan()
  if state.scanning then return end
  state.scanning = true
  state.error = nil
  tc.refresh('todos')
  local out = {}
  local ok, job = pcall(vim.fn.jobstart, scan_cmd(), {
    cwd = vim.fn.getcwd(),
    stdout_buffered = true,
    on_stdout = function(_, data) out = data or {} end,
    -- tc.wrap runs the callback with the same error guard as everything
    -- else the extension does.
    on_exit = tc.wrap(function(_, code)
      state.scanning = false
      -- rg / grep exit 1 = "no matches"
      if code > 1 then state.error = 'scan failed (exit ' .. code .. ')' end
      state.items = parse(out)
      state.scanned_at = os.date('%H:%M:%S')
      tc.refresh('todos')
    end, 'scan'),
  })
  if not ok or job <= 0 then
    state.scanning = false
    state.error = 'could not start ' .. scan_cmd()[1]
    tc.refresh('todos')
  end
end

tc.register_panel({
  id = 'todos',
  title = 'TODOs',
  icon = '✓',
  render = function(width)
    local lines, rows = {}, {}
    local function add(text, item)
      lines[#lines + 1] = text
      rows[#lines] = item
    end
    if state.error then add('{{error}}' .. state.error .. '{{/}}') end
    if state.scanning and #state.items == 0 then
      add('{{muted}}Scanning…{{/}}')
    elseif #state.items == 0 then
      add('{{muted}}No TODOs found.{{/}}')
      add('{{dim}}Enter here = rescan{{/}}')
    else
      local counts = {}
      for _, it in ipairs(state.items) do counts[it.tag] = (counts[it.tag] or 0) + 1 end
      local parts = {}
      for _, t in ipairs(TAGS) do
        if counts[t] then parts[#parts + 1] = '{{' .. TAG_STYLE[t] .. '}}' .. counts[t] .. ' ' .. t .. '{{/}}' end
      end
      add(table.concat(parts, '  '))
      local last_file = nil
      for _, it in ipairs(state.items) do
        if it.file ~= last_file then
          add('{{bold secondary}}' .. tc.escape(it.file) .. '{{/}}', { file = it.file, line = 1 })
          last_file = it.file
        end
        local prefix = string.format('  {{%s}}%s{{/}} {{dim}}%d{{/}} ', TAG_STYLE[it.tag], it.tag, it.line)
        local room = math.max(width - #it.tag - #tostring(it.line) - 5, 8)
        local text = it.text
        if vim.fn.strdisplaywidth(text) > room then text = vim.fn.strcharpart(text, 0, room - 1) .. '…' end
        add(prefix .. tc.escape(text), it)
      end
    end
    if state.scanned_at then add('{{dim}}scanned ' .. state.scanned_at .. ' · Enter here = rescan{{/}}') end
    state.rows = rows
    return lines
  end,
  on_select = function(row)
    local it = state.rows[row]
    if it then
      tc.open(it.file, it.line)
    else
      scan()
    end
  end,
})

tc.register_command({ id = 'refresh', title = 'TODO Tree: Rescan Workspace', run = scan })

tc.register_command({
  id = 'pick',
  title = 'TODO Tree: Go to TODO...',
  run = function()
    local items = {}
    for _, it in ipairs(state.items) do
      items[#items + 1] = { title = it.tag .. ': ' .. it.text, hint = it.file .. ':' .. it.line, item = it }
    end
    if #items == 0 then
      tc.notify('No TODOs found', 'info', 'Run "TODO Tree: Rescan Workspace" first.')
      return
    end
    tc.pick({ title = 'TODOs', items = items }, function(choice)
      if choice then tc.open(choice.item.file, choice.item.line) end
    end)
  end,
})

-- Rescan when a file is saved, and once at startup.
tc.on('save', function() scan() end)
tc.on('ready', function() scan() end)
