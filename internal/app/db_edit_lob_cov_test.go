package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// dbePrompt answers the open prompt through the real router and feeds
// what it returns back to the model.
func dbePrompt(t *testing.T, m *Model, value string) {
	t.Helper()
	if !m.promptOpen {
		t.Fatalf("no prompt open for %q (toast %q)", value, cdbToast(m))
	}
	m.promptOpen = false
	cmd, ok := m.handleHTTPPromptSubmit(value)
	if !ok {
		t.Fatalf("prompt %v not routed", m.promptKind)
	}
	dbePump(m, cmd)
}

// gS saves a cell to a new file and never replaces one; gL loads a file
// into a cell as a pending change.
func TestCovDBLOBSaveAndLoad(t *testing.T) {
	m, _ := dbeModel(t)
	dir := t.TempDir()
	if m.databaseLOBSaveStart(0, 0) != nil {
		t.Fatal("gS with no result")
	}
	dbeOpen(t, m, "files")
	s := m.dbConsole()
	tab := s.curTab()

	m.databaseLOBSaveStart(0, 9)
	dbeToastHas(t, m, "Move to a cell first")
	m.databaseLOBSaveStart(1, 1)
	dbeToastHas(t, m, "This cell has no value")

	m.databaseLOBSaveStart(0, 1)
	if !m.promptOpen || m.promptKind != promptKindDBLOBSave || m.prompt.Value() != "files-data.png" {
		t.Fatalf("save prompt %v %q", m.promptOpen, m.prompt.Value())
	}
	png := filepath.Join(dir, "v.png")
	dbePrompt(t, m, png)
	dbeToastHas(t, m, "Saved the value (12 B)")
	if b, _ := os.ReadFile(png); !bytes.HasPrefix(b, []byte("\x89PNG")) {
		t.Fatalf("file %q", b)
	}
	m.databaseLOBSaveStart(0, 1)
	dbePrompt(t, m, png)
	dbeToastHas(t, m, "That file already exists")
	m.databaseLOBSaveStart(0, 2)
	if m.prompt.Value() != "files-body.txt" {
		t.Fatalf("text name %q", m.prompt.Value())
	}
	dbePrompt(t, m, filepath.Join(dir, "no", "such", "dir.txt"))
	dbeToastHas(t, m, "Cannot save the value")
	m.databaseLOBSaveStart(0, 2)
	dbePrompt(t, m, "   ")
	dbeToastHas(t, m, "No file")
	m.databaseLOBSaveStart(0, 2)
	txt := filepath.Join(dir, "body.txt")
	dbePrompt(t, m, txt)
	if b, _ := os.ReadFile(txt); string(b) != "hello" {
		t.Fatalf("text file %q", b)
	}

	// gL: bytes into the binary column, text into the text column.
	bin := filepath.Join(dir, "in.bin")
	_ = os.WriteFile(bin, []byte{0, 1, 2, 0xff}, 0o644)
	dbePump(m, m.databaseEditKey("gL", 0, 1))
	if !m.promptOpen || m.promptKind != promptKindDBLOBLoad {
		t.Fatalf("no load prompt (toast %q)", cdbToast(m))
	}
	dbePrompt(t, m, bin)
	dbeToastHas(t, m, "Loaded 4 B into data")
	if c, _ := dbCellNow(tab, dbEditPos{orig: 0, added: -1, col: 1}); c.Text != "0x000102ff" || c.Kind != "binary" {
		t.Fatalf("binary cell %+v", c)
	}
	dbePump(m, m.databaseEditKey("gL", 0, 2))
	dbePrompt(t, m, bin)
	dbeToastHas(t, m, "in.bin is not text")
	dbePump(m, m.databaseEditKey("gL", 0, 2))
	dbePrompt(t, m, txt)
	if c, _ := dbCellNow(tab, dbEditPos{orig: 0, added: -1, col: 2}); c.Text != "hello" {
		t.Fatalf("text cell %+v", c)
	}
	// A typed column refuses text of another type.
	dbePump(m, m.databaseEditKey("gL", 0, 3))
	dbePrompt(t, m, txt)
	dbeToastHas(t, m, "n is int4: type")
	// A folder, a missing file.
	dbePump(m, m.databaseEditKey("gL", 0, 2))
	dbePrompt(t, m, dir)
	dbeToastHas(t, m, "not a regular file")
	dbePump(m, m.databaseEditKey("gL", 0, 2))
	dbePrompt(t, m, filepath.Join(dir, "missing"))
	dbeToastHas(t, m, "Cannot load")

	// A deleted row, a column that is not the table's.
	tab.edits.deleted[1] = true
	dbePump(m, m.databaseEditKey("gL", 1, 2))
	dbeToastHas(t, m, "This row will be deleted")
	delete(tab.edits.deleted, 1)
	tab.target.editable[2] = false
	dbePump(m, m.databaseEditKey("gL", 1, 2))
	dbeToastHas(t, m, "This column cannot be edited")
	tab.target.editable[2] = true

	// The result changed while the prompt was open.
	m.databaseLOBSaveStart(0, 2)
	s.lobAt = dbEditPos{orig: 9, added: -1, col: 2}
	m.promptOpen = false
	if cmd, ok := m.databaseLOBPrompt(promptKindDBLOBSave, txt); !ok || cmd != nil {
		t.Fatal("a stale cell was saved")
	}
	if _, ok := m.databaseLOBPrompt(promptKindDBEditCell, "x"); ok {
		t.Fatal("a foreign prompt")
	}
	m.onDBLOBLoaded(dbLOBLoadedMsg{tab: tab, run: &dbRun{}, pos: dbEditPos{orig: 0, added: -1, col: 2}, display: "f"})
	dbeToastHas(t, m, "The result changed")
	m.onDBLOBLoaded(dbLOBLoadedMsg{tab: tab, run: tab.run, pos: dbEditPos{orig: 0, added: 5, col: 2}, display: "f"})
	m.onDBLOBLoaded(dbLOBLoadedMsg{err: errors.New("x"), display: "f"})
	dbeToastHas(t, m, "Cannot load f")
	// A NULL now in the cell when gS's prompt answers.
	m.databaseLOBSaveStart(0, 2)
	tab.edits.cells[dbCellRef{0, 2}] = dbclient.Cell{Null: true}
	m.promptOpen = false
	if cmd, ok := m.databaseLOBPrompt(promptKindDBLOBSave, filepath.Join(dir, "n.txt")); !ok || cmd != nil {
		t.Fatal("a NULL was saved")
	}
	m.databases = nil
	if m.onDBLOBLoaded(dbLOBLoadedMsg{}) != nil {
		t.Fatal("no databases")
	}
}

// A new row's cell takes a loaded file too; a file over the limit is
// refused.
func TestCovDBLOBNewRowAndTooBig(t *testing.T) {
	m, _ := dbeModel(t)
	dir := t.TempDir()
	dbeOpen(t, m, "files")
	tab := m.dbConsole().curTab()
	dbePump(m, m.databaseEditKey("+", 0, 0))
	txt := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(txt, []byte("new"), 0o644)
	dbePump(m, m.databaseEditKey("gL", 2, 2))
	dbePrompt(t, m, txt)
	if c := tab.edits.added[0].set[2]; c.Text != "new" {
		t.Fatalf("new row cell %+v", c)
	}
	big := filepath.Join(dir, "big.txt")
	f, _ := os.Create(big)
	_ = f.Truncate(dbLOBMax + 1)
	_ = f.Close()
	dbePump(m, m.databaseEditKey("gL", 2, 2))
	dbePrompt(t, m, big)
	dbeToastHas(t, m, "the file is bigger than")
	if !dbPosValid(tab, dbEditPos{orig: -1, added: 0, col: 2}) || dbPosValid(tab, dbEditPos{col: 99}) {
		t.Fatal("dbPosValid")
	}
}

func TestCovDBLOBFileNames(t *testing.T) {
	for _, c := range []struct {
		data []byte
		want string
	}{
		{[]byte("\xff\xd8\xff\xe0rest"), "t-c.jpg"},
		{[]byte("GIF89a....."), "t-c.gif"},
		{[]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), "t-c.webp"},
		{[]byte{1, 2, 3}, "t-c.bin"},
	} {
		if got := dbLOBFileName("t", "c", dbclient.CellOfBytes(c.data)); got != c.want {
			t.Errorf("%q: %q", c.data, got)
		}
	}
	if got := dbLOBFileName("", "", dbclient.Cell{Text: "x"}); got != "value.txt" {
		t.Fatalf("%q", got)
	}
	if !strings.HasSuffix(dbLOBFileName("a b", "c/d", dbclient.Cell{Text: "x"}), ".txt") {
		t.Fatal("unsafe name")
	}
}
