// Package termcaps holds what termocode knows about the host terminal:
// whether the startup probe may run, and a small cache of how the
// terminal answered it on earlier runs (terminal-compat R3).
package termcaps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CacheTTL is how long a probe result is trusted. After that the terminal
// is asked again (it may have been updated).
const CacheTTL = 7 * 24 * time.Hour

// Entry is how one terminal answered the startup probe.
type Entry struct {
	// Answers: it answered DA1. A silent terminal is not asked again (it
	// would cost the full probe timeout at every start).
	Answers bool      `json:"answers"`
	At      time.Time `json:"at"`
}

// CacheKey names the terminal for the cache: what the environment says
// about it, the SSH hop and the layer (tmux, screen, zellij).
func CacheKey(getenv func(string) string) string {
	ssh := ""
	if getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != "" || getenv("SSH_CLIENT") != "" {
		ssh = "ssh"
	}
	return strings.Join([]string{
		getenv("TERM"), getenv("TERM_PROGRAM"), getenv("TERM_PROGRAM_VERSION"),
		ssh, Layer(getenv),
	}, "|")
}

// Layer is the multiplexer between termocode and the terminal: "tmux",
// "screen", "zellij" or "".
func Layer(getenv func(string) string) string {
	switch {
	case getenv("TMUX") != "":
		return "tmux"
	case getenv("STY") != "":
		return "screen"
	case getenv("ZELLIJ") != "":
		return "zellij"
	}
	return ""
}

// CachePath is $XDG_STATE_HOME/termocode/termcaps.json ("" without a
// home folder).
func CachePath(getenv func(string) string) string {
	state := getenv("XDG_STATE_HOME")
	if state == "" {
		home := getenv("HOME")
		if home == "" {
			return ""
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "termocode", "termcaps.json")
}

// jsonKey is key as it reads back from JSON: bytes that are not UTF-8
// become U+FFFD there, so a key from an odd TERM would never hit.
func jsonKey(key string) string { return strings.ToValidUTF8(key, "\uFFFD") }

func load(path string) map[string]Entry {
	m := map[string]Entry{}
	if path == "" {
		return m
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &m) != nil {
		return map[string]Entry{}
	}
	return m
}

// Lookup returns the cached entry for key when it is younger than
// CacheTTL.
func Lookup(path, key string, now time.Time) (Entry, bool) {
	e, ok := load(path)[jsonKey(key)]
	if !ok || now.Sub(e.At) > CacheTTL || e.At.After(now.Add(time.Hour)) {
		return Entry{}, false
	}
	return e, true
}

// Remember stores e (stamped with now) for key. Best effort: errors are
// ignored, the next start just probes again. Entries past the TTL are
// dropped so the file stays small.
func Remember(path, key string, e Entry, now time.Time) {
	if path == "" {
		return
	}
	m := load(path)
	for k, v := range m {
		if now.Sub(v.At) > CacheTTL {
			delete(m, k)
		}
	}
	e.At = now
	m[jsonKey(key)] = e
	b, err := json.Marshal(m)
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// ProbeAllowed reports whether the startup probe may write queries to
// the terminal at all, and why not.
func ProbeAllowed(getenv func(string) string) (bool, string) {
	term := getenv("TERM")
	switch {
	case strings.EqualFold(getenv("TERMOCODE_PROBE"), "off"):
		return false, "TERMOCODE_PROBE=off"
	case strings.HasPrefix(term, "screen") && getenv("STY") != "" && getenv("TMUX") == "":
		// GNU screen is not tmux: no passthrough, and what it does with
		// an APC string is unknown.
		return false, "inside GNU screen"
	case term == "dumb", term == "linux":
		// The Linux console has nothing to find (no images, kitty keys
		// or mode 2026).
		return false, "TERM=" + term
	}
	return true, ""
}
