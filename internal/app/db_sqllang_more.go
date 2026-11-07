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

// SQL files, part 2 (DBX-41):
//   - Databases: Set Folder SQL Dialect… gives a folder (and the folders
//     below it) a dialect, saved in .termocode/sql-dialects.json. The
//     order is: the file's connection, a "-- @dialect" line, the folder,
//     the hints in the text.
//   - Databases: Highlight Usages in File marks where the name under the
//     cursor is used. An alias or a CTE name resolves in its own statement
//     only; a table name in the whole file.
//   - Databases: Go to SQL in Strings… lists the SQL written inside the
//     strings of a Go, Python or JavaScript/TypeScript file.
//   - db_sql_syntax_off (config.json) lists dialects whose syntax errors
//     the live inspections do not show.

const (
	pickerKindDBSQLDialect  pickerKindEnum = 5385
	pickerKindDBSQLEmbedded pickerKindEnum = 5386
)

const dbSQLDialectClear = "clear" // never a dialect name

// dbSQLDialectFor is the dialect of a SQL file: the connection's driver,
// a @dialect line, the folder's dialect, the hints in the text. how says
// which ("connection", "detected", "folder" or "generic").
func dbSQLDialectFor(src, driver, folder string) (dialect, how string) {
	if d, ok := dbclient.DialectByName(driver); ok {
		return string(d), "connection"
	}
	if d, ok := dbclient.DialectLine(src); ok {
		return string(d), "detected"
	}
	if d, ok := dbclient.DialectByName(folder); ok {
		return string(d), "folder"
	}
	if d, ok := dbclient.DetectDialect(src); ok {
		return string(d), "detected"
	}
	return "", "generic"
}

func dbSQLFolderDialectsPath(root string) string {
	return filepath.Join(root, ".termocode", "sql-dialects.json")
}

// dbLoadSQLFolderDialects reads the saved folder → dialect map (keys are
// slash paths relative to the workspace, "." for the workspace itself).
func dbLoadSQLFolderDialects(root string) map[string]string {
	out := map[string]string{}
	if root == "" {
		return out
	}
	data, err := os.ReadFile(dbSQLFolderDialectsPath(root))
	if err != nil {
		return out
	}
	var saved map[string]string
	if json.Unmarshal(data, &saved) != nil {
		return out
	}
	for rel, d := range saved {
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
		if _, ok := dbclient.DialectByName(d); !ok || filepath.IsAbs(rel) || clean == ".." || strings.HasPrefix(clean, "../") {
			continue
		}
		out[clean] = d
	}
	return out
}

// dbSQLFolderDialect is the dialect saved for the nearest folder above
// file inside root ("" = none).
func dbSQLFolderDialect(root, file string) string {
	if root == "" || file == "" {
		return ""
	}
	rel, err := filepath.Rel(root, filepath.Dir(file))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	saved := dbLoadSQLFolderDialects(root)
	if len(saved) == 0 {
		return ""
	}
	for dir := filepath.ToSlash(rel); ; {
		if d := saved[dir]; d != "" {
			return d
		}
		if dir == "." {
			return ""
		}
		dir = filepath.ToSlash(filepath.Dir(filepath.FromSlash(dir)))
	}
}

// dbSaveSQLFolderDialect sets (dialect != "") or removes the dialect of
// folder.
func dbSaveSQLFolderDialect(root, folder, dialect string) error {
	if root == "" {
		return fmt.Errorf("no workspace folder")
	}
	rel, err := filepath.Rel(root, folder)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("the folder is outside the workspace")
	}
	saved := dbLoadSQLFolderDialects(root)
	key := filepath.ToSlash(rel)
	if dialect == "" {
		delete(saved, key)
	} else {
		d, ok := dbclient.DialectByName(dialect)
		if !ok {
			return fmt.Errorf("unknown SQL dialect %q", dialect)
		}
		saved[key] = string(d)
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dbSQLFolderDialectsPath(root)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dbSQLFolderDialectsPath(root), append(data, '\n'), 0o644)
}

var dbSQLDialectChoices = []struct{ id, title string }{
	{"postgres", "PostgreSQL"},
	{"mysql", "MySQL / MariaDB"},
	{"sqlite", "SQLite"},
}

// databaseSQLFolderDialectPicker asks the dialect of the folder of the
// .sql buffer. The item ID carries the folder.
func (m *Model) databaseSQLFolderDialectPicker() tea.Cmd {
	file, _, cmd, ok := m.databaseSQLBuffer()
	if !ok {
		return cmd
	}
	root := m.databaseWorkspaceRoot()
	folder := filepath.Dir(file)
	rel, err := filepath.Rel(root, folder)
	if root == "" || err != nil || strings.HasPrefix(rel, "..") {
		return m.pushToastDetail(toast.Info, "Not in the workspace", "A folder dialect is saved for folders of the open workspace.")
	}
	cur := dbLoadSQLFolderDialects(root)[filepath.ToSlash(rel)]
	var items []picker.Item
	for _, c := range dbSQLDialectChoices {
		hint := ""
		if c.id == cur {
			hint = "current"
		}
		items = append(items, picker.Item{ID: c.id + "\x00" + folder, Title: c.title, Hint: hint})
	}
	if cur != "" {
		items = append(items, picker.Item{ID: dbSQLDialectClear + "\x00" + folder, Title: "Clear (use the connection or the text)"})
	}
	m.picker = picker.NewItems("SQL dialect for "+sanitizeDBText(filepath.ToSlash(rel))+"/", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBSQLDialect
	m.pickerOpen = true
	return nil
}

// databaseSQLMorePicked handles the folder dialect and SQL in strings
// pickers. ok=false: not ours.
func (m *Model) databaseSQLMorePicked(id string) (tea.Cmd, bool) {
	switch m.pickerKind {
	case pickerKindDBSQLDialect:
		dialect, folder, ok := strings.Cut(id, "\x00")
		if !ok || folder == "" {
			return nil, true
		}
		if dialect == dbSQLDialectClear {
			dialect = ""
		}
		root := m.databaseWorkspaceRoot()
		if err := dbSaveSQLFolderDialect(root, folder, dialect); err != nil {
			return m.pushToastDetail(toast.Warn, "Dialect not saved", sanitizeDBText(err.Error())), true
		}
		name := filepath.Base(folder) + "/"
		if dialect == "" {
			return m.pushToastDetail(toast.Info, "Cleared the dialect of "+sanitizeDBText(name), "Its SQL files use their connection or the text again."), true
		}
		return m.pushToastDetail(toast.Info, sanitizeDBText(name)+" uses "+dialect, "Saved in .termocode/sql-dialects.json; a connection or a -- @dialect line still wins."), true
	case pickerKindDBSQLEmbedded:
		if n, err := strconv.Atoi(id); err == nil && n > 0 && m.nvim != nil {
			_ = m.nvim.Command(fmt.Sprintf("call cursor(%d, 1)", n))
			m.focus = FocusEditor
		}
		return nil, true
	}
	return nil, false
}

// dbSQLUsageSpans are the spans of name in text for the cursor at offset,
// with a note on the scope: an alias or CTE name of the statement around
// the cursor resolves in that statement only, any other name in the file.
func dbSQLUsageSpans(text, driver, name string, offset int) ([]dbclient.Span, string) {
	for _, sp := range dbStatementSpans(text, driver) {
		if offset < sp.start || offset > sp.end {
			continue
		}
		stmt := text[sp.start:sp.end]
		local := ""
		for _, c := range dbclient.CTENames(stmt) {
			if strings.EqualFold(c, name) {
				local = "CTE of this statement"
			}
		}
		if local == "" {
			if t := dbStatementAliases(stmt)[strings.ToLower(name)]; t != "" && !strings.EqualFold(t, name) {
				local = "alias of " + t + " in this statement"
			}
		}
		if local == "" {
			break
		}
		spans := dbclient.NameUsages(stmt, name)
		for i := range spans {
			spans[i].Start += sp.start
			spans[i].End += sp.start
		}
		return spans, local
	}
	return dbclient.NameUsages(text, name), "in this file"
}

// dbSQLUsagesLua marks the usages in the current buffer; they clear on
// the next change or when the buffer is left.
const dbSQLUsagesLua = `local marks=...
local ns=vim.api.nvim_create_namespace('termocode_sql_usages')
pcall(vim.api.nvim_set_hl,0,'TermocodeSQLUsage',{link='Visual',default=true})
local buf=vim.api.nvim_get_current_buf()
vim.api.nvim_buf_clear_namespace(buf,ns,0,-1)
for _,u in ipairs(marks) do
  pcall(vim.api.nvim_buf_set_extmark,buf,ns,u.Row,u.Col,{end_col=u.End,hl_group='TermocodeSQLUsage'})
end
vim.api.nvim_create_autocmd({'TextChanged','TextChangedI','BufLeave'},{buffer=buf,once=true,callback=function() vim.api.nvim_buf_clear_namespace(buf,ns,0,-1) end})
return #marks`

type dbUsageMark struct{ Row, Col, End int }

// databaseHighlightUsages marks the usages of the name under the cursor.
func (m *Model) databaseHighlightUsages() tea.Cmd {
	file, lines, _, name, cmd := m.databaseNameAtCursor()
	if name == "" {
		return cmd
	}
	m.ensureDatabasesLoaded()
	text := strings.Join(lines, "\n")
	driver := ""
	if c, ok := m.databases.connection(m.dbConsole().consoleConn(file, lines)); ok {
		driver = c.Driver
	}
	if driver == "" {
		driver, _ = dbSQLDialectFor(text, "", dbSQLFolderDialect(m.databaseWorkspaceRoot(), file))
	}
	spans, note := dbSQLUsageSpans(text, driver, name, m.dbCursorOffset(lines))
	var marks []dbUsageMark
	for _, s := range spans {
		row, col := dbLineCol(text, s.Start)
		erow, ecol := dbLineCol(text, s.End)
		if row == erow {
			marks = append(marks, dbUsageMark{Row: row, Col: col, End: ecol})
		}
	}
	if len(marks) == 0 {
		return m.pushToastDetail(toast.Info, "No usages", sanitizeDBText(name)+" is not used as a name here.")
	}
	var n int
	_ = m.nvim.ExecLuaResult(dbSQLUsagesLua, &n, marks)
	return m.pushToastDetail(toast.Info, fmt.Sprintf("%d usages of %s", len(marks), sanitizeDBText(name)), sanitizeDBText(note)+". They clear when you edit the file.")
}

// dbCursorOffset is the byte offset of the editor cursor in lines.
func (m *Model) dbCursorOffset(lines []string) int {
	var pos struct {
		Row int `json:"row"`
		Col int `json:"col"`
	}
	var raw string
	_ = m.nvim.ExecLuaResult(`local c=vim.api.nvim_win_get_cursor(0);return vim.fn.json_encode({row=c[1],col=c[2]})`, &raw)
	_ = json.Unmarshal([]byte(raw), &pos)
	off := 0
	for i := 0; i < pos.Row-1 && i < len(lines); i++ {
		off += len(lines[i]) + 1
	}
	return off + pos.Col
}

const dbEmbeddedLabelMax = 60

// dbEmbeddedSQLItems are the picker rows of SQL in strings: ID is the line.
func dbEmbeddedSQLItems(found []dbclient.EmbeddedSQL) []picker.Item {
	items := make([]picker.Item, 0, len(found))
	for _, e := range found {
		label := strings.Join(strings.Fields(e.Text), " ")
		if r := []rune(label); len(r) > dbEmbeddedLabelMax {
			label = string(r[:dbEmbeddedLabelMax-1]) + "…"
		}
		items = append(items, picker.Item{ID: strconv.Itoa(e.Line), Title: sanitizeDBText(label), Hint: fmt.Sprintf("line %d · %s", e.Line, e.Kind)})
	}
	return items
}

// databaseEmbeddedSQL lists the SQL in the strings of the code file in
// the editor.
func (m *Model) databaseEmbeddedSQL() tea.Cmd {
	file, lines, ok := m.httpBufText(0)
	lang := dbclient.EmbedLang(file)
	if !ok || lang == "" {
		return m.pushToastDetail(toast.Info, "Open a code file", "This lists the SQL in the strings of a Go, Python or JavaScript/TypeScript file.")
	}
	found := dbclient.FindEmbeddedSQL(strings.Join(lines, "\n"), lang)
	if len(found) == 0 {
		return m.pushToastDetail(toast.Info, "No SQL in strings", "No string in "+sanitizeDBText(filepath.Base(file))+" reads as a SQL statement.")
	}
	m.picker = picker.NewItems(fmt.Sprintf("SQL in strings · %s (%d)", sanitizeDBText(filepath.Base(file)), len(found)), dbEmbeddedSQLItems(found))
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBSQLEmbedded
	m.pickerOpen = true
	return nil
}

// syntaxOff says whether db_sql_syntax_off lists dialect (names such as
// "postgresql" or "mariadb" count for their dialect).
func (s dbConsoleSettings) syntaxOff(dialect string) bool {
	want := strings.ToLower(dialect)
	if d, ok := dbclient.DialectByName(dialect); ok {
		want = string(d)
	}
	for _, e := range s.SyntaxOff {
		got := strings.ToLower(strings.TrimSpace(e))
		if d, ok := dbclient.DialectByName(e); ok {
			got = string(d)
		}
		if got != "" && got == want {
			return true
		}
	}
	return false
}

// dbDropSyntaxDiags leaves out the syntax errors (the error severity);
// the other inspections stay. Never nil, so the buffer still clears.
func dbDropSyntaxDiags(diags []map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, d := range diags {
		if sev, _ := d["severity"].(int); sev == dbDiagError {
			continue
		}
		out = append(out, d)
	}
	return out
}
