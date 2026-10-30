package assets

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Lint of src/strings.ts (web UX 7.4, P6): every user-facing string in
// one file, short, calm, unique, no other product named, and every key
// the code uses exists.

type entry struct {
	key, text string
	line      int
}

var entryRe = regexp.MustCompile(`^\s*"([^"]+)":\s*"((?:[^"\\]|\\.)*)",\s*$`)

// parseStrings reads the entries of the exported object `name` in file.
func parseStrings(t *testing.T, file, name string) []entry {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "export const "+name+" ") || strings.HasPrefix(l, "export const "+name+":") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s has no export const %s", file, name)
	}
	var out []entry
	for i := start + 1; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "}") {
			return out
		}
		if l == "" || strings.HasPrefix(l, "//") {
			continue
		}
		m := entryRe.FindStringSubmatch(lines[i])
		if m == nil {
			t.Errorf("%s:%d: not one `\"key\": \"text\",` entry: %q", file, i+1, l)
			continue
		}
		out = append(out, entry{key: m[1], text: m[2], line: i + 1})
	}
	t.Fatalf("%s: %s is not closed", file, name)
	return nil
}

// bodyKeys may run to 40 words; every other string stays at 12 (P6).
func isBody(key string) bool {
	if strings.HasPrefix(key, "e.") || strings.HasPrefix(key, "err.") || strings.Contains(key, "body") {
		return true
	}
	switch key {
	case "firstrun", "chat.empty", "term.info", "term.screen_banner", "file.big", "edit.conflict",
		"card.viewonly", "runs.stop_confirm", "wait.e14":
		return true
	}
	return false
}

// otherProducts must never appear in the UI (owner rule): editors, IDEs,
// terminals and AI products. Matched as whole words, case-insensitive,
// except names that are also plain words, matched with their capital.
var otherProducts = regexp.MustCompile(`(?i)\b(vs ?code|visual studio|vim|neovim|nvim|emacs|sublime|jetbrains|intellij|goland|webstorm|xcode|eclipse|zed|copilot|chatgpt|openai|claude|anthropic|gemini|windsurf|replit|codespaces|gitpod|iterm2?|tmux|warp|termius|wezterm|alacritty|kitty|ghostty)\b`)

// otherProductsCased are names that are also plain words ("the cursor").
var otherProductsCased = regexp.MustCompile(`\b(Cursor|Atom|Helix)\b`)

func namesProduct(s string) string {
	if m := otherProducts.FindString(s); m != "" {
		return m
	}
	return otherProductsCased.FindString(s)
}

var calmBanned = regexp.MustCompile(`(?i)!|\boops\b|went wrong|\bsorry\b|\bplease\b|\berror\b|\bfailed to\b`)

func lintEntries(entries []entry) []string {
	var bad []string
	keys, texts := map[string]int{}, map[string]string{}
	keyRe := regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)*$`)
	for _, e := range entries {
		where := e.key
		if !keyRe.MatchString(e.key) {
			bad = append(bad, where+": key is not lower-case dotted words")
		}
		if _, dup := keys[e.key]; dup {
			bad = append(bad, where+": key listed twice")
		}
		keys[e.key] = e.line
		if other, dup := texts[e.text]; dup {
			bad = append(bad, where+": same text as "+other+"; use one key")
		}
		texts[e.text] = e.key
		if strings.TrimSpace(e.text) == "" {
			bad = append(bad, where+": empty")
		}
		if m := calmBanned.FindString(e.text); m != "" {
			bad = append(bad, where+": calm copy has "+m)
		}
		if m := namesProduct(e.text); m != "" {
			bad = append(bad, where+": names another product: "+m)
		}
		words, max := len(strings.Fields(e.text)), 12
		if isBody(e.key) {
			max = 40
		}
		if words > max {
			bad = append(bad, where+": "+strconv.Itoa(words)+" words, max "+strconv.Itoa(max))
		}
		if len(e.text) > 240 {
			bad = append(bad, where+": longer than 240 bytes")
		}
		for _, ph := range regexp.MustCompile(`\{([^}]*)\}`).FindAllStringSubmatch(e.text, -1) {
			if !regexp.MustCompile(`^[a-z_]+$`).MatchString(ph[1]) {
				bad = append(bad, where+": bad placeholder {"+ph[1]+"}")
			}
		}
	}
	return bad
}

func TestStringsLintCatchesEachRule(t *testing.T) {
	for _, bad := range [][]entry{
		{{key: "a", text: "Saved!"}},
		{{key: "a", text: "Oops, something went wrong."}},
		{{key: "a", text: "Open in VS Code"}},
		{{key: "a", text: "Ask Copilot"}},
		{{key: "a", text: "Move the Cursor"}},
		{{key: "a", text: "one two three four five six seven eight nine ten eleven twelve thirteen"}},
		{{key: "a", text: "x"}, {key: "a", text: "y"}},
		{{key: "a", text: "x"}, {key: "b", text: "x"}},
		{{key: "A.b", text: "x"}},
		{{key: "a", text: "Hi {Name}"}},
		{{key: "a", text: ""}},
	} {
		if len(lintEntries(bad)) == 0 {
			t.Errorf("lint passed %+v", bad)
		}
	}
	if got := lintEntries([]entry{{key: "x.y", text: "The cursor moves; no product here."}}); len(got) != 0 {
		t.Errorf("lint refused clean text: %v", got)
	}
}

func TestStringsFollowTheVoice(t *testing.T) {
	entries := parseStrings(t, "src/strings.ts", "S")
	if len(entries) < 150 {
		t.Fatalf("only %d strings; web UX 7.4 lists more", len(entries))
	}
	for _, bad := range lintEntries(entries) {
		t.Error(bad)
	}
}

// keyLiteral is a string literal shaped like a key ("tab.chat").
var keyLiteral = regexp.MustCompile(`"([a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+)"`)
var tCall = regexp.MustCompile(`\btn?\(\s*"([^"]+)"`)

func TestEveryUsedStringKeyExists(t *testing.T) {
	entries := parseStrings(t, "src/strings.ts", "S")
	keys, spaces := map[string]bool{}, map[string]bool{}
	for _, e := range entries {
		keys[e.key] = true
		spaces[strings.Split(e.key, ".")[0]] = true
	}
	exists := func(k string) bool { return keys[k] || (keys[k+".one"] && keys[k+".other"]) }
	n := 0
	err := filepath.WalkDir("src", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) == "strings.ts" || !strings.HasSuffix(path, "x") && !strings.HasSuffix(path, ".ts") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range tCall.FindAllStringSubmatch(string(b), -1) {
			n++
			if !exists(m[1]) {
				t.Errorf("%s: t(%q): no such string", path, m[1])
			}
		}
		for _, m := range keyLiteral.FindAllStringSubmatch(string(b), -1) {
			if spaces[strings.Split(m[1], ".")[0]] && !exists(m[1]) {
				t.Errorf("%s: %q looks like a string key but is not one", path, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no t() calls found")
	}
}

// The Persian file (WEB-UX-5 fills it) has exactly the English keys, so
// a new string cannot be forgotten there.
func TestPersianStringsHaveTheSameKeys(t *testing.T) {
	en := parseStrings(t, "src/strings.ts", "S")
	fa := parseStrings(t, "src/strings.fa.ts", "FA")
	enKeys := map[string]bool{}
	for _, e := range en {
		enKeys[e.key] = true
	}
	faKeys := map[string]bool{}
	for _, e := range fa {
		faKeys[e.key] = true
		if !enKeys[e.key] {
			t.Errorf("strings.fa.ts:%d: %s is not an English key", e.line, e.key)
		}
		if strings.Contains(e.text, "!") || namesProduct(e.text) != "" {
			t.Errorf("strings.fa.ts:%d: %s breaks the voice rules", e.line, e.key)
		}
	}
	for k := range enKeys {
		if !faKeys[k] {
			t.Errorf("strings.fa.ts has no %s", k)
		}
	}
}
