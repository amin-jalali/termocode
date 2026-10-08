-- Parser for the .http / .rest file format (VS Code REST Client /
-- JetBrains HTTP Client compatible). Port of mobocode's
-- lib/core/rest/http_file_parser.dart + HttpRequestDef.resolve.
--
--   @host = https://example.com        file variable (before any request)
--   ### Optional request name
--   # comment line (ignored)
--   # @name myRequest
--   GET {{host}}/api/items
--   Accept: application/json
--
--   ###
--   POST {{host}}/api/items
--   Content-Type: application/json
--
--   { "name": "{{itemName}}" }
--
-- Rules (same as the Dart parser):
--   * Requests are separated by lines starting with ###.
--   * Lines starting with # (outside a body) are comments; "# @name x"
--     (or "// @name x") sets the request name.
--   * The first non-comment, non-blank line of a block is METHOD URL
--     (an optional trailing HTTP/x.y is dropped). A line with no method
--     ("https://x") means GET.
--   * Following non-blank lines up to the first blank line are headers.
--   * Everything after that blank line is the body; trailing blank lines
--     are stripped.
--   * "### Title" names the next request.
-- Added for termocode: "@name = value" lines define variables used as
-- {{name}} in URL, headers and body; requests remember their 1-based
-- start / end line so "send request at cursor" works.
local M = {}

local METHODS = {
  GET = true, POST = true, PUT = true, PATCH = true, DELETE = true,
  HEAD = true, OPTIONS = true, TRACE = true, CONNECT = true,
}

local function trim(s) return (s:gsub('^%s+', ''):gsub('%s+$', '')) end

local function is_comment(line)
  return line:sub(1, 1) == '#' or line:sub(1, 2) == '//'
end

local function comment_text(line)
  if line:sub(1, 2) == '//' then return trim(line:sub(3)) end
  return trim(line:sub(2))
end

-- split_blocks returns { { lines = {...}, first = <line no of lines[1]>,
-- name = <### title or nil> } }.
local function split_blocks(src)
  local blocks = {}
  local cur = { lines = {}, first = 1, name = nil }
  local n = 0
  for raw in (src .. '\n'):gmatch('(.-)\n') do
    n = n + 1
    local line = raw:gsub('\r$', '')
    if line:sub(1, 3) == '###' then
      blocks[#blocks + 1] = cur
      local after = trim(line:sub(4))
      cur = { lines = {}, first = n + 1, name = after ~= '' and after or nil }
    else
      cur.lines[#cur.lines + 1] = line
    end
  end
  blocks[#blocks + 1] = cur
  return blocks, n
end

local function parse_block(block, vars)
  local lines, i = block.lines, 1
  local name = block.name
  local function blank(l) return trim(l) == '' end

  -- leading blanks, comments, @name directives and @var = value lines
  while i <= #lines do
    local l = lines[i]
    if blank(l) then
      i = i + 1
    elseif is_comment(l) then
      local c = comment_text(l)
      if c:sub(1, 5) == '@name' then name = trim(c:sub(6)) end
      i = i + 1
    else
      local k, v = l:match('^@([%w_.-]+)%s*=%s*(.-)%s*$')
      if k then
        vars[k] = v
        i = i + 1
      else
        break
      end
    end
  end
  if i > #lines then return nil end

  local start_line = block.first + i - 1
  local request_line = trim(lines[i])
  i = i + 1
  local method, url = request_line:match('^(%S+)%s+(.+)$')
  if method and METHODS[method:upper()] then
    method = method:upper()
  elseif request_line:match('^%a[%w+.-]*://') or request_line:match('^{{') then
    method, url = 'GET', request_line
  else
    return nil -- malformed
  end
  url = trim((url:gsub('%s+HTTP/%S+$', '')))
  if url == '' then return nil end

  local headers, order = {}, {}
  while i <= #lines and not blank(lines[i]) do
    local h = lines[i]
    i = i + 1
    if not is_comment(h) then
      local key, value = h:match('^([^:]+):(.*)$')
      if key then
        key = trim(key)
        if key ~= '' then
          if headers[key] == nil then order[#order + 1] = key end
          headers[key] = trim(value)
        end
      end
    end
  end
  if i <= #lines and blank(lines[i]) then i = i + 1 end

  local body = nil
  local last = #lines
  while last >= i and blank(lines[last]) do last = last - 1 end
  if last >= i then body = table.concat(lines, '\n', i, last) end

  return {
    method = method,
    url = url,
    headers = headers,
    header_order = order,
    body = body,
    name = name,
    start_line = start_line,
    end_line = block.first + #lines - 1,
  }
end

-- parse returns requests, vars. vars holds every "@k = v" line seen.
function M.parse(src)
  local vars = {}
  local out = {}
  local blocks = split_blocks(src or '')
  for _, b in ipairs(blocks) do
    local r = parse_block(b, vars)
    if r then out[#out + 1] = r end
  end
  return out, vars
end

-- substitute replaces {{name}} with vars[name] (unknown names stay as-is).
-- Values may reference other variables, resolved up to 5 levels deep.
function M.substitute(s, vars)
  if s == nil then return nil end
  for _ = 1, 5 do
    local changed = false
    s = s:gsub('{{%s*([%w_.-]+)%s*}}', function(k)
      local v = vars[k]
      if v == nil then return nil end
      changed = true
      return v
    end)
    if not changed then break end
  end
  return s
end

-- resolve returns a copy of req with every {{var}} substituted.
function M.resolve(req, vars)
  local headers = {}
  for k, v in pairs(req.headers) do headers[k] = M.substitute(v, vars) end
  return {
    method = req.method,
    url = M.substitute(req.url, vars),
    headers = headers,
    header_order = req.header_order,
    body = M.substitute(req.body, vars),
    name = req.name,
    start_line = req.start_line,
    end_line = req.end_line,
  }
end

-- at_line returns the request whose block contains line (1-based).
function M.at_line(requests, line)
  local best = nil
  for _, r in ipairs(requests) do
    if r.start_line <= line then best = r end
  end
  if best and line > best.end_line then
    -- cursor is in the separator area after a request: still take it
    return best
  end
  return best or requests[1]
end

-- serialize writes requests back to .http text (Dart serializeHttpFile).
function M.serialize(requests)
  local out = {}
  for idx, r in ipairs(requests) do
    if idx > 1 then out[#out + 1] = '###' end
    if r.name and r.name ~= '' then out[#out + 1] = '### ' .. r.name end
    out[#out + 1] = r.method .. ' ' .. r.url
    for _, k in ipairs(r.header_order or {}) do out[#out + 1] = k .. ': ' .. r.headers[k] end
    if r.body and r.body ~= '' then
      out[#out + 1] = ''
      out[#out + 1] = r.body
    end
  end
  return table.concat(out, '\n') .. '\n'
end

return M
