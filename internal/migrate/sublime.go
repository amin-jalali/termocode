package migrate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

// sublimeKeymapFiles are the per-OS user keymap names; a synced folder can hold several.
var sublimeKeymapFiles = []string{"Default (Linux).sublime-keymap", "Default (OSX).sublime-keymap", "Default (Windows).sublime-keymap"}

// detectSublime returns the user folder of the newest Sublime Text that has
// settings, a keymap or Package Control data (Text 4, else Text 3).
func detectSublime(home, goos, config string) []Source {
	var roots [][2]string // title, folder
	switch goos {
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support")
		roots = [][2]string{{"Sublime Text 4", filepath.Join(base, "Sublime Text")}, {"Sublime Text 3", filepath.Join(base, "Sublime Text 3")}}
	case "windows":
		base := filepath.Join(home, "AppData", "Roaming")
		roots = [][2]string{{"Sublime Text 4", filepath.Join(base, "Sublime Text")}, {"Sublime Text 3", filepath.Join(base, "Sublime Text 3")}}
	default:
		roots = [][2]string{{"Sublime Text 4", filepath.Join(config, "sublime-text")}, {"Sublime Text 3", filepath.Join(config, "sublime-text-3")}}
	}
	for _, r := range roots {
		user := filepath.Join(r[1], "Packages", "User")
		names := append([]string{"Preferences.sublime-settings", "Package Control.sublime-settings"}, sublimeKeymapFiles...)
		if existsAny(user, names...) {
			return []Source{{"sublime", r[0], user, []string{"Preferences.sublime-settings", "Default (<OS>).sublime-keymap", "Package Control.sublime-settings"}}}
		}
	}
	return nil
}

// ReadSublime reads a Sublime Text Packages/User folder. It never writes and
// never leaves dir.
func ReadSublime(dir string) (Document, error) {
	d := Document{}
	skip := func(kind RowKind, source, reason string) {
		d.Skipped = append(d.Skipped, Skipped{kind, source, reason})
	}
	// jsonRead reads one JSONC file (comments and trailing commas allowed).
	jsonRead := func(kind RowKind, rel string, dst any) bool {
		b, found, reason := readInside(dir, rel)
		if reason != "" {
			skip(kind, rel, reason)
		}
		if !found {
			return false
		}
		if err := json.Unmarshal(jsonc.Strip(trimBOM(b)), dst); err != nil {
			skip(kind, rel, "not valid JSON; skipped")
			return false
		}
		return true
	}

	prefs := map[string]any{}
	if jsonRead(RowSetting, "Preferences.sublime-settings", &prefs) {
		scheme, _ := prefs["color_scheme"].(string)
		if scheme == "auto" {
			scheme, _ = prefs["dark_color_scheme"].(string)
		}
		d.Theme = sublimeSchemeName(scheme)
		for k, v := range prefs {
			switch k {
			case "color_scheme", "dark_color_scheme", "light_color_scheme":
				continue
			}
			d.Settings = append(d.Settings, Setting{k, v})
		}
		sort.Slice(d.Settings, func(i, j int) bool { return d.Settings[i].Name < d.Settings[j].Name })
	}

	for _, f := range sublimeKeymapFiles {
		var rows []struct {
			Keys    []string       `json:"keys"`
			Command string         `json:"command"`
			Args    map[string]any `json:"args"`
			Context []any          `json:"context"`
		}
		if !jsonRead(RowKey, f, &rows) {
			continue
		}
		for _, r := range rows {
			keys := strings.Join(r.Keys, " ")
			switch {
			case r.Command == "" || keys == "":
				skip(RowKey, strings.TrimSpace(keys+"  "+r.Command), f+": entry without keys or command")
			case len(r.Context) > 0:
				skip(RowKey, keys+"  "+r.Command, "only works in a context (a condition on the file or cursor); not imported")
			default:
				d.Keys = append(d.Keys, KeyBinding{keys, sublimeCommand(r.Command, r.Args)})
			}
		}
	}

	var pc struct {
		Installed []string `json:"installed_packages"`
	}
	if jsonRead(RowExtension, "Package Control.sublime-settings", &pc) {
		for _, p := range pc.Installed {
			if p == "Package Control" {
				skip(RowExtension, p, "is the package manager itself; nothing to import")
				continue
			}
			d.Extensions = append(d.Extensions, p)
		}
	}
	return d, nil
}

// sublimeSchemeName turns "Packages/Color Scheme - Default/Monokai.sublime-color-scheme" into "Monokai".
func sublimeSchemeName(s string) string {
	s = s[strings.LastIndex(s, "/")+1:]
	for _, ext := range []string{".sublime-color-scheme", ".tmTheme"} {
		s = strings.TrimSuffix(s, ext)
	}
	return s
}

// sublimeCommand folds the arguments that change what a command does into the
// command name, so the mapping table can tell "go to line" from "go to file".
func sublimeCommand(cmd string, args map[string]any) string {
	str := func(k string) string { s, _ := args[k].(string); return s }
	switch cmd {
	case "show_overlay":
		if o := str("overlay"); o != "" && o != "goto" {
			return cmd + ":" + o
		}
		if t := str("text"); t != "" {
			return cmd + ":goto:" + t
		}
	case "show_panel":
		if p := str("panel"); p != "" {
			return cmd + ":" + p
		}
	case "toggle_comment":
		if b, _ := args["block"].(bool); b {
			return cmd + ":block"
		}
	case "toggle_setting":
		if s := str("setting"); s != "" {
			return fmt.Sprint(cmd, ":", s)
		}
	}
	return cmd
}
