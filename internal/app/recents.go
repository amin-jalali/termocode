package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RecentEntry is one row in the persisted recent-files list.
type RecentEntry struct {
	Path     string    `json:"path"`
	OpenedAt time.Time `json:"opened_at"`
}

const recentsCap = 20

// recentsPath mirrors theme.ConfigPath but with a separate filename. We
// keep recent files in their own file because they churn far more often
// than the theme config and we don't want to risk corrupting the latter.
func recentsPath() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "termocode", "recents.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "termocode", "recents.json"), nil
}

// loadRecents reads and parses the recents file. Missing/unreadable/corrupt
// files all return an empty slice — never block startup on bad config.
func loadRecents() []RecentEntry {
	path, err := recentsPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []RecentEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	return entries
}

// saveRecents writes the entries to disk. Errors are swallowed (best-effort).
func saveRecents(entries []RecentEntry) {
	path, err := recentsPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// pushRecent records that `path` was opened. Empty paths are ignored.
// Existing entries for the same path are removed and re-prepended so the
// newest is always at the front. The list is capped at recentsCap.
func pushRecent(path string) {
	if path == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	entries := loadRecents()
	out := make([]RecentEntry, 0, len(entries)+1)
	out = append(out, RecentEntry{Path: abs, OpenedAt: time.Now()})
	for _, e := range entries {
		if e.Path == abs {
			continue
		}
		out = append(out, e)
		if len(out) >= recentsCap {
			break
		}
	}
	saveRecents(out)
}

// formatRecentTime renders a relative-time string for the welcome list.
// Mirrors typical IDE conventions: "just now" / "Nm ago" / "Nh ago" /
// "Nd ago" / "Jan 2".
func formatRecentTime(t time.Time, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		return "just now"
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}
