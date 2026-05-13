package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/picker"
	"termocode/internal/toast"
)

// UserCommand is one shell command the user has registered to appear in
// the palette. Each entry becomes a "User: <Title>" row that runs `Cmd`
// in the workspace dir when picked.
type UserCommand struct {
	ID    string `json:"id"`    // unique id, used as the picker entry's ID
	Title string `json:"title"` // shown in the palette
	Cmd   string `json:"cmd"`   // the shell command to run
	// CWD overrides the working directory. Empty string = workspace cwd.
	CWD string `json:"cwd,omitempty"`
}

// userCommandsPath returns the on-disk location for user-defined commands.
// We use a separate file from `config.json` so editing it can never
// corrupt the theme/persistence config.
func userCommandsPath() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "termocode", "commands.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "termocode", "commands.json"), nil
}

// loadUserCommands reads the registered commands. Missing or malformed
// files return an empty list — never block startup on bad config.
func loadUserCommands() []UserCommand {
	path, err := userCommandsPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cmds []UserCommand
	if err := json.Unmarshal(data, &cmds); err != nil {
		return nil
	}
	return cmds
}

// userCommandsItems returns the user commands as picker items so the
// palette enumerator can splice them in alongside the built-in actions.
func userCommandsItems() []picker.Item {
	cmds := loadUserCommands()
	if len(cmds) == 0 {
		return nil
	}
	items := make([]picker.Item, 0, len(cmds))
	for _, c := range cmds {
		items = append(items, picker.Item{
			ID:    "user-" + c.ID,
			Title: "User: " + c.Title,
			Hint:  truncateHint(c.Cmd, 40),
		})
	}
	return items
}

// runUserCommand executes the picked entry. Output (combined stdout+stderr)
// is shown in the preview overlay, with a green "✓" or red "✘" prefix in
// the title based on exit status.
func (m *Model) runUserCommand(id string) tea.Cmd {
	target := strings.TrimPrefix(id, "user-")
	var cmd UserCommand
	for _, c := range loadUserCommands() {
		if c.ID == target {
			cmd = c
			break
		}
	}
	if cmd.Cmd == "" {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Command not found", target)
		return toastCmd
	}
	cwd := cmd.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return func() tea.Msg {
		// We use sh -c so users can pipe / chain commands without us having
		// to parse arguments ourselves. Trade-off: depends on a POSIX shell
		// being on PATH (true for any termocode-supported platform).
		c := exec.Command("sh", "-c", cmd.Cmd)
		c.Dir = cwd
		out, err := c.CombinedOutput()
		body := string(out)
		if len(body) > 200_000 {
			body = body[:200_000] + "\n…[truncated]…"
		}
		title := fmt.Sprintf("User · %s", cmd.Title)
		if err != nil {
			title = "✘ " + title + " — FAIL"
		} else {
			title = "✓ " + title
		}
		return PreviewMsg{Title: title, Body: colourTestOutput(body)}
	}
}

func truncateHint(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
