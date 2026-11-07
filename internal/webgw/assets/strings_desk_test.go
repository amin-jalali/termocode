package assets

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Strings only the desktop workbench shows live in src/strings.desk.ts,
// which only the workbench imports, so they are bundled into
// workbench.js and the phone never loads them. A new string goes into
// strings.ts by default; these tests say when it belongs in the other
// file, so each string lands in the right bundle.

var sideEffectImportRe = regexp.MustCompile(`(?m)^\s*import\s*["']([^"']+)["']`)

// staticDeps lists the source files src loads when it runs: its value
// imports and re-exports, and its side-effect imports. Type-only imports
// and dynamic imports load nothing.
func staticDeps(files map[string]string, from string) []string {
	code := regexp.MustCompile(`(?m)//.*$|(?s)/\*.*?\*/`).ReplaceAllString(files[from], "")
	var out []string
	add := func(spec string) {
		if !strings.HasPrefix(spec, ".") {
			return
		}
		if p, ok := resolveTS(files, from, spec); ok {
			out = append(out, p)
		}
	}
	for _, m := range importRe.FindAllStringSubmatch(code, -1) {
		if m[2] != "" {
			continue // import type
		}
		names, star := parseImportClause(m[3], false)
		value := star || len(names) == 0
		for _, n := range names {
			value = value || !n.isType
		}
		if value {
			add(m[4])
		}
	}
	for _, m := range sideEffectImportRe.FindAllStringSubmatch(code, -1) {
		add(m[1])
	}
	return out
}

// reachable lists every source file entry loads, entry included.
func reachable(files map[string]string, entry string) map[string]bool {
	seen := map[string]bool{}
	todo := []string{entry}
	for len(todo) > 0 {
		f := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if seen[f] {
			continue
		}
		seen[f] = true
		todo = append(todo, staticDeps(files, f)...)
	}
	return seen
}

func TestReachableFollowsValueImportsOnly(t *testing.T) {
	files := map[string]string{
		"src/main.tsx":  "import { a } from \"./a\";\nimport type { B } from \"./b\";\nimport { type C } from \"./c\";\nimport \"./d\";\nexport { e } from \"./e\";\nconst x = import(\"/assets/f.js\");",
		"src/a.ts":      `export const a = 1;`,
		"src/b.ts":      `export type B = 1;`,
		"src/c.ts":      `export type C = 1;`,
		"src/d.ts":      ``,
		"src/e.ts":      `// import { z } from "./z"` + "\nexport const e = 1;",
		"src/z.ts":      ``,
		"src/other.tsx": ``,
	}
	got := reachable(files, "src/main.tsx")
	var names []string
	for f := range got {
		names = append(names, f)
	}
	sort.Strings(names)
	if strings.Join(names, " ") != "src/a.ts src/d.ts src/e.ts src/main.tsx" {
		t.Errorf("reachable: %v", names)
	}
}

// keysUsedBy maps each string key to the source files that name it.
func keysUsedBy(files map[string]string, keys map[string]bool) map[string][]string {
	used := map[string][]string{}
	for f, src := range files {
		base := f[strings.LastIndex(f, "/")+1:]
		if strings.HasPrefix(base, "strings.") || strings.HasPrefix(f, "src/kit/") || !strings.HasPrefix(f, "src/") {
			continue
		}
		seen := map[string]bool{}
		for _, m := range keyLiteral.FindAllStringSubmatch(src, -1) {
			for _, k := range []string{m[1], m[1] + ".one", m[1] + ".other"} {
				if keys[k] && !seen[k] {
					seen[k] = true
					used[k] = append(used[k], f)
				}
			}
		}
	}
	return used
}

func TestDesktopOnlyStringsLiveInTheWorkbench(t *testing.T) {
	files, _ := loadSources(t)
	phone := reachable(files, "src/main.tsx")
	for f := range reachable(files, "src/home.tsx") { // the phone views (home.js: MOB-14 review and the rest)
		phone[f] = true
	}
	desk := reachable(files, "src/workbench.tsx")
	if !desk["src/strings.desk.ts"] {
		t.Fatal("the workbench does not import strings.desk.ts")
	}
	if phone["src/strings.desk.ts"] {
		t.Error("the phone's page imports strings.desk.ts; only the workbench may")
	}
	if desk["src/strings.phone.ts"] || reachable(files, "src/main.tsx")["src/strings.phone.ts"] {
		t.Error("strings.phone.ts is imported outside the phone views (home.tsx)")
	}
	page, onlyDesk := map[string]bool{}, map[string]bool{}
	for _, e := range parseStrings(t, "src/strings.ts", "S") {
		page[e.key] = true
	}
	for _, e := range parseStrings(t, "src/strings.desk.ts", "SD") {
		onlyDesk[e.key] = true
	}
	all := map[string]bool{}
	for k := range page {
		all[k] = true
	}
	for k := range onlyDesk {
		all[k] = true
	}
	var bad []string
	for k, users := range keysUsedBy(files, all) {
		deskOnly := true
		for _, f := range users {
			if phone[f] {
				deskOnly = false
				if onlyDesk[k] {
					bad = append(bad, f+" (the phone's page) uses "+k+": move it from src/strings.desk.ts to src/strings.ts")
				}
			}
		}
		if deskOnly && page[k] {
			sort.Strings(users)
			bad = append(bad, k+" is used only by the desktop workbench ("+strings.Join(users, ", ")+"): move it from src/strings.ts to src/strings.desk.ts (and keep its Persian text)")
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}

// The phone's files carry none of the desktop-only strings; the
// workbench's file carries them all.
func TestPhoneBundleHoldsNoDesktopStrings(t *testing.T) {
	page := readBuilt(t, "app.js") + readBuilt(t, "app-chunk.js")
	wb := readBuilt(t, "workbench.js")
	entries := parseStrings(t, "src/strings.desk.ts", "SD")
	if len(entries) < 100 {
		t.Fatalf("only %d desktop-only strings", len(entries))
	}
	for _, e := range entries {
		if strings.Contains(page, `"`+e.key+`"`) {
			t.Errorf("the phone's files hold the desktop-only string %s", e.key)
		}
		if !strings.Contains(wb, `"`+e.key+`"`) {
			t.Errorf("workbench.js has no %s", e.key)
		}
	}
	if _, err := fs.Stat(FS(), "strings.desk.js"); err == nil {
		t.Error("strings.desk.ts is built alone; it belongs in workbench.js")
	}
}
