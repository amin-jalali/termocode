package ext

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrExists is returned by Scaffold when the target folder already exists.
var ErrExists = errors.New("extension folder already exists")

// Scaffold creates <dir>/<name>/ with a commented init.lua and an
// extension.json, and returns the path of init.lua. It never overwrites.
func Scaffold(dir, name string) (string, error) {
	name = strings.TrimSpace(name)
	if !ValidName(name) {
		return "", fmt.Errorf("invalid name %q: use letters, digits, _ . - (max 64)", name)
	}
	root := filepath.Join(dir, name)
	if _, err := os.Stat(root); err == nil {
		return "", ErrExists
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	manifest := fmt.Sprintf("{\n  \"name\": %q,\n  \"version\": \"0.1.0\",\n  \"description\": \"\"\n}\n", name)
	if err := os.WriteFile(filepath.Join(root, ManifestFile), []byte(manifest), 0o644); err != nil {
		return "", err
	}
	initPath := filepath.Join(root, InitFile)
	body := strings.ReplaceAll(scaffoldInit, "NAME", name)
	if err := os.WriteFile(initPath, []byte(body), 0o644); err != nil {
		return "", err
	}
	return initPath, nil
}

const scaffoldInit = `-- NAME — a termocode extension.
--
-- This file runs inside termocode's embedded Neovim, so the whole vim.*
-- Lua API is available. The termocode API lives in the global "termocode".
-- After editing, run "Extensions: Reload" from the command palette (Ctrl+Shift+P).
-- Errors never block startup: they show as a toast and in
-- "Extensions: Show Log".
local tc = termocode

-- A palette entry: "Ext: Say Hello". Bind a key in keymap.json with
--   { "alt+h": "ext:NAME.hello" }
tc.register_command({
  id = 'hello',            -- becomes "NAME.hello"
  title = 'Say Hello',
  run = function()
    tc.prompt({ title = 'Hello', label = 'Your name:' }, function(value)
      if value then tc.notify('Hello, ' .. value .. '!') end
    end)
  end,
})

-- A status-bar item. text may be a string or a function.
tc.register_status_item({
  id = 'clock',
  text = function() return '{{muted}}' .. os.date('%H:%M') .. '{{/}}' end,
  command = 'NAME.hello',  -- run on click (optional)
})

-- A sidebar panel with its own activity-bar icon. render returns lines;
-- style text with {{token}}…{{/}} (tokens: accent, muted, error, bold, …).
local clicks = 0
tc.register_panel({
  id = 'main',
  title = 'NAME',
  icon = 'N',
  render = function(width, height)
    return {
      '{{accent bold}}NAME{{/}}',
      '',
      'Clicked ' .. clicks .. ' times.',
      '{{muted}}Press Enter or click a line.{{/}}',
    }
  end,
  on_select = function(row, line)
    clicks = clicks + 1
    tc.refresh('main')
  end,
})

-- Events: ready, save, open, changed, cursor, shutdown (or any nvim
-- autocmd name such as 'BufWritePost').
tc.on('save', function(ev)
  -- ev.path is the saved file
end)
`

// InstallBundled copies every top-level folder of src (an embedded FS of
// sample extensions) into dir. Existing folders are left alone. Returns
// the names that were installed.
func InstallBundled(src fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var installed []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		target := filepath.Join(dir, name)
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if _, err := os.Stat(target + ".disabled"); err == nil {
			continue
		}
		err := fs.WalkDir(src, name, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			out := filepath.Join(dir, filepath.FromSlash(p))
			if d.IsDir() {
				return os.MkdirAll(out, 0o755)
			}
			if path.Ext(p) == ".go" {
				return nil
			}
			b, err := fs.ReadFile(src, p)
			if err != nil {
				return err
			}
			return os.WriteFile(out, b, 0o644)
		})
		if err != nil {
			return installed, fmt.Errorf("%s: %w", name, err)
		}
		installed = append(installed, name)
	}
	return installed, nil
}
