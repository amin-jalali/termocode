package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-1: a console statement with query parameters (?, :name, $1,
// @var) asks for each value first, then runs with the values bound (the
// SQL only holds placeholders). The values are remembered per console
// file for the session and offered again next time.

const promptKindDBParam promptKindEnum = 1251

// dbParamAsk is one statement waiting for its parameter values.
type dbParamAsk struct {
	run   *dbRun
	refs  []dbclient.ParamRef
	names []string
	vals  map[string]string
	idx   int
}

// dbNeedsParams reports whether r is a statement the user typed that
// still has to ask for parameter values.
func dbNeedsParams(r *dbRun, driver string) []dbclient.ParamRef {
	if r.bound || r.by != "user" || r.tv != nil || r.describe != nil || r.redis != nil || r.plan != nil {
		return nil
	}
	return dbclient.FindParams(r.sql, driver)
}

// execSQL is the SQL and the values sent to the server.
func (r *dbRun) execSQL() (string, []any) {
	if r.bound {
		return r.boundSQL, r.args
	}
	return r.sql, nil
}

func (m *Model) databaseAskParams(r *dbRun, refs []dbclient.ParamRef) tea.Cmd {
	s := m.dbConsole()
	s.paramAsk = &dbParamAsk{run: r, refs: refs, names: dbclient.UniqueParamNames(refs), vals: map[string]string{}}
	return m.databaseAskNextParam()
}

func (m *Model) databaseAskNextParam() tea.Cmd {
	s := m.dbConsole()
	a := s.paramAsk
	name := a.names[a.idx]
	title := fmt.Sprintf("Query parameter %d of %d: %s", a.idx+1, len(a.names), name)
	cmd := m.openHTTPPrompt(promptKindDBParam, title, "Value (NULL for null):", s.params[a.run.file][name])
	m.prompt.SetAllowEmpty(true)
	return cmd
}

// databaseParamEntered is the promptKindDBParam submit.
func (m *Model) databaseParamEntered(value string) tea.Cmd {
	s := m.dbConsole()
	a := s.paramAsk
	if a == nil {
		return nil
	}
	a.vals[a.names[a.idx]] = value
	a.idx++
	if a.idx < len(a.names) {
		return m.databaseAskNextParam()
	}
	s.paramAsk = nil
	if s.params == nil {
		s.params = map[string]map[string]string{}
	}
	if s.params[a.run.file] == nil {
		s.params[a.run.file] = map[string]string{}
	}
	for k, v := range a.vals {
		s.params[a.run.file][k] = v
	}
	c, ok := m.databases.connection(a.run.conn)
	if !ok {
		return nil
	}
	r := a.run
	r.boundSQL, r.args = dbclient.BindParams(r.sql, a.refs, a.vals, c.Driver)
	r.bound = true
	return m.databaseReviewRun(r)
}
