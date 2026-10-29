package app

import (
	"encoding/json"
	"os"
)

// Terminal restore (G-RST). A detach keeps every shell alive in the
// session server; a restart of termocode ends them. So the terminal tabs
// of a workspace, their split panes, folders and focus are kept in the
// workspace session, and the next start opens fresh shells in the same
// places. Output, scrollback and environment values are never kept. A
// command line is kept only with terminal_restore_commands; otherwise only
// the name of the program that was running, for a hint.

// configBool reads the bool key of config.json; def when it is missing or
// the file can not be read.
func configBool(key string, def bool) bool {
	path, err := settingsConfigPath()
	if err != nil {
		return def
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return def
	}
	var cfg map[string]json.RawMessage
	if json.Unmarshal(b, &cfg) != nil {
		return def
	}
	var v bool
	if raw, ok := cfg[key]; !ok || json.Unmarshal(raw, &v) != nil {
		return def
	}
	return v
}

// terminalRestoreOn reads config.json "terminal_restore" (default true).
func terminalRestoreOn() bool { return configBool("terminal_restore", true) }

// terminalRestoreCommandsOn reads config.json "terminal_restore_commands"
// (default false): keep the last command line of a pane that was running
// one, so Enter can run it again after a restart.
func terminalRestoreCommandsOn() bool { return configBool("terminal_restore_commands", false) }

func init() {
	aiSettingPolicies["terminal_restore"] = aiNever("decides what is kept of the user's terminals between starts; a file-only switch the user edits in config.json")
	aiSettingPolicies["terminal_restore_commands"] = aiNever("keeps command lines on disk and re-runs them; a file-only switch the user edits in config.json")
}
