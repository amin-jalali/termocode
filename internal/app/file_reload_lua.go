package app

// fileReloadLua handles files that change on disk (git stash / checkout /
// pull, another editor):
//
//   - FileChangedShell answers nvim's question itself. Without it, a file
//     that changed on disk while its buffer has unsaved edits opens the
//     W12 "[O]K, (L)oad File" prompt; with cmdheight=0 nvim then waits for
//     a key, answers no RPC call and the whole app freezes (seen on
//     Git: Stash Changes after an edit). Unmodified buffers reload;
//     modified ones keep the user's edits and get a warning toast.
//   - FileChangedShellPost records the reloaded path so the Go side can
//     toast "Reloaded <file>".
//   - A timer runs :checktime every 2 s. nvim checks files only on focus,
//     buffer enter and idle after a key, and termocode gets no focus
//     events, so an idle file stayed stale after a pull in another
//     terminal; the user's next key then edited the old text, which a
//     save wrote over the new one.
const fileReloadLua = `
if _G._termocode_reload_timer then _G._termocode_reload_timer:stop() end
_G._termocode_reload_timer = (vim.uv or vim.loop).new_timer()
_G._termocode_reload_timer:start(2000, 2000, vim.schedule_wrap(function()
	if vim.fn.getcmdwintype() == '' and vim.fn.mode() ~= 'c' then
		pcall(vim.cmd, 'silent! checktime')
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
		if reason == 'deleted' or vim.bo[buf].modified then
			vim.v.fcs_choice = ''
			-- Warn once: a deleted file is reported on every check (the
			-- timer above runs one every 2 s).
			if vim.b[buf].termocode_disk_warned == reason then return end
			vim.b[buf].termocode_disk_warned = reason
			local name = vim.fn.fnamemodify(args.file or '', ':t')
			local msg = reason == 'deleted'
				and (name .. ' was deleted on disk; the editor keeps its text')
				or (name .. ' changed on disk; your unsaved edits are kept')
			vim.schedule(function()
				vim.notify('termocode: ' .. msg, vim.log.levels.WARN)
			end)
		else
			vim.v.fcs_choice = 'reload'
		end
	end,
})
vim.api.nvim_create_autocmd({ 'BufWritePost', 'BufReadPost' }, {
	group = 'TermocodeFileReload',
	callback = function(args) vim.b[args.buf].termocode_disk_warned = nil end,
})
vim.api.nvim_create_autocmd('FileChangedShellPost', {
	group = 'TermocodeFileReload',
	callback = function(args)
		local name = args.file or vim.api.nvim_buf_get_name(args.buf or 0)
		if name == nil or name == '' then return end
		vim.g.termocode_last_reload = name
		vim.g.termocode_reload_seq = (vim.g.termocode_reload_seq or 0) + 1
	end,
})
`
