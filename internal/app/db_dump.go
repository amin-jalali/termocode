package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Import upgrades (DBX-37):
//   - Databases: Run SQL Dump File… runs a whole .sql file on a connection
//     after ONE strong card (Cancel first; it counts the statements and the
//     destructive ones), with no card per statement: one by one in the
//     connection's session, a progress card (Esc / Cancel stops it), and
//     the first failing statement stops the run and is named.
//   - Databases: Import CSV as New Table… on a schema: a file → a card with
//     the CREATE TABLE (text columns named like the header; Cancel first)
//     → the table is made → the usual import preview card for it.

const (
	promptKindDBDumpPath     promptKindEnum  = 5343
	promptKindDBNewTablePath promptKindEnum  = 5344
	confirmKindDBDump        confirmKindEnum = 5343
	confirmKindDBNewTable    confirmKindEnum = 5344
)

// dbDumpMaxBytes caps the dump file read into memory.
const dbDumpMaxBytes = 64 << 20

// dbDump is a dump waiting for its card.
type dbDump struct {
	conn    dbclient.Conn
	sessKey string
	display string
	stmts   []string
}

// dbNewTable is an Import as New Table waiting for its card.
type dbNewTable struct {
	tgt     dbSearchTarget
	conn    dbclient.Conn
	path    string
	display string
	name    string
	sql     string
}

type dbDumpSum struct{ total, destructive int }

type dbDumpReport struct {
	ran    int   // statements that ran
	failed int   // 1-based statement that failed (0: none)
	err    error // its error, or context.Canceled
}

type dbDumpDoneMsg struct {
	job  *dbJob
	d    dbDump
	rep  dbDumpReport
	took time.Duration
}

type dbNewTableDoneMsg struct {
	nt  dbNewTable
	err error
}

// dbDumpStatements splits a dump into its statements (comment-only parts
// dropped).
func dbDumpStatements(text, driver string) ([]string, error) {
	parts, err := dbclient.Split(text, driver)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(dbStripComments(p)) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}
	if len(out) == 0 {
		return nil, errors.New("the file has no SQL statements")
	}
	return out, nil
}

// dbDumpSummary counts the statements and the ones that delete objects or rows.
func dbDumpSummary(stmts []string, driver string) dbDumpSum {
	sum := dbDumpSum{total: len(stmts)}
	for _, s := range stmts {
		switch strings.ToUpper(strings.Trim(firstSQLWord(dbStripComments(s)), ";")) {
		case "DROP", "TRUNCATE", "DELETE":
			sum.destructive++
		}
	}
	return sum
}

// dbRunDump runs the statements in order on sess and stops at the first
// error or when ctx ends. progress gets the count that ran.
func dbRunDump(ctx context.Context, sess dbclient.Session, stmts []string, progress func(int)) dbDumpReport {
	var rep dbDumpReport
	for i, st := range stmts {
		if err := ctx.Err(); err != nil {
			rep.err = err
			return rep
		}
		r := sess.Run(ctx, st, dbclient.RunOptions{MaxRows: 1})
		if r.Err != nil {
			if ctx.Err() != nil {
				rep.err = ctx.Err()
				return rep
			}
			rep.failed, rep.err = i+1, r.Err
			return rep
		}
		rep.ran++
		if progress != nil {
			progress(rep.ran)
		}
	}
	return rep
}

// dbNewTableName is a table name from a file name: lower case, letters,
// digits and _ only, never starting with a digit.
func dbNewTableName(file string) string {
	base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	var b strings.Builder
	under := false
	for _, r := range strings.ToLower(base) {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if !ok {
			if b.Len() > 0 && !under {
				b.WriteByte('_')
				under = true
			}
			continue
		}
		b.WriteRune(r)
		under = false
	}
	name := strings.Trim(b.String(), "_")
	if name == "" {
		name = "imported"
	}
	if name[0] >= '0' && name[0] <= '9' {
		name = "t_" + name
	}
	return name
}

// dbNewTableSQL is the CREATE TABLE of text columns for the file's header
// (a repeated name gets _2, _3 …).
func dbNewTableSQL(d dbclient.Dialect, schema, table string, cols []string) string {
	name := d.QuoteIdent(table)
	if schema != "" {
		name = d.QuoteIdent(schema) + "." + name
	}
	seen := map[string]int{}
	var defs []string
	for _, c := range cols {
		c = strings.TrimSpace(c)
		if c == "" {
			c = "column"
		}
		key := strings.ToLower(c)
		seen[key]++
		if n := seen[key]; n > 1 {
			c = fmt.Sprintf("%s_%d", c, n)
		}
		defs = append(defs, "  "+d.QuoteIdent(c)+" TEXT")
	}
	return "CREATE TABLE " + name + " (\n" + strings.Join(defs, ",\n") + "\n)"
}

// databaseTreeConn is the connection of the tree row and the session that
// reaches it.
func (m *Model) databaseTreeConn() (dbclient.Conn, string, tea.Cmd, bool) {
	m.ensureDatabasesLoaded()
	row, ok := m.databaseCurrentRow()
	if !ok || row.conn == "" {
		return dbclient.Conn{}, "", m.pushToastDetail(toast.Info, "Select a connection", "Move to a connection, a database or a schema in the tree first."), false
	}
	c, ok := m.databases.connection(row.conn)
	switch {
	case !ok || dbclient.DialectFor(c.Driver) == nil:
		return c, "", m.pushToastDetail(toast.Info, "Not a SQL connection", "Pick a PostgreSQL, MySQL or SQLite connection."), false
	case c.ReadOnly:
		return c, "", m.pushToastDetail(toast.Warn, "Cannot write", "The connection is read-only."), false
	}
	key := m.databases.sessionKeyFor(row.path, row.conn)
	if m.databases.sessions[key] == nil {
		return c, "", m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection."), false
	}
	s := m.dbConsole()
	if s.job != nil || s.running != nil || s.submitting != nil {
		return c, "", m.pushToastDetail(toast.Warn, "A query or transfer is running", "Esc cancels it."), false
	}
	return c, key, nil, true
}

// databaseDumpStart is Databases: Run SQL Dump File…: the file first.
func (m *Model) databaseDumpStart() tea.Cmd {
	c, key, cmd, ok := m.databaseTreeConn()
	if !ok {
		return cmd
	}
	s := m.dbConsole()
	s.dump = &dbDump{conn: c, sessKey: key}
	return m.openHTTPPrompt(promptKindDBDumpPath, "Run SQL Dump on "+sanitizeDBText(c.ID), "SQL file (in the workspace, or a full path):", "")
}

// databaseDumpLoad reads and splits the file, then shows the one card.
func (m *Model) databaseDumpLoad(value string) tea.Cmd {
	s := m.dbConsole()
	d := s.dump
	if d == nil {
		return nil
	}
	abs, display, err := m.dbResolvePath(value)
	if err != nil {
		s.dump = nil
		return m.pushToastDetail(toast.Warn, "Cannot run the dump", err.Error())
	}
	st, err := os.Stat(abs)
	switch {
	case err != nil:
		s.dump = nil
		return m.pushToastDetail(toast.Warn, "Cannot run the dump", sanitizeDBText(display)+" cannot be read.")
	case st.IsDir():
		s.dump = nil
		return m.pushToastDetail(toast.Warn, "Cannot run the dump", sanitizeDBText(display)+" is a folder.")
	case st.Size() > dbDumpMaxBytes:
		s.dump = nil
		return m.pushToastDetail(toast.Warn, "The dump is too big", "Files up to 64 MB run here; use the database's own client for bigger dumps.")
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		s.dump = nil
		return m.pushToastDetail(toast.Warn, "Cannot run the dump", sanitizeDBText(err.Error()))
	}
	stmts, err := dbDumpStatements(string(b), d.conn.Driver)
	if err != nil {
		s.dump = nil
		return m.pushToastDetail(toast.Warn, "Cannot run the dump", sanitizeDBText(err.Error()))
	}
	d.display, d.stmts = display, stmts
	sum := dbDumpSummary(stmts, d.conn.Driver)
	msg := fmt.Sprintf("%s · %s on %s.", sanitizeDBText(display), dbPlural(sum.total, "statement"), sanitizeDBText(d.conn.ID))
	if strings.EqualFold(d.conn.Environment, "production") {
		msg = "PRODUCTION connection.\n\n" + msg
	}
	if sum.destructive > 0 {
		msg += fmt.Sprintf("\n\n%s delete objects or rows (DROP, TRUNCATE, DELETE).", dbPlural(sum.destructive, "statement"))
	}
	msg += "\n\nThe statements run one by one in this connection's session, with no card for each. The first one that fails stops the run; what ran before it stays (unless the file uses its own transaction)."
	m.confirm = confirm.New("Run the whole SQL dump?", msg, []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "run", Title: "Run " + dbPlural(sum.total, "statement"), Style: confirm.StyleDestructive}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBDump
	return nil
}

func (m *Model) databaseDumpConfirm(id string) tea.Cmd {
	s := m.dbConsole()
	d := s.dump
	s.dump = nil
	if d == nil || id != "run" {
		return nil
	}
	if s.job != nil || s.running != nil {
		return m.pushToastDetail(toast.Warn, "A query or transfer is running", "Esc cancels it.")
	}
	sess := m.databases.sessions[d.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &dbJob{kind: "dump", title: "Running " + sanitizeDBText(d.display) + " on " + sanitizeDBText(d.conn.ID), conn: d.conn.ID, cancel: cancel, blocks: true, total: int64(len(d.stmts))}
	s.job = job
	m.databaseJobCard(job)
	dump := *d
	run := func() tea.Msg {
		defer cancel()
		start := time.Now()
		rep := dbRunDump(ctx, sess, dump.stmts, func(n int) { job.done.Store(int64(n)) })
		return dbDumpDoneMsg{job: job, d: dump, rep: rep, took: time.Since(start)}
	}
	return tea.Batch(run, dbJobTickCmd(job))
}

func (m *Model) onDBDumpDone(msg dbDumpDoneMsg) tea.Cmd {
	s := m.dbConsole()
	if s.job != msg.job {
		return nil
	}
	s.job = nil
	m.closeJobCard()
	rep := msg.rep
	refresh := m.pushToast(toast.Info, "R in the tree shows new or dropped objects")
	switch {
	case errors.Is(rep.err, context.Canceled):
		return tea.Batch(refresh, m.pushToastDetail(toast.Warn, "Dump stopped", fmt.Sprintf("%s ran before it stopped; they stay.", dbPlural(rep.ran, "statement"))))
	case rep.err != nil:
		stmt := []rune(dbOneLine(msg.d.stmts[rep.failed-1]))
		if len(stmt) > 120 {
			stmt = append(stmt[:120], '…')
		}
		return tea.Batch(refresh, m.pushToastDetail(toast.Errr, fmt.Sprintf("Dump failed at statement %d of %d", rep.failed, len(msg.d.stmts)),
			dbErrorText(rep.err)+" · "+sanitizeDBText(string(stmt))+fmt.Sprintf(" · %s before it ran and stay.", dbPlural(rep.ran, "statement"))))
	}
	return tea.Batch(refresh, m.pushToastDetail(toast.Info, "Dump done", fmt.Sprintf("%s ran on %s · %d ms", dbPlural(rep.ran, "statement"), sanitizeDBText(msg.d.conn.ID), msg.took.Milliseconds())))
}

// ── import as a new table ───────────────────────────────────────────────

// databaseNewTableStart is Databases: Import CSV as New Table…: the schema
// from the tree, then the file.
func (m *Model) databaseNewTableStart() tea.Cmd {
	c, _, cmd, ok := m.databaseTreeConn()
	if !ok {
		return cmd
	}
	row, _ := m.databaseCurrentRow()
	tgt, err := dbSearchTargetAt(m.databases, row)
	if err != nil {
		return m.pushToastDetail(toast.Info, "Cannot import here", sanitizeDBText(err.Error()))
	}
	s := m.dbConsole()
	s.newTable = &dbNewTable{tgt: tgt, conn: c}
	return m.openHTTPPrompt(promptKindDBNewTablePath, "Import CSV as New Table in "+tgt.label(), "CSV file (in the workspace, or a full path):", "")
}

// databaseNewTableLoad reads the file's header and shows the CREATE TABLE.
func (m *Model) databaseNewTableLoad(value string) tea.Cmd {
	s := m.dbConsole()
	nt := s.newTable
	if nt == nil {
		return nil
	}
	abs, display, err := m.dbResolvePath(value)
	if err != nil {
		s.newTable = nil
		return m.pushToastDetail(toast.Warn, "Cannot import", err.Error())
	}
	sn, err := dbclient.SniffCSV(abs)
	if err != nil {
		s.newTable = nil
		return m.pushToastDetail(toast.Warn, "Cannot import", sanitizeDBText(err.Error()))
	}
	d := dbclient.DialectFor(nt.conn.Driver)
	if d == nil {
		s.newTable = nil
		return m.pushToastDetail(toast.Info, "Not a SQL connection", "Pick a PostgreSQL, MySQL or SQLite connection.")
	}
	schema := nt.tgt.schema
	if nt.conn.Driver == "sqlite" {
		schema = ""
	}
	nt.path, nt.display = abs, display
	nt.name = dbNewTableName(abs)
	nt.sql = dbNewTableSQL(d, schema, nt.name, sn.Columns)
	msg := "From " + sanitizeDBText(display) + " (" + dbPlural(len(sn.Columns), "column") + "):\n\n" + dbSafeMultiline(nt.sql) +
		"\n\nEvery column is TEXT; change the types later with ALTER TABLE. After the table is made, the import card opens for it."
	if strings.EqualFold(nt.conn.Environment, "production") {
		msg = "PRODUCTION connection.\n\n" + msg
	}
	m.confirm = confirm.New("Create the table "+sanitizeDBText(nt.name)+"?", msg, []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "create", Title: "Create and import", Style: confirm.StylePrimary}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBNewTable
	return nil
}

func (m *Model) databaseNewTableConfirm(id string) tea.Cmd {
	s := m.dbConsole()
	nt := s.newTable
	s.newTable = nil
	if nt == nil || id != "create" {
		return nil
	}
	sess := m.databases.sessions[nt.tgt.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	x := *nt
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbMetaTimeout)
		defer cancel()
		return dbNewTableDoneMsg{nt: x, err: sess.Run(ctx, x.sql, dbclient.RunOptions{}).Err}
	}
}

// onDBNewTableDone opens the usual import card for the new table.
func (m *Model) onDBNewTableDone(msg dbNewTableDoneMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot create the table", dbErrorText(msg.err))
	}
	nt := msg.nt
	schema := nt.tgt.schema
	t := dbTable{conn: nt.conn.ID, driver: nt.conn.Driver, schema: schema, name: nt.name, path: nt.tgt.path + "/Tables/" + nt.name, sessKey: nt.tgt.sessKey}
	s := m.dbConsole()
	s.imp = &dbImport{t: t, conn: nt.conn, sessKey: nt.tgt.sessKey}
	return m.databaseImportLoad(nt.path)
}
