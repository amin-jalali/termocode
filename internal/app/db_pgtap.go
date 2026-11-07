package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/tests"
	"github.com/amin-jalali/termocode/internal/toast"
)

// pgTAP in the test view (DBX-52): U on a PostgreSQL connection (or
// Databases: Run pgTAP Tests…) asks for an optional name pattern, shows
// the runtests() statement on a card (Cancel first), runs it on the
// connection's session and puts one row per test function under the
// "pgTAP" node of the Testing view. Run on that node, a connection or a
// test runs it again; Enter on a test shows its TAP output.

const promptKindDBPgTAP promptKindEnum = 1465
const confirmKindDBPgTAP confirmKindEnum = 1465

// dbPgTAPRun is a pgTAP run waiting for its card, or running.
type dbPgTAPRun struct {
	conn  string
	sql   string
	whole bool // a full run: the connection's old results are dropped
}

type dbPgTAPDoneMsg struct {
	runID int
	run   *dbPgTAPRun
	lines []string
	err   error
}

// dbPgTAPCaseSQL runs one test function again: "schema.fn" runs fn in
// schema, a bare name runs it in every schema.
func dbPgTAPCaseSQL(name string) string {
	schema, fn, ok := strings.Cut(name, ".")
	if !ok {
		schema, fn = "", name
	}
	return tests.PgTAPRunSQL(schema, "^"+regexp.QuoteMeta(fn)+"$")
}

// databasePgTAPStart asks for the name pattern of a run on conn ("": the
// selected connection).
func (m *Model) databasePgTAPStart(conn string) tea.Cmd {
	m.ensureDatabasesLoaded()
	if conn == "" {
		c, cmd, ok := m.databaseSessionConn()
		if !ok {
			return cmd
		}
		conn = c.ID
	}
	c, ok := m.databases.connection(conn)
	if !ok || c.Driver != "postgres" {
		return m.pushToastDetail(toast.Info, "pgTAP needs a PostgreSQL connection", "Select a PostgreSQL connection in the Databases view.")
	}
	m.dbConsole().pgtapConn = conn
	cmd := m.openHTTPPrompt(promptKindDBPgTAP, "Run pgTAP tests", "Test function names (a regular expression; empty: all):", "")
	m.prompt.SetAllowEmpty(true)
	return cmd
}

// databasePgTAPPrompt handles the pattern answer.
func (m *Model) databasePgTAPPrompt(kind promptKindEnum, value string) (tea.Cmd, bool) {
	if kind != promptKindDBPgTAP {
		return nil, false
	}
	s := m.dbConsole()
	conn := s.pgtapConn
	s.pgtapConn = ""
	if conn == "" {
		return nil, true
	}
	pattern := strings.TrimSpace(value)
	if pattern != "" {
		if _, err := regexp.Compile(pattern); err != nil {
			return m.pushToastDetail(toast.Warn, "That pattern is not valid", err.Error()), true
		}
	}
	return m.databasePgTAPAsk(&dbPgTAPRun{conn: conn, sql: tests.PgTAPRunSQL("", pattern), whole: pattern == ""}), true
}

// databasePgTAPAsk shows the card with the exact statement.
func (m *Model) databasePgTAPAsk(r *dbPgTAPRun) tea.Cmd {
	c, ok := m.databases.connection(r.conn)
	if !ok {
		return nil
	}
	name := sanitizeDBText(dbConnName(c))
	title := "Run pgTAP tests on " + name + "?"
	style := confirm.StylePrimary
	if c.Environment == "production" {
		title = "Run pgTAP tests on production (" + name + ")?"
		style = confirm.StyleDestructive
	}
	body := r.sql + "\n\nruntests() calls your test functions. pgTAP rolls back each one, but a function can still have side effects (sequences, notices, outside calls)."
	m.dbConsole().pgtapRun = r
	m.confirm = confirm.New(title, body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "run", Title: "Run", Style: style},
	}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBPgTAP
	return nil
}

// databasePgTAPConfirm starts the run after Run on the card.
func (m *Model) databasePgTAPConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindDBPgTAP {
		return nil, false
	}
	s := m.dbConsole()
	r := s.pgtapRun
	s.pgtapRun = nil
	if id != "run" || r == nil {
		return nil, true
	}
	return m.databasePgTAPRun(r), true
}

// databasePgTAPRun runs r on the connection's session.
func (m *Model) databasePgTAPRun(r *dbPgTAPRun) tea.Cmd {
	sess := m.databases.sessions[r.conn]
	if sess == nil {
		return tea.Batch(m.databaseConnect(r.conn), m.pushToastDetail(toast.Info, "Connecting…", "Run the pgTAP tests again when the connection is open."))
	}
	if m.databases.txOpen(r.conn) {
		return m.pushToastDetail(toast.Warn, "A transaction is open", "Commit or roll back first; pgTAP tests do not run inside your transaction.")
	}
	st := m.testView
	if st == nil {
		return nil
	}
	if st.tree == nil {
		root, _ := procRootDir()
		st.tree = &tests.Tree{Root: root}
	}
	m.stopTestRun()
	st.runID++
	if r.whole {
		st.tree.ResetPgTAP(r.conn)
	}
	st.running, st.hasRun, st.proc = true, false, nil
	st.cmd = tests.Command{Args: []string{"pgTAP", r.conn}}
	st.started, st.elapsed = time.Now(), 0
	st.runError, st.results = "", 0
	st.log = []string{"$ " + r.sql}
	st.resultsScroll = 0
	m.revealTestsView(false)
	runID := st.runID
	timeout := loadDBConsoleSettings().timeout()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		res := sess.Run(ctx, r.sql, dbclient.RunOptions{MaxRows: 100000})
		var lines []string
		for _, row := range res.Rows {
			if len(row) > 0 {
				lines = append(lines, strings.Split(row[0].Text, "\n")...)
			}
		}
		return dbPgTAPDoneMsg{runID: runID, run: r, lines: lines, err: res.Err}
	}
}

// onDBPgTAPDone folds the TAP lines into the Testing view.
func (m *Model) onDBPgTAPDone(msg dbPgTAPDoneMsg) tea.Cmd {
	st := m.testView
	if st == nil || msg.runID != st.runID || !st.running {
		return nil
	}
	p := tests.NewPgTAPParser()
	apply := func(evs []tests.Event) {
		for _, ev := range evs {
			switch ev.Kind {
			case tests.EventOutput:
				st.appendLog(ev.Line)
			case tests.EventResult:
				if st.tree.ApplyPgTAP(msg.run.conn, ev) != nil {
					st.results++
				}
			}
		}
	}
	for _, ln := range msg.lines {
		apply(p.Line(ln))
	}
	apply(p.Flush())
	st.tree.FinishRun()
	st.running, st.hasRun = false, true
	st.elapsed = time.Since(st.started).Round(10 * time.Millisecond)
	if msg.err != nil {
		text := dbErrorText(msg.err)
		st.runError = text
		st.appendLog("[" + text + "]")
		detail := text
		if strings.Contains(text, "runtests") && strings.Contains(text, "does not exist") {
			detail = "pgTAP is not installed in this database. Run CREATE EXTENSION pgtap; first."
		}
		return m.pushToastDetail(toast.Errr, "pgTAP run failed", detail)
	}
	var passed, failed int
	if f := st.tree.PgTAP(); f != nil {
		for _, file := range f.Files {
			if conn, _ := tests.PgTAPConn(file.Path); conn != msg.run.conn {
				continue
			}
			for _, c := range file.Cases {
				switch c.Status {
				case tests.StatusPassed:
					passed++
				case tests.StatusFailed:
					failed++
				}
			}
		}
	}
	st.appendLog(fmt.Sprintf("[done in %s · ✓ %d ✗ %d]", st.elapsed, passed, failed))
	switch {
	case st.results == 0:
		return m.pushToastDetail(toast.Info, "No pgTAP tests found", "runtests() runs functions named test* (or startup/setup/teardown) in the search path.")
	case failed > 0:
		return m.pushToastDetail(toast.Warn, fmt.Sprintf("pgTAP: %d failed, %d passed", failed, passed), msg.run.conn)
	}
	return m.pushToastDetail(toast.Info, fmt.Sprintf("pgTAP: %d passed", passed), msg.run.conn)
}

// testsPgTAPRow runs or opens a row of the pgTAP node again. ok=false:
// not a pgTAP row.
func (m *Model) testsPgTAPRow(r testRow, run bool) (tea.Cmd, bool) {
	if r.pkg == nil || r.pkg.Name != tests.PgTAPPackage || r.pkg.Dir != "" {
		return nil, false
	}
	if !run {
		if r.c != nil {
			m.showTestOutput(&r)
		}
		return nil, true
	}
	file := r.file
	if file == nil && len(r.pkg.Files) > 0 {
		file = r.pkg.Files[0]
	}
	if file == nil {
		return nil, true
	}
	conn, _ := tests.PgTAPConn(file.Path)
	if m.databases == nil {
		m.ensureDatabasesLoaded()
	}
	if r.c != nil {
		return m.databasePgTAPAsk(&dbPgTAPRun{conn: conn, sql: dbPgTAPCaseSQL(r.c.Name)}), true
	}
	return m.databasePgTAPAsk(&dbPgTAPRun{conn: conn, sql: tests.PgTAPRunSQL("", ""), whole: true}), true
}
