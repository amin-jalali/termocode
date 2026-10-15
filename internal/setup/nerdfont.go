package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// Nerd Font detection runs `fc-list`, which lists every installed font
// (tens of ms with a warm fontconfig cache, much more with a cold one or
// thousands of fonts). The answer rarely changes, so HasNerdFont keeps it
// in $XDG_STATE_HOME/termocode/nerdfont.json and only runs fc-list again
// when the entry is older than nerdCacheTTL or the fontconfig / font
// folders changed since (installing a font or `fc-cache` touches them).
// The Doctor and `termocode setup` always run a fresh check.

// nerdCacheTTL bounds how long a cached answer is trusted.
const nerdCacheTTL = 24 * time.Hour

// fcList is the seam tests replace with a fake fc-list.
var fcList = func() (string, error) {
	out, err := spawn.Command("fc-list", ":", "family").Output()
	return string(out), err
}

// fontDirs are the folders whose mtimes key the cache. Tests replace it.
var fontDirs = defaultFontDirs

// NerdFontInfo is the result of one detection.
type NerdFontInfo struct {
	Found  bool      `json:"found"`
	Family string    `json:"family,omitempty"` // first Nerd family fc-list named
	Stamp  string    `json:"stamp"`            // fontDirs mtimes when checked
	At     time.Time `json:"at"`
	Cached bool      `json:"-"` // served from the cache file
}

// HasNerdFont reports whether a Nerd Font is installed, from the cache
// when it is still valid. Installed is not "in use": icons = auto reads
// the terminal's own font setting instead (TerminalNerdFont).
func HasNerdFont() bool { return DetectNerdFont(false).Found }

// DetectNerdFont returns the Nerd Font detection. fresh skips the cache
// (the Doctor uses it); either way a new answer is written back.
func DetectNerdFont(fresh bool) NerdFontInfo {
	stamp := fontStamp()
	if !fresh {
		if c, ok := readNerdCache(); ok && c.Stamp == stamp {
			if age := clock.Since(c.At); age >= 0 && age < nerdCacheTTL {
				c.Cached = true
				return c
			}
		}
	}
	info := NerdFontInfo{Stamp: stamp, At: clock.Now()}
	if out, err := fcList(); err == nil {
		info.Family, info.Found = nerdFamily(out)
	}
	writeNerdCache(info)
	return info
}

func hasNerdFont() bool { return DetectNerdFont(true).Found }

// nerdFamily finds the first font family with "nerd" in its name.
func nerdFamily(fcOut string) (string, bool) {
	for _, line := range strings.Split(fcOut, "\n") {
		if !strings.Contains(strings.ToLower(line), "nerd") {
			continue
		}
		// Plain fc-list prints "path: Family,Alias:style=…"; the
		// ": family" form prints just "Family,Alias".
		if i := strings.Index(line, ": "); i >= 0 {
			line = line[i+2:]
		}
		if i := strings.IndexAny(line, ",:"); i >= 0 {
			line = line[:i]
		}
		return strings.TrimSpace(line), true
	}
	return "", false
}

func nerdCachePath() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "termocode", "nerdfont.json")
}

func readNerdCache() (NerdFontInfo, bool) {
	var c NerdFontInfo
	p := nerdCachePath()
	if p == "" {
		return c, false
	}
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &c) != nil {
		return c, false
	}
	return c, true
}

func writeNerdCache(c NerdFontInfo) {
	p := nerdCachePath()
	if p == "" {
		return
	}
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}

// InvalidateNerdFontCache drops the cached answer (after a font install).
func InvalidateNerdFontCache() {
	if p := nerdCachePath(); p != "" {
		_ = os.Remove(p)
	}
}

// fontStamp joins the mtimes of the font and fontconfig cache folders.
// A missing folder counts as "-", so creating it changes the stamp too.
func fontStamp() string {
	var b strings.Builder
	for _, d := range fontDirs() {
		b.WriteString(d)
		b.WriteByte('=')
		if fi, err := os.Stat(d); err == nil {
			b.WriteString(fi.ModTime().UTC().Format(time.RFC3339Nano))
		} else {
			b.WriteByte('-')
		}
		b.WriteByte(';')
	}
	return b.String()
}

func defaultFontDirs() []string {
	home, _ := os.UserHomeDir()
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" && home != "" {
		cache = filepath.Join(home, ".cache")
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" && home != "" {
		data = filepath.Join(home, ".local", "share")
	}
	dirs := []string{"/var/cache/fontconfig", "/usr/share/fonts", "/usr/local/share/fonts"}
	if cache != "" {
		dirs = append(dirs, filepath.Join(cache, "fontconfig"))
	}
	if data != "" {
		dirs = append(dirs, filepath.Join(data, "fonts"))
	}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".fonts"), filepath.Join(home, "Library", "Fonts"))
	}
	return dirs
}
