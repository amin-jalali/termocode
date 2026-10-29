package tasks

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeIn(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAndParseBrokenFiles(t *testing.T) {
	root := t.TempDir()
	// tasks.json is a folder: a read error, not "missing".
	if err := os.MkdirAll(ConfigPath(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(root); err == nil || got != nil {
		t.Fatalf("folder: %v %v", got, err)
	}
	for _, bad := range []string{`[{"label": 1}]`, `{"tasks": {}}`, `{`} {
		if got, err := Parse([]byte(bad)); err == nil || got != nil || !strings.HasPrefix(err.Error(), "tasks.json:") {
			t.Errorf("Parse(%s) = %v, %v", bad, got, err)
		}
	}
	root2 := t.TempDir()
	writeIn(t, root2, ".termocode/tasks.json", Template)
	got, err := Load(root2)
	if err != nil || len(got) != 1 || got[0].Group != GroupBuild || !got[0].IsDefault || got[0].ProblemMatcher[0] != "$generic" {
		t.Fatalf("template: %+v %v", got, err)
	}
}

func TestOddGroupAndMatcherShapes(t *testing.T) {
	got, err := Parse([]byte(`[
		{"label":"a","command":"x","group":"TEST","problemMatcher":"  "},
		{"label":"b","command":"x","group":{"kind":"deploy"},"problemMatcher":["$go"," ","$tsc"]},
		{"label":"c","command":"x","group":42,"problemMatcher":{"owner":"x"}},
		{"label":" ","command":"x"},
		{"label":"d","command":" "}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("tasks %+v", got)
	}
	if got[0].Group != GroupTest || got[0].ProblemMatcher != nil {
		t.Errorf("a: %+v", got[0])
	}
	if got[1].Group != "" || !reflect.DeepEqual(got[1].ProblemMatcher, []string{"$go", "$tsc"}) {
		t.Errorf("b: %+v", got[1])
	}
	if got[2].Group != "" || got[2].ProblemMatcher != nil {
		t.Errorf("c: %+v", got[2])
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	in := []Task{
		{Label: "build", Command: "make", Args: []string{"-j", "4"}, Group: GroupBuild, IsDefault: true, ProblemMatcher: []string{"$gcc"}, Source: "tasks.json"},
		{Label: "test", Command: "go test", Cwd: "sub", Env: map[string]string{"A": "1"}, Group: GroupTest, ProblemMatcher: []string{"$go", "$generic"}, Source: "tasks.json"},
		{Label: "plain", Command: "echo", Source: "tasks.json"},
	}
	out, err := Parse([]byte(Marshal(in)))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip:\n%+v\n%+v", out, in)
	}
	if !strings.HasSuffix(Marshal(nil), "\n") || !strings.Contains(Marshal(nil), `"version": 1`) {
		t.Fatal("empty marshal")
	}
}

func TestFindDefaultAndExpandEdges(t *testing.T) {
	list := []Task{{Label: "t1", Group: GroupTest}, {Label: "t2", Group: GroupTest}}
	if d, ok := DefaultFor(list, GroupTest); !ok || d.Label != "t1" {
		t.Fatalf("first in group: %+v", d)
	}
	if _, ok := DefaultFor(list, GroupBuild); ok {
		t.Fatal("no build task")
	}
	if f, ok := Find(list, "t2"); !ok || f.Label != "t2" {
		t.Fatal("find")
	}
	if _, ok := Find(list, "zz"); ok {
		t.Fatal("find miss")
	}
	t.Setenv("TASKS_TEST_VAR", "val")
	v := Vars{WorkspaceFolder: "/w/proj"}
	for in, want := range map[string]string{
		"${env:TASKS_TEST_VAR}-${unknown}": "val-${unknown}",
		"a ${file":                         "a ${file",
		"${workspaceFolderBasename}":       "proj",
		"${relativeFile}${fileBasename}":   "",
	} {
		if got := Expand(in, v); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
	task := Task{Command: "run", Args: []string{"", "a b"}, Cwd: "/abs"}
	if got := task.CommandLine(v); got != `run '' 'a b'` {
		t.Fatalf("command line %q", got)
	}
	if task.Dir(v) != "/abs" {
		t.Fatal("absolute cwd")
	}
}

func TestDetectRunnersAndOddProjects(t *testing.T) {
	for lock, runner := range map[string]string{"pnpm-lock.yaml": "pnpm", "yarn.lock": "yarn", "bun.lock": "bun", "bun.lockb": "bun", "": "npm"} {
		root := t.TempDir()
		writeIn(t, root, "package.json", `{"name":"x","scripts":{"build":"tsc","test":"jest","lint":"eslint ."}}`)
		if lock != "" {
			writeIn(t, root, lock, "")
		}
		got := Detect(root)
		if len(got) != 3 || got[0].Label != runner+": build" || got[0].Group != GroupBuild || got[1].Group != GroupTest || got[2].Group != "" {
			t.Errorf("%s: %+v", lock, got)
		}
	}
	root := t.TempDir()
	writeIn(t, root, "package.json", `{"name":"x"}`)
	writeIn(t, root, "Makefile", ".PHONY: all\n%.o: %.c\n\tcc $<\n")
	writeIn(t, root, "justfile", "set shell := [\"bash\"]\n# c\nbuild:\n\techo\ntest arg:\n\techo\n_hidden:\n\techo\nbuild:\n")
	got := Detect(root)
	var labels []string
	for _, x := range got {
		labels = append(labels, x.Label+"/"+x.Group)
	}
	if strings.Join(labels, ",") != "make/build,just: build/build,just: test/test" {
		t.Fatalf("labels %v", labels)
	}
	for _, bad := range []string{`{"scripts": [1]}`, `{"a": {`, `{"scripts": {"a": `, `{"scripts": {"a": "x", 3}}`, `[1]`} {
		if s := PackageScripts([]byte(bad)); len(s) > 1 {
			t.Errorf("PackageScripts(%s) = %v", bad, s)
		}
	}
	if got := MakeTargets([]byte("a b: c\nb:\n$(X): y\n")); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("make targets %q", got)
	}
}

func TestMatchOddOutput(t *testing.T) {
	lines := []string{
		"\x1b[31msrc/a.ts(3,7): warning TS6133: unused\x1b[0m",
		"version 1.2: not a file",
		"main.go:0: line zero is not a place",
		"main.go:4: note: see here",
		"main.go:5:2: hint: try this",
		"main.go:6:1: info: fyi",
		"main.go:6:1: info: fyi",
		"x.c:9:",
	}
	got := Match([]string{"$gcc"}, lines)
	want := []Problem{
		{Path: "src/a.ts", Line: 3, Col: 7, Severity: SevWarning, Message: "TS6133: unused"},
		{Path: "main.go", Line: 4, Col: 1, Severity: SevInfo, Message: "see here"},
		{Path: "main.go", Line: 5, Col: 2, Severity: SevHint, Message: "try this"},
		{Path: "main.go", Line: 6, Col: 1, Severity: SevInfo, Message: "fyi"},
		{Path: "x.c", Line: 9, Col: 1, Severity: SevError, Message: "problem"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if !KnownMatcher("$tsc") || KnownMatcher("$nope") {
		t.Fatal("known matchers")
	}
}
