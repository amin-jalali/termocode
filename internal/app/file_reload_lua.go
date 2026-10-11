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
const fileReloadLua = `
vim.g.termocode_last_reload = ''
vim.g.termocode_reload_seq = 0
vim.api.nvim_create_augroup('TermocodeFileReload', { clear = true })
vim.api.nvim_create_autocmd('FileChangedShell', {
	group = 'TermocodeFileReload',
	callback = function(args)
		local buf = args.buf or 0
		if vim.v.fcs_reason == 'deleted' or vim.bo[buf].modified then
			vim.v.fcs_choice = ''
			local name = vim.fn.fnamemodify(args.file or '', ':t')
			vim.schedule(function()
				vim.notify('termocode: ' .. name .. ' changed on disk; your unsaved edits are kept', vim.log.levels.WARN)
			end)
		else
			vim.v.fcs_choice = 'reload'
		end
	end,
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
