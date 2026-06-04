package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/toast"
)

// configDir returns the directory termocode uses for its on-disk state
// (`recents.json`, `workspaces.json`, `commands.json`, `config.json`,
// `palette_recents.json`). Mirrors the lookup logic of the per-file
// helpers in recents.go / workspaces.go / etc.
func configDir() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "termocode"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "termocode"), nil
}

// editConfigFile opens (or creates) a config file in the editor as a
// regular buffer. Used by the palette's "Open Config..." entries so
// users can tweak their setup without leaving termocode.
func (m *Model) editConfigFile(name string, defaultBody string) tea.Cmd {
	dir, err := configDir()
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Config error", err.Error())
		return toastCmd
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Config error", err.Error())
		return toastCmd
	}
	path := filepath.Join(dir, name)
	// If the file doesn't exist yet, seed it with a sensible starting
	// body so the user has something to edit instead of staring at a
	// blank buffer wondering what the schema looks like.
	if _, err := os.Stat(path); os.IsNotExist(err) && defaultBody != "" {
		_ = os.WriteFile(path, []byte(defaultBody), 0o644)
	}
	if m.nvim != nil {
		m.ensureEditorWindowCurrent()
		_ = m.nvim.Command("edit " + path)
	}
	m.focus = FocusEditor
	return nil
}

// Default bodies used when seeding a fresh config file. Each one is also a
// concise schema reference for the user.

const defaultCommandsJSON = `[
  {
    "id": "build",
    "title": "Build",
    "cmd": "go build ./..."
  }
]
`

const defaultThemeConfigJSON = `{
  "theme": "vscode-dark-plus"
}
`

// prettyMarshal pretty-prints v as JSON. Used by the "Open Session" config
// editor — session.json is normally compact so reading it directly is
// painful; we re-format the user's view buffer with two-space indents.
// Save round-trips back through the standard JSON marshaler when the
// user writes the buffer.
func prettyMarshal(v interface{}) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil
	}
	return b
}
