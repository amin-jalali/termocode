package termcaps

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// FuzzCacheFile reads any termcaps.json. A hit is never older than the
// TTL nor from the future, and remembering keeps the file readable.
func FuzzCacheFile(f *testing.F) {
	f.Add([]byte(`{"xterm-256color|ssh=0|":{"answers":true,"at":"2026-10-29T08:00:00Z"}}`), "xterm-256color|ssh=0|")
	f.Add([]byte(`{"k":{"answers":true,"at":"2999-01-01T00:00:00Z"}}`), "k")
	f.Add([]byte(`{"k":null}`), "k")
	f.Add([]byte(`[]`), "")
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, b []byte, key string) {
		p := filepath.Join(t.TempDir(), "termcaps.json")
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		if e, ok := Lookup(p, key, now); ok && (now.Sub(e.At) > CacheTTL || e.At.After(now.Add(time.Hour))) {
			t.Fatalf("stale or future hit %+v", e)
		}
		Remember(p, key, Entry{Answers: true}, now)
		if e, ok := Lookup(p, key, now); !ok || !e.Answers {
			t.Fatalf("remembered entry not found: %+v %v", e, ok)
		}
	})
}
