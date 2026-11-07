package specs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// New makes .termocode/specs/<slug>/ with spec.json and the first file
// as a template; a second spec with the same goal gets -2; a bugfix
// spec starts with bugfix.md and its three behavior sections.
func TestCreate(t *testing.T) {
	root := t.TempDir()
	s, err := Create(root, "Add a dark mode toggle!", Feature, now)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "add-a-dark-mode-toggle" || s.Path != filepath.Join(root, ".termocode", "specs", "add-a-dark-mode-toggle") {
		t.Fatalf("spec %+v", s)
	}
	b, err := os.ReadFile(s.FilePath(Requirements))
	if err != nil || !strings.Contains(string(b), "Add a dark mode toggle!") || !strings.Contains(string(b), "Acceptance criteria") {
		t.Fatalf("requirements template %q %v", b, err)
	}
	if st := s.Status(Requirements); st != Template {
		t.Errorf("fresh file status %v", st)
	}
	s2, err := Create(root, "Add a dark mode toggle", Feature, now)
	if err != nil || s2.Name != "add-a-dark-mode-toggle-2" {
		t.Fatalf("second %+v %v", s2, err)
	}
	bug, err := Create(root, "Login times out on Safari", Bugfix, now)
	if err != nil || filepath.Base(bug.FilePath(Requirements)) != "bugfix.md" {
		t.Fatalf("bugfix %+v %v", bug, err)
	}
	b, _ = os.ReadFile(bug.FilePath(Requirements))
	for _, want := range []string{"Current behavior", "Expected behavior", "Unchanged behavior"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("bugfix template lacks %q", want)
		}
	}
	if _, err := Create(root, "  !!  ", Feature, now); err == nil {
		t.Error("a goal with no words made a spec")
	}
	list, err := List(root)
	if err != nil || len(list) != 3 {
		t.Fatalf("list %d %v", len(list), err)
	}
	got, err := Load(root, "login-times-out-on-safari")
	if err != nil || got.Kind != Bugfix || got.Goal != "Login times out on Safari" || !got.Created.Equal(now) {
		t.Errorf("load %+v %v", got, err)
	}
	for _, bad := range []string{"../x", "a/b", "", ".hidden"} {
		if _, err := Load(root, bad); err == nil {
			t.Errorf("Load(%q) worked", bad)
		}
	}
}

// Phases are approved in order; the approval is bound to the file's
// bytes (a change makes it stale), a template or missing file cannot be
// approved, ticking tasks does not make the Tasks approval stale, and
// Build needs every task ticked.
func TestApprove(t *testing.T) {
	root := t.TempDir()
	s, err := Create(root, "pin row", Feature, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(Requirements, now); err == nil {
		t.Error("the empty template was approved")
	}
	if err := s.Approve(Design, now); err == nil {
		t.Error("design approved before requirements")
	}
	write(t, s.FilePath(Requirements), "# R\n- WHEN x THEN y\n")
	if s.Status(Requirements) != Draft {
		t.Errorf("status %v", s.Status(Requirements))
	}
	if err := s.Approve(Requirements, now); err != nil {
		t.Fatal(err)
	}
	if p, done := s.Current(); p != Design || done {
		t.Errorf("current %v %v", p, done)
	}
	if err := s.Approve(Design, now); err == nil {
		t.Error("a missing design was approved")
	}
	re, _ := Load(root, s.Name)
	if re.Status(Requirements) != Approved {
		t.Error("the approval was not stored")
	}
	write(t, s.FilePath(Requirements), "# R\n- WHEN x THEN z\n")
	if s.Status(Requirements) != Stale {
		t.Errorf("changed file: %v", s.Status(Requirements))
	}
	if p, _ := s.Current(); p != Requirements {
		t.Errorf("stale requirements: current %v", p)
	}
	if err := s.Approve(Requirements, now); err != nil {
		t.Fatal(err)
	}
	write(t, s.FilePath(Design), "# D\n")
	if err := s.Approve(Design, now); err != nil {
		t.Fatal(err)
	}
	write(t, s.FilePath(Tasks), "- [ ] 1. model\n- [ ] 2. view (after 1)\n")
	if err := s.Approve(Tasks, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(Build, now); err == nil || !strings.Contains(err.Error(), "2 tasks") {
		t.Errorf("build with open tasks: %v", err)
	}
	write(t, s.FilePath(Tasks), "- [x] 1. model\n- [X] 2. view (after 1)\n")
	if s.Status(Tasks) != Approved {
		t.Errorf("ticking made tasks %v", s.Status(Tasks))
	}
	if err := s.Approve(Build, now); err != nil {
		t.Fatal(err)
	}
	write(t, s.FilePath(Review), "# Review\nall met\n")
	if err := s.Approve(Review, now); err != nil {
		t.Fatal(err)
	}
	if _, done := s.Current(); !done {
		t.Error("not done after review")
	}
}

// tasks.md: tick boxes, a leading number, "(after 1, 2)"; waves are the
// open tasks whose open dependencies come in an earlier wave.
func TestTasksAndWaves(t *testing.T) {
	ts := ParseTasks([]byte("# Tasks\n\n- [x] 1. model\n- [ ] 2. view (after 1)\n  - [ ] a sub step\n- [ ] 3) api\n- [ ] 4. wire it (After 2, 3)\n* [ ] loose\ntext\n"))
	if len(ts) != 5 {
		t.Fatalf("tasks %+v", ts)
	}
	if ts[0].N != 1 || !ts[0].Done || ts[1].Title != "view" || len(ts[1].After) != 1 || ts[1].After[0] != 1 {
		t.Errorf("first two %+v %+v", ts[0], ts[1])
	}
	if ts[2].N != 3 || ts[2].Title != "api" || ts[3].N != 4 || len(ts[3].After) != 2 || ts[4].N != 5 || ts[4].Title != "loose" {
		t.Errorf("rest %+v", ts[2:])
	}
	if done, all := Progress(ts); done != 1 || all != 5 {
		t.Errorf("progress %d/%d", done, all)
	}
	w := Waves(ts)
	if len(w) != 2 || len(w[0]) != 3 || w[0][0] != 2 || w[0][1] != 3 || w[0][2] != 5 || len(w[1]) != 1 || w[1][0] != 4 {
		t.Errorf("waves %v", w)
	}
	// A cycle never hangs: what is left is the last wave.
	w = Waves(ParseTasks([]byte("- [ ] 1. a (after 2)\n- [ ] 2. b (after 1)\n")))
	if len(w) != 1 || len(w[0]) != 2 {
		t.Errorf("cycle waves %v", w)
	}
}

// The AI prompt of each phase names the file to write and the approved
// files to read, and keeps to its phase.
func TestPrompt(t *testing.T) {
	root := t.TempDir()
	s, _ := Create(root, "pin row", Feature, now)
	p := s.Prompt(Requirements, root)
	for _, want := range []string{".termocode/specs/pin-row/requirements.md", "pin row", "WHEN", "Do not write the design or code yet"} {
		if !strings.Contains(p, want) {
			t.Errorf("requirements prompt lacks %q:\n%s", want, p)
		}
	}
	p = s.Prompt(Tasks, root)
	for _, want := range []string{"tasks.md", "requirements.md", "design.md", "- [ ] 1.", "(after"} {
		if !strings.Contains(p, want) {
			t.Errorf("tasks prompt lacks %q:\n%s", want, p)
		}
	}
	write(t, s.FilePath(Tasks), "- [x] 1. a\n- [ ] 2. b (after 1)\n- [ ] 3. c (after 2)\n")
	if p = s.Prompt(Build, root); !strings.Contains(p, "next: 2") || !strings.Contains(p, "test first") {
		t.Errorf("build prompt:\n%s", p)
	}
	bug, _ := Create(root, "login times out", Bugfix, now)
	p = bug.Prompt(Requirements, root)
	for _, want := range []string{"bugfix.md", "Current behavior", "Unchanged behavior", "do not fix it yet"} {
		if !strings.Contains(p, want) {
			t.Errorf("bugfix prompt lacks %q:\n%s", want, p)
		}
	}
	if p = bug.Prompt(Review, root); !strings.Contains(p, "review.md") || !strings.Contains(p, "unchanged") {
		t.Errorf("bugfix review prompt:\n%s", p)
	}
}
