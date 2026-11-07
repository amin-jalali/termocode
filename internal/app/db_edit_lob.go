package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// LOB values and files (DBX-55): gS saves the value of the cell under the
// cursor to a new file (binary as its bytes, text as it is), and gL loads
// a file into an editable cell as a pending change (bytes for a binary
// column, text for the others). The file is never replaced: gS refuses a
// name that is taken. A loaded value is bound as a parameter, and w shows
// the SQL before it runs, as for every edit.

const (
	promptKindDBLOBSave promptKindEnum = 9555
	promptKindDBLOBLoad promptKindEnum = 9556
)

// dbLOBMax is the biggest file gL loads.
const dbLOBMax = 16 << 20

type dbLOBSavedMsg struct {
	display string
	size    int
	err     error
}

type dbLOBLoadedMsg struct {
	tab     *dbTab
	run     *dbRun
	pos     dbEditPos
	display string
	data    []byte
	err     error
}

// dbBinaryColumnType reports whether a column type holds bytes.
func dbBinaryColumnType(typ string) bool {
	t := strings.ToUpper(typ)
	for _, w := range []string{"BLOB", "BYTEA", "BINARY", "IMAGE", "RAW"} {
		if strings.Contains(t, w) {
			return true
		}
	}
	return false
}

// dbLOBFileName is the name gS offers for a cell's value.
func dbLOBFileName(table, col string, c dbclient.Cell) string {
	ext := ".txt"
	if b, ok := dbclient.CellBytes(c); ok {
		ext = ".bin"
		switch kind, _, _ := dbImageInfo(b); kind {
		case "PNG":
			ext = ".png"
		case "JPEG":
			ext = ".jpg"
		case "GIF":
			ext = ".gif"
		case "WebP":
			ext = ".webp"
		}
	}
	base := dbFileSafe.ReplaceAllString(strings.Trim(table+"-"+col, "-"), "_")
	if base == "" {
		base = "value"
	}
	return base + ext
}

// dbLOBCell is the cell for loaded file bytes in a column of type typ
// (cur: the value the cell has now). false: the bytes are not text and
// the column does not hold bytes.
func dbLOBCell(typ string, cur dbclient.Cell, data []byte) (dbclient.Cell, bool) {
	if cur.Kind == "binary" || dbBinaryColumnType(typ) {
		return dbclient.CellOfBytes(data), true
	}
	if !utf8.Valid(data) {
		return dbclient.Cell{}, false
	}
	return dbclient.Cell{Text: string(data)}, true
}

// databaseLOBSaveStart is gS: ask for the file the cell's value goes to.
func (m *Model) databaseLOBSaveStart(row, col int) tea.Cmd {
	s := m.dbConsole()
	t := s.curTab()
	if t == nil || t.run == nil || t.run != s.shown {
		return nil
	}
	pos, ok := m.databaseEditPos(t, row, col)
	if !ok {
		return m.pushToastDetail(toast.Info, "Move to a cell first", "gS saves the value of the cell under the cursor to a file.")
	}
	cur, has := dbCellNow(t, pos)
	if !has || cur.Null || cur.Kind == dbDefaultKind {
		return m.pushToastDetail(toast.Info, "This cell has no value", "Only a value can be saved to a file.")
	}
	s.lobTab, s.lobAt = t, pos
	table := ""
	if t.run.tv != nil {
		table = t.run.tv.name
	}
	name := dbLOBFileName(table, t.run.result.Columns[pos.col].Name, cur)
	cmd := m.openHTTPPrompt(promptKindDBLOBSave, "Save Value to File", "New file (in the workspace, or a full path):", name)
	m.prompt.SetCompleter(prompt.CompletePath)
	return cmd
}

// databaseLOBLoadStart is gL on an editable cell: ask for the file.
func (m *Model) databaseLOBLoadStart(t *dbTab, tg *dbEditTarget, pos dbEditPos) tea.Cmd {
	if pos.orig >= 0 && t.edits != nil && t.edits.deleted[pos.orig] {
		return m.pushToastDetail(toast.Info, "This row will be deleted", "- again keeps it; then gL loads a value.")
	}
	if !tg.editable[pos.col] {
		return m.pushToastDetail(toast.Info, "This column cannot be edited", "It is not a column of "+sanitizeDBText(tg.table)+" (or it is in the result twice).")
	}
	s := m.dbConsole()
	s.lobTab, s.lobAt = t, pos
	col := t.run.result.Columns[pos.col]
	what := "text"
	if cur, has := dbCellNow(t, pos); has && cur.Kind == "binary" || dbBinaryColumnType(col.Type) {
		what = "bytes"
	}
	cmd := m.openHTTPPrompt(promptKindDBLOBLoad, "Load Value from File", "File for "+dbOneLine(col.Name)+" (its "+what+" become the new value):", "")
	m.prompt.SetCompleter(prompt.CompletePath)
	return cmd
}

// databaseLOBPrompt runs the gS / gL prompts. ok=false: not one of them.
func (m *Model) databaseLOBPrompt(kind promptKindEnum, value string) (tea.Cmd, bool) {
	if kind != promptKindDBLOBSave && kind != promptKindDBLOBLoad {
		return nil, false
	}
	s := m.dbConsole()
	t, pos := s.lobTab, s.lobAt
	s.lobTab = nil
	if t == nil || t.run == nil || s.tabIndex(t) < 0 || !dbPosValid(t, pos) {
		return nil, true
	}
	abs, display, err := m.dbResolvePath(value)
	if err != nil {
		return m.pushToastDetail(toast.Info, "No file", err.Error()), true
	}
	if kind == promptKindDBLOBSave {
		cur, has := dbCellNow(t, pos)
		if !has || cur.Null {
			return nil, true
		}
		data, ok := dbclient.CellBytes(cur)
		if !ok {
			data = []byte(cur.Text)
		}
		return func() tea.Msg {
			f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return dbLOBSavedMsg{display: display, err: err}
			}
			_, err = f.Write(data)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			return dbLOBSavedMsg{display: display, size: len(data), err: err}
		}, true
	}
	run := t.run
	return func() tea.Msg {
		msg := dbLOBLoadedMsg{tab: t, run: run, pos: pos, display: display}
		f, err := os.Open(abs)
		if err != nil {
			msg.err = err
			return msg
		}
		defer f.Close()
		if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
			msg.err = errors.New("not a regular file")
			return msg
		}
		data, err := io.ReadAll(io.LimitReader(f, dbLOBMax+1))
		switch {
		case err != nil:
			msg.err = err
		case len(data) > dbLOBMax:
			msg.err = fmt.Errorf("the file is bigger than %s", dbBytes(dbLOBMax))
		default:
			msg.data = data
		}
		return msg
	}, true
}

func (m *Model) onDBLOBSaved(msg dbLOBSavedMsg) tea.Cmd {
	switch {
	case errors.Is(msg.err, os.ErrExist):
		return m.pushToastDetail(toast.Warn, "That file already exists", "Pick another name: gS never replaces a file.")
	case msg.err != nil:
		return m.pushToastDetail(toast.Errr, "Cannot save the value", msg.err.Error())
	}
	return m.pushToastDetail(toast.Info, "Saved the value ("+dbBytes(msg.size)+")", sanitizeDBText(msg.display))
}

func (m *Model) onDBLOBLoaded(msg dbLOBLoadedMsg) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	s := m.dbConsole()
	t := msg.tab
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot load "+sanitizeDBText(msg.display), msg.err.Error())
	}
	if t == nil || t.run != msg.run || s.tabIndex(t) < 0 || !dbPosValid(t, msg.pos) {
		return m.pushToastDetail(toast.Info, "The result changed", "Nothing was loaded. gL again on the cell.")
	}
	col := t.run.result.Columns[msg.pos.col]
	cur, _ := dbCellNow(t, msg.pos)
	cell, ok := dbLOBCell(col.Type, cur, msg.data)
	if !ok {
		return m.pushToastDetail(toast.Warn, sanitizeDBText(msg.display)+" is not text", dbOneLine(col.Name)+" is "+dbOneLine(col.Type)+": load a text file, or a file into a binary column.")
	}
	if cell.Kind != "binary" {
		if cmd, ok := m.databaseCheckTyped(t, msg.pos, cell.Text); !ok {
			return cmd
		}
	}
	if !dbSetCellSpecial(t, msg.pos, cell) {
		return nil
	}
	s.keepCursor = true
	return tea.Batch(m.databaseShowPage(), m.pushToastDetail(toast.Info, "Loaded "+dbBytes(len(msg.data))+" into "+dbOneLine(col.Name), "w shows the SQL before it runs · u undoes it."))
}

// dbPosValid reports whether pos is still a cell of t (the changes may
// have been reverted while a prompt or a file read was open).
func dbPosValid(t *dbTab, pos dbEditPos) bool {
	if pos.col < 0 || pos.col >= len(t.run.result.Columns) {
		return false
	}
	if pos.added >= 0 {
		return t.edits != nil && pos.added < len(t.edits.added)
	}
	return pos.orig >= 0 && pos.orig < len(t.run.result.Rows)
}
