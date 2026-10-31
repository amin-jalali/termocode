package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/cfgfile"
)

// MCP profiles (MCPX-27): named sets of servers and tools, so a chat can
// use only what a task needs ("web": github and linear; "docs": notion
// read tools). A profile only narrows the list: it never adds a server,
// a command or a secret, so a project file (.termocode/mcp-profiles.json)
// is safe to share and to read without the trust card.
//
//	{"profiles": {"web": {"description": "…", "servers": ["github"],
//	  "tools": ["github.*", "!github.delete_*"]}}}
//
// servers empty: every server. tools: globs on "server.tool"; a leading
// "!" removes; with only "!" globs every other tool stays.
type Profile struct {
	Name        string   `json:"-"`
	Description string   `json:"description,omitempty"`
	Servers     []string `json:"servers,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Source      string   `json:"-"` // "user" or "project"
}

type profilesFile struct {
	Profiles map[string]Profile `json:"profiles"`
}

var profileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// ProfilesPath is the user's profiles file in the config dir.
func ProfilesPath(configDir string) string { return filepath.Join(configDir, "mcp-profiles.json") }

// ProjectProfilesPath is a workspace's shared profiles file.
func ProjectProfilesPath(root string) string {
	return filepath.Join(root, ".termocode", "mcp-profiles.json")
}

// AllowsServer reports whether the profile keeps server.
func (p Profile) AllowsServer(server string) bool {
	if len(p.Servers) == 0 {
		return true
	}
	for _, s := range p.Servers {
		if s == server {
			return true
		}
	}
	return false
}

// Allows reports whether the profile keeps server's tool.
func (p Profile) Allows(server, tool string) bool {
	if !p.AllowsServer(server) {
		return false
	}
	full := server + "." + tool
	var in, out []string
	for _, g := range p.Tools {
		if rest, ok := strings.CutPrefix(g, "!"); ok {
			out = append(out, rest)
		} else {
			in = append(in, g)
		}
	}
	if matchAny(out, full) {
		return false
	}
	return len(in) == 0 || matchAny(in, full)
}

// Summary is a short line for the picker: "2 servers · 3 tool rules".
func (p Profile) Summary() string {
	srv := "all servers"
	if n := len(p.Servers); n == 1 {
		srv = "1 server: " + p.Servers[0]
	} else if n > 1 {
		srv = fmt.Sprintf("%d servers: %s", n, strings.Join(p.Servers, ", "))
	}
	if len(p.Tools) == 0 {
		return srv + " · all tools"
	}
	return srv + " · " + strings.Join(p.Tools, " ")
}

func readProfiles(path, source string) ([]Profile, []string) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{path + ": " + err.Error()}
	}
	var f profilesFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, []string{path + ": " + err.Error()}
	}
	var out []Profile
	var warns []string
	for name, p := range f.Profiles {
		if !profileName.MatchString(name) {
			warns = append(warns, fmt.Sprintf("%s: profile %q: use letters, digits, . _ -", path, name))
			continue
		}
		p.Name, p.Source = name, source
		out = append(out, p)
	}
	return out, warns
}

// LoadProfiles reads the user's and the project's profiles, sorted by
// name; a user profile wins over a project one with the same name.
func LoadProfiles(userPath, projectPath string) ([]Profile, []string) {
	user, w1 := readProfiles(userPath, "user")
	proj, w2 := readProfiles(projectPath, "project")
	byName := map[string]Profile{}
	for _, p := range proj {
		byName[p.Name] = p
	}
	for _, p := range user {
		byName[p.Name] = p
	}
	out := make([]Profile, 0, len(byName))
	for _, p := range byName {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, append(w1, w2...)
}

// FindProfile picks name from ps.
func FindProfile(ps []Profile, name string) (Profile, bool) {
	for _, p := range ps {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

// FilterServerTools keeps what p allows; nil p keeps everything. sts is
// not changed.
func FilterServerTools(sts []ServerTools, p *Profile) []ServerTools {
	if p == nil {
		return sts
	}
	var out []ServerTools
	for _, st := range sts {
		if !p.AllowsServer(st.Server.Name) {
			continue
		}
		var tools []Tool
		for _, t := range st.Tools {
			if p.Allows(st.Server.Name, t.Name) {
				tools = append(tools, t)
			}
		}
		if len(tools) > 0 {
			st.Tools = tools
			out = append(out, st)
		}
	}
	return out
}

// ExportProfiles writes ps as a shareable profiles file.
func ExportProfiles(ps []Profile) ([]byte, error) {
	f := profilesFile{Profiles: map[string]Profile{}}
	for _, p := range ps {
		f.Profiles[p.Name] = p
	}
	b, err := json.MarshalIndent(f, "", "  ")
	return append(b, '\n'), err
}

var profilesMu sync.Mutex

// ImportProfiles adds the profiles of a shared file to the user's file
// (same names are replaced) and returns their names.
func ImportProfiles(userPath string, data []byte) ([]string, error) {
	var f profilesFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("not a profiles file: %w", err)
	}
	if len(f.Profiles) == 0 {
		return nil, errors.New("the file has no profiles")
	}
	for name := range f.Profiles {
		if !profileName.MatchString(name) {
			return nil, fmt.Errorf("profile %q: use letters, digits, . _ -", name)
		}
	}
	profilesMu.Lock()
	defer profilesMu.Unlock()
	cur, _ := readProfiles(userPath, "user")
	all := map[string]Profile{}
	for _, p := range cur {
		all[p.Name] = p
	}
	var names []string
	for name, p := range f.Profiles {
		p.Name = name
		all[name] = p
		names = append(names, name)
	}
	sort.Strings(names)
	list := make([]Profile, 0, len(all))
	for _, p := range all {
		list = append(list, p)
	}
	b, err := ExportProfiles(list)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		return nil, err
	}
	return names, cfgfile.WriteFile(userPath, b)
}
