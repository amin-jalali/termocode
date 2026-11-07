package tasklist

import (
	"reflect"
	"strings"
	"testing"
)

const sample = `# Tasks

Some intro text.

- [x] 1. Set up the project
- [ ] 2. Add the parser
  Read the grammar in docs/grammar.md.
  - [ ] keep the old API
- [ ] 3. Add the CLI flag (after: 2)
- [ ] 4. Write the docs (id: docs) (after: 2, 1)
* [ ] Update the changelog (after: docs, 3)

## Notes
- not a task
`

// CMP-76: a markdown checklist becomes tasks: numbers or (id: …) name
// them, (after: …) are the deps, done tasks are dropped with the deps on
// them, indented lines are the task's detail.
func TestParse(t *testing.T) {
	l, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	if l.Done != 1 {
		t.Errorf("done %d", l.Done)
	}
	var ids []string
	for _, tk := range l.Tasks {
		ids = append(ids, tk.ID)
	}
	if want := []string{"2", "3", "docs", "task-5"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids %v", ids)
	}
	if tk := l.Tasks[0]; tk.Title != "Add the parser" || !strings.Contains(tk.Detail, "docs/grammar.md") ||
		!strings.Contains(tk.Detail, "keep the old API") || len(tk.Deps) != 0 || tk.Line != 6 {
		t.Errorf("task 2 %+v", tk)
	}
	if d := l.Tasks[1].Deps; !reflect.DeepEqual(d, []string{"2"}) {
		t.Errorf("task 3 deps %v", d)
	}
	if tk := l.Tasks[2]; tk.Title != "Write the docs" || !reflect.DeepEqual(tk.Deps, []string{"2"}) {
		t.Errorf("docs %+v (the dep on done task 1 is dropped)", tk)
	}
	if d := l.Tasks[3].Deps; !reflect.DeepEqual(d, []string{"docs", "3"}) {
		t.Errorf("changelog deps %v", d)
	}
	waves, err := l.Waves()
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"2"}, {"3", "docs"}, {"task-5"}}; !reflect.DeepEqual(waves, want) {
		t.Errorf("waves %v", waves)
	}
	if leaves := l.Leaves(); !reflect.DeepEqual(leaves, []string{"task-5"}) {
		t.Errorf("leaves %v", leaves)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"no tasks":     "# Nothing\n\n- [x] done\n",
		"unknown dep":  "- [ ] a (after: zz)\n",
		"cycle":        "- [ ] a (id: a) (after: b)\n- [ ] b (id: b) (after: a)\n",
		"duplicate id": "- [ ] x (id: a)\n- [ ] y (id: a)\n",
		"bad id":       "- [ ] x (id: no spaces)\n",
		"empty title":  "- [ ] (id: a)\n",
	}
	want := map[string]string{"no tasks": "no open tasks", "unknown dep": `unknown task "zz"`, "cycle": "cycle",
		"duplicate id": "used twice", "bad id": "id", "empty title": "no title"}
	for name, src := range cases {
		_, err := Parse(src)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if !strings.Contains(err.Error(), want[name]) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var many strings.Builder
	for i := 0; i <= MaxTasks; i++ {
		many.WriteString("- [ ] task\n")
	}
	if _, err := Parse(many.String()); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Errorf("too many: %v", err)
	}
}

// Checkboxes inside code fences are no tasks.
func TestParseFencesIgnored(t *testing.T) {
	l, err := Parse("```\n- [ ] in code\n```\n- [ ] real\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Tasks) != 1 || l.Tasks[0].Title != "real" {
		t.Errorf("%+v", l.Tasks)
	}
}
