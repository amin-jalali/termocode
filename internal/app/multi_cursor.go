package app

import (
	"os"
	"path/filepath"
)

// vimVisualMultiRepo is the upstream URL for mg979/vim-visual-multi.
// Cloned with --depth 1 for speed; the plugin is a few hundred KB.
const vimVisualMultiRepo = "https://github.com/mg979/vim-visual-multi.git"

// pluginsDirEnv lets users (and tests) override where bootstrap clones land.
const pluginsDirEnv = "TERMOCODE_PLUGINS_DIR"

// pluginsDir returns the directory under which bootstrap-cloned nvim plugins
// live. Defaults to $XDG_DATA_HOME/termocode/plugins, falling back to
// ~/.local/share/termocode/plugins. The TERMOCODE_PLUGINS_DIR env var, if set,
// overrides everything (used in tests).
func pluginsDir() (string, error) {
	if override := os.Getenv(pluginsDirEnv); override != "" {
		return override, nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "termocode", "plugins"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "termocode", "plugins"), nil
}

// multiCursorSetupLua returns the Lua chunk that:
//  1. configures vim-visual-multi's key maps (must run BEFORE plugin load),
//  2. adds the plugin path to runtimepath,
//  3. lets nvim's built-in autoload pick up the plugin.
//
// The plugin's docs note that g:VM_maps must be set before the plugin is
// sourced; runtimepath manipulation triggers the load on next plugin call,
// and we explicitly :runtime to force it now.
func multiCursorSetupLua(pluginPath string) string {
	// Lua-side single-quoted string; pluginPath is a filesystem path we
	// control, so we just escape backslashes and single quotes defensively.
	escaped := luaEscape(pluginPath)
	return `
-- vim-visual-multi key bindings (must be set BEFORE the plugin loads).
vim.g.VM_default_mappings = 0
vim.g.VM_maps = {
  ['Add Cursor Up']      = '<C-A-Up>',
  ['Add Cursor Down']    = '<C-A-Down>',
  ['Find Under']         = '<C-d>',
  ['Find Subword Under'] = '<C-d>',
  ['Select All']         = '<C-A-d>',
  ['Skip Region']        = '<C-x>',
  ['Remove Region']      = '<C-S-x>',
  ['Exit']               = '<Esc>',
}
-- VM is mouse-driven enough; silence the leader-based defaults.
-- (In a Lua single-quoted string, '\\' is one literal backslash.)
vim.g.VM_leader = '\\'
vim.g.VM_silent_exit = 1
vim.g.VM_show_warnings = 0

-- Bootstrap: prepend the plugin to runtimepath, then source its plugin file.
local plugin_path = '` + escaped + `'
if vim.fn.isdirectory(plugin_path) == 1 then
  vim.opt.rtp:prepend(plugin_path)
  -- Force-load the plugin file so its <Plug> mappings register now.
  pcall(vim.cmd, 'runtime! plugin/visual-multi.vim')

  -- VM expects raw nvim_input style mappings; alias the Ctrl+Alt arrows so
  -- terminals that emit <C-M-Up> get the same effect as <C-A-Up>.
  vim.keymap.set({'n', 'i'}, '<C-M-Up>',   '<C-A-Up>',   { remap = true, silent = true })
  vim.keymap.set({'n', 'i'}, '<C-M-Down>', '<C-A-Down>', { remap = true, silent = true })
end
`
}

// luaEscape escapes a string for embedding inside a single-quoted Lua literal.
func luaEscape(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' || c == '\'' {
			out = append(out, '\\')
		}
		out = append(out, c)
	}
	return string(out)
}
