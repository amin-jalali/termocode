// Package migrate reads editor configuration into neutral, read-only data.
package migrate

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

type Setting struct {
	Name  string
	Value any
}
type KeyBinding struct{ Key, Command string }
type Document struct {
	Settings   []Setting
	Keys       []KeyBinding
	Theme      string
	Extensions []string
}
type Source struct {
	ID, Title, Dir string
	Files          []string
}

func jsonFile(path string, dst any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		return e
	}
	if len(b) > 1<<20 {
		return fmt.Errorf("%s exceeds 1 MB", path)
	}
	return json.Unmarshal(jsonc.Strip(b), dst)
}
func stringMap(path string) (map[string]any, error) {
	m := map[string]any{}
	e := jsonFile(path, &m)
	return m, e
}
func addSettings(d *Document, m map[string]any, themeKey string) {
	for k, v := range m {
		if k == themeKey {
			if s, ok := v.(string); ok {
				d.Theme = s
			}
			continue
		}
		d.Settings = append(d.Settings, Setting{k, v})
	}
	sort.Slice(d.Settings, func(i, j int) bool { return d.Settings[i].Name < d.Settings[j].Name })
}
func ReadVSCode(dir string) (Document, error) {
	d := Document{}
	m, e := stringMap(filepath.Join(dir, "settings.json"))
	if e != nil {
		return d, e
	}
	addSettings(&d, m, "workbench.colorTheme")
	var keys []struct{ Key, Command string }
	if e = jsonFile(filepath.Join(dir, "keybindings.json"), &keys); e != nil {
		return d, e
	}
	for _, k := range keys {
		d.Keys = append(d.Keys, KeyBinding{k.Key, k.Command})
	}
	extDir := filepath.Join(dir, "extensions")
	if _, err := os.Stat(extDir); err != nil {
		home := filepath.Dir(filepath.Dir(filepath.Dir(dir)))
		candidates := []string{filepath.Join(home, ".vscode", "extensions"), filepath.Join(home, ".vscode-oss", "extensions"), filepath.Join(home, ".cursor", "extensions"), filepath.Join(home, ".windsurf", "extensions")}
		for _, candidate := range candidates {
			if _, statErr := os.Stat(candidate); statErr == nil {
				extDir = candidate
				break
			}
		}
	}
	entries, _ := os.ReadDir(extDir)
	for _, x := range entries {
		if !x.IsDir() {
			continue
		}
		var p struct {
			Publisher string `json:"publisher"`
			Name      string `json:"name"`
		}
		if jsonFile(filepath.Join(extDir, x.Name(), "package.json"), &p) == nil && p.Publisher != "" && p.Name != "" {
			d.Extensions = append(d.Extensions, p.Publisher+"."+p.Name)
		}
	}
	return d, nil
}
func ReadZed(dir string) (Document, error) {
	d := Document{}
	m, e := stringMap(filepath.Join(dir, "settings.json"))
	if e != nil {
		return d, e
	}
	addSettings(&d, m, "theme")
	if v, ok := m["theme"].(string); ok {
		d.Theme = v
	}
	var rows []struct {
		Bindings map[string]string `json:"bindings"`
	}
	if e = jsonFile(filepath.Join(dir, "keymap.json"), &rows); e != nil {
		return d, e
	}
	for _, r := range rows {
		for k, v := range r.Bindings {
			d.Keys = append(d.Keys, KeyBinding{k, v})
		}
	}
	return d, nil
}
func ReadHelix(dir string) (Document, error) {
	d := Document{}
	for _, f := range []string{"config.toml", "languages.toml"} {
		b, e := os.ReadFile(filepath.Join(dir, f))
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return d, e
		}
		section := ""
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "[") {
				section = line
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.Trim(strings.TrimSpace(k), "\"'")
			v = strings.Trim(strings.TrimSpace(v), "\"'")
			if k == "theme" {
				d.Theme = v
			} else if strings.Contains(section, "keys") {
				d.Keys = append(d.Keys, KeyBinding{k, v})
			} else {
				d.Settings = append(d.Settings, Setting{section + "." + k, v})
			}
		}
	}
	return d, nil
}
func ReadJetBrains(dir string) (Document, error) {
	d := Document{}
	b, err := os.ReadFile(filepath.Join(dir, "options", "editor.xml"))
	if err == nil {
		var x struct {
			Options []struct {
				Name  string `xml:"name,attr"`
				Value string `xml:"value,attr"`
			} `xml:"component>option"`
		}
		if xml.Unmarshal(b, &x) == nil {
			for _, o := range x.Options {
				d.Settings = append(d.Settings, Setting{o.Name, o.Value})
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return d, err
	}
	files, _ := filepath.Glob(filepath.Join(dir, "keymaps", "*.xml"))
	for _, f := range files {
		bb, e := os.ReadFile(f)
		if e != nil {
			continue
		}
		var x struct {
			Actions []struct {
				ID        string `xml:"id,attr"`
				Shortcuts []struct {
					First  string `xml:"first-keystroke,attr"`
					Second string `xml:"second-keystroke,attr"`
				} `xml:"keyboard-shortcut"`
			} `xml:"action"`
		}
		if xml.Unmarshal(bb, &x) == nil {
			for _, a := range x.Actions {
				for _, k := range a.Shortcuts {
					key := k.First
					if k.Second != "" {
						key += ", " + k.Second
					}
					d.Keys = append(d.Keys, KeyBinding{key, a.ID})
				}
			}
		}
	}
	return d, nil
}
func ReadSublime(dir string) (Document, error) {
	d := Document{}
	m, err := stringMap(filepath.Join(dir, "Preferences.sublime-settings"))
	if err != nil {
		return d, err
	}
	addSettings(&d, m, "color_scheme")
	if v, ok := m["color_scheme"].(string); ok {
		d.Theme = v
	}
	files, _ := filepath.Glob(filepath.Join(dir, "Default (*).sublime-keymap"))
	for _, f := range files {
		var rows []struct {
			Keys    []string `json:"keys"`
			Command string   `json:"command"`
		}
		if jsonFile(f, &rows) == nil {
			for _, r := range rows {
				d.Keys = append(d.Keys, KeyBinding{strings.Join(r.Keys, " "), r.Command})
			}
		}
	}
	return d, nil
}
func ReadVim(path string) (Document, error) {
	d := Document{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "map ") || strings.HasPrefix(s, "nmap ") || strings.HasPrefix(s, "imap ") {
			f := strings.Fields(s)
			if len(f) >= 3 {
				d.Keys = append(d.Keys, KeyBinding{strings.Join(f[1:len(f)-1], " "), f[len(f)-1]})
			}
		}
		if strings.HasPrefix(s, "set ") {
			d.Settings = append(d.Settings, Setting{"vim.set", s[4:]})
		}
	}
	return d, nil
}

// Detect finds known editor configuration folders below home. It performs no writes.
func Detect(home, goos, xdg string) []Source {
	if home == "" {
		return nil
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	config := xdg
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	user := func(product string) string {
		if goos == "darwin" {
			return filepath.Join(home, "Library", "Application Support", product, "User")
		}
		if goos == "windows" {
			return filepath.Join(home, "AppData", "Roaming", product, "User")
		}
		return filepath.Join(config, product, "User")
	}
	var out []Source
	for _, p := range []struct{ id, title, folder string }{{"vscode", "VS Code", "Code"}, {"vscodium", "VSCodium", "VSCodium"}, {"cursor", "Cursor", "Cursor"}, {"windsurf", "Windsurf", "Windsurf"}} {
		d := user(p.folder)
		if existsAny(d, "settings.json", "keybindings.json") {
			out = append(out, Source{p.id, p.title, d, []string{"settings.json", "keybindings.json"}})
		}
	}
	z := filepath.Join(config, "zed")
	if goos == "darwin" {
		z = filepath.Join(home, ".config", "zed")
	}
	if existsAny(z, "settings.json", "keymap.json") {
		out = append(out, Source{"zed", "Zed", z, []string{"settings.json", "keymap.json"}})
	}
	h := filepath.Join(config, "helix")
	if existsAny(h, "config.toml", "languages.toml") {
		out = append(out, Source{"helix", "Helix", h, []string{"config.toml", "languages.toml"}})
	}
	jbroot := filepath.Join(config, "JetBrains")
	if goos == "darwin" {
		jbroot = filepath.Join(home, "Library", "Application Support", "JetBrains")
	}
	if goos == "windows" {
		jbroot = filepath.Join(home, "AppData", "Roaming", "JetBrains")
	}
	if ents, e := os.ReadDir(jbroot); e == nil {
		var dirs []string
		for _, x := range ents {
			if x.IsDir() {
				dirs = append(dirs, filepath.Join(jbroot, x.Name()))
			}
		}
		sort.Slice(dirs, func(i, j int) bool { return dirs[i] > dirs[j] })
		if len(dirs) > 0 {
			out = append(out, Source{"jetbrains", "JetBrains", dirs[0], []string{"keymaps/*.xml", "options/editor.xml"}})
		}
	}
	sublime := filepath.Join(config, "sublime-text", "Packages", "User")
	if goos == "darwin" {
		sublime = filepath.Join(home, "Library", "Application Support", "Sublime Text", "Packages", "User")
	}
	if existsAny(sublime, "Preferences.sublime-settings", "Default (Linux).sublime-keymap", "Default (OSX).sublime-keymap", "Default (Windows).sublime-keymap") {
		out = append(out, Source{"sublime", "Sublime", sublime, []string{"Preferences.sublime-settings", "Default (<OS>).sublime-keymap"}})
	}
	vim := filepath.Join(home, ".vimrc")
	if _, e := os.Stat(vim); e == nil {
		out = append(out, Source{"vim", "Vim", home, []string{".vimrc"}})
	}
	return out
}
func existsAny(dir string, names ...string) bool {
	for _, n := range names {
		if _, e := os.Stat(filepath.Join(dir, n)); e == nil {
			return true
		}
	}
	return false
}
