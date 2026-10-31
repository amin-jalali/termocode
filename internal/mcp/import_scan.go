package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Import from other apps: the known MCP config files of other apps are
// read (never written) and their servers offered for import. The paths
// live only here; the UI shows a file path, never an app name.

// FoundFile is one other app's file with servers to import.
type FoundFile struct {
	Path     string
	Servers  []Imported // tokens already moved to Secrets
	Problems []string   // entries that can not run (left out)
}

// OtherAppFiles lists the known config files for goos under home
// (appData is %APPDATA% on Windows).
func OtherAppFiles(home, goos, appData string) []string {
	if home == "" {
		return nil
	}
	j := func(p ...string) string { return filepath.Join(append([]string{home}, p...)...) }
	files := []string{
		j(".claude.json"),
		j(".cursor", "mcp.json"),
		j(".codeium", "windsurf", "mcp_config.json"),
		j(".gemini", "settings.json"),
	}
	switch goos {
	case "darwin":
		as := j("Library", "Application Support")
		files = append(files,
			filepath.Join(as, "Claude", "claude_desktop_config.json"),
			filepath.Join(as, "Code", "User", "mcp.json"))
	case "windows":
		if appData != "" {
			files = append(files,
				filepath.Join(appData, "Claude", "claude_desktop_config.json"),
				filepath.Join(appData, "Code", "User", "mcp.json"))
		}
	default:
		files = append(files,
			j(".config", "Claude", "claude_desktop_config.json"),
			j(".config", "Code", "User", "mcp.json"))
	}
	return files
}

// ScanOtherApps reads files and returns the ones with servers to import,
// sorted by path. A name in have (already configured) or offered by an
// earlier file is left out. Missing or broken files are skipped.
func ScanOtherApps(files []string, have map[string]bool) []FoundFile {
	seen := map[string]bool{}
	for n := range have {
		seen[n] = true
	}
	var out []FoundFile
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil || len(b) > 4<<20 {
			continue
		}
		list, problems, err := ParseImport(string(onlyServers(b)))
		if err != nil {
			continue
		}
		f := FoundFile{Path: p, Problems: problems}
		for _, im := range list {
			if !seen[im.Name] {
				seen[im.Name] = true
				f.Servers = append(f.Servers, im)
			}
		}
		if len(f.Servers) > 0 {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// onlyServers keeps the servers keys of a big settings file, so its other
// keys are not read as server names ("{}" when it has none).
func onlyServers(b []byte) []byte {
	var top map[string]any
	if err := json.Unmarshal(b, &top); err != nil {
		return b
	}
	keep := map[string]any{}
	for _, k := range []string{"mcpServers", "servers"} {
		if v, ok := top[k]; ok {
			keep[k] = v
		}
	}
	if len(keep) == 0 {
		keep["mcpServers"] = map[string]any{}
	}
	out, _ := json.Marshal(keep)
	return out
}

// PreviewLine is what an import would run: the command line or the url
// (tokens are already ${secret:} references).
func PreviewLine(im Imported) string {
	if u, ok := im.Entry["url"].(string); ok && u != "" {
		return u
	}
	parts := []string{}
	if c, ok := im.Entry["command"].(string); ok {
		parts = append(parts, c)
	}
	if args, ok := im.Entry["args"].([]any); ok {
		for _, a := range args {
			if s, ok := a.(string); ok {
				if secretLiteralRe.MatchString(refRe.ReplaceAllString(s, "")) {
					s = "•••" // a token typed into args is never shown
				}
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, " ")
}
