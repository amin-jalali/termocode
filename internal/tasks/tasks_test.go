package tasks

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseObjectAndArray(t *testing.T) {
	obj := `{"version": 1, "tasks": [
		{"label": "b", "command": "make", "args": ["all", "x y"], "group": {"kind": "build", "isDefault": true}, "problemMatcher": "$gcc"},
		{"label": "t", "command": "go test", "group": "test", "problemMatcher": ["$go", ""]},
		{"label": "", "command": "skip"},
		{"label": "nocmd"}
	]}`
	got, err := Parse([]byte(obj))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 tasks, got %d", len(got))
	}
	if got[0].Group != GroupBuild || !got[0].IsDefault || !reflect.DeepEqual(got[0].ProblemMatcher, []string{"$gcc"}) {
		t.Errorf("bad build task: %+v", got[0])
	}
	if got[1].Group != GroupTest || got[1].IsDefault || !reflect.DeepEqual(got[1].ProblemMatcher, []string{"$go"}) {
		t.Errorf("bad test task: %+v", got[1])
	}
	if cl := got[0].CommandLine(Vars{}); cl != "make all 'x y'" {
		t.Errorf("CommandLine = %q", cl)
	}

	arr := `[{"label": "a", "command": "echo"}]`
	got, err = Parse([]byte(arr))
	if err != nil || len(got) != 1 || got[0].Source != "tasks.json" {
		t.Fatalf("array form: %v %+v", err, got)
	}
	if _, err := Parse([]byte(`{bad`)); err == nil {
		t.Error("want error for malformed JSON")
	}
}

func TestLoadMissing(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil || got != nil {
		t.Fatalf("missing file: %v %v", got, err)
	}
}

func TestExpandAndDir(t *testing.T) {
	v := Vars{WorkspaceFolder: "/w", File: "/w/pkg/a.go"}
	if got := Expand("${workspaceFolder}/x ${relativeFile} ${fileBasenameNoExtension} ${unknown}", v); got != "/w/x pkg/a.go a ${unknown}" {
		t.Errorf("Expand = %q", got)
	}
	if got := Expand("${file}", Vars{WorkspaceFolder: "/w"}); got != "" {
		t.Errorf("no-file Expand = %q", got)
	}
	if d := (Task{Cwd: "sub"}).Dir(v); d != "/w/sub" {
		t.Errorf("Dir = %q", d)
	}
	if d := (Task{}).Dir(v); d != "/w" {
		t.Errorf("Dir default = %q", d)
	}
}

func TestMergeAndDefault(t *testing.T) {
	a := []Task{{Label: "x", Group: GroupBuild}, {Label: "y", Group: GroupBuild, IsDefault: true}}
	b := []Task{{Label: "x", Command: "dup"}, {Label: "z", Group: GroupTest}}
	all := Merge(a, b)
	if len(all) != 3 {
		t.Fatalf("Merge len = %d", len(all))
	}
	if d, ok := DefaultFor(all, GroupBuild); !ok || d.Label != "y" {
		t.Errorf("default build = %+v", d)
	}
	if d, ok := DefaultFor(all, GroupTest); !ok || d.Label != "z" {
		t.Errorf("default test = %+v", d)
	}
	if _, ok := DefaultFor(all, "nope"); ok {
		t.Error("unexpected default")
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func labels(ts []Task) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Label)
	}
	return out
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module x\n")
	write(t, dir, "package.json", `{"name": "x", "scripts": {"dev": "vite", "build": "tsc", "test": "jest"}}`)
	write(t, dir, "yarn.lock", "")
	write(t, dir, "Makefile", ".PHONY: all\nall: build\nbuild:\n\tgo build\n%.o: %.c\nVAR := 1\ntest lint:\n")
	write(t, dir, "justfile", "set shell := [\"bash\"]\n# c\ndefault:\n  echo\n_hidden:\nserve port='80':\n  x\n")
	write(t, dir, "pytest.ini", "")
	got := labels(Detect(dir))
	want := []string{
		"go: build", "go: test", "go: vet", "go: run",
		"yarn: dev", "yarn: build", "yarn: test",
		"pytest: run tests",
		"make: all", "make: build", "make: test", "make: lint",
		"just: default", "just: serve",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Detect =\n %v\nwant\n %v", got, want)
	}
	if d, _ := DefaultFor(Detect(dir), GroupTest); d.Label != "go: test" {
		t.Errorf("default test = %q", d.Label)
	}
}

func TestDetectCargoAndEmpty(t *testing.T) {
	dir := t.TempDir()
	if got := Detect(dir); len(got) != 0 {
		t.Fatalf("empty dir: %v", labels(got))
	}
	write(t, dir, "Cargo.toml", "[package]\n")
	if got := labels(Detect(dir)); len(got) != 4 || got[0] != "cargo: build" {
		t.Errorf("cargo: %v", got)
	}
}

func TestPackageScriptsMalformed(t *testing.T) {
	if got := PackageScripts([]byte(`{"scripts": [`)); got != nil {
		t.Errorf("got %v", got)
	}
	if got := PackageScripts([]byte(`{"a": {"scripts": {"x": 1}}, "scripts": {"b": "c"}}`)); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("nested scripts: %v", got)
	}
}
