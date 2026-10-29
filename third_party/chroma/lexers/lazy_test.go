package lexers

import (
	"io/fs"
	"os"
	"testing"

	"github.com/alecthomas/chroma/v2"
)

// filledAtInit records whether package init already built the lexer
// registry (termocode patch: it must wait for the first use).
var filledAtInit bool

func TestMain(m *testing.M) {
	filledAtInit = registryFilled()
	os.Exit(m.Run())
}

func TestRegistryIsFilledOnFirstUse(t *testing.T) {
	if filledAtInit {
		t.Fatal("package init built the lexer registry; it must wait for the first use")
	}
	if l := Get("go"); l == nil || l.Config().Name != "Go" {
		t.Fatalf("Get(go) = %v, want the Go lexer", l)
	}
	if !registryFilled() {
		t.Fatal("registry not filled after Get")
	}
}

func TestLazyRegistryKeepsUpstreamResults(t *testing.T) {
	for file, want := range map[string]string{
		"main.go":    "Go",
		"x.py":       "Python",
		"index.html": "HTML",
		"a.php":      "PHP",         // Go-defined lexer, registered after the XML ones
		"init.el":    "EmacsLisp",   // Go wrapper replaces the XML lexer of the same name
		"x.lisp":     "Common Lisp", // same
		"README.md":  "markdown",    // Go-defined
		"Caddyfile":  "Caddyfile",   // Go-defined
		"q.sql":      "MySQL",       // upstream picks MySQL among the SQL lexers
	} {
		if l := Match(file); l == nil || l.Config().Name != want {
			t.Errorf("Match(%q) = %v, want %s", file, l, want)
		}
	}
	// The Go wrappers win over the XML lexers they wrap (registered later).
	if _, ok := Get("EmacsLisp").(*chroma.RegexLexer); ok {
		t.Error("EmacsLisp is the plain XML lexer, want the Go type-remapping wrapper")
	}
	// Analysers set by package init hooks (dns, mysql, zed) still work.
	if l := Analyse("@ IN SOA ns.example.com. admin.example.com. (\n"); l == nil || l.Config().Name != "dns" {
		t.Errorf("Analyse(zone file) = %v, want dns", l)
	}
	if l := Analyse("SELECT `name`, `id` FROM `users`"); l == nil || l.Config().Name != "MySQL" {
		t.Errorf("Analyse(mysql) = %v, want MySQL", l)
	}
	// A lexer registered after the first use is found too.
	custom := chroma.MustNewLexer(&chroma.Config{Name: "TermocodeTest", Filenames: []string{"*.tctest"}}, PlaintextRules)
	Register(custom)
	if Get("TermocodeTest") != custom || Match("a.tctest") != custom {
		t.Error("a lexer registered after the first use is not found")
	}
}

// BenchmarkFillRegistry is the work moved from package init to the
// first use: reading the config of every embedded lexer.
func BenchmarkFillRegistry(b *testing.B) {
	paths, err := fs.Glob(embedded, "embedded/*.xml")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		reg := chroma.NewLexerRegistry()
		for _, path := range paths {
			reg.Register(chroma.MustNewXMLLexer(embedded, path))
		}
	}
}
