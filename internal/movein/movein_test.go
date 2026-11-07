package movein

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow"
)

func readSample(t *testing.T) Found {
	t.Helper()
	f, err := Read(filepath.Join("testdata", "export"))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Spec (CMP-86): Read walks an export folder and finds setup lines,
// memory notes, scheduled tasks and tasks in JSON, JSON Lines and
// Markdown files, by the shape of the data. Unknown files are skipped
// with their name; a bad line does not stop the rest.
func TestRead(t *testing.T) {
	f := readSample(t)
	if strings.Join(f.Setup, ";") != "npm ci;npm run build" {
		t.Errorf("setup %q", f.Setup)
	}
	if len(f.Memory) != 3 {
		t.Fatalf("memory %+v", f.Memory)
	}
	all := ""
	for _, m := range f.Memory {
		all += m.Text + "\n"
	}
	for _, want := range []string{"npm test before you commit", "pnpm only in the web folder", "The API lives in cmd/api"} {
		if !strings.Contains(all, want) {
			t.Errorf("memory misses %q: %q", want, all)
		}
	}
	if len(f.Scheduled) != 3 {
		t.Fatalf("scheduled %+v", f.Scheduled)
	}
	s0, s1, s2 := f.Scheduled[0], f.Scheduled[1], f.Scheduled[2]
	if s0.Title != "Nightly Deps Bump!" || s0.Cron != "0 3 * * *" || s0.TZ != "Europe/Berlin" || !strings.Contains(s0.Prompt, "dependencies") {
		t.Errorf("first scheduled %+v", s0)
	}
	if s1.Title != "Weekly report" || s1.Preset != "weekly" || s1.Time != "08:30" || s1.Cron != "" {
		t.Errorf("second scheduled %+v", s1)
	}
	if s2.Cron != "" || s2.Preset != "" || !strings.Contains(s2.Note, "every full moon") {
		t.Errorf("an unknown schedule must be left out with a note: %+v", s2)
	}
	if len(f.Tasks) != 3 {
		t.Fatalf("tasks %+v", f.Tasks)
	}
	titles := map[string]Task{}
	for _, x := range f.Tasks {
		titles[x.Title] = x
	}
	if x := titles["Fix login timeout"]; x.Done || !strings.Contains(x.Body, "5 minutes") || x.Status != "inprogress" {
		t.Errorf("open task %+v", x)
	}
	if x := titles["Add dark mode"]; !x.Done {
		t.Errorf("a done status must mark the task done: %+v", x)
	}
	if x := titles["Flaky upload test"]; !strings.Contains(x.Body, "one time in ten") {
		t.Errorf("JSON Lines task %+v", x)
	}
	skipped := strings.Join(f.Skipped, "\n")
	if !strings.Contains(skipped, "blob.bin") {
		t.Errorf("an unknown file must be listed as skipped: %q", skipped)
	}
	if !strings.Contains(skipped, "issues.jsonl") {
		t.Errorf("a bad JSON line must be listed: %q", skipped)
	}
	if f.Empty() {
		t.Error("the sample is not empty")
	}
	if !(Found{}).Empty() {
		t.Error("nothing found is empty")
	}
}

// Spec (CMP-86): a single JSON file works as well as a folder; a path
// that does not exist is an error.
func TestReadFileAndMissing(t *testing.T) {
	f, err := Read(filepath.Join("testdata", "export", "data.json"))
	if err != nil || len(f.Scheduled) != 3 || len(f.Tasks) != 2 {
		t.Errorf("one file: %v %+v", err, f)
	}
	if _, err := Read(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a missing path must fail")
	}
}

func byPath(ws []Write) map[string]Write {
	m := map[string]Write{}
	for _, w := range ws {
		m[w.Path] = w
	}
	return m
}

// Spec (CMP-86): in an empty workspace the plan creates one workflow per
// scheduled task (a valid file whose name matches; the schedule only when
// it was understood; the agent only reads until the user changes it),
// the worktree setup file, AGENTS.md with the notes and a task list.
func TestPlanEmptyWorkspace(t *testing.T) {
	ws := t.TempDir()
	plan, err := Plan(readSample(t), ws, Options{})
	if err != nil {
		t.Fatal(err)
	}
	m := byPath(plan)
	for _, name := range []string{"nightly-deps-bump", "weekly-report", "odd-one"} {
		p := ".termocode/workflows/" + name + workflow.Ext
		w, ok := m[p]
		if !ok || w.Mode != ModeCreate {
			t.Fatalf("no workflow %s in %+v", p, plan)
		}
		if _, probs, err := workflow.Parse(filepath.Join(ws, filepath.FromSlash(p)), w.Content); err != nil {
			t.Errorf("%s does not parse: %v\n%s", p, err, w.Content)
		} else if len(probs) > 0 {
			t.Errorf("%s warnings %v", p, probs)
		}
		if !strings.Contains(string(w.Content), `"agent": "codex"`) || !strings.Contains(string(w.Content), `"sandbox": "read-only"`) {
			t.Errorf("%s: default agent codex, read-only:\n%s", p, w.Content)
		}
	}
	if c := string(m[".termocode/workflows/nightly-deps-bump.jsonc"].Content); !strings.Contains(c, `"cron": "0 3 * * *"`) || !strings.Contains(c, `"tz": "Europe/Berlin"`) {
		t.Errorf("cron schedule:\n%s", c)
	}
	if c := string(m[".termocode/workflows/weekly-report.jsonc"].Content); !strings.Contains(c, `"preset": "weekly"`) || !strings.Contains(c, `"time": "08:30"`) {
		t.Errorf("preset schedule:\n%s", c)
	}
	if c := string(m[".termocode/workflows/odd-one.jsonc"].Content); strings.Contains(c, `"schedule"`) {
		t.Errorf("an unknown schedule must stay out:\n%s", c)
	}
	if w := m[".termocode/worktrees.json"]; w.Mode != ModeCreate || !strings.Contains(string(w.Content), `"npm ci"`) {
		t.Errorf("worktrees.json %+v", w)
	}
	if w := m["AGENTS.md"]; w.Mode != ModeCreate || !strings.Contains(string(w.Content), "npm test before you commit") {
		t.Errorf("AGENTS.md %+v", w)
	}
	w := m[".termocode/imported/tasks.md"]
	if w.Mode != ModeCreate || !strings.Contains(string(w.Content), "- [ ] Fix login timeout") || !strings.Contains(string(w.Content), "- [x] Add dark mode") {
		t.Errorf("tasks.md %+v\n%s", w, w.Content)
	}
	for _, w := range plan {
		if len(w.Preview) == 0 {
			t.Errorf("%s has no preview for the card", w.Path)
		}
	}
}

// Spec (CMP-86): the plan never overwrites. A taken workflow name gets a
// new one; AGENTS.md gets the new notes appended (none twice); an
// existing worktrees.json is skipped with the lines to add by hand; a
// taken task list gets the next free name. Options.Agent picks the agent.
func TestPlanExistingFiles(t *testing.T) {
	ws := t.TempDir()
	put := func(rel, s string) {
		p := filepath.Join(ws, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put(".termocode/workflows/nightly-deps-bump.jsonc", "{}")
	put("AGENTS.md", "# Notes\n\nRun the tests with npm test before you commit.\n")
	put(".termocode/worktrees.json", `{"setup": ["make"]}`)
	put(".termocode/imported/tasks.md", "mine")
	plan, err := Plan(readSample(t), ws, Options{Agent: "claude-code"})
	if err != nil {
		t.Fatal(err)
	}
	m := byPath(plan)
	w2, ok := m[".termocode/workflows/nightly-deps-bump-2.jsonc"]
	if !ok {
		t.Fatalf("no new name: %+v", plan)
	}
	if _, _, err := workflow.Parse(filepath.Join(ws, ".termocode", "workflows", "nightly-deps-bump-2.jsonc"), w2.Content); err != nil {
		t.Errorf("renamed workflow: %v\n%s", err, w2.Content)
	}
	if !strings.Contains(string(w2.Content), `"agent": "claude-code"`) {
		t.Errorf("agent option:\n%s", w2.Content)
	}
	a := m["AGENTS.md"]
	if a.Mode != ModeAppend || strings.Contains(string(a.Content), "npm test before you commit") || !strings.Contains(string(a.Content), "pnpm only") {
		t.Errorf("AGENTS.md append %+v\n%s", a, a.Content)
	}
	if w := m[".termocode/worktrees.json"]; w.Mode != ModeSkip || !strings.Contains(strings.Join(w.Preview, "\n"), "npm ci") || w.Why == "" {
		t.Errorf("worktrees.json must be skipped with the lines: %+v", w)
	}
	if _, ok := m[".termocode/imported/tasks-2.md"]; !ok {
		t.Errorf("no free task list name: %+v", plan)
	}
}

// Spec (CMP-86): Apply writes the create and append rows inside the
// workspace and leaves skip rows alone; a file that appeared after the
// plan is never overwritten.
func TestApply(t *testing.T) {
	ws := t.TempDir()
	plan, err := Plan(readSample(t), ws, Options{})
	if err != nil {
		t.Fatal(err)
	}
	done, err := Apply(ws, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != len(plan) {
		t.Errorf("done %v of %d", done, len(plan))
	}
	b, err := os.ReadFile(filepath.Join(ws, "AGENTS.md"))
	if err != nil || !strings.Contains(string(b), "pnpm only") {
		t.Errorf("AGENTS.md %v %q", err, b)
	}
	// The same plan again: every create row now finds its file.
	if _, err := Apply(ws, plan); err == nil {
		t.Error("a create over an existing file must fail")
	}
	if b2, _ := os.ReadFile(filepath.Join(ws, ".termocode", "worktrees.json")); string(b2) != string(byPath(plan)[".termocode/worktrees.json"].Content) {
		t.Errorf("worktrees.json was changed: %q", b2)
	}
	bad := []Write{{Path: "../out.txt", Mode: ModeCreate, Content: []byte("x")}}
	if _, err := Apply(ws, bad); err == nil {
		t.Error("a path out of the workspace must fail")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(ws), "out.txt")); err == nil {
		t.Error("wrote out of the workspace")
	}
}

// Spec (CMP-86): Slug makes a workflow name: a-z, 0-9 and -, a letter
// first, at most 40 characters.
func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Nightly Deps Bump!":      "nightly-deps-bump",
		"123 go":                  "task-123-go",
		"":                        "imported",
		"!!!":                     "imported",
		strings.Repeat("ab ", 30): strings.TrimSuffix(strings.Repeat("ab-", 13), "-"),
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// Spec (CMP-86): text from the export is data. Quotes, comment marks and
// new lines in a prompt keep the workflow file valid, and a "{{" in it
// never becomes a template.
func TestPromptIsEscaped(t *testing.T) {
	f := Found{Scheduled: []Scheduled{{Title: "x", Prompt: "say \"hi\" */ // \n and {{vars.x}}"}}}
	plan, err := Plan(f, t.TempDir(), Options{})
	if err != nil || len(plan) != 1 {
		t.Fatalf("%v %+v", err, plan)
	}
	if _, _, err := workflow.Parse("/w/.termocode/workflows/x.jsonc", plan[0].Content); err != nil {
		t.Errorf("escaped prompt: %v\n%s", err, plan[0].Content)
	}
	if strings.Contains(string(plan[0].Content), "{{") {
		t.Errorf("a template mark from the export:\n%s", plan[0].Content)
	}
}
