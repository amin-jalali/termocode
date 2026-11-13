package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/tests"
	"github.com/amin-jalali/termocode/internal/toast"
)

// tSQLt in the test view (DBX-52), the SQL Server twin of pgTAP: U on a SQL
// Server connection (or Databases: Run tSQLt Tests…) asks for an optional
// test class, shows the T-SQL on a card (Cancel first), runs it on the
// connection's session and puts one row per test under the "tSQLt" node of
// the Testing view. Run on that node, a connection or a test runs it again.

const (
	promptKindDBTSQLt  promptKindEnum  = 7790
	confirmKindDBTSQLt confirmKindEnum = 7790
	dbTSQLtPaletteID                   = "db-tsqlt-run"
)

// dbTSQLtRun is a tSQLt run waiting for its card, or running.
type dbTSQLtRun struct {
	conn  string
	sql   string
	whole bool // a full run: the connection's old results are dropped
}

type dbTSQLtDoneMsg struct {
	runID int
	run   *dbTSQLtRun
	cols  []string
	rows  [][]string
	err   error
}

// dbTSQLtCaseSQL runs one test again: "Class.test name".
func dbTSQLtCaseSQL(name string) string {
	class, test, ok := strings.Cut(name, ".")
	if !ok {
		return tests.TSQLtRunSQL(name, "")
	}
	return tests.TSQLtRunSQL(class, test)
}

// databaseTSQLtStart asks for the test class of a run on conn ("": the
// selected connection).
func (m *Model) databaseTSQLtStart(conn string) tea.Cmd {
	m.ensureDatabasesLoaded()
	if conn == "" {
		c, cmd, ok := m.databaseSessionConn()
		if !ok {
			return cmd
		}
		conn = c.ID
	}
	c, ok := m.databases.connection(conn)
	if !ok || c.Driver != "sqlserver" {
		return m.pushToastDetail(toast.Info, "tSQLt needs a SQL Server connection", "Select a SQL Server connection in the Databases view.")
	}
	m.dbConsole().tsqltConn = conn
	cmd := m.openHTTPPrompt(promptKindDBTSQLt, "Run tSQLt tests", "Test class (empty: all tests):", "")
	m.prompt.SetAllowEmpty(true)
	return cmd
}

// databaseTSQLtPrompt handles the class answer.
func (m *Model) databaseTSQLtPrompt(kind promptKindEnum, value string) (tea.Cmd, bool) {
	if kind != promptKindDBTSQLt {
		return nil, false
	}
	s := m.dbConsole()
	conn := s.tsqltConn
	s.tsqltConn = ""
	if conn == "" {
		return nil, true
	}
	class := strings.TrimSpace(value)
	return m.databaseTSQLtAsk(&dbTSQLtRun{conn: conn, sql: tests.TSQLtRunSQL(class, ""), whole: class == ""}), true
}

// databaseTSQLtAsk shows the card with the exact statement.
func (m *Model) databaseTSQLtAsk(r *dbTSQLtRun) tea.Cmd {
	c, ok := m.databases.connection(r.conn)
	if !ok {
		return nil
	}
	name := sanitizeDBText(dbConnName(c))
	title := "Run tSQLt tests on " + name + "?"
	style := confirm.StylePrimary
	if c.Environment == "production" {
		title = "Run tSQLt tests on production (" + name + ")?"
		style = confirm.StyleDestructive
	}
	body := r.sql + "\n\ntSQLt runs your test procedures, each in a transaction it rolls back. A test can still have side effects outside the transaction."
	m.dbConsole().tsqltRun = r
	m.confirm = confirm.New(title, body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "run", Title: "Run", Style: style},
	}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBTSQLt
	return nil
}

// databaseTSQLtConfirm starts the run after Run on the card.
func (m *Model) databaseTSQLtConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindDBTSQLt {
		return nil, false
	}
	s := m.dbConsole()
	r := s.tsqltRun
	s.tsqltRun = nil
	if id != "run" || r == nil {
		return nil, true
	}
	return m.databaseTSQLtRun(r), true
}

// databaseTSQLtRun runs r on the connection's session.
func (m *Model) databaseTSQLtRun(r *dbTSQLtRun) tea.Cmd {
	sess := m.databases.sessions[r.conn]
	if sess == nil {
		return tea.Batch(m.databaseConnect(r.conn), m.pushToastDetail(toast.Info, "Connecting…", "Run the tSQLt tests again when the connection is open."))
	}
	if m.databases.txOpen(r.conn) {
		return m.pushToastDetail(toast.Warn, "A transaction is open", "Commit or roll back first; tSQLt tests do not run inside your transaction.")
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
		st.tree.ResetTSQLt(r.conn)
	}
	st.running, st.hasRun, st.proc = true, false, nil
	st.cmd = tests.Command{Args: []string{"tSQLt", r.conn}}
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
		msg := dbTSQLtDoneMsg{runID: runID, run: r, err: res.Err}
		for _, c := range res.Columns {
			msg.cols = append(msg.cols, c.Name)
		}
		for _, row := range res.Rows {
			cells := make([]string, len(row))
			for i, c := range row {
				cells[i] = c.Text
			}
			msg.rows = append(msg.rows, cells)
		}
		return msg
	}
}

// onDBTSQLtDone folds the TestResult rows into the Testing view.
func (m *Model) onDBTSQLtDone(msg dbTSQLtDoneMsg) tea.Cmd {
	st := m.testView
	if st == nil || msg.runID != st.runID || !st.running {
		return nil
	}
	for _, ev := range tests.TSQLtEvents(msg.cols, msg.rows) {
		if st.tree.ApplyTSQLt(msg.run.conn, ev) != nil {
			st.results++
			if ev.Status == tests.StatusFailed && ev.Message != "" {
				st.appendLog(ev.Class + "." + ev.Test + ": " + ev.Message)
			}
		}
	}
	st.tree.FinishRun()
	st.running, st.hasRun = false, true
	st.elapsed = time.Since(st.started).Round(10 * time.Millisecond)
	if msg.err != nil {
		text := dbErrorText(msg.err)
		st.runError = text
		st.appendLog("[" + text + "]")
		detail := text
		if strings.Contains(text, "tSQLt") && (strings.Contains(text, "Could not find") || strings.Contains(text, "Invalid object")) {
			detail = "tSQLt is not installed in this database. Install the tSQLt framework first."
		}
		return m.pushToastDetail(toast.Errr, "tSQLt run failed", detail)
	}
	var passed, failed int
	if p := st.tree.TSQLt(); p != nil {
		for _, file := range p.Files {
			if conn, _ := tests.TSQLtConn(file.Path); conn != msg.run.conn {
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
		return m.pushToastDetail(toast.Info, "No tSQLt tests found", "tSQLt tests are procedures in a schema made with tSQLt.NewTestClass, named test*.")
	case failed > 0:
		return m.pushToastDetail(toast.Warn, fmt.Sprintf("tSQLt: %d failed, %d passed", failed, passed), msg.run.conn)
	}
	return m.pushToastDetail(toast.Info, fmt.Sprintf("tSQLt: %d passed", passed), msg.run.conn)
}

// testsTSQLtRow runs or opens a row of the tSQLt node again. ok=false: not
// a tSQLt row.
func (m *Model) testsTSQLtRow(r testRow, run bool) (tea.Cmd, bool) {
	if r.pkg == nil || r.pkg.Name != tests.TSQLtPackage || r.pkg.Dir != "" {
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
	conn, _ := tests.TSQLtConn(file.Path)
	if m.databases == nil {
		m.ensureDatabasesLoaded()
	}
	if r.c != nil {
		return m.databaseTSQLtAsk(&dbTSQLtRun{conn: conn, sql: dbTSQLtCaseSQL(r.c.Name)}), true
	}
	return m.databaseTSQLtAsk(&dbTSQLtRun{conn: conn, sql: tests.TSQLtRunSQL("", ""), whole: true}), true
}
