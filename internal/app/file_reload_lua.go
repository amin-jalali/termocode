package app

// fileReloadLua handles files that change on disk (git stash / checkout /
// pull, another editor):
//
//   - FileChangedShell answers nvim's question itself. Without it, a file
//     that changed on disk while its buffer has unsaved edits opens the
//     W12 "[O]K, (L)oad File" prompt; with cmdheight=0 nvim then waits for
//     a key, answers no RPC call and the whole app freezes (seen on
//     Git: Stash Changes after an edit). Unmodified buffers reload;
//     modified ones keep the user's edits and get a warning toast. A file
//     that grew past 64 MB is not reloaded either (it froze nvim).
//   - FileChangedShellPost records the reloaded path so the Go side can
//     toast "Reloaded <file>".
//   - _G._termocode_checktime_all checks every loaded file buffer one by
//     one. A plain :checktime looks only at buffers shown in a window and
//     waits while an RPC call runs, so the other tabs kept the old text
//     after a branch switch or Replace in Workspace.
//   - A timer runs it every 2 s. nvim checks files only on focus,
//     buffer enter and idle after a key, and termocode gets no focus
//     events, so an idle file stayed stale after a pull in another
//     terminal; the user's next key then edited the old text, which a
//     save wrote over the new one.
const fileReloadLua = `
-- A file that grew past this on disk (a log, a dump) is not read in:
-- reading 300 MB kept nvim busy for 20 s and the app hung.
_G._termocode_reload_max = 64 * 1024 * 1024
_G._termocode_checktime_all = function()
	for _, b in ipairs(vim.api.nvim_list_bufs()) do
		local name = vim.api.nvim_buf_get_name(b)
		if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buftype == '' and name ~= '' then
			-- 'autoread' reloads without asking FileChangedShell (below),
			-- so a big file gets it off and the handler keeps the text.
			local st = (vim.uv or vim.loop).fs_stat(name)
			if st and st.size > _G._termocode_reload_max then vim.bo[b].autoread = false end
			pcall(vim.cmd, 'silent! checktime ' .. b)
		end
	end
end
if _G._termocode_reload_timer then _G._termocode_reload_timer:stop() end
_G._termocode_reload_timer = (vim.uv or vim.loop).new_timer()
_G._termocode_reload_timer:start(2000, 2000, vim.schedule_wrap(function()
	if vim.fn.getcmdwintype() == '' and vim.fn.mode() ~= 'c' then
		_G._termocode_checktime_all()
	end
end))
vim.g.termocode_last_reload = ''
vim.g.termocode_reload_seq = 0
vim.api.nvim_create_augroup('TermocodeFileReload', { clear = true })
vim.api.nvim_create_autocmd('FileChangedShell', {
	group = 'TermocodeFileReload',
	callback = function(args)
		local buf = args.buf or 0
		local reason = vim.v.fcs_reason
		local st = reason ~= 'deleted' and (vim.uv or vim.loop).fs_stat(vim.api.nvim_buf_get_name(buf)) or nil
		local big = st ~= nil and st.size > _G._termocode_reload_max
		if reason == 'deleted' or big or vim.bo[buf].modified then
			vim.v.fcs_choice = ''
			-- Warn once: a deleted file is reported on every check (the
			-- timer above runs one every 2 s).
			local why = big and 'big' or reason
			if vim.b[buf].termocode_disk_warned == why then return end
			vim.b[buf].termocode_disk_warned = why
			local name = vim.fn.fnamemodify(args.file or '', ':t')
			local msg = reason == 'deleted'
				and (name .. ' was deleted on disk; the editor keeps its text')
				or big and (name .. ' changed on disk and is too big to reload (' .. math.floor(st.size / 1048576) .. ' MB); the editor keeps its text')
				or (name .. ' changed on disk; your unsaved edits are kept')
			vim.schedule(function()
				vim.notify('termocode: ' .. msg, vim.log.levels.WARN)
			end)
		else
			vim.v.fcs_choice = 'reload'
		end
	end,
})
-- termocode_mtime: the file's mtime when the buffer last matched it
-- (read, written, reloaded). Ctrl+S compares it with the disk
-- (save_conflict.go): :checktime inside an RPC call is postponed.
_G._termocode_mtime = function(buf)
	local st = (vim.uv or vim.loop).fs_stat(vim.api.nvim_buf_get_name(buf))
	return st and (st.mtime.sec .. '.' .. st.mtime.nsec) or nil
end
vim.api.nvim_create_autocmd({ 'BufWritePost', 'BufReadPost' }, {
	group = 'TermocodeFileReload',
	callback = function(args)
		vim.b[args.buf].termocode_disk_warned = nil
		vim.b[args.buf].termocode_mtime = _G._termocode_mtime(args.buf)
	end,
})
vim.api.nvim_create_autocmd('FileChangedShellPost', {
	group = 'TermocodeFileReload',
	callback = function(args)
		if args.buf and args.buf > 0 then vim.b[args.buf].termocode_mtime = _G._termocode_mtime(args.buf) end
		local name = args.file or vim.api.nvim_buf_get_name(args.buf or 0)
		if name == nil or name == '' then return end
		vim.g.termocode_last_reload = name
		vim.g.termocode_reload_seq = (vim.g.termocode_reload_seq or 0) + 1
	end,
})
`

// checkTimeAllLua reloads every open file that changed on disk (see
// _G._termocode_checktime_all above; a plain :checktime before the
// handlers are in).
// Remote buffers of a container workspace are read again too
// (remote_buffers_lua.go).
const checkTimeAllLua = `if _G._termocode_checktime_all then _G._termocode_checktime_all() else pcall(vim.cmd, 'silent! checktime') end
if _G._termocode_remote_reload then _G._termocode_remote_reload() end`

// nvimCheckTimeAll reloads the open files a git op, a replace or another
// program changed on disk, the hidden tabs too.
func (m *Model) nvimCheckTimeAll() {
	if m.nvim != nil {
		_ = m.nvim.ExecLua(checkTimeAllLua)
	}
}
