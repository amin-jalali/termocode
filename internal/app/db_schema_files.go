package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// DBX-46: F on a schema (or a row under it) works with the schema as
// files: dump it to a folder (one file per object, see
// dbclient.SchemaObjectFiles), or compare the live schema with a folder.
// The compare lists changed, database-only, folder-only and identical
// objects; Space selects rows and Enter asks (Cancel first) to update the
// folder from the database, or opens the folder's SQL of the selected
// rows' migration script (ALTER/CREATE/DROP that makes the database match
// the folder, dbclient.SchemaFolderMigration) in a read-only buffer to
// review and run by hand. "Use a folder for completion" reads a DDL folder
// (ours or hand-written) as one more completion source. The schema is
// read on a read-only session; nothing here runs SQL on the server.

const (
	pickerKindDBSchemaFiles   pickerKindEnum  = 1470
	pickerKindDBSchemaCompare pickerKindEnum  = 1471
	promptKindDBSchemaFolder  promptKindEnum  = 1470
	confirmKindDBSchemaFiles  confirmKindEnum = 1470
)

// dbSchemaFilesState is the F flow in progress.
type dbSchemaFilesState struct {
	gen      uint64
	side     dbCompareSide
	action   string // "dump" or "compare"
	dir      string // the absolute folder
	files    []dbclient.CanonFile
	diffs    []dbclient.SchemaFileDiff
	selected map[string]bool
	apply    []dbclient.SchemaFileDiff // the rows the open card is about
}

type dbSchemaFilesMsg struct {
	gen    uint64
	schema string
	files  []dbclient.CanonFile
	folder map[string]string
	ddl    *dbclient.CompletionSchema // action "complete": the folder as a completion source
	warns  []string                   // statements of the folder the parser could not read
	tables int                        // action "complete": tables read from the folder
	err    error
}

// dbSchemaFolderPath is folder (typed) as an absolute path: a relative
// one is under the workspace root.
func dbSchemaFolderPath(root, folder string) string {
	folder = strings.TrimSpace(folder)
	if filepath.IsAbs(folder) {
		return filepath.Clean(folder)
	}
	return filepath.Join(root, folder)
}

// dbSchemaFolderDefault is the folder offered first: schema/<conn>-<schema>.
func dbSchemaFolderDefault(side dbCompareSide) string {
	name := side.conn
	if side.schema != "" {
		name += "-" + side.schema
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return "schema/" + strings.TrimLeft(b.String(), ".")
}

// databaseSchemaFilesStart is F in the tree.
func (m *Model) databaseSchemaFilesStart() tea.Cmd {
	s := m.databases
	row, ok := m.databaseCurrentRow()
	if s == nil || !ok || row.conn == "" || row.kind == dbNodeSuggestion {
		return m.pushToastDetail(toast.Info, "Select a schema", "Move to a schema (or a database) in the tree first.")
	}
	c, ok := s.connection(row.conn)
	if !ok || dbclient.DialectFor(c.Driver) == nil {
		return m.pushToastDetail(toast.Info, "Not a SQL database", "Schema files work for PostgreSQL, MySQL and SQLite.")
	}
	key := s.sessionKeyFor(row.path, row.conn)
	if s.sessions[key] == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	side := dbCompareSide{conn: row.conn, name: dbConnName(c), sessKey: key, driver: dbDriverFamily(c.Driver), database: dbAncestorDatabase(row.path), schema: dbAncestorSchema(row.path)}
	if side.schema == "" && side.driver == "mysql" {
		side.schema = side.database
	}
	if side.driver != "postgres" {
		side.database = ""
	}
	s.schemaFiles = &dbSchemaFilesState{side: side}
	title := "Schema files"
	if side.schema != "" {
		title += " for " + side.label()
	}
	m.picker = picker.NewItems(title, []picker.Item{
		{ID: "dump", Title: "Dump to a folder…", Hint: "one .sql file per table, view and routine"},
		{ID: "compare", Title: "Compare with a folder…", Hint: "changed, only on one side, identical"},
		{ID: "complete", Title: "Use a folder for completion…", Hint: "SQL completion also offers the folder's tables and columns"},
	})
	if tool := dbclient.DetectMigrationTool(s.root); tool != "" {
		m.picker.SetNote([]string{"This project uses " + tool + ": a schema folder is a snapshot, not a migration."})
	}
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBSchemaFiles
	m.pickerOpen = true
	return nil
}

// databaseSchemaFilesPicker handles both pickers of the flow.
func (m *Model) databaseSchemaFilesPicker(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBSchemaFiles && m.pickerKind != pickerKindDBSchemaCompare {
		return nil, false
	}
	st := m.databases.schemaFiles
	if st == nil {
		return nil, true
	}
	if m.pickerKind == pickerKindDBSchemaCompare {
		return m.databaseSchemaCompareChosen(id), true
	}
	st.action = id
	title := "Dump the schema to a folder"
	switch id {
	case "compare":
		title = "Compare the schema with a folder"
	case "complete":
		title = "Read a DDL folder for completion"
	}
	return m.openHTTPPrompt(promptKindDBSchemaFolder, title, "Folder (relative to the workspace):", dbSchemaFolderDefault(st.side)), true
}

// databaseSchemaFolderEntered is the promptKindDBSchemaFolder submit: the
// schema (and, to compare, the folder) is read off the UI goroutine.
func (m *Model) databaseSchemaFolderEntered(value string) tea.Cmd {
	s := m.databases
	if s == nil || s.schemaFiles == nil || strings.TrimSpace(value) == "" {
		return nil
	}
	st := s.schemaFiles
	if st.action == "complete" {
		return m.databaseSchemaFolderForCompletion(value)
	}
	sess := s.sessions[st.side.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	st.dir = dbSchemaFolderPath(s.root, value)
	st.gen++
	gen, side, dir, compare := st.gen, st.side, st.dir, st.action == "compare"
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.PushKeyed(dbCompareToastKey, toast.Info, "Reading "+side.label()+"…", "Read-only.")
	return tea.Batch(toastCmd, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbCompareTimeout)
		defer cancel()
		msg := dbSchemaFilesMsg{gen: gen, schema: side.schema}
		ro, err := sess.ReadOnlySession()
		if err != nil {
			msg.err = err
			return msg
		}
		defer ro.Close()
		if msg.schema == "" {
			_, schemas, err := dbclient.ListSchemas(ctx, ro, side.driver)
			if err != nil {
				msg.err = err
				return msg
			}
			if len(schemas) == 0 {
				msg.err = fmt.Errorf("the database lists no schema")
				return msg
			}
			msg.schema = schemas[0]
		}
		canon, err := dbclient.ReadCanonSchema(ctx, ro, side.driver, msg.schema)
		if err != nil {
			msg.err = err
			return msg
		}
		if canon.Truncated {
			msg.err = fmt.Errorf("the schema is too large to write as files")
			return msg
		}
		msg.files = dbclient.SchemaObjectFiles(canon)
		if compare {
			msg.folder, msg.err = dbclient.ReadSchemaFolder(dir)
		}
		return msg
	})
}

// databaseSchemaFolderForCompletion reads and parses the folder off the
// UI goroutine; no session is needed, the folder is plain files.
func (m *Model) databaseSchemaFolderForCompletion(value string) tea.Cmd {
	s := m.databases
	st := s.schemaFiles
	st.dir = dbSchemaFolderPath(s.root, value)
	st.gen++
	gen, side, dir := st.gen, st.side, st.dir
	return func() tea.Msg {
		msg := dbSchemaFilesMsg{gen: gen, schema: side.schema}
		folder, err := dbclient.ReadSchemaFolder(dir)
		if err != nil {
			msg.err = err
			return msg
		}
		if len(folder) == 0 {
			msg.err = fmt.Errorf("no tables/, views/ or routines/ .sql files in %s", dir)
			return msg
		}
		canon, warns := dbclient.ParseSchemaFolder(side.driver, side.schema, folder)
		ddl := dbclient.CanonCompletion(canon)
		msg.ddl, msg.warns, msg.tables = &ddl, warns, len(canon.Tables)
		return msg
	}
}

func (m *Model) onDBSchemaFiles(msg dbSchemaFilesMsg) tea.Cmd {
	if m.databases == nil || m.databases.schemaFiles == nil || m.databases.schemaFiles.gen != msg.gen {
		return nil
	}
	m.toast = m.toast.Dismiss(dbCompareToastKey)
	st := m.databases.schemaFiles
	if msg.err != nil {
		return m.dbConsoleError(msg.err)
	}
	if st.action == "complete" {
		m.databases.setDDLSchema(st.side.conn, *msg.ddl)
		m.databaseSchemaChanged()
		detail := "SQL files of " + sanitizeDBText(st.side.name) + " now also complete these names. Live names win."
		if len(msg.warns) > 0 {
			detail = fmt.Sprintf("%s could not be read (first: %s). %s", dbPlural(len(msg.warns), "statement"), sanitizeDBText(msg.warns[0]), detail)
		}
		return m.pushToastDetail(toast.Info, fmt.Sprintf("Completion reads %s from %s", dbPlural(msg.tables, "table"), m.dbShortPath(st.dir)), detail)
	}
	st.side.schema = msg.schema
	st.files = msg.files
	if st.action == "dump" {
		folder, err := dbclient.ReadSchemaFolder(st.dir)
		if err != nil {
			return m.dbConsoleError(err)
		}
		changed := 0
		for _, d := range dbclient.CompareSchemaFolder(st.files, folder) {
			if d.State == dbclient.SchemaFileChanged {
				changed++
			}
		}
		if changed == 0 {
			return m.databaseSchemaDump()
		}
		st.apply = nil
		m.confirm = confirm.New("Overwrite "+dbPlural(changed, "file")+"?",
			fmt.Sprintf("%s already holds %s with other text. Write the database's version over them?", m.dbShortPath(st.dir), dbPlural(changed, "object file")),
			[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "dump", Title: "Overwrite", Style: confirm.StylePrimary}})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindDBSchemaFiles
		return nil
	}
	st.diffs = dbclient.CompareSchemaFolder(st.files, msg.folder)
	st.selected = map[string]bool{}
	return m.databaseSchemaCompareOpen("")
}

// dbShortPath is dir relative to the workspace when it is inside it.
func (m *Model) dbShortPath(dir string) string {
	if m.databases != nil {
		if rel, err := filepath.Rel(m.databases.root, dir); err == nil && !strings.HasPrefix(rel, "..") {
			return sanitizeDBText(filepath.ToSlash(rel))
		}
	}
	return sanitizeDBText(dir)
}

func (m *Model) databaseSchemaDump() tea.Cmd {
	st := m.databases.schemaFiles
	n, err := dbclient.WriteSchemaFolder(st.dir, st.files)
	if err != nil {
		return m.dbConsoleError(err)
	}
	return m.pushToastDetail(toast.Info, fmt.Sprintf("Wrote %s to %s", dbPlural(n, "file"), m.dbShortPath(st.dir)),
		fmt.Sprintf("%s in the schema; the others were already up to date. Files of dropped objects stay: F → Compare shows them.", dbPlural(len(st.files), "object")))
}

var dbSchemaStateTitles = []string{"Changed", "Only in the database", "Only in the folder", "Identical"}

// dbSchemaCompareItems are the compare rows under one header per state;
// a row that can change shows [x] or [ ].
func dbSchemaCompareItems(diffs []dbclient.SchemaFileDiff, selected map[string]bool) []picker.Item {
	var out []picker.Item
	last := dbclient.SchemaFileState(-1)
	for _, d := range diffs {
		if d.State != last {
			last = d.State
			out = append(out, picker.Item{Title: dbSchemaStateTitles[d.State], Header: true, Group: dbSchemaStateTitles[d.State]})
		}
		title := sanitizeDBText(d.Name)
		if d.State != dbclient.SchemaFileIdentical {
			mark := "[ ] "
			if selected[d.Name] {
				mark = "[x] "
			}
			title = mark + title
		}
		out = append(out, picker.Item{ID: d.Name, Title: title, Group: dbSchemaStateTitles[d.State]})
	}
	return out
}

func (m *Model) databaseSchemaCompareOpen(cursor string) tea.Cmd {
	st := m.databases.schemaFiles
	diffs := 0
	for _, d := range st.diffs {
		if d.State != dbclient.SchemaFileIdentical {
			diffs++
		}
	}
	title := fmt.Sprintf("%s ↔ %s · %s", st.side.label(), m.dbShortPath(st.dir), dbPlural(diffs, "difference"))
	m.picker = picker.NewItems(title, dbSchemaCompareItems(st.diffs, st.selected))
	m.picker.SetRowAction("space", "select")
	m.picker.SetNote([]string{"Space selects · Enter: update the folder, or open the folder's SQL to review"})
	m.picker.SetSize(m.w, m.h)
	if cursor != "" {
		m.picker.SetCursorID(cursor)
	}
	m.pickerKind = pickerKindDBSchemaCompare
	m.pickerOpen = true
	return nil
}

// databaseSchemaCompareToggle is Space in the compare picker.
func (m *Model) databaseSchemaCompareToggle(id string) tea.Cmd {
	st := m.databases.schemaFiles
	if st == nil {
		return nil
	}
	for _, d := range st.diffs {
		if d.Name == id && d.State != dbclient.SchemaFileIdentical {
			st.selected[id] = !st.selected[id]
		}
	}
	m.picker.SetItems(dbSchemaCompareItems(st.diffs, st.selected))
	m.picker.SetCursorID(id)
	return nil
}

// databaseSchemaCompareChosen is Enter: the selected rows (or the row
// under the cursor) go to the Cancel-first card.
func (m *Model) databaseSchemaCompareChosen(id string) tea.Cmd {
	st := m.databases.schemaFiles
	st.apply = nil
	for _, d := range st.diffs {
		if d.State != dbclient.SchemaFileIdentical && st.selected[d.Name] {
			st.apply = append(st.apply, d)
		}
	}
	if len(st.apply) == 0 {
		for _, d := range st.diffs {
			if d.Name == id && d.State != dbclient.SchemaFileIdentical {
				st.apply = append(st.apply, d)
			}
		}
	}
	if len(st.apply) == 0 {
		return m.pushToastDetail(toast.Info, "Nothing to change", "This object is the same in the database and the folder.")
	}
	write, remove := 0, 0
	for _, d := range st.apply {
		if d.State == dbclient.SchemaFileFolderOnly {
			remove++
		} else {
			write++
		}
	}
	msg := fmt.Sprintf("Update the folder from the database: write %s and remove %s in %s. Or open a migration script (ALTER, CREATE, DROP) that makes the database match the folder, in a read-only buffer, to review and run by hand.",
		dbPlural(write, "file"), dbPlural(remove, "file"), m.dbShortPath(st.dir))
	buttons := []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "to-folder", Title: "Update the folder", Style: confirm.StylePrimary},
		{ID: "from-folder", Title: "Open the migration script"}}
	m.confirm = confirm.New(dbPlural(len(st.apply), "object")+" selected", msg, buttons)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBSchemaFiles
	return nil
}

// databaseSchemaFilesConfirm runs a card button.
func (m *Model) databaseSchemaFilesConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindDBSchemaFiles {
		return nil, false
	}
	st := m.databases.schemaFiles
	if st == nil {
		return nil, true
	}
	switch id {
	case "dump":
		return m.databaseSchemaDump(), true
	case "to-folder":
		wrote, removed, err := dbclient.ApplySchemaFolder(st.dir, st.apply)
		if err != nil {
			return m.dbConsoleError(err), true
		}
		return m.pushToastDetail(toast.Info, "Updated "+m.dbShortPath(st.dir),
			fmt.Sprintf("Wrote %s and removed %s.", dbPlural(wrote, "file"), dbPlural(removed, "file"))), true
	case "from-folder":
		var b strings.Builder
		b.WriteString("-- Folder " + m.dbShortPath(st.dir) + " -> database " + sanitizeDBText(st.side.label()) + ". Review before you run it.\n")
		script := dbclient.SchemaFolderMigration(st.side.driver, st.side.schema, m.dbShortPath(st.dir), st.apply)
		for _, line := range strings.Split(strings.TrimRight(script, "\n"), "\n") {
			b.WriteString(sanitizeOutputLine(line) + "\n")
		}
		if m.nvim == nil {
			return m.copyWithToast(b.String(), "No editor: the migration script is copied instead"), true
		}
		m.ensureDBLua()
		m.ensureEditorWindowCurrent()
		_ = m.nvim.ExecLuaArgs(`_G._termocode_db_ddl(...)`, "schema-folder-migration.sql", strings.Split(strings.TrimRight(b.String(), "\n"), "\n"))
		m.focus = FocusEditor
		return m.fetchStateCmd(), true
	}
	return nil, true
}
