package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Databases: Export ER Diagram… (DBX-47): the schema under the tree
// cursor, read on a read-only session, written as Mermaid, PlantUML or
// DOT text into a scratch buffer (save it or copy it from there).

const pickerKindDBERFormat pickerKindEnum = 3847

// dbERTarget is the schema the diagram is of.
type dbERTarget struct {
	sessKey, driver, schema, label string
}

type dbERReadyMsg struct {
	title, text string
	err         error
}

// dbERSchemaOf is the schema of the tree node at path: the nearest schema
// node (a MySQL database is its schema); SQLite is always "main". "" when
// the node is not in a schema.
func dbERSchemaOf(nodes map[string]dbNode, path, driver string) string {
	if dbDriverFamily(driver) == "sqlite" {
		return "main"
	}
	for p := path; p != ""; {
		if n, ok := nodes[p]; ok {
			if n.kind == dbNodeSchema || dbDriverFamily(driver) == "mysql" && n.kind == dbNodeDatabase {
				return n.name
			}
		}
		i := strings.LastIndex(p, "/")
		if i < 0 {
			break
		}
		p = p[:i]
	}
	return ""
}

// databaseERStart finds the schema under the tree cursor and asks for the
// format.
func (m *Model) databaseERStart() tea.Cmd {
	s := m.databases
	r, ok := m.databaseCurrentRow()
	if s == nil || !ok {
		return tea.Batch(m.revealDatabasesView(false), m.pushToastDetail(toast.Info, "Select a schema first",
			"Put the cursor on a schema (or a table in it) in the Databases view."))
	}
	c, ok := s.connection(r.conn)
	if !ok || dbclient.DialectFor(c.Driver) == nil {
		return m.pushToastDetail(toast.Info, "ER diagrams need a SQL database", "")
	}
	schema := dbERSchemaOf(s.nodes, r.path, c.Driver)
	if schema == "" {
		return m.pushToastDetail(toast.Info, "Select a schema first",
			"Put the cursor on a schema (or a table in it) in the Databases view.")
	}
	key := s.sessionKeyFor(r.path, r.conn)
	if s.sessions[key] == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Press Enter on the connection in the Databases view first.")
	}
	s.er = dbERTarget{sessKey: key, driver: dbDriverFamily(c.Driver), schema: schema, label: sanitizeDBText(dbConnName(c) + " · " + schema)}
	items := []picker.Item{
		{ID: "mermaid", Title: "Mermaid", Hint: "erDiagram"},
		{ID: "plantuml", Title: "PlantUML", Hint: "@startuml"},
		{ID: "dot", Title: "Graphviz DOT", Hint: "digraph"},
		{ID: "mermaid+color", Title: "Mermaid · coloured", Hint: "tables coloured by name prefix"},
		{ID: "plantuml+color", Title: "PlantUML · coloured", Hint: "tables coloured by name prefix"},
		{ID: "dot+color", Title: "Graphviz DOT · coloured", Hint: "tables coloured by name prefix"},
	}
	m.picker = picker.NewItems(" ER diagram of "+s.er.label+" ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindDBERFormat
	return nil
}

// databaseERPicker reads the schema and writes the diagram.
func (m *Model) databaseERPicker(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBERFormat {
		return nil, false
	}
	s := m.databases
	if s == nil {
		return nil, true
	}
	t := s.er
	sess := s.sessions[t.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Warn, "Not connected any more", ""), true
	}
	format, color := dbERFormatID(id)
	title := "ER Diagram: " + t.label + " (" + format + ")"
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbCompareTimeout)
		defer cancel()
		ro, err := sess.ReadOnlySession()
		if err != nil {
			return dbERReadyMsg{title: title, err: err}
		}
		defer ro.Close()
		cs, err := dbclient.ReadCanonSchema(ctx, ro, t.driver, t.schema)
		if err != nil {
			return dbERReadyMsg{title: title, err: err}
		}
		text, err := dbclient.ERDiagramStyled(cs, format, dbclient.ERStyle{Colors: color})
		return dbERReadyMsg{title: title, text: text, err: err}
	}, true
}

// dbERFormatID splits a format picker id ("dot+color") into the format
// and whether tables are coloured.
func dbERFormatID(id string) (string, bool) {
	f, color := strings.CutSuffix(id, "+color")
	return f, color
}

func (m *Model) onDBERReady(msg dbERReadyMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "ER diagram failed", dbclient.Redact(sanitizeDBText(msg.err.Error())))
	}
	if m.nvim == nil {
		return m.copyWithToast(msg.text, "No editor: ER diagram copied instead")
	}
	m.ensureDBLua()
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLuaArgs(`_G._termocode_db_ddl(...)`, msg.title, strings.Split(strings.TrimRight(msg.text, "\n"), "\n"))
	m.focus = FocusEditor
	return m.fetchStateCmd()
}
