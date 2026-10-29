package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// collect runs the tests and returns the merged blocks. Failing tests do
// not stop the report: the profile still shows what ran.
func collect(root, outDir, module string, sys *Systems, withE2E bool) ([]Block, error) {
	var listOut bytes.Buffer
	list := goCmd(root, nil, "list", "./...")
	list.Stdout = &listOut
	if err := list.Run(); err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	pkgs := coverPkgs(strings.Fields(listOut.String()), module, sys)
	if pkgs == "" {
		return nil, fmt.Errorf("no measured packages found")
	}

	_, nvimErr := exec.LookPath("nvim")
	haveNvim := nvimErr == nil

	unitOut := filepath.Join(outDir, "unit.out")
	args := []string{"test", "-p", "2", "-count=1", "-timeout", "40m", "-covermode=set",
		"-coverpkg=" + pkgs, "-coverprofile=" + unitOut}
	if !haveNvim {
		fmt.Fprintln(os.Stderr, "NOTE: nvim is not on PATH. Running with -short, so the nvim-based tests are skipped and the numbers are too low.")
		args = append(args, "-short")
	}
	fmt.Fprintln(os.Stderr, "Unit tests (this takes a while)...")
	unitCmd := goCmd(root, nil, append(args, "./...")...)
	filter := &lineFilter{w: os.Stderr, f: shortenCoverLine}
	unitCmd.Stdout = filter
	err := unitCmd.Run()
	filter.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "NOTE: some unit tests failed or timed out:", err, "- coverage below is for what ran.")
	}
	profiles := [][]Block{}
	unit, err := readProfile(unitOut)
	if err != nil {
		return nil, fmt.Errorf("unit profile: %w", err)
	}
	profiles = append(profiles, unit)

	if withE2E {
		if !haveNvim {
			fmt.Fprintln(os.Stderr, "NOTE: nvim is not on PATH: skipping the e2e suite.")
		} else if e2e, err := collectE2E(root, outDir, pkgs); err != nil {
			fmt.Fprintln(os.Stderr, "NOTE: e2e coverage skipped:", err)
		} else {
			profiles = append(profiles, e2e)
		}
	}

	merged := Merge(profiles...)
	var buf bytes.Buffer
	if err := WriteProfile(&buf, merged); err != nil {
		return nil, err
	}
	mergedOut := filepath.Join(outDir, "merged.out")
	if err := os.WriteFile(mergedOut, buf.Bytes(), 0o644); err != nil {
		return nil, err
	}
	fmt.Fprintln(os.Stderr, "Merged profile:", mergedOut)
	return merged, nil
}

// collectE2E builds the binary with coverage, runs the e2e suite against it
// and converts the counters it wrote into a text profile.
func collectE2E(root, outDir, pkgs string) ([]Block, error) {
	bin := filepath.Join(outDir, "termocode-cover")
	covDir := filepath.Join(outDir, "covdata")
	if err := os.RemoveAll(covDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(covDir, 0o755); err != nil {
		return nil, err
	}
	fmt.Fprintln(os.Stderr, "Building the cover binary...")
	if err := goCmd(root, nil, "build", "-cover", "-covermode=set", "-coverpkg="+pkgs, "-o", bin, "./cmd/termocode").Run(); err != nil {
		return nil, fmt.Errorf("cover build: %w", err)
	}
	fmt.Fprintln(os.Stderr, "e2e suite...")
	run := goCmd(root, []string{"E2E_BIN=" + bin, "GOCOVERDIR=" + covDir},
		"-C", "e2e", "test", "-tags", "e2e", "-count=1", "-timeout", "30m", "./...")
	if err := run.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "NOTE: some e2e tests failed:", err, "- coverage below is for what ran.")
	}
	textOut := filepath.Join(outDir, "e2e.out")
	if err := goCmd(root, nil, "tool", "covdata", "textfmt", "-i="+covDir, "-o="+textOut).Run(); err != nil {
		return nil, fmt.Errorf("covdata: %w", err)
	}
	return readProfile(textOut)
}
