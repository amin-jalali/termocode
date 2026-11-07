package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// SQL files (DBX-41): Databases: Go to Statement… lists the statements of
// the .sql buffer (the outline) in the file's dialect, and Databases:
// Switch File Connection… picks the connection a .sql file runs on. The
// choice is saved in .termocode/sql-connections.json (paths relative to
// the workspace), so it survives a restart; "-- @conn id" in the file still
// wins.

const (
	pickerKindDBSQLOutline pickerKindEnum = 5383
	pickerKindDBSQLSwitch  pickerKindEnum = 5384
)

const dbSQLDetach = "\x00detach"

// dbSQLFileDialect is the dialect of a SQL file: the attached connection's
// driver, else "-- @dialect" or the hints in the text. how says which.
func dbSQLFileDialect(src, driver string) (dialect, how string) {
	return dbSQLDialectFor(src, driver, "") // db_sqllang_more.go
}

// dbSQLOutlineItems are the picker rows of the outline: ID is the line.
func dbSQLOutlineItems(src, dialect string) []picker.Item {
	var items []picker.Item
	for _, it := range dbclient.Outline(src, dialect) {
		items = append(items, picker.Item{ID: strconv.Itoa(it.Line), Title: sanitizeDBText(it.Label), Hint: fmt.Sprintf("line %d · %s", it.Line, it.Kind)})
	}
	return items
}

// databaseSQLBuffer is the .sql buffer in the editor with its text.
func (m *Model) databaseSQLBuffer() (string, string, tea.Cmd, bool) {
	file, lines, ok := m.httpBufText(0)
	if !ok || !strings.EqualFold(lastExt(file), ".sql") {
		return "", "", m.pushToastDetail(toast.Info, "Open a .sql file", "This works on the SQL file in the editor."), false
	}
	return file, strings.Join(lines, "\n"), nil, true
}

func (m *Model) databaseSQLOutline() tea.Cmd {
	m.ensureDatabasesLoaded()
	file, src, cmd, ok := m.databaseSQLBuffer()
	if !ok {
		return cmd
	}
	driver := ""
	if c, ok := m.databases.connection(m.dbConsole().consoleConn(file, strings.Split(src, "\n"))); ok {
		driver = c.Driver
	}
	dialect, how := dbSQLDialectFor(src, driver, dbSQLFolderDialect(m.databaseWorkspaceRoot(), file))
	items := dbSQLOutlineItems(src, dialect)
	if len(items) == 0 {
		return m.pushToastDetail(toast.Info, "No statements", "The file is empty or a quote is not closed.")
	}
	title := "Statements"
	if dialect != "" {
		title += " · " + dialect + " (" + how + ")"
	}
	m.picker = picker.NewItems(title, items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBSQLOutline
	m.pickerOpen = true
	return nil
}

// databaseSQLSwitchPicker lists the SQL connections for the .sql buffer.
func (m *Model) databaseSQLSwitchPicker() tea.Cmd {
	m.ensureDatabasesLoaded()
	file, src, cmd, ok := m.databaseSQLBuffer()
	if !ok {
		return cmd
	}
	cur := m.dbConsole().consoleConn(file, strings.Split(src, "\n"))
	var items []picker.Item
	for _, c := range m.databases.conns {
		if dbclient.DialectFor(c.Driver) == nil {
			continue
		}
		hint := sanitizeDBText(c.Driver)
		if c.ID == cur {
			hint = "current · " + hint
		}
		items = append(items, picker.Item{ID: c.ID, Title: sanitizeDBText(dbConnName(c)), Hint: hint})
	}
	if len(items) == 0 {
		return m.pushToastDetail(toast.Info, "No SQL connections", "Databases: Add Connection… first.")
	}
	if m.dbConsole().attached[file] != "" {
		items = append(items, picker.Item{ID: dbSQLDetach, Title: "Detach (ask on the next run)"})
	}
	m.dbConsole().switchFile = file
	m.picker = picker.NewItems("Connection for "+sanitizeDBText(filepath.Base(file)), items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBSQLSwitch
	m.pickerOpen = true
	return nil
}

// databaseSQLPicked handles both pickers. ok=false: not ours.
func (m *Model) databaseSQLPicked(id string) (tea.Cmd, bool) {
	switch m.pickerKind {
	case pickerKindDBSQLOutline:
		if n, err := strconv.Atoi(id); err == nil && n > 0 && m.nvim != nil {
			_ = m.nvim.Command(fmt.Sprintf("call cursor(%d, 1)", n))
			m.focus = FocusEditor
		}
		return nil, true
	case pickerKindDBSQLSwitch:
		s := m.dbConsole()
		file := s.switchFile
		s.switchFile = ""
		if file == "" {
			return nil, true
		}
		if id == dbSQLDetach {
			id = ""
			delete(s.attached, file)
		} else {
			s.attached[file] = id
		}
		m.databaseRefreshLens(file, "", "")
		if err := dbSaveSQLAttach(m.databaseWorkspaceRoot(), file, id); err != nil {
			return m.pushToastDetail(toast.Warn, "Connection set for this session only", sanitizeDBText(err.Error())), true
		}
		if id == "" {
			return m.pushToast(toast.Info, "Detached "+sanitizeDBText(filepath.Base(file))), true
		}
		return m.pushToastDetail(toast.Info, sanitizeDBText(filepath.Base(file))+" runs on "+m.dbConnLabel(id), "Saved for this project."), true
	}
	return nil, false
}

func dbSQLAttachPath(root string) string {
	return filepath.Join(root, ".termocode", "sql-connections.json")
}

// dbLoadSQLAttach reads the saved file → connection map (absolute paths).
func dbLoadSQLAttach(root string) map[string]string {
	out := map[string]string{}
	if root == "" {
		return out
	}
	data, err := os.ReadFile(dbSQLAttachPath(root))
	if err != nil {
		return out
	}
	var saved map[string]string
	if json.Unmarshal(data, &saved) != nil {
		return out
	}
	for rel, id := range saved {
		if id == "" || filepath.IsAbs(rel) || strings.HasPrefix(filepath.Clean(rel), "..") {
			continue
		}
		out[filepath.Join(root, filepath.FromSlash(rel))] = id
	}
	return out
}

// dbSaveSQLAttach sets (id != "") or removes the saved connection of file.
func dbSaveSQLAttach(root, file, id string) error {
	if root == "" {
		return fmt.Errorf("no workspace folder")
	}
	rel, err := filepath.Rel(root, file)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("the file is outside the workspace")
	}
	saved := map[string]string{}
	for abs, v := range dbLoadSQLAttach(root) {
		if r, err := filepath.Rel(root, abs); err == nil {
			saved[filepath.ToSlash(r)] = v
		}
	}
	if id == "" {
		delete(saved, filepath.ToSlash(rel))
	} else {
		saved[filepath.ToSlash(rel)] = id
	}
	data, err := json.MarshalIndent(saved, "", "  ") // keys come out sorted
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dbSQLAttachPath(root)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dbSQLAttachPath(root), append(data, '\n'), 0o644)
}

// mergeSQLAttach adds the saved attachments of root to the console (a
// choice made in this session wins).
func (s *dbConsoleState) mergeSQLAttach(root string) {
	for file, id := range dbLoadSQLAttach(root) {
		if _, ok := s.attached[file]; !ok {
			s.attached[file] = id
		}
	}
}
