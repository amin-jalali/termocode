package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// C-6: in Databases, Containers, the MCP server and Import, every palette
// title and button that opens more input ends with "…" (U+2026), never
// "...", palette titles are in Title Case and buttons in sentence case.

// featureTitle reports whether a palette id belongs to one of the four
// features.
func featureTitle(id string) bool {
	for _, p := range []string{"db-", "redis-", "containers-", "services-", "mcp-serve-", "prefs-import-"} {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

var titleSmallWords = map[string]bool{"a": true, "an": true, "and": true, "as": true, "at": true, "by": true, "for": true, "from": true,
	"in": true, "into": true, "of": true, "on": true, "or": true, "the": true, "to": true, "with": true}

func TestFeaturePaletteTitlesEllipsisAndCase(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	titles := map[string]string{}
	for _, e := range PaletteCatalog() {
		if featureTitle(e.ID) {
			titles[e.ID] = e.Title
		}
	}
	if len(titles) < 80 {
		t.Fatalf("only %d feature titles", len(titles))
	}
	for id, title := range titles {
		if strings.Contains(title, "...") {
			t.Errorf("%s: %q has three dots, want …", id, title)
		}
		_, rest, ok := strings.Cut(title, ": ")
		if !ok {
			t.Errorf("%s: %q has no group prefix", id, title)
			continue
		}
		for i, w := range strings.Fields(strings.TrimRight(rest, "…")) {
			r, _ := utf8.DecodeRuneInString(w)
			if (i == 0 || !titleSmallWords[w]) && unicode.IsLower(r) && w != "termocode" && w != "curl" && w != "(services.json)" {
				t.Errorf("%s: %q is not in Title Case (%q)", id, title, w)
			}
		}
	}
	// These open a card, a picker or an input first: they end with "…".
	for _, id := range []string{"db-explain-analyze", "db-disconnect", "db-submit-changes", "db-revert-changes", "db-history", "db-add",
		"db-filter", "db-export", "db-import-csv", "mcp-serve-status", "prefs-import-editor", "prefs-import-undo",
		"containers-remove", "containers-prune", "containers-compose-down", "containers-image-pull", "containers-search"} {
		if !strings.HasSuffix(titles[id], "…") {
			t.Errorf("%s: %q opens more input and must end with …", id, titles[id])
		}
	}
	// The ids do not change.
	for _, id := range []string{"db-disconnect", "db-submit-changes", "db-revert-changes", "db-explain-analyze", "mcp-serve-status", "prefs-import-undo"} {
		if titles[id] == "" {
			t.Errorf("palette id %s is gone", id)
		}
	}
}

// C-6: the buttons of the cards of the four features: no three dots, sentence
// case (the first word capitalised, the others lower case, except acronyms
// and names), and the ones that open more input end with "…".
func TestFeatureButtonsAreSentenceCase(t *testing.T) {
	var files []string
	for _, g := range []string{"db_*.go", "containers_*.go", "services_*.go", "mcp_*.go", "agent_mcp*.go", "ai_mcp*.go", "import_*.go"} {
		m, _ := filepath.Glob(g)
		files = append(files, m...)
	}
	btn := regexp.MustCompile(`confirm\.Button\{[^}]*?Title: *"([^"]+)"`)
	short := regexp.MustCompile(`\{ID: *"[^"]*", *Title: *"([^"]+)"`)
	known := map[string]bool{"ID": true, "SQL": true, "CSV": true, "DDL": true, "TTL": true, "SSH": true, "SSL": true, "TLS": true, "CA": true,
		"Docker": true, "Hub": true, "OK": true, "JSON": true, "known_hosts": true, "termocode": true}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		var all [][]string
		all = append(all, btn.FindAllStringSubmatch(src, -1)...)
		for _, line := range strings.Split(src, "\n") {
			if strings.Contains(line, "confirm.Button") {
				all = append(all, short.FindAllStringSubmatch(line, -1)...)
			}
		}
		for _, m := range all {
			title := m[1]
			checked++
			if strings.Contains(title, "...") {
				t.Errorf("%s: button %q has three dots, want …", f, title)
			}
			for i, w := range strings.Fields(strings.TrimRight(title, "…")) {
				r, _ := utf8.DecodeRuneInString(w)
				switch {
				case i == 0 && !unicode.IsUpper(r):
					t.Errorf("%s: button %q does not start with a capital", f, title)
				case i > 0 && unicode.IsUpper(r) && !known[w] && !strings.HasPrefix(w, "%"):
					t.Errorf("%s: button %q is not in sentence case (%q)", f, title, w)
				}
			}
		}
	}
	if checked < 60 {
		t.Fatalf("only %d buttons found: the scan is broken", checked)
	}
	for _, c := range []struct{ file, want string }{
		{"db_import.go", `Title: "Map columns…"`}, {"containers_registries.go", `Title: "Type a new token…"`},
		{"containers_remote.go", `Title: "Pull…"`}, {"import_undo.go", `Title: "Undo import"`},
	} {
		b, _ := os.ReadFile(c.file)
		if !strings.Contains(string(b), c.want) {
			t.Errorf("%s lacks %s", c.file, c.want)
		}
	}
}
