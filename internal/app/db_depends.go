package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Dependencies and the text ER diagram (DBX-16). Databases: Show
// Dependencies (on a table, view or routine) lists what the object uses
// and what uses it; Databases: Show ER Diagram (Text) draws the tables as
// boxes, the table under the cursor and its neighbours, or the whole
// schema from a schema row. Both read the schema on a read-only session
// and open the text in a scratch buffer (the answer comes back as a
// dbERReadyMsg, like Export ER Diagram).

// dbDepsText is the dependencies buffer of one object.
func dbDepsText(schema string, d dbclient.Deps) string {
	name := sanitizeDBText(d.Name)
	if schema != "" {
		name = sanitizeDBText(schema) + "." + name
	}
	if d.Missing {
		return name + ": not found in the schema (refresh the tree and try again)\n"
	}
	var b strings.Builder
	b.WriteString(d.Kind + " " + name + "\n\n")
	section := func(title string, refs []dbclient.DepRef) {
		b.WriteString(title + "\n")
		if len(refs) == 0 {
			b.WriteString("  (nothing)\n")
		}
		for _, r := range refs {
			b.WriteString("  " + r.Kind + " " + sanitizeDBText(r.Name))
			if r.Via != "" {
				b.WriteString("  — " + sanitizeDBText(r.Via))
			}
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	section("Uses", d.Uses)
	section("Used by", d.UsedBy)
	b.WriteString("Foreign keys link tables; views and routines link by the names in their SQL text.\n")
	return b.String()
}

// databaseDependsStart runs Show Dependencies (er false) or Show ER
// Diagram (Text) (er true) for the tree row under the cursor.
func (m *Model) databaseDependsStart(er bool) tea.Cmd {
	s := m.databases
	r, ok := m.databaseCurrentRow()
	if s == nil || !ok {
		return tea.Batch(m.revealDatabasesView(false), m.pushToastDetail(toast.Info, "Select an object first",
			"Put the cursor on a table, view or routine in the Databases view."))
	}
	c, ok := s.connection(r.conn)
	if !ok || dbclient.DialectFor(c.Driver) == nil {
		return m.pushToastDetail(toast.Info, "This needs a SQL database", "")
	}
	schema := dbERSchemaOf(s.nodes, r.path, c.Driver)
	if schema == "" {
		return m.pushToastDetail(toast.Info, "Select a schema or an object in it", "")
	}
	focus := ""
	switch r.kind {
	case dbNodeTable, dbNodeView, dbNodeRoutine:
		focus = s.nodes[r.path].name
	case dbNodeSchema, dbNodeDatabase, dbNodeGroup:
		if !er {
			return m.pushToastDetail(toast.Info, "Select a table, view or routine", "Dependencies are of one object.")
		}
	default:
		return m.pushToastDetail(toast.Info, "Select a table, view or routine", "")
	}
	if er && r.kind != dbNodeTable {
		focus = "" // the whole schema
	}
	key := s.sessionKeyFor(r.path, r.conn)
	sess := s.sessions[key]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Press Enter on the connection in the Databases view first.")
	}
	driver := dbDriverFamily(c.Driver)
	label := sanitizeDBText(dbConnName(c) + " · " + schema)
	title := "Dependencies: " + label + "." + sanitizeDBText(focus)
	if er {
		title = "ER Diagram: " + label
		if focus != "" {
			title += "." + sanitizeDBText(focus)
		}
	}
	shown := schema
	if driver == "sqlite" {
		shown = ""
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbCompareTimeout)
		defer cancel()
		ro, err := sess.ReadOnlySession()
		if err != nil {
			return dbERReadyMsg{title: title, err: err}
		}
		defer ro.Close()
		cs, err := dbclient.ReadCanonSchema(ctx, ro, driver, schema)
		if err != nil {
			return dbERReadyMsg{title: title, err: err}
		}
		if er {
			text := dbclient.ERDiagramText(cs, focus, 1)
			if strings.TrimSpace(text) == "" {
				text = "No tables to draw.\n"
			}
			return dbERReadyMsg{title: title, text: text}
		}
		return dbERReadyMsg{title: title, text: dbDepsText(shown, dbclient.Dependencies(cs, focus))}
	}
}
