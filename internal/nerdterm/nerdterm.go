// Package nerdterm tells whether the running terminal draws with a Nerd
// Font. An installed Nerd Font is not enough: GNOME Terminal on the
// system font (Ubuntu Mono) shows tofu boxes for every Nerd icon even
// when JetBrainsMono Nerd Font sits in ~/.local/share/fonts. So this
// package reads the terminal's own font setting, read-only:
//
//   - kitty, WezTerm, Ghostty: bundle the Nerd symbols, any font works.
//   - GNOME Terminal (and other VTE terminals): the default profile via
//     gsettings (custom font, or the desktop monospace font).
//   - Alacritty: font.normal.family in alacritty.toml.
//   - foot: font= in foot.ini.
//   - anything else, or over SSH (the local config is not the drawing
//     terminal's): unknown.
//
// Every outside read goes through Env, so tests never touch the real
// gsettings or config files.
package nerdterm

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Terminal names in Info.Terminal.
const (
	GNOME     = "GNOME Terminal"
	Kitty     = "kitty"
	WezTerm   = "WezTerm"
	Ghostty   = "Ghostty"
	Alacritty = "Alacritty"
	Foot      = "foot"
)

// Timeout bounds all gsettings calls of one Detect.
const Timeout = 100 * time.Millisecond

// Env holds the outside reads. Nil fields use the real system.
type Env struct {
	Getenv    func(string) string
	ReadFile  func(string) ([]byte, error)
	Gsettings func(ctx context.Context, args ...string) (string, error)
	Stat      func(path string) (mtime time.Time, ok bool)
}

// Info is one detection.
type Info struct {
	Terminal string `json:"terminal,omitempty"` // "" = unknown terminal
	Font     string `json:"font,omitempty"`     // configured font, "" = could not read
	Nerd     bool   `json:"nerd"`               // Nerd glyphs will render
	Builtin  bool   `json:"builtin,omitempty"`  // the terminal bundles the Nerd symbols
	System   bool   `json:"system,omitempty"`   // GNOME: the profile uses the system font
	Source   string `json:"source,omitempty"`   // where Font came from
}

// Known reports whether the answer comes from the terminal's settings
// (or a terminal that bundles the symbols) rather than a guess.
func (i Info) Known() bool { return i.Builtin || i.Font != "" }

// Describe is a one-line summary for the Doctor and `termocode setup`.
func (i Info) Describe() string {
	switch {
	case i.Builtin:
		return i.Terminal + " — Nerd symbols built in"
	case i.Terminal == "":
		return "unknown terminal — font not readable"
	case i.Font == "":
		s := i.Terminal + " — font not readable"
		if i.Source != "" {
			s += " (" + i.Source + ")"
		}
		return s
	}
	s := i.Terminal + ": " + i.Font
	if i.Source != "" {
		s += " (" + i.Source + ")"
	}
	if i.Nerd {
		return s + " — a Nerd Font"
	}
	return s + " — not a Nerd Font"
}

func (e Env) withDefaults() Env {
	if e.Getenv == nil {
		e.Getenv = os.Getenv
	}
	if e.ReadFile == nil {
		e.ReadFile = os.ReadFile
	}
	if e.Gsettings == nil {
		e.Gsettings = runGsettings
	}
	if e.Stat == nil {
		e.Stat = func(p string) (time.Time, bool) {
			fi, err := os.Stat(p)
			if err != nil {
				return time.Time{}, false
			}
			return fi.ModTime(), true
		}
	}
	return e
}

func runGsettings(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gsettings", args...)
	cmd.WaitDelay = 50 * time.Millisecond
	out, err := cmd.Output()
	return string(out), err
}

// BundlesNerdSymbols reports terminals that ship the Nerd Font symbols
// as a built-in fallback, so Nerd icons render whatever font is set:
// WezTerm, Ghostty and kitty (0.36+). Over SSH the env usually still
// names the client terminal, which is the one doing the drawing.
func BundlesNerdSymbols(getenv func(string) string) string {
	switch strings.ToLower(getenv("TERM_PROGRAM")) {
	case "wezterm":
		return WezTerm
	case "ghostty":
		return Ghostty
	}
	switch strings.ToLower(getenv("TERM")) {
	case "xterm-kitty":
		return Kitty
	case "xterm-ghostty":
		return Ghostty
	case "wezterm":
		return WezTerm
	}
	return ""
}

// which names the terminal from the environment alone.
func which(getenv func(string) string) string {
	if t := BundlesNerdSymbols(getenv); t != "" {
		return t
	}
	term := strings.ToLower(getenv("TERM"))
	switch {
	case getenv("ALACRITTY_WINDOW_ID") != "" || getenv("ALACRITTY_SOCKET") != "" ||
		getenv("ALACRITTY_LOG") != "" || term == "alacritty":
		return Alacritty
	case term == "foot" || strings.HasPrefix(term, "foot-"):
		return Foot
	case getenv("GNOME_TERMINAL_SCREEN") != "" || getenv("GNOME_TERMINAL_SERVICE") != "" ||
		getenv("VTE_VERSION") != "":
		return GNOME
	}
	return ""
}

func overSSH(getenv func(string) string) bool {
	return getenv("SSH_CONNECTION") != "" || getenv("SSH_CLIENT") != "" || getenv("SSH_TTY") != ""
}

// Detect reads the terminal's configured font. It never changes anything.
func Detect(e Env) Info {
	e = e.withDefaults()
	t := which(e.Getenv)
	info := Info{Terminal: t}
	switch t {
	case "":
		return info
	case Kitty, WezTerm, Ghostty:
		info.Builtin, info.Nerd = true, true
		return info
	}
	if overSSH(e.Getenv) {
		info.Source = "over SSH"
		return info
	}
	switch t {
	case GNOME:
		detectGNOME(e, &info)
	case Alacritty:
		detectAlacritty(e, &info)
	case Foot:
		detectFoot(e, &info)
	}
	info.Nerd = IsNerdFamily(info.Font)
	return info
}

const (
	gnomeList    = "org.gnome.Terminal.ProfilesList"
	gnomeProfile = "org.gnome.Terminal.Legacy.Profile:/org/gnome/terminal/legacy/profiles:/:"
	gnomeDesktop = "org.gnome.desktop.interface"
)

func detectGNOME(e Env, info *Info) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	get := func(schema, key string) string {
		out, err := e.Gsettings(ctx, "get", schema, key)
		if err != nil {
			return ""
		}
		return unquote(out)
	}
	id := get(gnomeList, "default")
	if id == "" {
		// No default set: GNOME Terminal uses the first listed profile.
		list := strings.Trim(get(gnomeList, "list"), "[]")
		if i := strings.IndexByte(list, ','); i >= 0 {
			list = list[:i]
		}
		id = unquote(list)
	}
	if id == "" {
		info.Source = "gsettings: no profile"
		return
	}
	path := gnomeProfile + id + "/"
	// The three keys are independent; read them at once to stay fast.
	var useSys, font, mono string
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); useSys = get(path, "use-system-font") }()
	go func() { defer wg.Done(); font = get(path, "font") }()
	go func() { defer wg.Done(); mono = get(gnomeDesktop, "monospace-font-name") }()
	wg.Wait()
	switch useSys {
	case "true":
		info.System = true
		info.Font, info.Source = mono, "system monospace font"
	case "false":
		info.Font, info.Source = font, "default profile, custom font"
	default:
		info.Source = "gsettings: no answer"
	}
}

func configHome(getenv func(string) string) string {
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return x
	}
	if h := getenv("HOME"); h != "" {
		return filepath.Join(h, ".config")
	}
	return ""
}

func alacrittyPaths(getenv func(string) string) []string {
	var out []string
	if c := configHome(getenv); c != "" {
		out = append(out, filepath.Join(c, "alacritty", "alacritty.toml"))
	}
	if h := getenv("HOME"); h != "" {
		out = append(out, filepath.Join(h, ".config", "alacritty", "alacritty.toml"), filepath.Join(h, ".alacritty.toml"))
	}
	return dedupe(out)
}

func footPaths(getenv func(string) string) []string {
	var out []string
	if c := configHome(getenv); c != "" {
		out = append(out, filepath.Join(c, "foot", "foot.ini"))
	}
	if h := getenv("HOME"); h != "" {
		out = append(out, filepath.Join(h, ".config", "foot", "foot.ini"))
	}
	return dedupe(out)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// detectAlacritty reads font.normal.family from alacritty.toml, as a
// [font.normal] table or an inline `normal = { family = "…" }`.
func detectAlacritty(e Env, info *Info) {
	for _, p := range alacrittyPaths(e.Getenv) {
		b, err := e.ReadFile(p)
		if err != nil {
			continue
		}
		info.Source = p
		section := ""
		sc := bufio.NewScanner(strings.NewReader(string(b)))
		for sc.Scan() {
			line := stripComment(sc.Text(), "#")
			if strings.HasPrefix(line, "[") {
				section = strings.TrimSpace(strings.Trim(line, "[]"))
				continue
			}
			k, v, ok := keyValue(line)
			if !ok {
				continue
			}
			switch {
			case section == "font.normal" && k == "family":
				info.Font = unquote(v)
			case section == "font" && k == "normal":
				if f := inlineField(v, "family"); f != "" {
					info.Font = f
				}
			}
		}
		if info.Font != "" {
			return
		}
		break
	}
	info.Font = "monospace"
	if info.Source == "" {
		info.Source = "Alacritty default"
	} else {
		info.Source += ", default"
	}
}

// detectFoot reads font= from the [main] section (or before any section)
// of foot.ini. A comma list holds fallbacks; the Nerd check covers all.
func detectFoot(e Env, info *Info) {
	for _, p := range footPaths(e.Getenv) {
		b, err := e.ReadFile(p)
		if err != nil {
			continue
		}
		info.Source = p
		section := "main"
		sc := bufio.NewScanner(strings.NewReader(string(b)))
		for sc.Scan() {
			line := stripComment(sc.Text(), "#")
			if strings.HasPrefix(line, "[") {
				section = strings.TrimSpace(strings.Trim(line, "[]"))
				continue
			}
			k, v, ok := keyValue(line)
			if !ok || section != "main" || k != "font" {
				continue
			}
			var fams []string
			for _, f := range strings.Split(v, ",") {
				if i := strings.IndexByte(f, ':'); i >= 0 {
					f = f[:i]
				}
				if f = strings.TrimSpace(f); f != "" {
					fams = append(fams, f)
				}
			}
			info.Font = strings.Join(fams, ", ")
		}
		if info.Font != "" {
			return
		}
		break
	}
	info.Font = "monospace"
	if info.Source == "" {
		info.Source = "foot default"
	} else {
		info.Source += ", default"
	}
}

func stripComment(line, mark string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, mark) {
		return ""
	}
	return line
}

func keyValue(line string) (string, string, bool) {
	i := strings.IndexByte(line, '=')
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

// inlineField reads key from a TOML inline table `{ a = "x", b = "y" }`.
func inlineField(v, key string) string {
	v = strings.Trim(strings.TrimSpace(v), "{}")
	for _, part := range strings.Split(v, ",") {
		if k, val, ok := keyValue(part); ok && k == key {
			return unquote(val)
		}
	}
	return ""
}

// unquote trims whitespace and one pair of ' or " quotes.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	}
	return s
}

// IsNerdFamily reports whether a font name (family, or a Pango
// description like "JetBrainsMono Nerd Font 11") is a Nerd Font: it
// says "Nerd Font", or has the short NF / NFM / NFP suffix word.
func IsNerdFamily(name string) bool {
	low := strings.ToLower(name)
	if strings.Contains(low, "nerd font") {
		return true
	}
	for _, w := range strings.FieldsFunc(low, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		switch w {
		case "nf", "nfm", "nfp":
			return true
		}
	}
	return false
}

// Stamp keys a cached Detect: the terminal plus the mtimes of the files
// its answer comes from (dconf's user database for GNOME). It reads no
// file content and runs nothing.
func Stamp(e Env) string {
	e = e.withDefaults()
	t := which(e.Getenv)
	var paths []string
	switch t {
	case GNOME:
		if c := configHome(e.Getenv); c != "" {
			paths = append(paths, filepath.Join(c, "dconf", "user"))
		}
	case Alacritty:
		paths = alacrittyPaths(e.Getenv)
	case Foot:
		paths = footPaths(e.Getenv)
	}
	var b strings.Builder
	b.WriteString(t)
	if overSSH(e.Getenv) {
		b.WriteString(";ssh")
	}
	for _, p := range paths {
		b.WriteString(";" + p + "=")
		if mt, ok := e.Stat(p); ok {
			b.WriteString(mt.UTC().Format(time.RFC3339Nano))
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// FixHint is short, exact steps to switch this terminal to the Nerd Font
// family nerd. Nil when the terminal already draws Nerd glyphs.
func FixHint(i Info, nerd string) []string {
	if i.Nerd {
		return nil
	}
	switch i.Terminal {
	case GNOME:
		return []string{
			"GNOME Terminal: open the menu ☰ → Preferences → your profile (left) → Text,",
			"tick \"Custom font\" and pick \"" + nerd + "\". Then restart termocode.",
		}
	case Alacritty:
		return []string{
			"Alacritty: in ~/.config/alacritty/alacritty.toml add",
			"  [font.normal]",
			"  family = \"" + nerd + "\"",
		}
	case Foot:
		return []string{
			"foot: in ~/.config/foot/foot.ini, section [main], set",
			"  font=" + nerd + ":size=11",
		}
	}
	return []string{
		"Set your terminal's font to \"" + nerd + "\" in its settings,",
		"or keep the Unicode icons (icons = auto does this for you).",
	}
}
