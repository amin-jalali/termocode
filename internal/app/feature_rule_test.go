package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/keymap"
)

// STAB-1: the feature rule. Every palette command, every Settings row and
// every key action needs a behavior test: a *_test.go file somewhere in
// the repo that names it (the palette id, the setting id or config key, the
// keymap.Action identifier or action name). Old gaps sit on the reviewed
// baseline testdata/feature_rule_untested.txt; it may only shrink.
//
// Rebuild the baseline (only to drop fixed gaps or after a rename):
//
//	TERMOCODE_UPDATE_FEATURE_RULE=1 go test ./internal/app -run TestFeatureRuleGate
const featureRuleBaseline = "testdata/feature_rule_untested.txt"

// featureRule is one user-facing thing and the strings that, found in a test
// file, count as a test of it.
type featureRule struct {
	ID      string
	Needles []string
}

func untestedFeatures(features []featureRule, tests string) []string {
	var out []string
	for _, f := range features {
		hit := false
		for _, n := range f.Needles {
			if strings.Contains(tests, n) {
				hit = true
				break
			}
		}
		if !hit {
			out = append(out, f.ID)
		}
	}
	sort.Strings(out)
	return out
}

// featureRuleProblems returns the untested features not on the baseline
// (new gaps) and the baseline ids that are tested now or gone (stale).
func featureRuleProblems(features []featureRule, tests string, baseline []string) (newGaps, stale []string) {
	untested := map[string]bool{}
	for _, id := range untestedFeatures(features, tests) {
		untested[id] = true
	}
	onBase := map[string]bool{}
	for _, id := range baseline {
		onBase[id] = true
		if !untested[id] {
			stale = append(stale, id)
		}
	}
	for _, id := range untestedFeatures(features, tests) {
		if !onBase[id] {
			newGaps = append(newGaps, id)
		}
	}
	sort.Strings(stale)
	return newGaps, stale
}

// appFeatures lists every palette command, Settings row and key action.
func appFeatures() []featureRule {
	var out []featureRule
	seen := map[string]bool{}
	add := func(f featureRule) {
		if !seen[f.ID] {
			seen[f.ID] = true
			out = append(out, f)
		}
	}
	consts := idConstNames()
	for _, it := range paletteItems() {
		if !it.File && it.ID != "" {
			// A test may name the id by its Go constant (aiHistorySearchID).
			add(featureRule{ID: "palette/" + it.ID, Needles: append([]string{`"` + it.ID + `"`}, consts[it.ID]...)})
		}
	}
	for _, r := range settingsRows {
		add(featureRule{ID: "setting/" + r.ID, Needles: []string{`"` + r.ID + `"`, `"` + r.Label + `"`}})
	}
	for a := keymap.Action(1); a < 2000; a++ {
		if name := keymap.ActionName(a); name != "" {
			add(featureRule{ID: "key/" + name, Needles: []string{"Action" + name, `"` + name + `"`}})
		}
	}
	return out
}

// idConstNames maps a string value to the names of the package-level
// constants and variables of this package (non-test files) that hold it,
// so a test that names aiHistorySearchID names "ai-history-search".
func idConstNames() map[string][]string {
	out := map[string][]string{}
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, d := range af.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, sp := range gd.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING || i >= len(vs.Names) {
						continue
					}
					if val, err := strconv.Unquote(lit.Value); err == nil {
						out[val] = append(out[val], vs.Names[i].Name)
					}
				}
			}
		}
	}
	return out
}

// repoTestText joins every *_test.go file of the repo, except the rule's
// own files, testdata and tool folders.
func repoTestText(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	skipDir := map[string]bool{".git": true, ".claude": true, "node_modules": true, "testdata": true, "vendor": true, "docs-local": true}
	skipFile := map[string]bool{"feature_rule_test.go": true, "feature_rule_logic_test.go": true}
	var b strings.Builder
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (skipDir[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") || skipFile[d.Name()] {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		b.Write(data)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func readFeatureBaseline(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(featureRuleBaseline)
	if err != nil {
		t.Fatalf("the feature rule baseline is missing: %v", err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

// Spec (STAB-1): a PR that adds a palette command, a setting or a key
// action without a behavior test fails here; so does a baseline id that
// is tested now or no longer exists (the baseline only shrinks).
func TestFeatureRuleGate(t *testing.T) {
	features := appFeatures()
	tests := repoTestText(t)
	if os.Getenv("TERMOCODE_UPDATE_FEATURE_RULE") == "1" {
		head := "# STAB-1 feature rule: features with no behavior test yet (old gaps).\n" +
			"# This list may only shrink. A new palette command, setting or key\n" +
			"# action needs a test that names it; do not add it here.\n"
		body := strings.Join(untestedFeatures(features, tests), "\n")
		if err := os.WriteFile(featureRuleBaseline, []byte(head+body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	newGaps, stale := featureRuleProblems(features, tests, readFeatureBaseline(t))
	if len(newGaps) > 0 {
		t.Errorf("%d features have no behavior test. Add a test that names each one (palette id, setting id or key, keymap.Action):\n%s",
			len(newGaps), strings.Join(newGaps, "\n"))
	}
	if len(stale) > 0 {
		t.Errorf("%d ids on %s are tested now or gone; remove them from the baseline:\n%s",
			len(stale), featureRuleBaseline, strings.Join(stale, "\n"))
	}
	t.Logf("feature rule: %d features, %d old gaps on the baseline", len(features), len(readFeatureBaseline(t)))
}
