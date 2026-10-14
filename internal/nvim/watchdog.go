package nvim

// watchdogLua runs in nvim from the start (--cmd) and makes sure nvim
// never outlives termocode and never hangs in its exit:
//
//   - termocode is gone (nvim's parent pid changed): quit, and kill
//     everything if that has not worked after 1.5 s. On Linux nvim also
//     gets SIGTERM when termocode dies (Pdeathsig, proc_linux.go); this
//     covers macOS and an nvim that ignores the signal.
//   - nvim has been exiting for 3 s: kill everything. nvim 0.12 never
//     ends an exit that starts while it waits in vim.fn.system() (EOF on
//     the embed pipe, SIGTERM): its loop keeps running with v:exiting
//     set, and it ignores SIGTERM from then on.
//
// The parent check runs in the timer itself (a fast callback), so it
// works even when nvim no longer runs scheduled code; killing nvim there
// is the last resort. "Kill everything" first kills the processes of
// nvim's jobs (a killed nvim cannot stop them), then nvim.
const watchdogLua = `lua do
  local uv = vim.uv
  local parent, me = uv.os_getppid(), uv.os_getpid()
  local orphan_at, exit_at, asked
  local function kill_all()
    pcall(function()
      for _, ch in ipairs(vim.api.nvim_list_chans()) do
        if ch.stream == 'job' then
          local ok, pid = pcall(vim.fn.jobpid, ch.id)
          if ok and pid > 0 then
            uv.kill(-pid, 'sigkill')
            uv.kill(pid, 'sigkill')
          end
        end
      end
    end)
    uv.kill(me, 'sigkill')
  end
  local t = uv.new_timer()
  t:start(250, 250, function()
    local now = uv.hrtime() / 1e6
    if not orphan_at and uv.os_getppid() ~= parent then orphan_at = now end
    if orphan_at and now - orphan_at >= 3000 then uv.kill(me, 'sigkill') return end
    vim.schedule(function()
      local n = uv.hrtime() / 1e6
      if orphan_at and n - orphan_at >= 1500 then return kill_all() end
      if vim.v.exiting ~= vim.NIL then
        exit_at = exit_at or n
        if n - exit_at >= 3000 then return kill_all() end
      elseif orphan_at and not asked then
        asked = true
        pcall(vim.cmd, 'qall!')
      end
    end)
  end)
  t:unref()
end`
