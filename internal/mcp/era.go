package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

// The server's era (MCPX-20): a server that answered the server/discover
// probe with "only the old handshake" is remembered in the tool cache
// folder (<name>.era.json), so its next starts go straight to
// initialize and do not wait for the probe. The record holds for
// eraRecheck; a changed entry (its hash), another server version, or an
// initialize the server refuses makes termocode ask again.

const eraRecheck = 24 * time.Hour

type eraFile struct {
	Hash    string    `json:"hash"`
	Legacy  bool      `json:"legacy"`
	Version string    `json:"version"` // the server's own version when it was asked
	Checked time.Time `json:"checked"`
}

func (m *Manager) eraPath(name string) string {
	if m.opts.CacheDir == "" {
		return ""
	}
	return filepath.Join(m.opts.CacheDir, name+".era.json")
}

func (m *Manager) loadEra(cfg Server) (eraFile, bool) {
	p := m.eraPath(cfg.Name)
	if p == "" {
		return eraFile{}, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return eraFile{}, false
	}
	var e eraFile
	if json.Unmarshal(b, &e) != nil || e.Hash != cfg.Hash() {
		return eraFile{}, false
	}
	return e, true
}

// skipDiscover reports a server known to speak only the old handshake.
func (m *Manager) skipDiscover(cfg Server) bool {
	e, ok := m.loadEra(cfg)
	return ok && e.Legacy && clock.Now().Sub(e.Checked) < eraRecheck
}

// noteEra records what a start learned: the probe's answer, or (when
// the probe was skipped) that the server's version changed.
func (m *Manager) noteEra(cfg Server, c *Client) {
	p := m.eraPath(cfg.Name)
	if p == "" {
		return
	}
	info := c.ServerInfo()
	if !c.Probed() {
		if e, ok := m.loadEra(cfg); ok && e.Version != info.Version {
			_ = os.Remove(p) // a new server version: ask again next time
		}
		return
	}
	b, err := json.MarshalIndent(eraFile{Hash: cfg.Hash(), Legacy: !c.Stateless(), Version: info.Version, Checked: clock.Now()}, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(p), 0o755) == nil {
		_ = os.WriteFile(p, b, 0o644)
	}
}
