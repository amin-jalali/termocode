// Package ext finds termocode extensions on disk and describes what they
// registered.
//
// An extension is a folder under Dir():
//
//	~/.config/termocode/extensions/<name>/
//	    init.lua         required — runs inside the embedded nvim
//	    extension.json   optional — {"name", "version", "description"}
//	    lua/             optional — modules the extension can require()
//
// The Lua side (the _G.termocode API) lives in internal/app/ext_lua.go;
// this package is pure Go so discovery, manifests, markup and scaffolding
// can be unit-tested without nvim. See docs/adr/0006-lua-extension-host.md.
package ext

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// InitFile is the entry point every extension folder must contain.
const InitFile = "init.lua"

// ManifestFile is the optional metadata file next to InitFile.
const ManifestFile = "extension.json"

// Dir returns the folder that holds the user's extensions. It honours
// $XDG_CONFIG_HOME like every other termocode config file.
func Dir() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "termocode", "extensions"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "termocode", "extensions"), nil
}

// Manifest is the optional extension.json. Every field is optional.
type Manifest struct {
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
}

// Extension is one folder found by Discover.
type Extension struct {
	// Name is the folder name. It is the extension's identity: commands
	// without a dot in their id are prefixed with it, and its autocmds
	// live in the augroup "termocode_ext_<Name>".
	Name     string `json:"name"`
	Dir      string `json:"dir"`
	Init     string `json:"init"`
	Version  string `json:"version"`
	Manifest Manifest
	// Problem is a non-fatal issue found while reading the folder (for
	// example a broken extension.json). The extension still loads.
	Problem string `json:"-"`
}

// validName limits extension folder names to something safe to use in an
// augroup name, a command id prefix and a file path.
var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// ValidName reports whether s can be used as an extension name.
func ValidName(s string) bool { return validName.MatchString(s) && len(s) <= 64 }

// Discover lists the extensions in dir, sorted by name. Folders without an
// init.lua, hidden folders ("." prefix) and folders whose name ends in
// ".disabled" are skipped. A missing dir is not an error. Problems with a
// single extension never stop the others: they are reported in
// Extension.Problem. Folders that cannot load at all (bad name) are
// listed in skipped with the reason.
func Discover(dir string) (exts []Extension, skipped []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".disabled") {
			continue
		}
		full := filepath.Join(dir, name)
		info, err := os.Stat(full) // follow symlinks to folders
		if err != nil || !info.IsDir() {
			continue
		}
		initPath := filepath.Join(full, InitFile)
		if st, err := os.Stat(initPath); err != nil || st.IsDir() {
			continue
		}
		if !ValidName(name) {
			skipped = append(skipped, fmt.Sprintf("%s: folder name has unsupported characters (use letters, digits, _ . -)", name))
			continue
		}
		x := Extension{Name: name, Dir: full, Init: initPath}
		if b, err := os.ReadFile(filepath.Join(full, ManifestFile)); err == nil {
			if err := json.Unmarshal(b, &x.Manifest); err != nil {
				x.Problem = "extension.json: " + err.Error()
				x.Manifest = Manifest{}
			}
		}
		x.Version = x.Manifest.Version
		exts = append(exts, x)
	}
	sort.Slice(exts, func(i, j int) bool { return exts[i].Name < exts[j].Name })
	return exts, skipped
}

// ── Registry (what the Lua side reports back) ────────────────────────────

// Command is one palette entry registered with termocode.register_command.
type Command struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Ext   string `json:"ext"`
	Hint  string `json:"hint"`
}

// Panel is a sidebar panel registered with termocode.register_panel.
type Panel struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Icon  string `json:"icon"`
	Ext   string `json:"ext"`
}

// StatusItem is a status-bar item registered with
// termocode.register_status_item.
type StatusItem struct {
	ID      string `json:"id"`
	Ext     string `json:"ext"`
	Command string `json:"command"`
}

// Loaded is the per-extension load result.
type Loaded struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
}

// Registry is the JSON document returned by _termocode_ext.registry().
type Registry struct {
	Commands   []Command    `json:"commands"`
	Panels     []Panel      `json:"panels"`
	Status     []StatusItem `json:"status"`
	Extensions []Loaded     `json:"extensions"`
}

// ParseRegistry decodes the registry JSON. Empty input is an empty
// registry. Lua encodes an empty table as {} (not []); those fields are
// tolerated and read as empty.
func ParseRegistry(s string) (Registry, error) {
	var r Registry
	s = strings.TrimSpace(s)
	if s == "" {
		return r, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return r, err
	}
	dec := func(key string, into any) error {
		b, ok := raw[key]
		if !ok || isEmptyJSON(b) {
			return nil
		}
		return json.Unmarshal(b, into)
	}
	if err := dec("commands", &r.Commands); err != nil {
		return r, err
	}
	if err := dec("panels", &r.Panels); err != nil {
		return r, err
	}
	if err := dec("status", &r.Status); err != nil {
		return r, err
	}
	if err := dec("extensions", &r.Extensions); err != nil {
		return r, err
	}
	return r, nil
}

// Poll is the JSON document returned by _termocode_ext.poll(): the lines
// of the visible panel (nil when none was asked for) and the current text
// of every status item.
type Poll struct {
	Panel  string       `json:"panel"`
	Lines  []string     `json:"lines"`
	Status []PollStatus `json:"status"`
}

// PollStatus is one status item's current text.
type PollStatus struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ParsePoll decodes the poll JSON (tolerating Lua's {} for empty arrays).
func ParsePoll(s string) (Poll, error) {
	var p Poll
	s = strings.TrimSpace(s)
	if s == "" {
		return p, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return p, err
	}
	if b, ok := raw["panel"]; ok && !isEmptyJSON(b) {
		_ = json.Unmarshal(b, &p.Panel)
	}
	if b, ok := raw["lines"]; ok && !isEmptyJSON(b) {
		if err := json.Unmarshal(b, &p.Lines); err != nil {
			return p, err
		}
	}
	if b, ok := raw["status"]; ok && !isEmptyJSON(b) {
		if err := json.Unmarshal(b, &p.Status); err != nil {
			return p, err
		}
	}
	return p, nil
}

func isEmptyJSON(b json.RawMessage) bool {
	t := strings.TrimSpace(string(b))
	return t == "" || t == "null" || t == "{}" || t == "[]"
}
