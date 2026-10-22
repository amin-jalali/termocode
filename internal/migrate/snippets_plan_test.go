package migrate

import (
	"encoding/json"
	"strings"
	"testing"
)

var vsSource = Source{ID: "vscode", Title: "VS Code"}

func snipDoc() Document {
	return Document{Snippets: []Snippet{
		{Scope: "go", Trigger: "errw", Name: "Error wrap", Body: "if err != nil {\n\treturn err\n}"},
		{Scope: "*", Trigger: "todo", Name: "Todo", Body: "// TODO"},
		{Scope: "python", Trigger: "pr", Name: "Print", Body: "print($1)"},
	}}
}

func snipRows(p Plan) []ReviewRow {
	var out []ReviewRow
	for _, r := range p.Rows {
		if r.Kind == RowSnippet {
			out = append(out, r)
		}
	}
	return out
}

func TestPlanSnippetRowsAreOnByDefault(t *testing.T) {
	p := Build(vsSource, snipDoc(), Current{})
	rows := snipRows(p)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	r := rows[0]
	if !r.Apply || r.Conflict || r.Scope != "go" || r.Trigger != "errw" || !strings.Contains(r.SourceText, "go · errw · Error wrap") || r.Reason != "" {
		t.Errorf("row = %+v", r)
	}
	if rows[1].SourceText != "all languages · todo · Todo" {
		t.Errorf("global row = %q", rows[1].SourceText)
	}
	if p.Counts()[RowSnippet] != 3 {
		t.Errorf("counts = %v", p.Counts())
	}
}

func TestPlanSnippetClashIsAConflictAndOff(t *testing.T) {
	cur := Current{SnippetsRaw: []byte(`{"GO": {"errw": ["old", "two"]}, "global": {"todo": "// TODO"}}`), SnippetsExist: true}
	p := Build(vsSource, snipDoc(), cur)
	rows := snipRows(p)
	// go:errw differs -> conflict (current -> new), off.
	if r := rows[0]; !r.Conflict || r.Apply || r.Reason == "" || !strings.Contains(r.Equivalent, "old") || !strings.Contains(r.Equivalent, "→") || !strings.Contains(r.Equivalent, "if err != nil {") {
		t.Errorf("conflict row = %+v", r)
	}
	// the same body already there -> nothing to do, no conflict.
	if r := rows[1]; r.Conflict || r.Apply || !strings.Contains(r.Reason, "already") {
		t.Errorf("same row = %+v", r)
	}
	if r := rows[2]; !r.Apply || r.Conflict {
		t.Errorf("free row = %+v", r)
	}
}

func TestApplySnippetsWritesOnlySelectedAndKeepsTheRest(t *testing.T) {
	cur := Current{SnippetsRaw: []byte(`{"go": {"keep": ["a", "b"], "errw": "old"}, "markdown": {"x": "y"}}`), SnippetsExist: true}
	p := Build(vsSource, snipDoc(), cur)
	for i := range p.Rows {
		if p.Rows[i].Kind == RowSnippet && p.Rows[i].Trigger != "pr" && p.Rows[i].Trigger != "errw" {
			p.Rows[i].Apply = false
		}
		if p.Rows[i].Trigger == "errw" {
			p.Rows[i].Apply = true // the user turned the conflict on
		}
	}
	out, err := Apply(p)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]map[string]any
	if err := json.Unmarshal(out.Snippets, &got); err != nil {
		t.Fatalf("%v\n%s", err, out.Snippets)
	}
	if got["go"]["errw"].([]any)[0] != "if err != nil {" || len(got["go"]["errw"].([]any)) != 3 {
		t.Errorf("errw = %v", got["go"]["errw"])
	}
	if l := got["go"]["keep"].([]any); len(l) != 2 || l[0] != "a" {
		t.Errorf("existing snippet changed: %v", got["go"]["keep"])
	}
	if got["markdown"]["x"] != "y" || got["python"]["pr"] != "print($1)" {
		t.Errorf("got = %v", got)
	}
	if _, ok := got["*"]; ok {
		t.Errorf("an unselected snippet was written: %v", got["*"])
	}
	if out.Snippets[len(out.Snippets)-1] != '\n' || strings.Contains(string(out.Snippets), `<`) {
		t.Errorf("format: %q", out.Snippets)
	}
}

func TestApplySnippetsIntoAMissingOrAliasedFile(t *testing.T) {
	p := Build(vsSource, snipDoc(), Current{})
	out, err := Apply(p)
	if err != nil || out.Snippets == nil {
		t.Fatalf("%v %s", err, out.Snippets)
	}
	var got map[string]map[string]string
	_ = json.Unmarshal(out.Snippets, &got)
	if got["*"]["todo"] != "// TODO" {
		t.Errorf("global = %v", got)
	}
	// A replaced snippet is written where the user has it (alias scope "global").
	cur := Current{SnippetsRaw: []byte(`{"global": {"todo": "old"}}`), SnippetsExist: true}
	p = Build(vsSource, snipDoc(), cur)
	for i := range p.Rows {
		if p.Rows[i].Trigger == "todo" {
			p.Rows[i].Apply = true
		}
	}
	out, err = Apply(p)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]map[string]any
	_ = json.Unmarshal(out.Snippets, &raw)
	if raw["global"]["todo"] != "// TODO" || len(raw) != 3 {
		t.Errorf("raw = %v", raw)
	}
	if _, dup := raw["*"]["todo"]; dup {
		t.Errorf("trigger written twice: %v", raw)
	}
	// Nothing selected: no file.
	for i := range p.Rows {
		p.Rows[i].Apply = false
	}
	if out, _ := Apply(p); out.Snippets != nil {
		t.Errorf("snippets written without a selection: %s", out.Snippets)
	}
}

func TestApplySnippetsRefusesAFileItCannotKeep(t *testing.T) {
	p := Build(vsSource, snipDoc(), Current{SnippetsRaw: []byte(`[1,2]`), SnippetsExist: true})
	if _, err := Apply(p); err == nil {
		t.Fatal("a snippets.json that is not an object was replaced")
	}
	p = Build(vsSource, snipDoc(), Current{SnippetsExist: true})
	if _, err := Apply(p); err == nil {
		t.Fatal("raw snippets.json is required when the file exists")
	}
}
