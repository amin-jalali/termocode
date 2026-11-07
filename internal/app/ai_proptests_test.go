package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func propWriteFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Spec (CMP-102): the property test action picks the property-based test
// library the project already uses, per language, and a test file path
// next to the code.
func TestAIPropFrameworkDetect(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		file  string
		lib   string
		have  bool
		path  string
	}{
		{"go stdlib", map[string]string{"go.mod": "module x\n", "pkg/codec.go": ""}, "pkg/codec.go", "testing/quick", true, "pkg/codec_prop_test.go"},
		{"go rapid", map[string]string{"go.mod": "module x\nrequire pgregory.net/rapid v1.1.0\n", "codec.go": ""}, "codec.go", "rapid", true, "codec_prop_test.go"},
		{"go nested module", map[string]string{"sub/go.mod": "module y\nrequire pgregory.net/rapid v1.1.0\n", "sub/a/b.go": ""}, "sub/a/b.go", "rapid", true, "sub/a/b_prop_test.go"},
		{"python hypothesis", map[string]string{"pyproject.toml": "[project]\ndependencies=['hypothesis']\n", "tests/x": "", "lib/parse.py": ""}, "lib/parse.py", "Hypothesis", true, "tests/test_parse_props.py"},
		{"python missing", map[string]string{"lib/parse.py": ""}, "lib/parse.py", "Hypothesis", false, "lib/test_parse_props.py"},
		{"ts fast-check", map[string]string{"package.json": `{"devDependencies":{"fast-check":"^3"}}`, "src/sum.ts": ""}, "src/sum.ts", "fast-check", true, "src/sum.prop.test.ts"},
		{"js missing", map[string]string{"package.json": `{}`, "sum.js": ""}, "sum.js", "fast-check", false, "sum.prop.test.js"},
		{"rust proptest", map[string]string{"Cargo.toml": "[dev-dependencies]\nproptest = \"1\"\n", "src/lib.rs": ""}, "src/lib.rs", "proptest", true, "tests/lib_props.rs"},
		{"java jqwik", map[string]string{"pom.xml": "<artifactId>jqwik</artifactId>", "src/main/java/A.java": ""}, "src/main/java/A.java", "jqwik", true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			propWriteFiles(t, root, c.files)
			fw := aiDetectPropFramework(root, c.file)
			if fw.Name != c.lib || fw.Installed != c.have || fw.TestPath != c.path {
				t.Errorf("got %+v, want lib %q have %v path %q", fw, c.lib, c.have, c.path)
			}
			if !fw.Installed && fw.Add == "" {
				t.Error("a missing library must say how to add it")
			}
		})
	}
	if fw := aiDetectPropFramework(t.TempDir(), "notes.txt"); fw.Lang != "" || fw.Name == "" {
		t.Errorf("unknown language: %+v", fw)
	}
}

// Spec (CMP-102): the prompt asks for properties (round trip,
// idempotence, invariants), names the file and the test path, keeps the
// code under test unchanged, and never lets the AI add a dependency on
// its own.
func TestAIPropTestPrompt(t *testing.T) {
	fw := aiPropFramework{Lang: "Python", Name: "Hypothesis", Add: "pip install hypothesis", TestPath: "tests/test_parse_props.py"}
	p := aiPropTestPrompt(fw, "lib/parse.py", 0, 0)
	for _, want := range []string{"lib/parse.py", "Hypothesis", "tests/test_parse_props.py", "round trip", "idempotence", "Do not change the code under test", "pip install hypothesis", "Do not add the dependency yourself", "tests_run"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	fw.Installed = true
	if p := aiPropTestPrompt(fw, "lib/parse.py", 10, 20); strings.Contains(p, "Do not add the dependency yourself") || !strings.Contains(p, "lines 10-20") {
		t.Errorf("installed / selection prompt:\n%s", p)
	}
}

// Spec (CMP-102): the action is in the palette, has an AI policy (the AI
// may not start it: it starts a chat turn) and a web parity row.
func TestAIPropTestsWired(t *testing.T) {
	found := false
	for _, e := range aiPaletteItems() {
		if e.id == "ai-property-tests" {
			found = true
		}
	}
	if !found {
		t.Fatal("ai-property-tests is not in the palette")
	}
	if _, ok := aiActionPolicies["ai-property-tests"]; !ok {
		t.Error("ai-property-tests has no AI policy")
	}
	b, err := os.ReadFile(filepath.Join("testdata", "web_parity.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\tai-property-tests\t") {
		t.Error("ai-property-tests has no web_parity.txt row")
	}
}
