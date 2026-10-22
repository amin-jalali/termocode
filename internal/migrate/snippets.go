package migrate

import (
	"bytes"
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

// normalizeScope folds a snippets.json scope the way termocode reads it:
// lower case, and "", "global" and "all" mean "*".
func normalizeScope(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "global", "all":
		return globalScope
	}
	return s
}

// existingSnippets reads the user's snippets.json into scope -> trigger ->
// body. Entries termocode would skip (bad body) are left out.
func existingSnippets(raw []byte) map[string]map[string]string {
	out := map[string]map[string]string{}
	var top map[string]map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return out
	}
	for scope, items := range top {
		sc := normalizeScope(scope)
		for trig, rb := range items {
			lines, ok := stringOrLines(rb)
			if !ok {
				continue
			}
			if out[sc] == nil {
				out[sc] = map[string]string{}
			}
			out[sc][trig] = strings.Join(lines, "\n")
		}
	}
	return out
}

// oneLine shortens a body to its first line for a review row.
func oneLine(s string) string {
	first, rest, more := strings.Cut(s, "\n")
	first = strings.TrimSpace(first)
	if more && rest != "" {
		first += " …"
	}
	return first
}

// addSnippetRows adds one review row per snippet. A snippet whose trigger
// the user already has in the same scope is a conflict (off) when the body
// differs, and "already there" (off) when it is the same.
func (p *Plan) addSnippetRows(snippets []Snippet) {
	have := existingSnippets(p.Current.SnippetsRaw)
	for _, s := range snippets {
		row := ReviewRow{Kind: RowSnippet, Scope: s.Scope, Trigger: s.Trigger, Body: s.Body, Apply: true,
			SourceText: scopeLabel(s.Scope) + " · " + s.Trigger + " · " + s.Name}
		label := s.Scope + ":" + s.Trigger
		if s.Scope == globalScope {
			label = s.Trigger
		}
		row.Equivalent = label + "  " + oneLine(s.Body)
		if cur, ok := have[s.Scope][s.Trigger]; ok {
			row.Apply = false
			if cur != s.Body {
				row.Conflict = true
				row.Current = cur
				row.Equivalent = label + "  " + oneLine(cur) + " → " + oneLine(s.Body)
			} else {
				row.Equivalent += " (already there)"
			}
		}
		p.Rows = append(p.Rows, row)
	}
}

// applySnippets renders the selected snippet rows into the complete
// snippets.json, keeping everything else in the user's file as it is.
// nil when no snippet row is selected.
func applySnippets(plan Plan) ([]byte, error) {
	var picked []ReviewRow
	for _, r := range plan.Rows {
		if r.Kind == RowSnippet && r.Apply && r.Trigger != "" {
			picked = append(picked, r)
		}
	}
	if len(picked) == 0 {
		return nil, nil
	}
	top := map[string]json.RawMessage{}
	if plan.Current.SnippetsExist && len(strings.TrimSpace(string(plan.Current.SnippetsRaw))) == 0 {
		return nil, fmt.Errorf("raw snippets.json is required when the file exists")
	}
	if len(plan.Current.SnippetsRaw) > 0 {
		if err := json.Unmarshal(plan.Current.SnippetsRaw, &top); err != nil || top == nil {
			return nil, fmt.Errorf("snippets.json is not a JSON object; it was left alone")
		}
	}
	scopes := map[string]map[string]json.RawMessage{} // the scopes that changed, by their key
	scopeOf := func(key string) (map[string]json.RawMessage, error) {
		if m, ok := scopes[key]; ok {
			return m, nil
		}
		m := map[string]json.RawMessage{}
		if raw, ok := top[key]; ok {
			if err := json.Unmarshal(raw, &m); err != nil || m == nil {
				return nil, fmt.Errorf("snippets.json: %q is not an object; it was left alone", key)
			}
		}
		scopes[key] = m
		return m, nil
	}
	for _, r := range picked {
		key := r.Scope
		var keys []string
		for k := range top {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if normalizeScope(k) != r.Scope {
				continue
			}
			var m map[string]json.RawMessage
			if json.Unmarshal(top[k], &m) == nil {
				if _, has := m[r.Trigger]; has {
					key = k // replace the snippet where the user keeps it
					break
				}
			}
			if _, exact := top[r.Scope]; !exact && key == r.Scope {
				key = k // keep to an existing alias such as "global"
			}
		}
		m, err := scopeOf(key)
		if err != nil {
			return nil, err
		}
		var v any = r.Body
		if strings.Contains(r.Body, "\n") {
			v = strings.Split(r.Body, "\n")
		}
		m[r.Trigger] = marshalNoEscape(v)
	}
	for key, m := range scopes {
		top[key] = marshalNoEscape(m)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(top); err != nil {
		return nil, fmt.Errorf("encode snippets: %w", err)
	}
	return buf.Bytes(), nil
}

// marshalNoEscape encodes v compactly, leaving "<", ">" and "&" readable.
func marshalNoEscape(v any) json.RawMessage {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return json.RawMessage(bytes.TrimSpace(buf.Bytes()))
}
