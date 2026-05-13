package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"termocode/internal/picker"
)

// paletteRecentsCap is the max number of recent palette commands we keep.
// Showing too many at the top would push the regular alphabetic list out of
// view; ten is enough to capture "the things I use this hour".
const paletteRecentsCap = 10

// loadPaletteRecents returns the recent-palette-action ID list, newest first.
// Errors and corrupt files yield an empty list — never block palette open.
func loadPaletteRecents() []string {
	path, err := paletteRecentsPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil
	}
	return ids
}

func paletteRecentsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "palette_recents.json"), nil
}

// pushPaletteRecent records that `id` was just dispatched. Existing entries
// for the same id are removed and re-prepended so the freshest command is
// always at the front.
func pushPaletteRecent(id string) {
	if id == "" {
		return
	}
	existing := loadPaletteRecents()
	out := make([]string, 0, len(existing)+1)
	out = append(out, id)
	for _, e := range existing {
		if e == id {
			continue
		}
		out = append(out, e)
		if len(out) >= paletteRecentsCap {
			break
		}
	}
	path, err := paletteRecentsPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(out)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// promoteRecents reorders `items` so that any with IDs in the recent list
// appear first, in recency order. Items not in the recent list keep their
// relative order. We mark recent ones in the Hint with a "recent" tag so
// the user can tell why they're at the top.
func promoteRecents(items []picker.Item, recents []string) []picker.Item {
	if len(recents) == 0 || len(items) == 0 {
		return items
	}
	recentSet := make(map[string]int, len(recents))
	for i, id := range recents {
		recentSet[id] = i
	}
	pinned := make([]picker.Item, 0, len(recents))
	other := make([]picker.Item, 0, len(items))
	for _, it := range items {
		if _, ok := recentSet[it.ID]; ok {
			// Mark it for visual clarity. Don't blow away an existing hint;
			// just prefix.
			if it.Hint == "" {
				it.Hint = "recent"
			} else {
				it.Hint = "recent · " + it.Hint
			}
			pinned = append(pinned, it)
		} else {
			other = append(other, it)
		}
	}
	// Sort pinned by recency rank — the first id in `recents` is the most
	// recent so it should appear first.
	for i := 0; i < len(pinned); i++ {
		for j := i + 1; j < len(pinned); j++ {
			if recentSet[pinned[j].ID] < recentSet[pinned[i].ID] {
				pinned[i], pinned[j] = pinned[j], pinned[i]
			}
		}
	}
	return append(pinned, other...)
}
