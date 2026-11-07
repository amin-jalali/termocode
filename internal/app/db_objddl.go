package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Show DDL for every object kind (DBX-13): D (or Databases: Show DDL) on
// a routine, trigger or sequence reads its stored DDL on a read-only
// session and opens it in a scratch buffer, like a table's DDL.

// dbObjectDDLKind maps a tree node kind to the object kind of its DDL.
func dbObjectDDLKind(k dbNodeKind) (dbclient.ObjectKind, bool) {
	switch k {
	case dbNodeRoutine:
		return dbclient.ObjectFunction, true
	case dbNodeTrigger:
		return dbclient.ObjectTrigger, true
	case dbNodeSequence:
		return dbclient.ObjectSequence, true
	}
	return "", false
}

type dbObjectDDLMsg struct {
	label, text string
	err         error
}

// databaseShowDDLKey shows the DDL of the object under the tree cursor:
// tables and views take the table path, other kinds their stored source.
func (m *Model) databaseShowDDLKey() tea.Cmd {
	s := m.databases
	r, ok := m.databaseCurrentRow()
	if s == nil || !ok {
		return m.databaseTableMeta(true)
	}
	kind, ok := dbObjectDDLKind(r.kind)
	if !ok {
		return m.databaseTableMeta(true)
	}
	c, ok := s.connection(r.conn)
	if !ok {
		return nil
	}
	key := s.sessionKeyFor(r.path, r.conn)
	sess := s.sessions[key]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	driver := dbDriverFamily(c.Driver)
	schema := dbERSchemaOf(s.nodes, r.path, c.Driver)
	if driver == "sqlite" {
		schema = ""
	}
	name := s.nodes[r.path].name
	label := sanitizeDBText(name)
	if schema != "" {
		label = sanitizeDBText(schema) + "." + label
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbMetaTimeout)
		defer cancel()
		text, err := dbReadObjectDDL(ctx, sess, driver, kind, schema, name)
		if err != nil && kind == dbclient.ObjectFunction && driver == "mysql" {
			// A MySQL routine is a function or a procedure: try the other.
			text, err = dbReadObjectDDL(ctx, sess, driver, dbclient.ObjectProcedure, schema, name)
		}
		return dbObjectDDLMsg{label: label, text: text, err: err}
	}
}

func dbReadObjectDDL(ctx context.Context, sess dbclient.Session, driver string, kind dbclient.ObjectKind, schema, name string) (string, error) {
	q, err := dbclient.ObjectDDLQuery(driver, kind, schema, name)
	if err != nil {
		return "", err
	}
	r := dbRunReadOnly(ctx, sess, q.SQL, dbclient.RunOptions{MaxRows: 1, Args: q.Args})
	if r.Err != nil {
		return "", r.Err
	}
	if len(r.Rows) == 0 || len(r.Rows[0]) <= q.Column || r.Rows[0][q.Column].Null {
		return "", errNoObjectDDL
	}
	return strings.TrimRight(r.Rows[0][q.Column].Text, "\n") + "\n", nil
}

type dbErr string

func (e dbErr) Error() string { return string(e) }

const errNoObjectDDL = dbErr("the server returned no DDL for this object")

func (m *Model) onDBObjectDDL(msg dbObjectDDLMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Show DDL failed", dbclient.Redact(sanitizeDBText(msg.err.Error())))
	}
	if m.nvim == nil {
		return m.copyWithToast(msg.text, "No editor: DDL copied instead")
	}
	m.ensureDBLua()
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLuaArgs(`_G._termocode_db_ddl(...)`, "SQL DDL: "+msg.label, strings.Split(strings.TrimRight(msg.text, "\n"), "\n"))
	m.focus = FocusEditor
	return m.fetchStateCmd()
}
