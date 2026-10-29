package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Systems maps package paths (relative to the module root) to the named
// systems of the coverage table, and lists the packages that are not
// measured at all (test helpers and dev tools).
type Systems struct {
	Order    []string // system names in file order
	patterns []pattern
	excludes []pattern
}

type pattern struct {
	system string // empty for an exclude
	path   string
	tree   bool // "path/..." matches path and everything below it
}

func (p pattern) matches(pkg string) bool {
	if pkg == p.path {
		return true
	}
	return p.tree && strings.HasPrefix(pkg, p.path+"/")
}

func parsePatterns(system string, fields []string) []pattern {
	var out []pattern
	for _, f := range fields {
		p := pattern{system: system, path: f}
		if rest, ok := strings.CutSuffix(f, "/..."); ok {
			p.path, p.tree = rest, true
		}
		out = append(out, p)
	}
	return out
}

// ParseSystems reads the mapping file: "# comment", "Name: patterns" and
// "exclude: patterns".
func ParseSystems(r io.Reader) (*Systems, error) {
	s := &Systems{}
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, rest, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		fields := strings.Fields(rest)
		if !ok || name == "" || len(fields) == 0 {
			return nil, fmt.Errorf("line %d: want \"Name: package patterns\"", n)
		}
		if name == "exclude" {
			s.excludes = append(s.excludes, parsePatterns("", fields)...)
			continue
		}
		if seen[name] {
			return nil, fmt.Errorf("line %d: system %q listed twice", n, name)
		}
		seen[name] = true
		s.Order = append(s.Order, name)
		s.patterns = append(s.patterns, parsePatterns(name, fields)...)
	}
	return s, sc.Err()
}

// Classify returns the system of a package ("" when it is unmapped) and
// whether the package is excluded from measuring. An exclude wins over a
// system pattern.
func (s *Systems) Classify(pkg string) (system string, excluded bool) {
	for _, p := range s.excludes {
		if p.matches(pkg) {
			return "", true
		}
	}
	for _, p := range s.patterns {
		if p.matches(pkg) {
			return p.system, false
		}
	}
	return "", false
}

// Measured reports whether pkg is a shipped package that a system owns.
func (s *Systems) Measured(pkg string) bool {
	sys, ex := s.Classify(pkg)
	return !ex && sys != ""
}
