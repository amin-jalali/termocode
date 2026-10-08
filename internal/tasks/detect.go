package tasks

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Detect builds tasks from well-known project files in root (ported from
// mobocode's run_targets.dart / detectRunConfigs, extended with package.json
// scripts, Makefile targets, Cargo, pytest and justfile recipes). Pure apart
// from reading files; the order is stable.
func Detect(root string) []Task {
	var out []Task
	out = append(out, detectGo(root)...)
	out = append(out, detectNpm(root)...)
	out = append(out, detectCargo(root)...)
	out = append(out, detectPytest(root)...)
	out = append(out, detectMake(root)...)
	out = append(out, detectJust(root)...)
	return out
}

func exists(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, name))
	return err == nil
}

func detectGo(root string) []Task {
	if !exists(root, "go.mod") {
		return nil
	}
	m := []string{"$go"}
	return []Task{
		{Label: "go: build", Command: "go build ./...", Group: GroupBuild, ProblemMatcher: m, Source: "go"},
		{Label: "go: test", Command: "go test ./...", Group: GroupTest, ProblemMatcher: m, Source: "go"},
		{Label: "go: vet", Command: "go vet ./...", ProblemMatcher: m, Source: "go"},
		{Label: "go: run", Command: "go run .", ProblemMatcher: m, Source: "go"},
	}
}

// npmRunner picks the package manager from the lock file.
func npmRunner(root string) string {
	switch {
	case exists(root, "pnpm-lock.yaml"):
		return "pnpm"
	case exists(root, "yarn.lock"):
		return "yarn"
	case exists(root, "bun.lockb"), exists(root, "bun.lock"):
		return "bun"
	}
	return "npm"
}

func detectNpm(root string) []Task {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return nil
	}
	scripts := PackageScripts(data)
	if len(scripts) == 0 {
		return nil
	}
	runner := npmRunner(root)
	out := make([]Task, 0, len(scripts))
	for _, s := range scripts {
		t := Task{
			Label:          runner + ": " + s,
			Command:        runner + " run " + ShellQuote(s),
			ProblemMatcher: []string{"$tsc"},
			Source:         "npm",
		}
		switch s {
		case "build":
			t.Group = GroupBuild
		case "test":
			t.Group = GroupTest
		}
		out = append(out, t)
	}
	return out
}

// PackageScripts returns the script names of a package.json in file
// order. Malformed JSON returns nil.
func PackageScripts(data []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil
		}
		key, _ := keyTok.(string)
		if key != "scripts" {
			var skip json.RawMessage
			if dec.Decode(&skip) != nil {
				return nil
			}
			continue
		}
		t, err := dec.Token()
		if err != nil || t != json.Delim('{') {
			return nil
		}
		var names []string
		for dec.More() {
			nt, err := dec.Token()
			if err != nil {
				return names
			}
			name, _ := nt.(string)
			var skip json.RawMessage
			if dec.Decode(&skip) != nil {
				return names
			}
			if name != "" {
				names = append(names, name)
			}
		}
		return names
	}
	return nil
}

func detectCargo(root string) []Task {
	if !exists(root, "Cargo.toml") {
		return nil
	}
	m := []string{"$rustc"}
	return []Task{
		{Label: "cargo: build", Command: "cargo build", Group: GroupBuild, ProblemMatcher: m, Source: "cargo"},
		{Label: "cargo: test", Command: "cargo test", Group: GroupTest, ProblemMatcher: m, Source: "cargo"},
		{Label: "cargo: run", Command: "cargo run", ProblemMatcher: m, Source: "cargo"},
		{Label: "cargo: clippy", Command: "cargo clippy", ProblemMatcher: m, Source: "cargo"},
	}
}

func fileContains(root, name, needle string) bool {
	data, err := os.ReadFile(filepath.Join(root, name))
	return err == nil && bytes.Contains(data, []byte(needle))
}

func detectPytest(root string) []Task {
	if !(exists(root, "pytest.ini") || exists(root, "conftest.py") ||
		fileContains(root, "pyproject.toml", "pytest") ||
		fileContains(root, "setup.cfg", "pytest") ||
		fileContains(root, "tox.ini", "pytest")) {
		return nil
	}
	return []Task{{
		Label: "pytest: run tests", Command: "python -m pytest", Group: GroupTest,
		ProblemMatcher: []string{"$pytest"}, Source: "pytest",
	}}
}

var makeTargetRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.\-/ \t]*):([^=:]|$)`)

// MakeTargets returns the explicit targets of a Makefile in file order
// (no pattern rules, no special .TARGETS, no variables).
func MakeTargets(data []byte) []string {
	seen := map[string]bool{}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		m := makeTargetRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, name := range strings.Fields(strings.SplitN(line, ":", 2)[0]) {
			if strings.ContainsAny(name, "%$") || strings.HasPrefix(name, ".") || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func detectMake(root string) []Task {
	var data []byte
	for _, name := range []string{"GNUmakefile", "Makefile", "makefile"} {
		if d, err := os.ReadFile(filepath.Join(root, name)); err == nil {
			data = d
			break
		}
	}
	if data == nil {
		return nil
	}
	m := []string{"$gcc"}
	targets := MakeTargets(data)
	if len(targets) == 0 {
		return []Task{{Label: "make", Command: "make", Group: GroupBuild, ProblemMatcher: m, Source: "make"}}
	}
	out := make([]Task, 0, len(targets))
	for i, tg := range targets {
		t := Task{Label: "make: " + tg, Command: "make " + ShellQuote(tg), ProblemMatcher: m, Source: "make"}
		switch {
		case tg == "test" || tg == "check":
			t.Group = GroupTest
		case tg == "build" || tg == "all" || i == 0:
			t.Group = GroupBuild
		}
		out = append(out, t)
	}
	return out
}

var justRecipeRe = regexp.MustCompile(`^@?([A-Za-z_][A-Za-z0-9_-]*)(\s[^:]*)?:([^=]|$)`)

// JustRecipes returns the public recipe names of a justfile in file order.
func JustRecipes(data []byte) []string {
	seen := map[string]bool{}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line[0] == '[' {
			continue
		}
		first := strings.Fields(line)[0]
		switch first {
		case "set", "export", "alias", "import", "mod":
			continue
		}
		m := justRecipeRe.FindStringSubmatch(line)
		if m == nil || strings.HasPrefix(m[1], "_") || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, m[1])
	}
	return out
}

func detectJust(root string) []Task {
	var data []byte
	for _, name := range []string{"justfile", "Justfile", ".justfile"} {
		if d, err := os.ReadFile(filepath.Join(root, name)); err == nil {
			data = d
			break
		}
	}
	if data == nil {
		return nil
	}
	var out []Task
	for _, r := range JustRecipes(data) {
		t := Task{Label: "just: " + r, Command: "just " + r, ProblemMatcher: []string{"$generic"}, Source: "just"}
		switch r {
		case "build":
			t.Group = GroupBuild
		case "test":
			t.Group = GroupTest
		}
		out = append(out, t)
	}
	return out
}
