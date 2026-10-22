package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkippedItemsBecomeNotImportedRows(t *testing.T) {
	doc := Document{Skipped: []Skipped{
		{RowKey, "keymaps/Mine.xml", "not valid XML; skipped"},
		{RowExtension, "com.github.copilot", "is turned off in the source editor; not imported"},
	}}
	p := Build(Source{ID: "jetbrains"}, doc, Current{})
	if len(p.Rows) != 2 {
		t.Fatalf("rows = %+v", p.Rows)
	}
	for i, want := range []RowKind{RowKey, RowExtension} {
		r := p.Rows[i]
		if r.Kind != want || r.Apply || r.Equivalent != "not imported" || r.Reason != doc.Skipped[i].Reason || r.SourceText != doc.Skipped[i].Source {
			t.Errorf("row %d = %+v", i, r)
		}
	}
	// Nothing from a not-imported row reaches the files.
	c, err := Apply(p)
	if err != nil || len(c.Tools) != 0 || c.Theme != "" {
		t.Fatalf("%v %+v", err, c)
	}
}

func TestEveryRowThatIsNotImportedSaysWhy(t *testing.T) {
	for _, tc := range []struct {
		source, fixture string
		read            func(string) (Document, error)
	}{
		{"jetbrains", "jetbrains-linux", ReadJetBrains},
		{"jetbrains", "jetbrains-macos", ReadJetBrains},
		{"sublime", "sublime-linux", ReadSublime},
		{"sublime", "sublime-macos", ReadSublime},
	} {
		d, err := tc.read(filepath.Join("testdata", tc.fixture))
		if err != nil {
			t.Fatal(err)
		}
		p := Build(Source{ID: tc.source}, d, Current{})
		for _, r := range p.Rows {
			if !r.Apply && !r.Conflict && r.Reason == "" && r.Kind != RowExtension {
				// Rows left alone only because the value is already set carry no reason.
				if strings.Contains(r.Equivalent, "already") {
					continue
				}
				t.Errorf("%s %s: row without a reason: %+v", tc.source, tc.fixture, r)
			}
			if r.Kind == RowExtension && !r.Apply && r.Equivalent == "no equivalent" && r.Reason == "" {
				t.Errorf("extension without a reason: %+v", r)
			}
		}
	}
}

func TestReadDispatchesBySourceAndStaysInsideTheFolder(t *testing.T) {
	d, err := Read(Source{ID: "jetbrains", Dir: filepath.Join("testdata", "jetbrains-linux")})
	if err != nil || len(d.Keys) == 0 {
		t.Fatalf("%v %+v", err, d)
	}
	d, err = Read(Source{ID: "sublime", Dir: filepath.Join("testdata", "sublime-linux")})
	if err != nil || len(d.Keys) == 0 {
		t.Fatalf("%v %+v", err, d)
	}
	if _, err := Read(Source{ID: "nope", Dir: "x"}); err == nil {
		t.Fatal("unknown source accepted")
	}
	// A read never creates or changes anything.
	dir := t.TempDir()
	writeAt(t, dir, "Preferences.sublime-settings", `{"tab_size": 2}`)
	before, _ := os.ReadDir(dir)
	if _, err := Read(Source{ID: "sublime", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadDir(dir)
	if len(before) != len(after) {
		t.Fatal("reader wrote into the folder")
	}
}
