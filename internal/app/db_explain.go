package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
	tea "github.com/charmbracelet/bubbletea"
)

// Explain (plan F8): x shows the plan of the statement under the cursor,
// X is EXPLAIN ANALYZE, which RUNS the statement and so asks first. A plan
// is a result on the same SQL Result page (no second grid): the Plan view
// is the page's first view, indented text with nvim indent folds and the
// costly node highlighted; Tab shows the server's raw answer.
//
// Safety, in this order: a plain plan never runs the statement and goes
// through the read-only session; an analyzed read also runs read-only;
// an analyzed write runs inside a transaction that is always rolled back
// (the card says so); DDL cannot be rolled back on every server and is
// refused; a read-only connection analyzes reads only; with a manual
// transaction open an analyzed write is refused (rolling back would take
// the user's own changes with it).

const confirmKindDBExplain confirmKindEnum = 1400

// dbPlanRun is what makes a run a plan run.
type dbPlanRun struct {
	analyze bool
	target  string // the statement that is explained (as typed, without ";")
	wrap    bool   // an analyzed write: begin … always roll back
	parsed  dbclient.Plan
}

func (p *dbPlanRun) label() string {
	if p.analyze {
		return "EXPLAIN ANALYZE "
	}
	return "EXPLAIN "
}

func init() {
	registerNotifyHandler("termocode_db_explain", func(m *Model, args []any) tea.Cmd {
		return m.databaseExplainKey(notifyArgString(args, 0) == "analyze")
	})
}

// databaseExplainKey is x / X in a console and the palette entries: from a
// SQL file it explains the statement at the cursor, from the result page
// the statement of the result.
func (m *Model) databaseExplainKey(analyze bool) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	p := m.editor.Path()
	switch {
	case strings.EqualFold(lastExt(p), ".sql"):
		return m.databaseSendMode(false, &dbPlanRun{analyze: analyze})
	case lastPathPart(p) == "SQL Result" && m.databases != nil:
		return m.databasePageExplain(analyze)
	}
	return m.pushToastDetail(toast.Info, "Open a SQL file", "Press n on a database connection to open its console.")
}

// databasePageExplain is x / X on the result page: the plan of the
// statement that produced the result (or of the statement of a plan).
func (m *Model) databasePageExplain(analyze bool) tea.Cmd {
	s := m.dbConsole()
	r := s.shown
	if r == nil || s.running != nil {
		return nil
	}
	target := r.sql
	switch {
	case r.plan != nil:
		target = r.plan.target
	case r.describe != nil:
		return m.pushToastDetail(toast.Info, "Nothing to explain", "A table description is not a query.")
	}
	run := &dbRun{conn: r.conn, file: r.file, line: r.line, by: "user", sessKey: r.sessKey, plan: &dbPlanRun{analyze: analyze}, statements: []string{target}}
	return m.databaseExplainStart(run)
}

// databaseExplainStart checks the explain run r (r.statements holds the one
// statement) and starts it, or asks first.
func (m *Model) databaseExplainStart(r *dbRun) tea.Cmd {
	c, ok := m.databases.connection(r.conn)
	if !ok {
		return m.pushToastDetail(toast.Warn, "Connection not found", sanitizeDBText(r.conn))
	}
	if len(r.statements) != 1 {
		return m.pushToastDetail(toast.Info, "Explain one statement", "Put the cursor in the statement, or select just one.")
	}
	pl := r.plan
	pl.target = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(dbStripComments(r.statements[0])), ";"))
	if pl.target == "" {
		return m.pushToastDetail(toast.Info, "No SQL at the cursor", "")
	}
	if dbclient.DialectFor(c.Driver) == nil {
		return m.pushToastDetail(toast.Info, "No query plan", "This connection does not use SQL.")
	}
	if strings.EqualFold(firstSQLWord(pl.target), "EXPLAIN") {
		return m.pushToastDetail(toast.Info, "Already an EXPLAIN", "Run it with the send key, or put the cursor in the statement itself.")
	}
	var note tea.Cmd
	if pl.analyze && dbIsSQLite(c.Driver) {
		pl.analyze = false // SQLite has no ANALYZE: the plan is all there is, and it never runs the statement
		note = m.pushToastDetail(toast.Info, "SQLite has no EXPLAIN ANALYZE", "Showing the query plan; the statement does not run.")
	}
	r.sql = pl.label() + pl.target
	r.statements = []string{r.sql}
	r.index = 0
	s := m.dbConsole()
	s.batchSeq++
	r.batch = s.batchSeq
	if !pl.analyze {
		r.readOnly = true
		return tea.Batch(note, m.databaseStartRun(r))
	}
	kind, _ := dbclient.Classify(pl.target, c.Driver)
	pl.wrap = kind != dbclient.KindRead
	switch {
	case kind == dbclient.KindDDL:
		return m.pushToastDetail(toast.Warn, "Analyze is not offered: this cannot be rolled back", "EXPLAIN ANALYZE would run the statement. x shows the plan without running it.")
	case pl.wrap && c.ReadOnly:
		return m.pushToastDetail(toast.Warn, sanitizeDBText(dbConnName(c))+" is read-only: analyze reads only", "EXPLAIN ANALYZE would run the statement. x shows the plan without running it.")
	case pl.wrap && m.databases.txOpen(dbRunKey(r)):
		return m.pushToastDetail(toast.Warn, "Commit or roll back first: a transaction is open", "The analyze rollback would undo your own changes too (K commits, Z rolls back).")
	}
	r.readOnly = !pl.wrap
	s.pending = r
	name := sanitizeDBText(dbConnName(c))
	title := "EXPLAIN ANALYZE runs the statement"
	body := dbSafeMultiline(dbCapLines(pl.target, 12))
	var notes []string
	if c.Environment == "production" {
		title = "Analyze on production (" + name + ")?"
		notes = append(notes, "EXPLAIN ANALYZE runs the statement on a production database. Load and locks are real.")
	}
	if pl.wrap {
		notes = append(notes, "A write is rolled back afterwards: it runs in a transaction that is always rolled back. Sequences and triggers can still act.")
	} else {
		notes = append(notes, "It runs in a read-only session. No data is changed.")
	}
	style := confirm.StylePrimary
	if pl.wrap || c.Environment == "production" {
		style = confirm.StyleDestructive
	}
	m.confirm = confirm.New(title, body+"\n\n"+strings.Join(notes, "\n"), []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "run", Title: "Run", Style: style},
	}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBExplain
	return note
}

// dbRunKey is the session key of a run.
func dbRunKey(r *dbRun) string {
	if r.sessKey != "" {
		return r.sessKey
	}
	return r.conn
}

// dbCapLines is s cut to n lines ("… N more lines").
func dbCapLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n… %d more lines", len(lines)-n)
}

// databaseExplainConfirm answers the analyze card.
func (m *Model) databaseExplainConfirm(id string) tea.Cmd {
	s := m.dbConsole()
	r := s.pending
	s.pending = nil
	if id == "run" && r != nil && r.plan != nil {
		return m.databaseStartRun(r)
	}
	return nil
}

// dbExplainExec asks the server for the plan. The forms come from
// dbclient.ExplainForms; a plan that is not analyzed or whose statement is
// a read goes through the read-only session (inside the open transaction
// when there is one); an analyzed write runs between Begin and a Rollback
// that always happens, and a failed rollback is an error, not a footnote.
func dbExplainExec(ctx context.Context, sess dbclient.Session, driver string, pl *dbPlanRun, inTx bool, limit int) dbclient.Result {
	maria := false
	if x, ok := sess.(interface{ MariaDB() bool }); ok {
		maria = x.MariaDB()
	}
	forms := dbclient.ExplainForms(driver, pl.target, pl.analyze, maria)
	if len(forms) == 0 {
		return dbclient.Result{Err: errors.New("this connection has no query plan")}
	}
	opt := dbclient.RunOptions{MaxRows: limit}
	var res dbclient.Result
	for i, form := range forms {
		switch {
		case pl.wrap:
			if err := sess.Begin(ctx); err != nil {
				return dbclient.Result{Err: err}
			}
			res = sess.Run(ctx, form, opt)
			if err := sess.Rollback(context.Background()); err != nil {
				// The statement ran and could not be undone: say so loudly.
				res.Err = fmt.Errorf("the statement ran but the rollback failed (%v): check the data", err)
			} else {
				res.Notices = append(res.Notices, "The statement ran inside a transaction that was rolled back. No data was changed.")
			}
			return res // never run a write twice: no second form
		case inTx:
			res = dbReadInTx(ctx, sess, form, opt)
		default:
			res = dbRunReadOnly(ctx, sess, form, opt)
		}
		if res.Err == nil || i == len(forms)-1 || ctx.Err() != nil {
			break
		}
	}
	return res
}

// databasePlanView writes the plan: one line per node, indented by depth
// (nvim folds by indent), the costly node marked and coloured.
func (m *Model) databasePlanView(p *dbPage, pl *dbPlanRun, width int) {
	p.fold = true
	costly := "  <- costly"
	for _, l := range pl.parsed.Lines {
		text := dbSafeMultiline(strings.ReplaceAll(strings.TrimSpace(l.Text), "\n", " "))
		line := " " + strings.Repeat("  ", l.Depth) + text
		group := ""
		switch {
		case l.Costly:
			line = "!" + line[1:] + costly // the "!" stays visible on a narrow page
			group = "TermocodeHttpStatusWarn"
		case l.Detail:
			group = "TermocodeHttpSummary"
		}
		n := p.add(line)
		if group != "" {
			p.hl(n, 0, len(line), group)
		}
	}
	if len(pl.parsed.Lines) == 0 {
		n := p.add(" (the server returned no plan)")
		p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpSummary")
	}
	if len(pl.parsed.Summary) > 0 {
		p.add("")
		for _, s := range pl.parsed.Summary {
			n := p.add(" " + dbSafeMultiline(s))
			p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpSummary")
		}
	}
	_ = width
}

// databasePlanRaw is the Raw view: the server's own answer (JSON indented).
func databasePlanRaw(p *dbPage, pl *dbPlanRun) {
	raw := strings.TrimSpace(pl.parsed.Raw)
	for _, l := range strings.Split(dbSafeMultiline(raw), "\n") {
		p.add(" " + l)
	}
}

// databasePlanSummary is row 2 of the Plan view.
func databasePlanSummary(r *dbRun) []string {
	pl := r.plan
	base := dbPlural(pl.parsed.Nodes(), "node")
	if pl.analyze {
		base = "Analyzed · " + base
	}
	ms := fmt.Sprintf(" · %d ms", r.result.Elapsed.Milliseconds())
	if pl.wrap {
		return []string{base + ms + " · rolled back", base + " · rolled back", base}
	}
	return []string{base + ms, base}
}

// databasePlanCopy copies the plan text.
func (m *Model) databasePlanCopy(pl *dbPlanRun) tea.Cmd {
	var b strings.Builder
	for _, l := range pl.parsed.Lines {
		b.WriteString(strings.Repeat("  ", l.Depth) + dbSafeMultiline(strings.ReplaceAll(l.Text, "\n", " ")) + "\n")
	}
	for _, s := range pl.parsed.Summary {
		b.WriteString(dbSafeMultiline(s) + "\n")
	}
	return m.copyWithToast(b.String(), "Copied the plan")
}

func dbIsSQLite(driver string) bool {
	_, ok := dbclient.DialectFor(driver).(dbclient.SQLiteDialect)
	return ok
}
