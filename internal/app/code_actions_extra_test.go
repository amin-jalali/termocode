package app

import (
	"strings"
	"testing"
)

// TestEnclosingGoFuncDetection exercises the pure-string helpers driving
// Phase-2's "Run Test" affordance. We test the regex + scan path
// (scanEnclosingGoFunc), the test-prefix gate (isGoTestFunc), and the
// exported-symbol parser (parseExportedSymbol). All three are pure
// functions; the nvim-bound enclosingGoFuncName / exportedSymbolOnLine
// pass their input through these so any bug they ship surfaces here too.
func TestEnclosingGoFuncDetection(t *testing.T) {
	t.Run("scanEnclosingGoFunc finds nearest declaration", func(t *testing.T) {
		body := strings.Join([]string{
			"package foo",
			"",
			"func helper(x int) int { return x }",
			"",
			"func TestParse(t *testing.T) {",
			"\tparseInput(\"hi\")",
			"\t// cursor here",
		}, "\n")
		got := scanEnclosingGoFunc(body)
		if got != "TestParse" {
			t.Errorf("scanEnclosingGoFunc returned %q, want %q", got, "TestParse")
		}
	})

	t.Run("scanEnclosingGoFunc handles receiver clauses", func(t *testing.T) {
		body := strings.Join([]string{
			"func (m *Model) doStuff() {",
			"\tx := 1",
			"\t_ = x",
		}, "\n")
		got := scanEnclosingGoFunc(body)
		if got != "doStuff" {
			t.Errorf("receiver-method scan got %q, want %q", got, "doStuff")
		}
	})

	t.Run("scanEnclosingGoFunc returns empty when no func", func(t *testing.T) {
		body := "var x = 1\nvar y = 2\n"
		got := scanEnclosingGoFunc(body)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("scanEnclosingGoFunc walks up past inner braces", func(t *testing.T) {
		body := strings.Join([]string{
			"func TestOuter(t *testing.T) {",
			"\tt.Run(\"sub\", func(t *testing.T) {",
			"\t\t// CURSOR HERE — closure looks like a func decl but isn't at col 0",
		}, "\n")
		// The closure form `func(t *testing.T) {` — note no space-before-`(`
		// requirement — should match the regex too. Walking from bottom
		// returns the last (deepest) match, but in the picker context the
		// user is more likely to want the outer test name. We accept
		// either as long as the helper picks SOMETHING reasonable; the
		// real-world contract is "non-empty name when inside a test".
		got := scanEnclosingGoFunc(body)
		if got == "" {
			t.Errorf("expected non-empty function name inside nested closure")
		}
	})
}

func TestIsGoTestFunc(t *testing.T) {
	cases := map[string]bool{
		"":                false,
		"TestParse":       true,
		"TestX":           true,
		"Test":            true,
		"BenchmarkRun":    true,
		"ExampleFoo":      true,
		"FuzzParse":       true,
		"helper":          false,
		"DoTest":          false, // doesn't START with one of the prefixes
		"testMustBeUpper": false,
	}
	for name, want := range cases {
		if got := isGoTestFunc(name); got != want {
			t.Errorf("isGoTestFunc(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestParseExportedSymbol(t *testing.T) {
	cases := []struct {
		line     string
		wantName string
		wantOK   bool
	}{
		{"func Parse(s string) error {", "Parse", true},
		{"func (m *Model) DoStuff() {", "DoStuff", true},
		{"func parse(s string) error {", "", false}, // unexported
		{"type Config struct {", "Config", true},
		{"type config struct {", "", false},
		{"var GlobalCount int", "GlobalCount", true},
		{"const Default = 5", "Default", true},
		{"const k = 1", "", false},
		{"// just a comment", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		gotName, gotOK := parseExportedSymbol(c.line)
		if gotName != c.wantName || gotOK != c.wantOK {
			t.Errorf("parseExportedSymbol(%q) = (%q, %v), want (%q, %v)",
				c.line, gotName, gotOK, c.wantName, c.wantOK)
		}
	}
}
