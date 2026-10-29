package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `# comment
Alpha: internal/a internal/tree/...

Beta: cmd/b
exclude: internal/tree/fake internal/devtools/...
`

func TestClassify(t *testing.T) {
	s, err := ParseSystems(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.Order, ","); got != "Alpha,Beta" {
		t.Fatalf("order = %q", got)
	}
	cases := []struct {
		pkg, system string
		excluded    bool
	}{
		{"internal/a", "Alpha", false},
		{"internal/a/sub", "", false}, // exact pattern, no tree
		{"internal/tree", "Alpha", false},
		{"internal/tree/deep/er", "Alpha", false},
		{"internal/tree/fake", "", true}, // exclude beats the tree
		{"internal/devtools/x/y", "", true},
		{"cmd/b", "Beta", false},
		{"internal/unknown", "", false},
		{"internal/treehouse", "", false}, // prefix must end at a slash
	}
	for _, c := range cases {
		sys, ex := s.Classify(c.pkg)
		if sys != c.system || ex != c.excluded {
			t.Errorf("Classify(%q) = %q,%v want %q,%v", c.pkg, sys, ex, c.system, c.excluded)
		}
	}
	if s.Measured("internal/tree/fake") || !s.Measured("cmd/b") || s.Measured("nope") {
		t.Error("Measured is wrong")
	}
}

func TestParseSystemsErrors(t *testing.T) {
	for _, in := range []string{"no colon here", "Name:", "A: x\nA: y"} {
		if _, err := ParseSystems(strings.NewReader(in)); err == nil {
			t.Errorf("ParseSystems(%q) should fail", in)
		}
	}
}

// TestEveryPackageIsMapped keeps the checked-in mapping complete: each Go
// package under cmd/ and internal/ is in a system or in the exclude list,
// and every pattern still names a package that exists.
func TestEveryPackageIsMapped(t *testing.T) {
	root := filepath.Join("..", "..")
	f, err := os.Open(filepath.Join(root, "scripts", "coverage-systems.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := ParseSystems(f)
	if err != nil {
		t.Fatal(err)
	}
	pkgs := map[string]bool{}
	for _, top := range []string{"cmd", "internal"} {
		_ = filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			if d.Name() == "testdata" {
				return fs.SkipDir
			}
			ents, _ := os.ReadDir(path)
			for _, e := range ents {
				if n := e.Name(); strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
					rel, _ := filepath.Rel(root, path)
					pkgs[filepath.ToSlash(rel)] = true
					break
				}
			}
			return nil
		})
	}
	for pkg := range pkgs {
		if sys, ex := s.Classify(pkg); sys == "" && !ex {
			t.Errorf("%s is in no system and not excluded: add it to scripts/coverage-systems.txt", pkg)
		}
	}
	all := append(append([]pattern{}, s.patterns...), s.excludes...)
	for _, p := range all {
		found := false
		for pkg := range pkgs {
			if p.matches(pkg) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pattern %q matches no package", p.path)
		}
	}
}
