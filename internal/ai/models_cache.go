package ai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/amin-jalali/termocode/internal/cfgfile"
)

// ModelsRefreshAfter is the model list age: Itermoc refreshes an entry
// older than this in the background when it opens. Refresh always
// fetches.
const ModelsRefreshAfter = time.Hour

// ModelsCache is models.json: the last fetched model list of each
// provider kind. It holds model ids only, never a key or a base URL.
type ModelsCache map[string]ModelsEntry

// ModelsEntry is one provider's list and when it was fetched.
type ModelsEntry struct {
	At     int64       `json:"at"` // unix seconds
	Models []ModelInfo `json:"models"`
}

// ModelsCachePath is $XDG_CACHE_HOME/termocode/ai/models.json
// (~/.cache/termocode/ai/models.json), next to the MCP tool cache and
// outside the config folder, so an export never carries it.
func ModelsCachePath() (string, error) {
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		cache = filepath.Join(home, ".cache")
	}
	return filepath.Join(cache, "termocode", "ai", "models.json"), nil
}

// LoadModelsCache reads models.json. A missing or corrupt file is an
// empty cache (it is only a cache); the error is for an unknown home.
func LoadModelsCache() (ModelsCache, error) {
	c := ModelsCache{}
	p, err := ModelsCachePath()
	if err != nil {
		return c, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return c, nil
	}
	if json.Unmarshal(data, &c) != nil || c == nil {
		return ModelsCache{}, nil
	}
	return c, nil
}

// SaveModelsCache writes c to models.json.
func SaveModelsCache(c ModelsCache) error {
	p, err := ModelsCachePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return cfgfile.WriteFile(p, append(data, '\n'))
}

// Put stores kind's models fetched at now.
func (c ModelsCache) Put(kind string, ms []ModelInfo, now time.Time) {
	c[kind] = ModelsEntry{At: now.Unix(), Models: ms}
}

// Drop forgets kind (Disconnect).
func (c ModelsCache) Drop(kind string) { delete(c, kind) }

// DueRefresh reports whether kind's list is old enough (ModelsRefreshAfter)
// to fetch again in the background.
func (c ModelsCache) DueRefresh(kind string, now time.Time) bool {
	return c.older(kind, now, ModelsRefreshAfter)
}

// older reports a missing entry, one at least d old, or one written in
// the future (the clock moved back).
func (c ModelsCache) older(kind string, now time.Time, d time.Duration) bool {
	e, ok := c[kind]
	if !ok {
		return true
	}
	age := now.Sub(time.Unix(e.At, 0))
	return age < 0 || age >= d
}
