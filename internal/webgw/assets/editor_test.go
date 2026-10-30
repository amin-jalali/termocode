package assets

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The in-page editor (WEB-39) is its own file, loaded on first edit.

// Budgets of the editor, gzipped: the editor file alone, and the whole
// page once the editor is loaded (web design open question 5: vendor the
// editor only if the page stays at most 350 KiB).
const (
	budgetEditor   = 100 << 10
	budgetWithEdit = 350 << 10
)

func TestPerfGuardEditorBudget(t *testing.T) {
	ed := gzSize(t, "editor.js")
	all := ed + gzSize(t, "app.js") + gzSize(t, "app.css") + gzSize(t, "index.html")
	t.Logf("gzipped: editor.js %d B, page with the editor %d B", ed, all)
	if ed > budgetEditor {
		t.Errorf("editor.js is %d B gzipped, budget %d B", ed, budgetEditor)
	}
	if all > budgetWithEdit {
		t.Errorf("the page with the editor is %d B gzipped, budget %d B", all, budgetWithEdit)
	}
}

// The page's one dynamic import is the editor file, by a fixed path.
func TestTheOnlyDynamicImportIsTheEditor(t *testing.T) {
	re := regexp.MustCompile(`\bimport\s*\(([^)]*)\)`)
	var found []string
	err := filepath.WalkDir("src", func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		code := regexp.MustCompile(`(?m)//.*$|(?s)/\*.*?\*/`).ReplaceAllString(string(b), "")
		for _, m := range re.FindAllStringSubmatch(code, -1) {
			found = append(found, filepath.ToSlash(p)+" "+m[1])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0] != `src/lib/lazy.ts "/assets/editor.js"` {
		t.Errorf("dynamic imports %q, want only lib/lazy.ts loading /assets/editor.js", found)
	}
}

// The editor's syntax classes (inside its shadow root) are the read
// view's: same classes, same properties, same tokens.
func TestEditorHlStylesMatchTheReadView(t *testing.T) {
	css, err := os.ReadFile("src/styles/hl.css")
	if err != nil {
		t.Fatal(err)
	}
	ts, err := os.ReadFile("src/editor/hlstyle.ts")
	if err != nil {
		t.Fatal(err)
	}
	var a, b []string
	for _, m := range regexp.MustCompile(`(\.hl-[a-z]+)\s*\{([^}]*)\}`).FindAllStringSubmatch(string(css), -1) {
		for _, d := range strings.Split(m[2], ";") {
			if kv := strings.SplitN(strings.TrimSpace(d), ":", 2); len(kv) == 2 {
				a = append(a, m[1]+" "+strings.TrimSpace(kv[0])+"="+strings.TrimSpace(kv[1]))
			}
		}
	}
	camel := regexp.MustCompile(`[A-Z]`)
	for _, m := range regexp.MustCompile(`"(\.hl-[a-z]+)":\s*\{([^}]*)\}`).FindAllStringSubmatch(string(ts), -1) {
		for _, kv := range regexp.MustCompile(`(\w+):\s*"([^"]*)"`).FindAllStringSubmatch(m[2], -1) {
			prop := camel.ReplaceAllStringFunc(kv[1], func(s string) string { return "-" + strings.ToLower(s) })
			b = append(b, m[1]+" "+prop+"="+kv[2])
		}
	}
	sort.Strings(a)
	sort.Strings(b)
	if len(a) == 0 || strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Errorf("hl.css and editor/hlstyle.ts differ:\ncss:\n%s\nts:\n%s", strings.Join(a, "\n"), strings.Join(b, "\n"))
	}
}
