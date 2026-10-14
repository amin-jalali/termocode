-- Fake nvim-dap breakpoints for tests (Group K7): the part of
-- require('dap.breakpoints') that termocode's dap_lua.go uses. The tests
-- use the real nvim-dap when it is downloaded; this one runs when not.
local M = {}
local bps = {} -- buf -> { [line] = { condition, hit_condition, log_message } }

local function sign_name(bp)
  if bp.log_message then return 'DapLogPoint' end
  if bp.condition or bp.hit_condition then return 'DapBreakpointCondition' end
  return 'DapBreakpoint'
end

local function place(buf, line, bp)
  pcall(vim.fn.sign_unplace, 'dap_breakpoints', { buffer = buf, id = line })
  if bp then
    pcall(vim.fn.sign_place, line, 'dap_breakpoints', sign_name(bp), buf, { lnum = line, priority = 21 })
  end
end

function M.get(bufexpr)
  local out = {}
  for buf, t in pairs(bps) do
    if bufexpr == nil or buf == bufexpr then
      local list = {}
      for line, bp in pairs(t) do
        table.insert(list, {
          line = line, condition = bp.condition, hitCondition = bp.hit_condition, logMessage = bp.log_message,
        })
      end
      table.sort(list, function(a, b) return a.line < b.line end)
      out[buf] = list
    end
  end
  return out
end

function M.set(opts, buf, line)
  opts = opts or {}
  bps[buf] = bps[buf] or {}
  local bp = { condition = opts.condition, hit_condition = opts.hit_condition, log_message = opts.log_message }
  bps[buf][line] = bp
  place(buf, line, bp)
end

function M.remove(buf, line)
  if bps[buf] then bps[buf][line] = nil end
  place(buf, line, nil)
end

function M.toggle(opts, buf, line)
  if bps[buf] and bps[buf][line] then
    M.remove(buf, line)
  else
    M.set(opts, buf, line)
  end
end

function M.clear()
  for buf, t in pairs(bps) do
    for line in pairs(t) do place(buf, line, nil) end
  end
  bps = {}
end

return M
