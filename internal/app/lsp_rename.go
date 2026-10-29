package app

// lspRenameLua tells the running language servers that a file or folder
// moved on disk (explorer or AI rename). A server that asks for
// workspace/didRenameFiles gets it; the others get "old deleted, new
// created" (workspace/didChangeWatchedFiles), since nvim does not watch
// files for them. Without this a server can keep the old file in its
// view and fail requests for the new one (gopls: "no package metadata").
// Only servers whose folder holds the old path are told.
const lspRenameLua = `
local old, new = ...
if not (vim.lsp and vim.lsp.get_clients) then return end
local function under(p, root)
  return root ~= nil and root ~= '' and (p == root or p:sub(1, #root + 1) == root .. '/')
end
local oldUri, newUri = vim.uri_from_fname(old), vim.uri_from_fname(new)
-- 0.11 made notify a method (client:notify); 0.10 had client.notify.
local method = vim.fn.has('nvim-0.11') == 1
local function send(c, m, params)
  if method then pcall(c.notify, c, m, params) else pcall(c.notify, m, params) end
end
for _, c in ipairs(vim.lsp.get_clients()) do
  local roots = { c.root_dir }
  for _, f in ipairs(c.workspace_folders or {}) do roots[#roots + 1] = vim.uri_to_fname(f.uri) end
  local mine = false
  for _, r in ipairs(roots) do mine = mine or under(old, r) end
  if mine and not c:is_stopped() then
    local ws = (c.server_capabilities or {}).workspace or {}
    if ws.fileOperations and ws.fileOperations.didRename then
      send(c, 'workspace/didRenameFiles', { files = { { oldUri = oldUri, newUri = newUri } } })
    else
      send(c, 'workspace/didChangeWatchedFiles', { changes = {
        { uri = oldUri, type = 3 }, { uri = newUri, type = 1 } } })
    end
  end
end
`
