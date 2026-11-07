package webproto

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (MOB-8): the projects topic exists (v2) and is read only: it adds
// no op the page could call. A project is named by its id (12 hex
// digits), never by its folder.
func TestProjectsTopicIsReadOnly(t *testing.T) {
	tp, err := ParseTopic("projects")
	if err != nil || tp.Name != "projects" || tp.Keyed {
		t.Fatalf("projects: %v %+v", err, tp)
	}
	if d, ok := topicDef("projects"); !ok || d.Phase != PhaseV2 {
		t.Errorf("projects def %+v", d)
	}
	for _, p := range opTable {
		if strings.HasPrefix(p.Op, "projects.") {
			t.Errorf("the read-only projects topic has an op: %s", p.Op)
		}
	}
	ts, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"export interface ProjectsSnap", "export interface ProjectItem", `"projects": { snap: ProjectsSnap; delta: ProjectsSnap };`} {
		if !strings.Contains(string(ts), want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
	if MaxProjects != 50 || ProjectIDLen != 12 {
		t.Errorf("limits %d %d", MaxProjects, ProjectIDLen)
	}
}

// The topic's wire form is pinned by testdata/golden/topics/projects.json.
func TestProjectsSnapGolden(t *testing.T) {
	snap := ProjectsSnap{State: "ready", Working: 2, Waiting: 1, Projects: []ProjectItem{
		{ID: "a1b2c3d4e5f6", Name: "app", Where: "~/src/app", State: "here", Attached: true, Recent: true, Working: 1, OpenedAt: 1_700_000_000_000},
		{ID: "0f0e0d0c0b0a", Name: "api", Where: "~/src/api", State: "running", Service: true, Recent: true, Working: 1, Waiting: 1},
		{ID: "112233445566", Name: "notes", Where: "notes", State: "asleep", Recent: true, OpenedAt: 1_690_000_000_000},
	}}
	got, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join("testdata", "golden", "topics", "projects.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(p, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bytes.TrimSuffix(want, []byte("\n"))) {
		t.Errorf("projects topic changed\n got %s\nwant %s", got, want)
	}
}
