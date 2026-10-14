-- Fake nvim-dap for tests (Group K7): breakpoints only, no sessions. The
-- tests use the real nvim-dap when it is downloaded; this one runs when
-- not, so the breakpoint rows always run.
local bp = require('dap.breakpoints')
local M = { adapters = {}, configurations = {} }

local function auto()
  return setmetatable({}, { __index = function(t, k)
    local v = {}
    rawset(t, k, v)
    return v
  end })
end
M.listeners = { before = auto(), after = auto(), on_session = {}, on_config = {} }

function M.session() return nil end
function M.sessions() return {} end

local function here()
  return vim.api.nvim_get_current_buf(), vim.api.nvim_win_get_cursor(0)[1]
end

function M.set_breakpoint(condition, hit_condition, log_message)
  local buf, line = here()
  bp.set({ condition = condition, hit_condition = hit_condition, log_message = log_message }, buf, line)
end

function M.toggle_breakpoint()
  local buf, line = here()
  bp.toggle({}, buf, line)
end

function M.clear_breakpoints() bp.clear() end

function M.run() error('fake nvim-dap: no debug adapter in tests') end

return M
