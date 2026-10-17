package termcaps

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// The key names the terminal, the SSH hop and the layer; it does not
// change with unrelated variables.
func TestCacheKey(t *testing.T) {
	a := CacheKey(envOf(map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "vscode", "HOME": "/a"}))
	b := CacheKey(envOf(map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "vscode", "HOME": "/b"}))
	if a != b {
		t.Errorf("key depends on HOME: %q vs %q", a, b)
	}
	for name, m := range map[string]map[string]string{
		"ssh":     {"TERM": "xterm-256color", "TERM_PROGRAM": "vscode", "SSH_CONNECTION": "1 2 3 4"},
		"program": {"TERM": "xterm-256color", "TERM_PROGRAM": "WezTerm"},
		"term":    {"TERM": "foot", "TERM_PROGRAM": "vscode"},
		"tmux":    {"TERM": "xterm-256color", "TERM_PROGRAM": "vscode", "TMUX": "/tmp/x,1,0"},
		"version": {"TERM": "xterm-256color", "TERM_PROGRAM": "vscode", "TERM_PROGRAM_VERSION": "1.90"},
	} {
		if CacheKey(envOf(m)) == a {
			t.Errorf("%s: same key as the plain terminal", name)
		}
	}
}

// A silent terminal is remembered for 7 days; then it is asked again.
func TestCacheRoundTripAndTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termocode", "termcaps.json")
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if _, ok := Lookup(path, "k", now); ok {
		t.Fatal("empty cache has an entry")
	}
	Remember(path, "k", Entry{Answers: false}, now)
	Remember(path, "other", Entry{Answers: true}, now)
	e, ok := Lookup(path, "k", now.Add(6*24*time.Hour))
	if !ok || e.Answers {
		t.Fatalf("after 6 days: %+v %v, want a silent entry", e, ok)
	}
	if e, ok := Lookup(path, "other", now); !ok || !e.Answers {
		t.Errorf("second key lost: %+v %v", e, ok)
	}
	if _, ok := Lookup(path, "k", now.Add(CacheTTL+time.Minute)); ok {
		t.Error("entry older than the TTL still used")
	}
	// A broken file is an empty cache, and the next write repairs it.
	if err := os.WriteFile(path, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Lookup(path, "k", now); ok {
		t.Error("broken file gave an entry")
	}
	Remember(path, "k", Entry{Answers: true}, now)
	if e, ok := Lookup(path, "k", now); !ok || !e.Answers {
		t.Errorf("after repair: %+v %v", e, ok)
	}
	// No path (no home folder): nothing is kept, nothing breaks.
	Remember("", "k", Entry{Answers: true}, now)
	if _, ok := Lookup("", "k", now); ok {
		t.Error("empty path gave an entry")
	}
}

func TestCachePath(t *testing.T) {
	if got := CachePath(envOf(map[string]string{"XDG_STATE_HOME": "/s"})); got != "/s/termocode/termcaps.json" {
		t.Errorf("XDG_STATE_HOME: %q", got)
	}
	if got := CachePath(envOf(map[string]string{"HOME": "/h"})); got != "/h/.local/state/termocode/termcaps.json" {
		t.Errorf("HOME: %q", got)
	}
}

// Terminal R3: when the startup probe may not run at all.
func TestProbeAllowed(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		ok   bool
	}{
		{"plain", map[string]string{"TERM": "xterm-256color"}, true},
		{"off", map[string]string{"TERM": "xterm-256color", "TERMOCODE_PROBE": "off"}, false},
		{"real screen", map[string]string{"TERM": "screen-256color", "STY": "1.pts-0"}, false},
		// tmux also sets TERM=screen*, but not STY; tmux has its own path.
		{"tmux screen term", map[string]string{"TERM": "screen-256color", "TMUX": "/tmp/t,1,0"}, true},
		{"dumb", map[string]string{"TERM": "dumb"}, false},
	}
	for _, c := range cases {
		if ok, why := ProbeAllowed(envOf(c.env)); ok != c.ok || (!ok && why == "") {
			t.Errorf("%s: ProbeAllowed = %v %q, want %v", c.name, ok, why, c.ok)
		}
	}
}
