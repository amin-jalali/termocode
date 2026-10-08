-- Small helpers for showing HTTP responses.
local M = {}

-- pretty_json re-indents JSON text without decoding it, so key order and
-- number formatting stay exactly as the server sent them. Invalid JSON
-- comes back unchanged (best effort: it only moves whitespace).
function M.pretty_json(src, indent)
  indent = indent or '  '
  local out, depth, in_str, esc = {}, 0, false, false
  local function nl()
    out[#out + 1] = '\n' .. string.rep(indent, depth)
  end
  local i, n = 1, #src
  while i <= n do
    local c = src:sub(i, i)
    if in_str then
      out[#out + 1] = c
      if esc then
        esc = false
      elseif c == '\\' then
        esc = true
      elseif c == '"' then
        in_str = false
      end
    elseif c == '"' then
      in_str = true
      out[#out + 1] = c
    elseif c == '{' or c == '[' then
      -- keep empty {} / [] on one line
      local j = i + 1
      while j <= n and src:sub(j, j):match('%s') do j = j + 1 end
      local close = (c == '{') and '}' or ']'
      if src:sub(j, j) == close then
        out[#out + 1] = c .. close
        i = j
      else
        out[#out + 1] = c
        depth = depth + 1
        nl()
      end
    elseif c == '}' or c == ']' then
      depth = math.max(depth - 1, 0)
      nl()
      out[#out + 1] = c
    elseif c == ',' then
      out[#out + 1] = c
      nl()
    elseif c == ':' then
      out[#out + 1] = ': '
    elseif not c:match('%s') then
      out[#out + 1] = c
    end
    i = i + 1
  end
  return table.concat(out)
end

-- looks_like_json is a cheap check on the first non-blank byte.
function M.looks_like_json(s)
  local c = (s or ''):match('^%s*(.)')
  return c == '{' or c == '['
end

local REASONS = {
  [200] = 'OK', [201] = 'Created', [202] = 'Accepted', [204] = 'No Content',
  [301] = 'Moved Permanently', [302] = 'Found', [304] = 'Not Modified',
  [400] = 'Bad Request', [401] = 'Unauthorized', [403] = 'Forbidden', [404] = 'Not Found',
  [405] = 'Method Not Allowed', [409] = 'Conflict', [422] = 'Unprocessable Entity',
  [429] = 'Too Many Requests', [500] = 'Internal Server Error', [502] = 'Bad Gateway',
  [503] = 'Service Unavailable', [504] = 'Gateway Timeout',
}

function M.reason(code) return REASONS[code] or '' end

-- status_token maps a status code to a termocode markup token.
function M.status_token(code)
  if code >= 200 and code < 300 then return 'success' end
  if code >= 300 and code < 400 then return 'info' end
  if code >= 400 and code < 500 then return 'warning' end
  return 'error'
end

function M.human_size(bytes)
  bytes = tonumber(bytes) or 0
  if bytes < 1024 then return bytes .. ' B' end
  if bytes < 1024 * 1024 then return string.format('%.1f KB', bytes / 1024) end
  return string.format('%.1f MB', bytes / 1024 / 1024)
end

return M
