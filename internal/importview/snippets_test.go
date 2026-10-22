package importview

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/migrate"
)

func snippetPanel(t *testing.T, raw string) Model {
	t.Helper()
	doc := migrate.Document{
		Snippets: []migrate.Snippet{
			{Scope: "go", Trigger: "errw", Name: "Error wrap", Body: "if err != nil {\n\treturn err\n}"},
			{Scope: "*", Trigger: "todo", Name: "Todo", Body: "// TODO"},
			{Scope: "python", Trigger: "pr", Name: "Print", Body: "print($1)"},
		},
		Skipped: []migrate.Skipped{{Kind: migrate.RowSnippet, Source: "snippets/go.json: Dotted", Reason: "the prefix \"a.b\" has characters termocode cannot expand"}},
	}
	cur := current()
	if raw != "" {
		cur.SnippetsRaw, cur.SnippetsExist = []byte(raw), true
	}
	m := New(migrate.Build(migrate.Source{ID: "zed", Title: "Editor"}, doc, cur), "~/.config/x")
	m.SetSize(200, 200)
	return m
}

func TestPanelSnippetsSection(t *testing.T) {
	m := snippetPanel(t, "")
	var names []string
	for _, s := range m.sections {
		names = append(names, s.name)
	}
	if strings.Join(names, ",") != "Snippets" {
		t.Fatalf("sections = %v", names)
	}
	text := strings.Join(plain(m), "\n")
	for _, want := range []string{"go · errw · Error wrap", "all languages · todo · Todo", "python · pr · Print", "not imported"} {
		if !strings.Contains(text, want) {
			t.Errorf("panel lacks %q:\n%s", want, text)
		}
	}
	c := m.Counts()
	if c.Snippets != 3 || c.Total() != 3 || c.Summary() != "3 snippets" {
		t.Errorf("counts = %+v %q", c, c.Summary())
	}
	// The not-imported row cannot be turned on.
	m.SelectNone()
	if c := m.Counts(); c.Snippets != 0 || c.Summary() != "nothing selected" {
		t.Errorf("after none: %+v", c)
	}
	m.SelectAll()
	if m.Counts().Snippets != 3 {
		t.Errorf("select all: %+v", m.Counts())
	}
	p := m.Plan()
	n := 0
	for _, r := range p.Rows {
		if r.Kind == migrate.RowSnippet && r.Apply {
			n++
		}
	}
	if n != 3 {
		t.Errorf("plan applies %d snippets", n)
	}
}

func TestPanelSnippetConflictsAreOffAndShowCurrentToNew(t *testing.T) {
	m := snippetPanel(t, `{"go": {"errw": "old body"}, "*": {"todo": "// TODO"}}`)
	k, r := findRow(t, m, "go · errw")
	if r.on || !r.conflict || r.state != stateToggle || !strings.Contains(r.right, "old body → if err != nil {") {
		t.Fatalf("conflict row = %+v", r)
	}
	// The identical one is not selectable.
	if _, same := findRow(t, m, "all languages · todo"); same.state != stateSkip || same.on {
		t.Errorf("same row = %+v", same)
	}
	if c := m.Counts(); c.Snippets != 1 {
		t.Errorf("counts = %+v (only the free python snippet is on)", c)
	}
	m.cursor = k
	m.Toggle()
	if c := m.Counts(); c.Snippets != 2 {
		t.Errorf("after turning the conflict on: %+v", c)
	}
	if got := m.status(); !strings.Contains(got, "replaces") {
		t.Errorf("status = %q", got)
	}
	m.SelectNone()
	m.SelectAll()
	if c := m.Counts(); c.Snippets != 2 {
		t.Errorf("select all keeps identical row off, turns conflict on: %+v", c)
	}
}
