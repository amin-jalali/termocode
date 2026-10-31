package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProfilesFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProfileAllows(t *testing.T) {
	p := Profile{Servers: []string{"github", "linear"}, Tools: []string{"github.*", "!github.delete_*", "linear.list_*"}}
	for _, c := range []struct {
		server, tool string
		want         bool
	}{
		{"github", "create_issue", true},
		{"github", "delete_repo", false},
		{"linear", "list_issues", true},
		{"linear", "create_issue", false},
		{"slack", "post", false},
	} {
		if got := p.Allows(c.server, c.tool); got != c.want {
			t.Errorf("%s.%s = %v, want %v", c.server, c.tool, got, c.want)
		}
	}
	if !(Profile{}).Allows("any", "thing") {
		t.Error("an empty profile allows everything")
	}
	if !(Profile{Servers: []string{"a"}}).AllowsServer("a") || (Profile{Servers: []string{"a"}}).AllowsServer("b") {
		t.Error("AllowsServer")
	}
	if !(Profile{Tools: []string{"!a.x"}}).Allows("a", "y") {
		t.Error("only excludes: the rest is allowed")
	}
}

func TestLoadProfilesUserWinsAndBadNamesWarn(t *testing.T) {
	d := t.TempDir()
	user := ProfilesPath(filepath.Join(d, "cfg"))
	proj := ProjectProfilesPath(filepath.Join(d, "ws"))
	writeProfilesFile(t, user, `{"profiles":{"web":{"description":"mine","servers":["github"]}}}`)
	writeProfilesFile(t, proj, `{"profiles":{"web":{"description":"theirs"},"docs":{"servers":["notion"],"tools":["notion.*"]},"bad name!":{}}}`)
	ps, warns := LoadProfiles(user, proj)
	if len(ps) != 2 || ps[0].Name != "docs" || ps[1].Name != "web" {
		t.Fatalf("profiles = %+v", ps)
	}
	if ps[1].Description != "mine" || ps[1].Source != "user" || ps[0].Source != "project" {
		t.Fatalf("sources = %+v", ps)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "bad name!") {
		t.Fatalf("warnings = %v", warns)
	}
	if _, w := LoadProfiles(filepath.Join(d, "none.json"), ""); len(w) != 0 {
		t.Fatalf("a missing file warned: %v", w)
	}
	writeProfilesFile(t, user, `{not json`)
	if _, w := LoadProfiles(user, ""); len(w) != 1 {
		t.Fatalf("bad json gave %v", w)
	}
}

func TestFilterServerTools(t *testing.T) {
	sts := []ServerTools{
		{Server: Server{Name: "github"}, Tools: []Tool{{Name: "a"}, {Name: "delete_x"}}},
		{Server: Server{Name: "slack"}, Tools: []Tool{{Name: "post"}}},
	}
	got := FilterServerTools(sts, &Profile{Servers: []string{"github"}, Tools: []string{"!github.delete_*"}})
	if len(got) != 1 || len(got[0].Tools) != 1 || got[0].Tools[0].Name != "a" {
		t.Fatalf("got %+v", got)
	}
	if len(FilterServerTools(sts, nil)) != 2 {
		t.Fatal("nil profile changed the list")
	}
	if sts[0].Tools[1].Name != "delete_x" {
		t.Fatal("input changed")
	}
}

func TestExportImportProfile(t *testing.T) {
	d := t.TempDir()
	user := ProfilesPath(d)
	p := Profile{Name: "web", Description: "web work", Servers: []string{"github"}, Tools: []string{"github.*"}}
	b, err := ExportProfiles([]Profile{p})
	if err != nil || !strings.Contains(string(b), `"web"`) {
		t.Fatalf("export %s %v", b, err)
	}
	names, err := ImportProfiles(user, b)
	if err != nil || len(names) != 1 || names[0] != "web" {
		t.Fatalf("import %v %v", names, err)
	}
	ps, _ := LoadProfiles(user, "")
	if len(ps) != 1 || ps[0].Description != "web work" || ps[0].Tools[0] != "github.*" {
		t.Fatalf("loaded %+v", ps)
	}
	if _, err := ImportProfiles(user, []byte(`{"profiles":{}}`)); err == nil {
		t.Fatal("an empty file was accepted")
	}
	if _, err := ImportProfiles(user, []byte(`{"profiles":{"x y":{}}}`)); err == nil {
		t.Fatal("a bad name was accepted")
	}
}
