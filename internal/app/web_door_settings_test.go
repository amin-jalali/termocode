package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/docs"
)

// Spec (MOB-56, plan §3.7): the door's two limits. web_projects_max caps
// the projects the paired gateway keeps open at once (default 4, 1..8);
// web_project_idle_minutes stops a project the phone opened headless
// after that many idle minutes (default 60, 5..240). A bad value is the
// default and one problem line (ADR 0005).
func TestWebDoorSettingsDefaultsAndBadValues(t *testing.T) {
	withConfigDir(t)
	c := webSettings()
	if c.ProjectsMax != 4 || c.ProjectIdleMinutes != 60 || len(c.Problems) != 0 {
		t.Fatalf("defaults %+v", c)
	}
	writeUserConfig(t, `{"web_projects_max":0,"web_project_idle_minutes":4}`)
	c = webSettings()
	if c.ProjectsMax != 4 || c.ProjectIdleMinutes != 60 || len(c.Problems) != 2 {
		t.Fatalf("too low %+v", c)
	}
	writeUserConfig(t, `{"web_projects_max":9,"web_project_idle_minutes":241}`)
	c = webSettings()
	if c.ProjectsMax != 4 || c.ProjectIdleMinutes != 60 || len(c.Problems) != 2 {
		t.Fatalf("too high %+v", c)
	}
	for _, p := range c.Problems {
		if !strings.Contains(p, "web_projects_max") && !strings.Contains(p, "web_project_idle_minutes") {
			t.Errorf("problem line names no key: %q", p)
		}
	}
	writeUserConfig(t, `{"web_projects_max":1,"web_project_idle_minutes":240}`)
	c = webSettings()
	if c.ProjectsMax != 1 || c.ProjectIdleMinutes != 240 || len(c.Problems) != 0 {
		t.Fatalf("good values %+v", c)
	}
}

// Spec (MOB-56): the door settings are file only and the AI can never
// change them (more open projects = more of the laptop reachable from
// the phone).
func TestWebDoorSettingsAINeverAndDocumented(t *testing.T) {
	keys := map[string]bool{"web_projects_max": false, "web_project_idle_minutes": false}
	for _, d := range fileOnlySettings {
		if _, ok := keys[d.Key]; !ok {
			continue
		}
		keys[d.Key] = true
		if !strings.Contains(d.Category, "file only") || d.Default == "" || d.Description == "" {
			t.Errorf("%s: row %+v", d.Key, d)
		}
		if p, ok := aiSettingPolicies[d.Key]; !ok || p.Gate != aiGateNever {
			t.Errorf("%s: the AI may change it", d.Key)
		}
	}
	for k, seen := range keys {
		if !seen {
			t.Errorf("%s: not in the settings catalog", k)
		}
	}
}

// Spec (MOB-56): a project's own config files can never raise the door's
// limits; only the user's config.json counts.
func TestProjectConfigCannotChangeWebDoor(t *testing.T) {
	withConfigDir(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "settings.json"} {
		_ = os.WriteFile(filepath.Join(root, ".termocode", name), []byte(`{"web_projects_max":8,"web_project_idle_minutes":240}`), 0o644)
	}
	t.Chdir(root)
	if c := webSettings(); c.ProjectsMax != 4 || c.ProjectIdleMinutes != 60 {
		t.Fatalf("project config changed the door limits: %+v", c)
	}
}

// Spec (MOB-56): the web guide explains the door (several projects from
// one phone), its two settings, its limits and its safety rules, and the
// settings reference lists both keys.
func TestWebGuideCoversTheDoor(t *testing.T) {
	b, err := docs.FS.ReadFile("guides/web.md")
	if err != nil {
		t.Fatal(err)
	}
	g := string(b)
	for _, want := range []string{
		"\n### Several projects from one phone\n",
		"`web_projects_max`",
		"`web_project_idle_minutes`",
		"a card on the laptop",
		"Projects open now",
		"asleep",
		"**One door, your own projects.**",
	} {
		if !strings.Contains(g, want) {
			t.Errorf("guides/web.md misses %q", want)
		}
	}
	ref, err := docs.FS.ReadFile("reference/settings.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"| `web_projects_max` |", "| `web_project_idle_minutes` |"} {
		if !strings.Contains(string(ref), k) {
			t.Errorf("reference/settings.md misses %s", k)
		}
	}
}
