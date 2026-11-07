package app

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// AI: Generate Property-Based Tests (CMP-102). The chat writes tests
// that check rules which hold for every input (round trips, idempotence,
// invariants) with the property-based test library the project already
// uses. The file it writes goes through the normal edit review; the AI
// never adds the library itself.

// synthKindAIPropTests is the Ctrl+. code action of the same name.
const synthKindAIPropTests = "ai.proptests"

// aiPropFramework is the property-based test library of one file.
type aiPropFramework struct {
	Lang      string // "Go", "Python" … ("" unknown)
	Name      string // the library
	Installed bool   // the project uses it already (or it is built in)
	Add       string // how to add it when it is missing
	TestPath  string // the suggested test file, relative to the root ("" the project's layout)
}

// aiPropManifestMax caps a manifest read (a lock file can be big).
const aiPropManifestMax = 4 << 20

// aiPropDirs are the folders from the file's folder up to the root
// (relative, slash form; "." is the root).
func aiPropDirs(rel string) []string {
	d := path.Dir(filepath.ToSlash(rel))
	var out []string
	for {
		out = append(out, d)
		if d == "." || d == "/" || d == "" {
			return out
		}
		d = path.Dir(d)
	}
}

// aiPropManifestHas reports whether a manifest named like one of names
// (a "*" glob is fine) in dir holds one of needles; it returns that
// needle.
func aiPropManifestHas(root, dir string, names, needles []string) (string, bool) {
	for _, n := range names {
		pattern := filepath.Join(root, filepath.FromSlash(dir), n)
		files := []string{pattern}
		if strings.Contains(n, "*") {
			files, _ = filepath.Glob(pattern)
		}
		for _, f := range files {
			fi, err := os.Stat(f)
			if err != nil || fi.IsDir() || fi.Size() > aiPropManifestMax {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			for _, nd := range needles {
				if strings.Contains(string(b), nd) {
					return nd, true
				}
			}
		}
	}
	return "", false
}

// aiPropNearest walks from the file's folder up to the root and returns
// the first needle a manifest holds, and the folder of the nearest
// manifest (found or not; "" when there is none).
func aiPropNearest(root, rel string, names, needles []string) (needle, manifestDir string) {
	for _, d := range aiPropDirs(rel) {
		if manifestDir == "" {
			for _, n := range names {
				if m, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(d), n)); len(m) > 0 {
					manifestDir = d
					break
				}
			}
		}
		if nd, ok := aiPropManifestHas(root, d, names, needles); ok {
			return nd, d
		}
	}
	return "", manifestDir
}

func aiPropIsDir(root, rel string) bool {
	fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil && fi.IsDir()
}

// aiDetectPropFramework picks the library and test path for rel (a file
// relative to root).
func aiDetectPropFramework(root, rel string) aiPropFramework {
	rel = filepath.ToSlash(rel)
	dir, file := path.Dir(rel), path.Base(rel)
	ext := strings.ToLower(path.Ext(file))
	base := strings.TrimSuffix(file, path.Ext(file))
	switch ext {
	case ".go":
		fw := aiPropFramework{Lang: "Go", Name: "testing/quick", Installed: true, TestPath: path.Join(dir, base+"_prop_test.go")}
		if nd, _ := aiPropNearest(root, rel, []string{"go.mod"}, []string{"pgregory.net/rapid"}); nd != "" {
			fw.Name = "rapid"
		}
		return fw
	case ".py":
		fw := aiPropFramework{Lang: "Python", Name: "Hypothesis", Add: "pip install hypothesis (and add it to your dev dependencies)"}
		nd, _ := aiPropNearest(root, rel, []string{"pyproject.toml", "setup.cfg", "setup.py", "Pipfile", "requirements*.txt", "poetry.lock", "uv.lock"}, []string{"hypothesis"})
		fw.Installed = nd != ""
		if aiPropIsDir(root, "tests") {
			fw.TestPath = path.Join("tests", "test_"+base+"_props.py")
		} else {
			fw.TestPath = path.Join(dir, "test_"+base+"_props.py")
		}
		return fw
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		lang := "JavaScript"
		if strings.Contains(ext, "t") && ext != ".jsx" {
			lang = "TypeScript"
		}
		nd, _ := aiPropNearest(root, rel, []string{"package.json"}, []string{`"fast-check"`})
		return aiPropFramework{Lang: lang, Name: "fast-check", Installed: nd != "",
			Add: "npm install --save-dev fast-check", TestPath: path.Join(dir, base+".prop.test"+ext)}
	case ".rs":
		nd, crate := aiPropNearest(root, rel, []string{"Cargo.toml"}, []string{"proptest", "quickcheck"})
		if crate == "" {
			crate = "."
		}
		fw := aiPropFramework{Lang: "Rust", Name: "proptest", Installed: nd != "", Add: "cargo add --dev proptest",
			TestPath: path.Join(crate, "tests", base+"_props.rs")}
		if nd == "quickcheck" {
			fw.Name = "quickcheck"
		}
		return fw
	case ".java", ".kt", ".kts":
		lang := "Java"
		if ext != ".java" {
			lang = "Kotlin"
		}
		nd, _ := aiPropNearest(root, rel, []string{"pom.xml", "build.gradle", "build.gradle.kts"}, []string{"jqwik", "kotest-property"})
		fw := aiPropFramework{Lang: lang, Name: "jqwik", Installed: nd != "", Add: "add net.jqwik:jqwik as a test dependency"}
		if nd == "kotest-property" {
			fw.Name = "Kotest property testing"
		}
		return fw
	case ".ex", ".exs":
		nd, proj := aiPropNearest(root, rel, []string{"mix.exs"}, []string{"stream_data"})
		if proj == "" {
			proj = "."
		}
		return aiPropFramework{Lang: "Elixir", Name: "StreamData", Installed: nd != "",
			Add: `add {:stream_data, "~> 1.0", only: :test} to mix.exs`, TestPath: path.Join(proj, "test", base+"_property_test.exs")}
	case ".hs":
		nd, _ := aiPropNearest(root, rel, []string{"*.cabal", "package.yaml"}, []string{"QuickCheck", "hedgehog"})
		return aiPropFramework{Lang: "Haskell", Name: "QuickCheck", Installed: nd != "", Add: "add QuickCheck to the test-suite build-depends"}
	}
	return aiPropFramework{Name: "the property-based testing library common for this language"}
}

// aiPropTestPrompt is the chat message. from/to are the 1-based lines of
// a selection (0: the whole file).
func aiPropTestPrompt(fw aiPropFramework, rel string, from, to int) string {
	var b strings.Builder
	what := rel
	if from > 0 && to >= from {
		what = fmt.Sprintf("%s (lines %d-%d)", rel, from, to)
	}
	lib := fw.Name
	if fw.Lang != "" {
		lib = fw.Lang + ", " + fw.Name
	}
	fmt.Fprintf(&b, "Write property-based tests for %s (%s).\n", what, lib)
	b.WriteString("1. Read the code and pick the functions whose behaviour follows clear rules. For each one, name the " +
		"properties that must hold for every input: round trip (decode(encode(x)) == x), idempotence (f(f(x)) == f(x)), " +
		"invariants (the output stays sorted, the length is kept, it never panics or throws), bounds, and agreement with a " +
		"simple reference version.\n")
	if fw.TestPath != "" {
		fmt.Fprintf(&b, "2. Write the properties to %s (a new file; follow the project's test layout if it differs). ", fw.TestPath)
	} else {
		b.WriteString("2. Write the properties to a new test file in the project's test layout. ")
	}
	b.WriteString("Build input generators that reach the edge cases (empty, zero, negative, huge, unicode) and let the " +
		"library shrink a failure to the smallest input. Keep each property fast (the default number of runs).\n")
	b.WriteString("3. Do not change the code under test. When a property fails, show the smallest failing input and say " +
		"whether the code or the property is wrong.\n")
	if !fw.Installed {
		if fw.Add != "" {
			fmt.Fprintf(&b, "The project does not use %s yet. Do not add the dependency yourself: tell me to run `%s`, "+
				"then go on once I say it is there.\n", fw.Name, fw.Add)
		} else {
			b.WriteString("If the project has no such library yet, do not add the dependency yourself: tell me what to add.\n")
		}
	}
	b.WriteString("4. Run the new tests (tests_run, then tests_results) and show the result.\n")
	b.WriteString("End with a short list: one line for each property.")
	return b.String()
}

// aiPropertyTests sends the property test request for the open file (or
// its selection) to the chat.
func (m *Model) aiPropertyTests() tea.Cmd {
	if c, ok := m.aiReady(); !ok {
		return c
	}
	t, ok := m.aiCaptureTarget(false)
	if !ok {
		return m.aiInfo("Open a file first")
	}
	root := aiWorkspaceRoot()
	rel := t.path
	if r, err := filepath.Rel(root, t.path); err == nil && !strings.HasPrefix(r, "..") {
		rel = r
	}
	from, to := 0, 0
	if t.hadSelection {
		from, to = t.start+1, t.end
	}
	q := aiPropTestPrompt(aiDetectPropFramework(root, rel), filepath.ToSlash(rel), from, to)
	m.openAIPanel(true)
	return m.sendAIChat(q)
}
