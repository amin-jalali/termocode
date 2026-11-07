package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Search Data (DBX-34): Databases: Search Data… on a schema, a database, a
// table or a connection → the value → every table of that schema gets one
// bounded SELECT in a read-only session (the value is a bound parameter) →
// a progress card (Esc or Cancel stops it) → a list of the tables with
// matching rows; Enter opens the table's data view filtered to the rows.

const (
	promptKindDBSearchValue promptKindEnum = 5340
	pickerKindDBSearchHits  pickerKindEnum = 5340
)

// dbSearchMaxHits stops a search after this many matching rows in all.
const dbSearchMaxHits = 2000

// dbSearchTarget is the schema (MySQL: database) a search looks in.
type dbSearchTarget struct {
	conn, driver, schema string
	path                 string // the tree node the schema belongs to
	sessKey              string
}

func (t dbSearchTarget) label() string {
	if t.schema == "" {
		return sanitizeDBText(t.conn)
	}
	return sanitizeDBText(t.conn) + " · " + sanitizeDBText(t.schema)
}

// dbSearchResult is a finished search: the report and the columns of each
// searched table (to filter the data view of a hit).
type dbSearchResult struct {
	tgt    dbSearchTarget
	opt    dbclient.DataSearchOptions
	report dbclient.DataSearchReport
	cols   map[string][]string // table → the columns searched
	tables int                 // tables in the schema (before the cap)
}

type dbSearchDoneMsg struct {
	job *dbJob
	res dbSearchResult
	err error
}

// dbSearchParse reads the typed value: "=v" is an exact match of v, any
// other text is "contains, any case".
func dbSearchParse(value string) dbclient.DataSearchOptions {
	v := strings.TrimSpace(value)
	if strings.HasPrefix(v, "=") && len(v) > 1 {
		return dbclient.DataSearchOptions{Value: v[1:], Exact: true}
	}
	return dbclient.DataSearchOptions{Value: v}
}

// dbSearchTargetAt is the schema a tree row belongs to.
func dbSearchTargetAt(s *dbState, row dbTreeRow) (dbSearchTarget, error) {
	c, ok := s.connection(row.conn)
	if !ok || row.conn == "" {
		return dbSearchTarget{}, errors.New("select a schema, a database or a table first")
	}
	if dbclient.DialectFor(c.Driver) == nil {
		return dbSearchTarget{}, errDBNotSQL
	}
	t := dbSearchTarget{conn: c.ID, driver: c.Driver}
	_, pg := dbclient.DialectFor(c.Driver).(dbclient.PostgresDialect)
	upTo := func(marker string) string { // the path up to and with the node after marker
		p := strings.Split(row.path, "/")
		for i, v := range p {
			if v == marker && i+1 < len(p) {
				return strings.Join(p[:i+2], "/")
			}
		}
		return ""
	}
	switch row.kind {
	case dbNodeConnection:
		t.path = row.path
		switch {
		case pg:
			t.schema = "public"
		case c.Driver == "mysql":
			t.schema = c.Database
		default:
			t.schema = "main"
		}
	case dbNodeDatabase, dbNodeSchema, dbNodeGroup, dbNodeTable, dbNodeView, dbNodeColumn, dbNodeRoutine, dbNodeSequence:
		if pg {
			t.schema = dbAncestorSchema(row.path)
			t.path = upTo("schema")
			if t.schema == "" { // a database row: its public schema
				t.schema, t.path = "public", upTo("db")
			}
		} else {
			t.schema = dbAncestorDatabase(row.path)
			t.path = upTo("db")
		}
		if t.path == "" {
			t.path = row.path
		}
	default:
		return dbSearchTarget{}, errors.New("select a schema, a database or a table first")
	}
	t.sessKey = s.sessionKeyFor(t.path, t.conn)
	return t, nil
}

// dbDataSearch lists the tables of the target, reads their columns and runs
// the bounded search in one read-only session. progress gets the tables
// searched and the tables to search.
func dbDataSearch(ctx context.Context, sess dbclient.Session, tgt dbSearchTarget, opt dbclient.DataSearchOptions, progress func(done, total int)) (dbSearchResult, error) {
	res := dbSearchResult{tgt: tgt, opt: opt, cols: map[string][]string{}}
	ro, err := sess.ReadOnlySession()
	if err != nil {
		return res, err
	}
	defer ro.Close()
	db := dbAncestorDatabase(tgt.path)
	if tgt.driver == "mysql" {
		db = ""
	}
	cat, err := ro.Introspect(ctx, dbclient.IntrospectionRequest{Database: db, Schema: tgt.schema})
	if err != nil {
		return res, err
	}
	var tables []dbclient.DataSearchTable
	for _, o := range cat.Objects {
		if strings.Contains(strings.ToLower(o.Kind), "view") {
			continue // a view repeats the rows of its tables
		}
		res.tables++
		if len(tables) == 200 {
			continue
		}
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		ir, err := ro.Introspect(ctx, dbclient.IntrospectionRequest{Database: db, Schema: tgt.schema, Object: o.Name, Kind: "table"})
		if err != nil {
			res.report.Errors = append(res.report.Errors, dbclient.DataSearchError{Schema: tgt.schema, Table: o.Name, Err: err})
			continue
		}
		var cols []string
		for _, c := range ir.Columns {
			cols = append(cols, c.Name)
		}
		res.cols[o.Name] = cols
		tables = append(tables, dbclient.DataSearchTable{Schema: tgt.schema, Table: o.Name, Columns: cols})
	}
	queries, err := dbclient.DataSearchQueries(tgt.driver, tables, opt)
	if err != nil {
		return res, err
	}
	failed := res.report.Errors
	done := 0
	if progress != nil {
		progress(0, len(queries))
	}
	res.report = dbclient.RunDataSearch(ctx, queries, dbSearchMaxHits, func(ctx context.Context, q dbclient.DataSearchQuery) (int, error) {
		r := ro.Run(ctx, q.SQL, dbclient.RunOptions{MaxRows: dataSearchRows(opt), Args: q.Args})
		done++
		if progress != nil {
			progress(done, len(queries))
		}
		return len(r.Rows), r.Err
	})
	res.report.Errors = append(failed, res.report.Errors...)
	if res.report.Canceled {
		return res, context.Canceled
	}
	return res, nil
}

func dataSearchRows(opt dbclient.DataSearchOptions) int {
	if opt.RowsPerTable > 0 {
		return min(opt.RowsPerTable, 1000)
	}
	return 50
}

// dbSearchHitItems is the hit list: one row per table with matches.
func dbSearchHitItems(res dbSearchResult) []picker.Item {
	var items []picker.Item
	limit := dataSearchRows(res.opt)
	for _, h := range res.report.Hits {
		name := sanitizeDBText(h.Table)
		if h.Schema != "" {
			name = sanitizeDBText(h.Schema) + "." + name
		}
		rows := dbPlural(h.Rows, "row")
		if h.Rows >= limit {
			rows = strconv.Itoa(limit) + "+ rows"
		}
		items = append(items, picker.Item{ID: h.Table, Title: name, Hint: rows + " · Enter opens them"})
	}
	return items
}

// dbSearchWhere is the data view filter that shows a hit's rows: the same
// test as the search, with the value as a quoted literal.
func dbSearchWhere(driver string, cols []string, opt dbclient.DataSearchOptions) string {
	d := dbclient.DialectFor(driver)
	if d == nil {
		return ""
	}
	cast := "TEXT"
	if driver == "mysql" {
		cast = "CHAR"
	}
	v := opt.Value
	if !opt.Exact {
		v = "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(v) + "%"
	}
	lit := dbclient.SQLLiteral(d, dbclient.Cell{Text: v})
	conds := make([]string, 0, len(cols))
	for _, c := range cols {
		col := "CAST(" + d.QuoteIdent(c) + " AS " + cast + ")"
		if opt.Exact {
			conds = append(conds, col+" = "+lit)
		} else {
			conds = append(conds, "LOWER("+col+") LIKE LOWER("+lit+") ESCAPE '!'")
		}
	}
	return strings.Join(conds, " OR ")
}

// databaseSearchStart is Databases: Search Data…: the target, then the value.
func (m *Model) databaseSearchStart() tea.Cmd {
	m.ensureDatabasesLoaded()
	row, ok := m.databaseCurrentRow()
	if !ok {
		return m.pushToastDetail(toast.Info, "Select a schema", "Move to a schema, a database or a table in the tree first.")
	}
	tgt, err := dbSearchTargetAt(m.databases, row)
	if err != nil {
		return m.pushToastDetail(toast.Info, "Cannot search here", sanitizeDBText(err.Error()))
	}
	if m.databases.sessions[tgt.sessKey] == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	s := m.dbConsole()
	if s.job != nil {
		return m.pushToastDetail(toast.Warn, "Another job is running", "Esc cancels it.")
	}
	s.search = &tgt
	return m.openHTTPPrompt(promptKindDBSearchValue, "Search Data in "+tgt.label(), "Value to find (any case; =value for an exact match):", "")
}

// databaseSearchRun starts the search off the UI goroutine.
func (m *Model) databaseSearchRun(value string) tea.Cmd {
	s := m.dbConsole()
	if s.search == nil {
		return nil
	}
	tgt := *s.search
	s.search = nil
	opt := dbSearchParse(value)
	if opt.Value == "" {
		return m.pushToastDetail(toast.Info, "Nothing to search", "Type a value.")
	}
	sess := m.databases.sessions[tgt.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	if s.job != nil {
		return m.pushToastDetail(toast.Warn, "Another job is running", "Esc cancels it.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	job := &dbJob{kind: "search", title: "Searching " + tgt.label() + " for " + sanitizeDBText(dbOneLine(opt.Value)), conn: tgt.conn, cancel: cancel}
	s.job = job
	m.databaseJobCard(job)
	run := func() tea.Msg {
		defer cancel()
		res, err := dbDataSearch(ctx, sess, tgt, opt, func(done, total int) {
			job.of.Store(int64(total))
			job.done.Store(int64(done))
		})
		return dbSearchDoneMsg{job: job, res: res, err: err}
	}
	return tea.Batch(run, dbJobTickCmd(job))
}

func (m *Model) onDBSearchDone(msg dbSearchDoneMsg) tea.Cmd {
	s := m.dbConsole()
	if s.job != msg.job {
		return nil
	}
	s.job = nil
	m.closeJobCard()
	switch {
	case errors.Is(msg.err, context.Canceled):
		return m.pushToastDetail(toast.Info, "Search cancelled", fmt.Sprintf("%s searched before it stopped.", dbPlural(msg.res.report.Searched, "table")))
	case msg.err != nil:
		return m.pushToastDetail(toast.Errr, "Search failed", dbErrorText(msg.err))
	}
	rep := msg.res.report
	note := dbPlural(rep.Searched, "table") + " searched"
	if msg.res.tables > rep.Searched+len(rep.Errors) {
		note += fmt.Sprintf(" (of %d)", msg.res.tables)
	}
	if len(rep.Errors) > 0 {
		note += " · " + dbPlural(len(rep.Errors), "table") + " could not be read"
	}
	if rep.Truncated {
		note += " · stopped at " + strconv.Itoa(dbSearchMaxHits) + " rows"
	}
	if len(rep.Hits) == 0 {
		return m.pushToastDetail(toast.Info, "No match", note)
	}
	r := msg.res
	s.hits = &r
	m.picker = picker.NewItems("Matches in "+msg.res.tgt.label()+" · "+note, dbSearchHitItems(r))
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBSearchHits
	m.pickerOpen = true
	return nil
}

// databaseSearchOpenHit opens the data view of a hit's table, filtered to
// the matching rows.
func (m *Model) databaseSearchOpenHit(table string) tea.Cmd {
	s := m.dbConsole()
	res := s.hits
	if res == nil {
		return nil
	}
	if s.running != nil {
		return m.pushToastDetail(toast.Warn, "A query is running", "Esc cancels it.")
	}
	tgt := res.tgt
	sess := m.databases.sessions[tgt.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	t := dbTable{conn: tgt.conn, driver: tgt.driver, schema: tgt.schema, name: table, path: tgt.path + "/Tables/" + table, sessKey: tgt.sessKey}
	if tgt.driver == "sqlite" && t.schema == "" {
		t.schema = "main"
	}
	tv := newDBTableView(t)
	tv.where = dbSearchWhere(tgt.driver, res.cols[table], res.opt)
	if err := dbWhereCheck(tgt.driver, tv.where); err != nil {
		return m.pushToastDetail(toast.Warn, "Cannot open the rows", sanitizeDBText(err.Error()))
	}
	gen := m.nextTableGen()
	open := m.revealDatabasesView(false)
	return tea.Batch(open, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbMetaTimeout)
		defer cancel()
		ir, err := sess.Introspect(ctx, t.request())
		return dbTableMetaMsg{gen: gen, tv: tv, meta: dbTableMeta{ir: ir}, err: err}
	})
}
