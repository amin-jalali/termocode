package setup

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/nerdterm"
)

// termEnv is the seam for the terminal font reads (env, gsettings,
// config files). Tests replace it so the real machine never leaks in.
var termEnv = nerdterm.Env{}

// termFontCache is the cached terminal font answer. Like the fc-list
// cache, it is trusted for termFontTTL while the stamp (terminal + the
// mtimes of its settings files) is unchanged.
type termFontCache struct {
	Stamp string        `json:"stamp"`
	At    time.Time     `json:"at"`
	Info  nerdterm.Info `json:"info"`
}

const termFontTTL = 24 * time.Hour

func termFontCachePath() string {
	p := nerdCachePath()
	if p == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p), "termfont.json")
}

// DetectTerminalFont reads the terminal's configured font (read-only).
// fresh skips the cache; either way a new answer is written back.
func DetectTerminalFont(fresh bool) nerdterm.Info {
	stamp := nerdterm.Stamp(termEnv)
	p := termFontCachePath()
	if !fresh && p != "" {
		var c termFontCache
		if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &c) == nil && c.Stamp == stamp {
			if age := clock.Since(c.At); age >= 0 && age < termFontTTL {
				return c.Info
			}
		}
	}
	info := nerdterm.Detect(termEnv)
	if p != "" {
		if b, err := json.Marshal(termFontCache{Stamp: stamp, At: clock.Now(), Info: info}); err == nil &&
			os.MkdirAll(filepath.Dir(p), 0o755) == nil {
			tmp := p + ".tmp"
			if os.WriteFile(tmp, b, 0o644) == nil {
				_ = os.Rename(tmp, p)
			}
		}
	}
	return info
}

// TerminalNerdFont reports whether the terminal draws with a Nerd Font
// (cached). The "icons: auto" setting uses it.
func TerminalNerdFont() bool { return DetectTerminalFont(false).Nerd }

// TerminalFontCheck is the Doctor row for the terminal's font. nerd is
// the installed Nerd Font family to suggest.
func TerminalFontCheck(info nerdterm.Info, nerd string) Check {
	c := Check{Group: "terminal", Name: "terminal font", Status: Warn, Detail: info.Describe()}
	if info.Nerd {
		c.Status = OK
		return c
	}
	if info.Known() {
		c.Detail += " · icons auto = unicode · for Nerd icons: " + strings.Join(trimHint(nerdterm.FixHint(info, nerd)), " ")
	}
	return c
}

func trimHint(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimSpace(l)
	}
	return out
}

// installedNerdName is the Nerd Font family to suggest: the one setup
// installs when present, else the first one fc-list found.
func installedNerdName(info NerdFontInfo) string {
	if info.Family == "" || strings.Contains(info.Family, "JetBrainsMono") {
		return jbmName
	}
	return info.Family
}

// printTerminalFont tells what font the terminal uses and, if it is not
// a Nerd Font, how to switch. It never changes the terminal's settings.
func printTerminalFont(w io.Writer, info nerdterm.Info, nerd string) {
	mark := "?"
	if info.Nerd {
		mark = "✓"
	}
	fmt.Fprintf(w, "%s terminal font: %s\n", mark, info.Describe())
	if info.Nerd {
		return
	}
	fmt.Fprintln(w, "  Icons stay Unicode (icons = auto) until the terminal uses a Nerd Font.")
	if !info.Known() {
		return
	}
	for _, l := range nerdterm.FixHint(info, nerd) {
		fmt.Fprintln(w, "  "+l)
	}
}
