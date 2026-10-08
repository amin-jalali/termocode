package tests

import (
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ScopeKind says what a run covers.
type ScopeKind int

const (
	ScopeAll ScopeKind = iota
	ScopePackage
	ScopeFile
	ScopeCases // one or more individual tests (Run Test, Rerun Failed)
)

// Scope selects what to run. Package / File / Cases are set per Kind.
type Scope struct {
	Kind    ScopeKind
	Package *Package
	File    *File
	Cases   []*Case
}

// Includes reports whether c belongs to the scope (used to reset statuses
// before the run).
func (s Scope) Includes(c *Case) bool {
	switch s.Kind {
	case ScopePackage:
		if s.Package == nil {
			return false
		}
		for _, f := range s.Package.Files {
			if f.Path == c.File {
				return true
			}
		}
		return false
	case ScopeFile:
		return s.File != nil && s.File.Path == c.File
	case ScopeCases:
		for _, x := range s.Cases {
			if x == c {
				return true
			}
		}
		return false
	}
	return true
}

// Command is a ready-to-exec test command.
type Command struct {
	Args []string // Args[0] is the program
	Dir  string
	Env  []string // extra KEY=VALUE entries
	// Report is the file the runner writes its machine-readable report to
	// (pytest junitxml, jest/vitest JSON); "" when results stream on
	// stdout only.
	Report string
}

// Label is the command line as shown in the UI.
func (c Command) Label() string {
	parts := make([]string, len(c.Args))
	for i, a := range c.Args {
		if a == "" || strings.ContainsAny(a, " \t'\"|()$^*?[]") {
			parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}

// ErrNoFramework is returned when the workspace has no known test runner.
var ErrNoFramework = errors.New("no test runner detected for this project")

// BuildCommand returns the command that runs scope in tree. report is the
// path the runner should write its report to (ignored by go/cargo).
func BuildCommand(t *Tree, scope Scope, report string) (Command, error) {
	if t == nil || t.FW == FrameworkNone {
		return Command{}, ErrNoFramework
	}
	cmd := Command{Dir: t.Root, Env: []string{"CI=true", "NO_COLOR=1", "FORCE_COLOR=0"}}
	rel := func(p string) string {
		r, err := filepath.Rel(t.Root, p)
		if err != nil {
			return p
		}
		return filepath.ToSlash(r)
	}
	goPkg := func(dir string) string {
		r := rel(dir)
		if r == "." {
			return "."
		}
		return "./" + r
	}
	switch t.FW {
	case FrameworkGo:
		cmd.Args = []string{"go", "test", "-json"}
		switch scope.Kind {
		case ScopeAll:
			cmd.Args = append(cmd.Args, "./...")
		case ScopePackage:
			cmd.Args = append(cmd.Args, goPkg(scope.Package.Dir))
		case ScopeFile:
			cmd.Args = append(cmd.Args, "-run", anchoredAlt(caseNames(scope.File.Cases), false), goPkg(filepath.Dir(scope.File.Path)))
		case ScopeCases:
			dirs := map[string]bool{}
			for _, c := range scope.Cases {
				dirs[goPkg(filepath.Dir(c.File))] = true
			}
			cmd.Args = append(cmd.Args, "-run", anchoredAlt(caseNames(scope.Cases), false))
			cmd.Args = append(cmd.Args, sortedKeys(dirs)...)
		}
	case FrameworkPytest:
		cmd.Report = report
		cmd.Args = []string{"pytest", "-v", "-o", "junit_family=xunit1", "--junitxml=" + report}
		switch scope.Kind {
		case ScopePackage:
			cmd.Args = append(cmd.Args, rel(scope.Package.Dir))
		case ScopeFile:
			cmd.Args = append(cmd.Args, rel(scope.File.Path))
		case ScopeCases:
			seen := map[string]bool{}
			for _, c := range scope.Cases {
				id := rel(c.File) + "::" + c.Name
				if !seen[id] {
					seen[id] = true
					cmd.Args = append(cmd.Args, id)
				}
			}
		}
	case FrameworkCargo:
		cmd.Args = []string{"cargo", "test", "--no-fail-fast"}
		var names []string
		switch scope.Kind {
		case ScopeFile:
			names = caseNames(scope.File.Cases)
		case ScopeCases:
			names = caseNames(scope.Cases)
		}
		if len(names) > 0 {
			cmd.Args = append(cmd.Args, "--")
			cmd.Args = append(cmd.Args, names...)
		}
	case FrameworkJest, FrameworkVitest:
		cmd.Report = report
		if t.FW == FrameworkJest {
			cmd.Args = []string{"npx", "--no-install", "jest", "--json", "--testLocationInResults", "--outputFile=" + report}
		} else {
			cmd.Args = []string{"npx", "--no-install", "vitest", "run", "--reporter=default", "--reporter=json", "--outputFile=" + report}
		}
		switch scope.Kind {
		case ScopePackage:
			cmd.Args = append(cmd.Args, rel(scope.Package.Dir))
		case ScopeFile:
			cmd.Args = append(cmd.Args, rel(scope.File.Path))
		case ScopeCases:
			files := map[string]bool{}
			var titles []string
			for _, c := range scope.Cases {
				files[rel(c.File)] = true
				// jest/vitest -t matches the titles joined by a space.
				titles = append(titles, strings.ReplaceAll(c.Name, JSNameSep, " "))
			}
			cmd.Args = append(cmd.Args, "-t", anchoredAlt(titles, true))
			cmd.Args = append(cmd.Args, sortedKeys(files)...)
		}
	}
	return cmd, nil
}

func caseNames(cs []*Case) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range cs {
		if !seen[c.Name] {
			seen[c.Name] = true
			out = append(out, c.Name)
		}
	}
	return out
}

// anchoredAlt builds "^(?:a|b)$" (escape=true quotes regex metacharacters;
// Go test names never contain any).
func anchoredAlt(names []string, escape bool) string {
	parts := make([]string, len(names))
	for i, n := range names {
		if escape {
			n = regexp.QuoteMeta(n)
		}
		parts[i] = n
	}
	if len(parts) == 1 {
		return "^" + parts[0] + "$"
	}
	return "^(" + strings.Join(parts, "|") + ")$"
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
