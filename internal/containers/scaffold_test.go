package containers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectTemplateByProjectFiles(t *testing.T) {
	dir := t.TempDir()
	if got := DetectTemplate(dir); got != "generic" {
		t.Fatalf("empty dir = %q", got)
	}
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644)
	if got := DetectTemplate(dir); got != "go" {
		t.Fatalf("go.mod dir = %q", got)
	}
}

func TestScaffoldNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "Dockerfile")
	_ = os.WriteFile(old, []byte("FROM mine\n"), 0o644)
	var tmpl Template
	for _, x := range BuiltinTemplates() {
		if x.ID == "go" {
			tmpl = x
		}
	}
	p := PlanScaffold(dir, tmpl)
	if len(p.Kept()) != 1 || p.Kept()[0].Rel != "Dockerfile" {
		t.Fatalf("kept = %+v", p.Kept())
	}
	wrote, err := WriteScaffold(p)
	if err != nil || strings.Join(wrote, ",") != ".dockerignore,compose.yaml" {
		t.Fatalf("wrote %v, %v", wrote, err)
	}
	if b, _ := os.ReadFile(old); string(b) != "FROM mine\n" {
		t.Fatalf("Dockerfile was overwritten: %q", b)
	}
	// A file that appears after the plan is kept too.
	_ = os.Remove(filepath.Join(dir, "compose.yaml"))
	p = PlanScaffold(dir, tmpl)
	_ = os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("late\n"), 0o644)
	if _, err := WriteScaffold(p); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "compose.yaml")); string(b) != "late\n" {
		t.Fatalf("late compose.yaml was overwritten: %q", b)
	}
}

func TestLoadCustomTemplatesSkipsLinksAndSubfolders(t *testing.T) {
	dir := t.TempDir()
	tdir := filepath.Join(dir, "svc")
	_ = os.MkdirAll(filepath.Join(tdir, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(tdir, "Dockerfile"), []byte("FROM x\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644)
	_ = os.Symlink("/etc/passwd", filepath.Join(tdir, "passwd"))
	ts, err := LoadCustomTemplates(dir)
	if err != nil || len(ts) != 1 || ts[0].ID != "custom:svc" || len(ts[0].Files) != 1 || ts[0].Files["Dockerfile"] != "FROM x\n" {
		t.Fatalf("templates = %+v, %v", ts, err)
	}
	if ts, err := LoadCustomTemplates(filepath.Join(dir, "none")); err != nil || ts != nil {
		t.Fatalf("missing dir = %v, %v", ts, err)
	}
}

func TestAddComposeServiceAppendsInsideServices(t *testing.T) {
	var redis ComposeSnippet
	for _, s := range ComposeSnippets() {
		if s.ID == "redis" {
			redis = s
		}
	}
	in := "services:\n    api:\n        build: .\n\nvolumes:\n    data:\n"
	out, err := AddComposeService(in, redis)
	if err != nil {
		t.Fatal(err)
	}
	want := "services:\n    api:\n        build: .\n    redis:\n        image: redis:7\n        ports:\n            - \"6379:6379\"\n\nvolumes:\n    data:\n"
	if out != want {
		t.Fatalf("out =\n%s\nwant\n%s", out, want)
	}
	if _, err := AddComposeService(out, redis); err == nil {
		t.Fatal("a second redis was added")
	}
	fresh, err := AddComposeService("", redis)
	if err != nil || !strings.HasPrefix(fresh, "services:\n  redis:\n") {
		t.Fatalf("new file = %q, %v", fresh, err)
	}
}
