package app

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Databases: Diagram Path Between Tables… (DBX-47): the cursor is on a
// table; the schema is read on a read-only session, a picker asks for the
// other table, and the shortest foreign-key path between the two opens as
// a Mermaid diagram of just those tables (both ends highlighted) in a
// scratch buffer.

const pickerKindDBERPathTo pickerKindEnum = 3913

// dbERPathState is the path being asked for.
type dbERPathState struct {
	target dbERTarget
	from   string
	schema dbclient.CanonSchema
}

type dbERPathSchemaMsg struct {
	schema dbclient.CanonSchema
	err    error
}

// dbERPathText is the path diagram (a "%% path:" comment line first) and
// the path as "a → b → c".
func dbERPathText(s dbclient.CanonSchema, from, to string) (string, string, error) {
	path, ok := dbclient.ERPath(s, from, to)
	if !ok {
		return "", "", errors.New("No foreign-key path between " + from + " and " + to)
	}
	text, err := dbclient.ERDiagramStyled(s, "mermaid", dbclient.ERStyle{Only: path, Highlight: []string{from, to}})
	if err != nil {
		return "", "", err
	}
	shown := strings.Join(path, " → ")
	return "%% path: " + sanitizeDBText(shown) + "\n" + text, shown, nil
}

// databaseERPathStart reads the schema of the table under the cursor.
func (m *Model) databaseERPathStart() tea.Cmd {
	s := m.databases
	r, ok := m.databaseCurrentRow()
	var n dbNode
	if ok && s != nil {
		n, ok = s.nodes[r.path]
	}
	if s == nil || !ok || n.kind != dbNodeTable {
		return tea.Batch(m.revealDatabasesView(false), m.pushToastDetail(toast.Info, "Select a table first",
			"Put the cursor on a table in the Databases view; the path starts there."))
	}
	c, ok := s.connection(r.conn)
	if !ok || dbclient.DialectFor(c.Driver) == nil {
		return m.pushToastDetail(toast.Info, "ER diagrams need a SQL database", "")
	}
	schema := dbERSchemaOf(s.nodes, r.path, c.Driver)
	key := s.sessionKeyFor(r.path, r.conn)
	sess := s.sessions[key]
	if schema == "" || sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Press Enter on the connection in the Databases view first.")
	}
	t := dbERTarget{sessKey: key, driver: dbDriverFamily(c.Driver), schema: schema, label: sanitizeDBText(dbConnName(c) + " · " + schema), conn: r.conn}
	s.erPath = &dbERPathState{target: t, from: n.name}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbCompareTimeout)
		defer cancel()
		ro, err := sess.ReadOnlySession()
		if err != nil {
			return dbERPathSchemaMsg{err: err}
		}
		defer ro.Close()
		cs, err := dbclient.ReadCanonSchema(ctx, ro, t.driver, t.schema)
		return dbERPathSchemaMsg{schema: cs, err: err}
	}
}

// onDBERPathSchema asks for the other end of the path.
func (m *Model) onDBERPathSchema(msg dbERPathSchemaMsg) tea.Cmd {
	s := m.databases
	if s == nil || s.erPath == nil {
		return nil
	}
	if msg.err != nil {
		s.erPath = nil
		return m.pushToastDetail(toast.Errr, "Reading the schema failed", dbclient.Redact(sanitizeDBText(msg.err.Error())))
	}
	p := s.erPath
	v, _ := s.dbVirtual(p.target.conn) // a path may follow virtual keys (db_virtual.go)
	msg.schema = v.WithForeignKeys(msg.schema)
	p.schema = msg.schema
	var items []picker.Item
	for _, t := range msg.schema.Tables {
		if t.Name == p.from {
			continue
		}
		hint := ""
		if _, ok := dbclient.ERPath(msg.schema, p.from, t.Name); !ok {
			hint = "no path"
		}
		items = append(items, picker.Item{ID: t.Name, Title: sanitizeDBText(t.Name), Hint: hint})
	}
	if len(items) == 0 {
		s.erPath = nil
		return m.pushToastDetail(toast.Info, "No other tables in "+p.target.label, "")
	}
	m.picker = picker.NewItems(" Path from "+sanitizeDBText(p.from)+" to… ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindDBERPathTo
	return nil
}

// databaseERPathPicker writes the path diagram for the picked table.
func (m *Model) databaseERPathPicker(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBERPathTo {
		return nil, false
	}
	s := m.databases
	if s == nil || s.erPath == nil {
		return nil, true
	}
	p := s.erPath
	s.erPath = nil
	text, shown, err := dbERPathText(p.schema, p.from, id)
	if err != nil {
		return m.pushToastDetail(toast.Info, sanitizeDBText(err.Error()), "Tables are joined only by foreign keys in "+p.target.label+"."), true
	}
	title := "ER Path: " + p.target.label + " (" + sanitizeDBText(p.from) + " → " + sanitizeDBText(id) + ")"
	return tea.Batch(m.onDBERReady(dbERReadyMsg{title: title, text: text}),
		m.pushToastDetail(toast.Info, "Path: "+sanitizeDBText(shown), "")), true
}
