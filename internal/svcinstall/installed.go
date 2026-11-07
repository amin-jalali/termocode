package svcinstall

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Installed lists the workspaces of the service files termocode wrote
// for goos under home (systemd user units on Linux, launch agents on
// macOS), sorted. A file without the Marker is not ours and is skipped,
// as is one that cannot be read or names no absolute workspace. It reads
// the files only: it never asks systemctl or launchctl.
func Installed(goos, home string) []string {
	var pat string
	switch goos {
	case "linux":
		pat = filepath.Join(home, ".config", "systemd", "user", "termocode-*.service")
	case "darwin":
		pat = filepath.Join(home, "Library", "LaunchAgents", "dev.termocode.*.plist")
	default:
		return nil
	}
	files, err := filepath.Glob(pat)
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil || !strings.Contains(string(b), Marker) {
			continue
		}
		if ws := workspaceOf(goos, string(b)); ws != "" && filepath.IsAbs(ws) {
			out = append(out, ws)
		}
	}
	sort.Strings(out)
	return out
}

// workspaceOf is the workspace a rendered service file starts (Render's
// escapes undone), or "".
func workspaceOf(goos, s string) string {
	if goos == "linux" {
		for _, l := range strings.Split(s, "\n") {
			rest, ok := strings.CutPrefix(strings.TrimRight(l, "\r"), "ExecStart=")
			if !ok {
				continue
			}
			_, ws, ok := strings.Cut(rest, ` __session-server "`)
			if !ok {
				return ""
			}
			if ws, ok = strings.CutSuffix(ws, `"`); !ok {
				return ""
			}
			return strings.NewReplacer("%%", "%", "$$", "$").Replace(ws)
		}
		return ""
	}
	_, rest, ok := strings.Cut(s, "<string>__session-server</string>")
	if !ok {
		return ""
	}
	_, rest, ok = strings.Cut(rest, "<string>")
	if !ok {
		return ""
	}
	ws, _, ok := strings.Cut(rest, "</string>")
	if !ok {
		return ""
	}
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(ws)
}
