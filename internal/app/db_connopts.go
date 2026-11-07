package app

import (
	"github.com/amin-jalali/termocode/internal/dbclient"
	tea "github.com/charmbracelet/bubbletea"
)

// Connection options in the app (DBX-28): the tree shows only the
// databases, schemas and objects the options let through, tables in
// readOnlyTables refuse grid edits, and txMode "manual" starts the
// connection in manual commit. The start SQL and the idle timer live in
// the session pool (dbclient).

// connOptions are the options of connection id (nil: none; the
// ConnOptions methods treat nil as "no options").
func (s *dbState) connOptions(id string) *dbclient.ConnOptions {
	if s == nil {
		return nil
	}
	c, ok := s.connection(dbTxConn(id))
	if !ok {
		return nil
	}
	return c.Options
}

// applyConnTxMode turns manual commit on for a connection whose options
// say so, once per app run: the user's own switch stays afterwards.
func (s *dbState) applyConnTxMode(id string) {
	o := s.connOptions(id)
	if o == nil || o.TxMode != "manual" || s.txModeApplied[id] {
		return
	}
	if s.txModeApplied == nil {
		s.txModeApplied = map[string]bool{}
	}
	s.txModeApplied[id] = true
	if s.manual == nil {
		s.manual = map[string]bool{}
	}
	s.manual[id] = true
}

// databaseEditTarget is databaseEditTargetOf plus readOnlyTables.
func (m *Model) databaseEditTarget(t *dbTab) (*dbEditTarget, tea.Cmd) {
	tg, cmd := m.databaseEditTargetOf(t)
	if tg == nil || tg.loading || tg.why != "" || tg.table == "" {
		return tg, cmd
	}
	if m.databases.connOptions(tg.conn).TableReadOnly(tg.schema, tg.table) {
		ro := &dbEditTarget{why: "read-only table", detail: "The connection options list this table in readOnlyTables."}
		t.target = ro
		return ro, cmd
	}
	return tg, cmd
}
