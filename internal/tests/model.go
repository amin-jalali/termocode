// Package tests discovers tests in a workspace, builds the command lines
// that run them, and parses the runners' output back into per-test
// results. It is pure Go with no Bubble Tea or nvim dependency so every
// piece can be unit-tested against recorded runner output.
//
// Supported frameworks (first marker file wins, see Detect):
//
//	go.mod                              → go test -json
//	Cargo.toml                          → cargo test (libtest text output)
//	pyproject.toml / setup.py / pytest.ini / setup.cfg / tox.ini → pytest -v + junitxml
//	package.json                        → jest / vitest JSON report
//
// Ported from mobocode's lib/core/tests/{test_models,test_discovery,
// test_result_parser}.dart and extended to four toolchains.
package tests

import "time"

// Framework identifies the test toolchain of a workspace.
type Framework int

const (
	FrameworkNone Framework = iota
	FrameworkGo
	FrameworkPytest
	FrameworkCargo
	FrameworkJest
	FrameworkVitest
)

// String is the human label shown in the UI.
func (f Framework) String() string {
	switch f {
	case FrameworkGo:
		return "go test"
	case FrameworkPytest:
		return "pytest"
	case FrameworkCargo:
		return "cargo test"
	case FrameworkJest:
		return "jest"
	case FrameworkVitest:
		return "vitest"
	}
	return "none"
}

// Status is the result state of one test.
type Status int

const (
	StatusPending Status = iota // discovered, not run yet
	StatusRunning               // part of the active run, no result yet
	StatusPassed
	StatusFailed
	StatusSkipped
)

// Case is one test (a leaf of the tree).
type Case struct {
	// Name is the test name as the runner reports it, without file:
	// "TestParse" (Go), "TestCls::test_x" (pytest), "it_works" (Rust),
	// "suite › does a thing" (jest/vitest).
	Name string
	// File is the absolute path of the file that declares the test.
	File string
	// Line is the 1-based declaration line (0 = unknown).
	Line int

	Status   Status
	Duration time.Duration
	// Message is the failure message (first meaningful lines).
	Message string
	// FailFile / FailLine point at the failing assertion when the runner
	// reported one (0 = unknown → use File/Line).
	FailFile string
	FailLine int
	// Output holds the test's own output lines from the last run.
	Output []string
}

// Key is the stable identity of a case across rediscovery.
func (c *Case) Key() string { return c.File + "::" + c.Name }

// Location returns where "jump to" should land: the failure line when
// known, else the declaration.
func (c *Case) Location() (string, int) {
	if c.FailFile != "" && c.FailLine > 0 {
		return c.FailFile, c.FailLine
	}
	return c.File, c.Line
}

// File groups the cases declared in one source file.
type File struct {
	Path  string // absolute
	Rel   string // relative to the workspace root (slash-separated)
	Cases []*Case
}

// Package groups test files: a Go package (directory), a Python/JS
// directory, or a Rust crate directory.
type Package struct {
	// Name is the display name: the Go import path, or the directory
	// relative to the root ("." for the root itself).
	Name string
	// Dir is the absolute directory.
	Dir   string
	Files []*File
	// Error holds a package-level failure (build failed, collection
	// error) from the last run.
	Error string
	// ErrFile / ErrLine locate the error when the runner reported one.
	ErrFile string
	ErrLine int
}

// Counts tallies statuses.
type Counts struct {
	Total, Passed, Failed, Skipped, Running int
}

// Add folds one status into the counts.
func (c *Counts) Add(s Status) {
	c.Total++
	switch s {
	case StatusPassed:
		c.Passed++
	case StatusFailed:
		c.Failed++
	case StatusSkipped:
		c.Skipped++
	case StatusRunning:
		c.Running++
	}
}

// CountFile tallies one file.
func CountFile(f *File) Counts {
	var c Counts
	for _, tc := range f.Cases {
		c.Add(tc.Status)
	}
	return c
}

// CountPackage tallies one package.
func CountPackage(p *Package) Counts {
	var c Counts
	for _, f := range p.Files {
		for _, tc := range f.Cases {
			c.Add(tc.Status)
		}
	}
	return c
}

// CountAll tallies every package.
func CountAll(pkgs []*Package) Counts {
	var c Counts
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, tc := range f.Cases {
				c.Add(tc.Status)
			}
		}
	}
	return c
}

// MergeStatus folds a new result into the current status of a case that
// may receive several results in one run (parametrised pytest cases, Go
// tests reported in several packages). Failed beats passed beats skipped;
// a pending/running case takes whatever arrives.
func MergeStatus(cur, next Status) Status {
	if cur == StatusPending || cur == StatusRunning {
		return next
	}
	rank := func(s Status) int {
		switch s {
		case StatusFailed:
			return 3
		case StatusPassed:
			return 2
		case StatusSkipped:
			return 1
		}
		return 0
	}
	if rank(next) > rank(cur) {
		return next
	}
	return cur
}
