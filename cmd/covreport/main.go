// Command covreport prints test coverage per system (see
// scripts/coverage-systems.txt). Run it through scripts/coverage.sh.
//
//	scripts/coverage.sh                # unit tests (no -short), table
//	scripts/coverage.sh -e2e           # plus the e2e suite on a cover build
//	scripts/coverage.sh -min 95        # exit 1 when a system is below 95%
//	scripts/coverage.sh -profile f.out # only report an existing profile
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

func main() {
	root := flag.String("root", ".", "repository root")
	systemsFile := flag.String("systems", "scripts/coverage-systems.txt", "system mapping file (relative to root)")
	e2e := flag.Bool("e2e", false, "also run the e2e suite on a cover build and merge its coverage")
	minPct := flag.Float64("min", 0, "exit 1 when any system is below this percent (0 = off)")
	minTotal := flag.Float64("min-total", 0, "exit 1 when the total is below this percent (0 = off)")
	top := flag.Int("top", 20, "uncovered functions to list per system")
	out := flag.String("out", "", "folder for the profiles (default: a new temp folder)")
	profile := flag.String("profile", "", "report this existing profile instead of running tests")
	flag.Parse()

	fail, err := run(*root, *systemsFile, *out, *profile, *e2e, *top, *minPct, *minTotal)
	if err != nil {
		fmt.Fprintln(os.Stderr, "covreport:", err)
		os.Exit(2)
	}
	if fail {
		os.Exit(1)
	}
}

func run(root, systemsFile, outDir, profileFile string, withE2E bool, top int, minPct, minTotal float64) (bool, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return false, err
	}
	module, err := modulePath(gomod)
	if err != nil {
		return false, err
	}
	f, err := os.Open(filepath.Join(root, systemsFile))
	if err != nil {
		return false, err
	}
	sys, err := ParseSystems(f)
	f.Close()
	if err != nil {
		return false, fmt.Errorf("%s: %w", systemsFile, err)
	}

	var blocks []Block
	if profileFile != "" {
		if blocks, err = readProfile(profileFile); err != nil {
			return false, err
		}
	} else {
		if outDir == "" {
			if outDir, err = os.MkdirTemp("", "termocode-cov-"); err != nil {
				return false, err
			}
		}
		if outDir, err = filepath.Abs(outDir); err != nil {
			return false, err
		}
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return false, err
		}
		if blocks, err = collect(root, outDir, module, sys, withE2E); err != nil {
			return false, err
		}
	}

	rep := Summarize(blocks, module, sys)
	fmt.Println()
	PrintTable(os.Stdout, rep)
	if len(rep.Unmapped) > 0 {
		fmt.Printf("\nNot in any system (add to %s): %s\n", systemsFile, strings.Join(rep.Unmapped, ", "))
	}
	PrintTopFuncs(os.Stdout, rep, TopUncovered(blocks, module, root, sys, top))

	if bad := Below(rep, minPct, minTotal); len(bad) > 0 {
		fmt.Fprintln(os.Stderr, "\nCoverage below the limit:")
		for _, b := range bad {
			fmt.Fprintln(os.Stderr, "  "+b)
		}
		return true, nil
	}
	return false, nil
}

func readProfile(path string) ([]Block, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseProfile(f)
}

// modulePath reads the module path from the text of a go.mod.
func modulePath(gomod []byte) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(gomod))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "//")
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`), nil
		}
	}
	return "", fmt.Errorf("go.mod has no module line")
}

// coverPkgs is the -coverpkg value: the measured packages of list.
func coverPkgs(list []string, module string, sys *Systems) string {
	var keep []string
	for _, p := range list {
		if sys.Measured(strings.TrimPrefix(p, module+"/")) {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, ",")
}

// goCmd builds a "go" command, at the lowest CPU priority when nice exists,
// with a modest GOMAXPROCS unless the caller set one. Output goes to stderr
// so stdout stays the report.
func goCmd(dir string, env []string, args ...string) *exec.Cmd {
	name, full := "go", args
	if _, err := exec.LookPath("nice"); err == nil {
		name, full = "nice", append([]string{"-n", "19", "go"}, args...)
	}
	c := spawn.Command(name, full...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false")
	if os.Getenv("GOMAXPROCS") == "" {
		c.Env = append(c.Env, "GOMAXPROCS=4")
	}
	c.Env = append(c.Env, env...)
	c.Stdout, c.Stderr = os.Stderr, os.Stderr
	return c
}
