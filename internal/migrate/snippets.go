package migrate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

// Limits of the snippet reader (the editor files are not trusted).
const (
	maxSnippetFiles = 200
	maxSnippets     = 5000
	maxSnippetBody  = 64 << 10
)

// Snippet is one snippet of the other editor for one scope and trigger.
type Snippet struct {
	Scope       string // a Neovim filetype, or "*" for every filetype
	Trigger     string
	Name        string
	Description string
	Body        string // the lines joined with "\n", in termocode's snippet syntax
	File        string // the source file, relative to the editor folder
}

// vscodeFiletypes maps the VS Code language ids that differ from the
// Neovim filetype of the same language.
var vscodeFiletypes = map[string]string{
	"shellscript": "sh", "plaintext": "text", "csharp": "cs", "objective-c": "objc",
	"objective-cpp": "objcpp", "latex": "tex", "makefile": "make", "bat": "dosbatch",
	"powershell": "ps1", "git-commit": "gitcommit", "git-rebase": "gitrebase",
	"ignore": "gitignore", "properties": "jproperties", "restructuredtext": "rst",
	"jsonc": "jsonc", "dockerfile": "dockerfile", "vb": "vb", "perl6": "raku",
}

// snippetScope turns a VS Code language id into a termocode scope.
func snippetScope(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if ft, ok := vscodeFiletypes[lang]; ok {
		return ft
	}
	return lang
}

// validTrigger mirrors termocode's matcher: letters, digits and "_" only.
func validTrigger(t string) bool {
	if t == "" {
		return false
	}
	for _, r := range t {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// stringOrLines reads a JSON string or a list of strings.
func stringOrLines(raw json.RawMessage) ([]string, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []string{s}, true
	}
	var l []string
	if json.Unmarshal(raw, &l) == nil {
		return l, true
	}
	return nil, false
}

// readVSCodeSnippets reads User/snippets: per-language *.json (the file
// name is the language) and global *.code-snippets (an optional "scope"
// lists languages). It never leaves dir, never follows links and writes
// nothing. Problems become Skipped rows.
func readVSCodeSnippets(dir string, d *Document) {
	skip := func(source, reason string) {
		d.Skipped = append(d.Skipped, Skipped{RowSnippet, source, reason})
	}
	root, reason := resolveInside(dir, "snippets")
	if root == "" {
		if reason != "" {
			skip("snippets", reason)
		}
		return
	}
	names, links := listInside(dir, "snippets")
	for _, l := range links {
		skip("snippets/"+l, "is a link; not followed")
	}
	sort.Strings(names)
	type seenAt struct{ file, name string }
	seen := map[[2]string]seenAt{}
	files := 0
	for _, n := range names {
		lower := strings.ToLower(n)
		global := strings.HasSuffix(lower, ".code-snippets")
		if strings.HasPrefix(n, ".") || lower == "package.json" || !(global || strings.HasSuffix(lower, ".json")) {
			continue
		}
		rel := "snippets/" + n
		if files++; files > maxSnippetFiles {
			skip("snippets", fmt.Sprintf("more than %d files; the rest are not read", maxSnippetFiles))
			return
		}
		b, found, reason := readInside(dir, rel)
		if reason != "" {
			skip(rel, reason)
		}
		if !found {
			continue
		}
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(jsonc.Strip(trimBOM(b)), &entries); err != nil {
			skip(rel, "not valid JSON; skipped")
			continue
		}
		keys := make([]string, 0, len(entries))
		for k := range entries {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, name := range keys {
			var e struct {
				Prefix      json.RawMessage `json:"prefix"`
				Body        json.RawMessage `json:"body"`
				Description json.RawMessage `json:"description"`
				Scope       string          `json:"scope"`
			}
			label := rel + ": " + name
			if json.Unmarshal(entries[name], &e) != nil {
				skip(label, "is not a snippet; skipped")
				continue
			}
			prefixes, okp := stringOrLines(e.Prefix)
			lines, okb := stringOrLines(e.Body)
			body := strings.Join(lines, "\n")
			switch {
			case !okp || len(prefixes) == 0:
				skip(label, "has no prefix; termocode expands a snippet only by its trigger word")
				continue
			case !okb || strings.TrimSpace(body) == "":
				skip(label, "has no body; skipped")
				continue
			case len(body) > maxSnippetBody:
				skip(label, "body is larger than 64 KB; skipped")
				continue
			}
			converted, why := ConvertSnippetBody(body)
			var scopes []string
			if global {
				for _, s := range strings.Split(e.Scope, ",") {
					if s = snippetScope(s); s != "" {
						scopes = append(scopes, s)
					}
				}
			} else {
				scopes = []string{snippetScope(strings.TrimSuffix(n, filepath.Ext(n)))}
			}
			if len(scopes) == 0 {
				scopes = []string{globalScope}
			}
			desc, _ := stringOrLines(e.Description)
			for _, scope := range scopes {
				for _, trigger := range prefixes {
					src := scopeLabel(scope) + " · " + trigger + " · " + name
					if !validTrigger(trigger) {
						skip(src, fmt.Sprintf("the prefix %q has characters termocode cannot expand (letters, digits and _ only)", trigger))
						continue
					}
					if why != "" {
						skip(src, why)
						continue
					}
					if prev, dup := seen[[2]string{scope, trigger}]; dup {
						skip(src, fmt.Sprintf("same trigger as %q in %s; the first one is imported", prev.name, prev.file))
						continue
					}
					if len(d.Snippets) >= maxSnippets {
						skip("snippets", fmt.Sprintf("more than %d snippets; the rest are not read", maxSnippets))
						return
					}
					seen[[2]string{scope, trigger}] = seenAt{rel, name}
					d.Snippets = append(d.Snippets, Snippet{Scope: scope, Trigger: trigger, Name: name,
						Description: strings.Join(desc, " "), Body: converted, File: rel})
				}
			}
		}
	}
}

// globalScope is termocode's scope for "every filetype".
const globalScope = "*"

// scopeLabel is how a scope reads in a review row.
func scopeLabel(scope string) string {
	if scope == globalScope {
		return "all languages"
	}
	return scope
}

func trimBOM(b []byte) []byte { return []byte(strings.TrimPrefix(string(b), "\xef\xbb\xbf")) }
