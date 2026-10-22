package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	tea "github.com/charmbracelet/bubbletea"
)

// SQL consoles (plan F4): one console file per connection in the user
// config, any project .sql file attached with "-- @conn <id>" or a picker
// once per session; the send key runs the statement at the cursor, the
// selection, or (Databases: Run File) every statement. Writes and anything
// the classifier is not sure of ask first, with the statement shown.

const (
	pickerKindDBAttach pickerKindEnum = 1250 + iota
	pickerKindDBHistory
	pickerKindDBSort
)
const (
	confirmKindDBRun confirmKindEnum = 1250 + iota
	confirmKindDBClearHistory
)
const promptKindDBResultFilter promptKindEnum = 1250

// dbRun is one statement sent from a console (or a history rerun). by is
// "user" or "ai" (the result page says "by AI"; DB-5 sets it).
type dbRun struct {
	conn, file, sql, by string
	line                int
	statements          []string
	index               int
	result              dbclient.Result
	sessKey             string       // the session that runs it, when not the connection's own
	maxRows             int          // 0: db_max_rows
	tv                  *dbTableView // a data view: the server pages it
	describe            *dbTable     // a describe page: rerun reads the catalogs again
	readOnly            bool         // run in a read-only session (the server refuses writes)
	tab                 *dbTab       // the tab it replaces (a page, a sort, a rerun); nil: a new result
	batch               uint64       // the send it belongs to (one tab per statement with rows)
	notes               []string     // lines added to Messages (what a submit did)
	plan                *dbPlanRun   // an EXPLAIN run (db_explain.go): the page shows the Plan view
	redis               *dbRedisView // a Redis value page (db_redis.go)
}

type dbConsoleState struct {
	lens     map[string]string // file + "\x00" + SQL → the lens status
	attached map[string]string // project .sql file → connection (session only)
	pending  *dbRun            // waits for the attach picker or the guard card
	waiting  *dbRun            // waits for its connection
	running  *dbRun
	shown    *dbRun // the result on the page
	cancel   context.CancelFunc
	gen      uint64         // bumps on each run; stale results are dropped
	pageGen  uint64         // bumps on each page write; stale batches are dropped
	tableGen uint64         // bumps on each table command; stale answers are dropped
	pushed   map[int]string // SQL buffer -> the schema stamp last pushed to it
	view     int
	page     int
	size     int // rows per page (db_page_rows, read at run time)
	filter   string
	sortCol  int
	desc     bool
	trans    bool // the Transposed view (one row as column | value)
	trow     int  // the row it shows, on the page
	cache    *dbclient.Result
	cacheIdx []int // the loaded row of each displayed row
	cacheKey dbViewKey
	history  []dbclient.HistoryEntry
	hist     *dbclient.History

	tabs         []*dbTab // result tabs (db_tabs.go)
	tab          int      // the tab on the page
	batchSeq     uint64
	keepCursor   bool // the next page write keeps the cursor where it is
	cursorNewRow bool // the next page write puts the cursor on the last new row
	// editing (db_edit.go)
	editTab                         *dbTab
	editAt                          dbEditPos
	pendingKey                      string // an edit key waiting for the key lookup
	pendingRow, pendingCol          int
	submitTab, submitting, staleTab *dbTab
	submitStmts                     []dbclient.EditStatement
	afterDiscard                    func() tea.Cmd
	contRun                         *dbRun // the next statement after a failed one (Continue)
	// export and import (db_export.go, db_import.go)
	exp *dbExport
	imp *dbImport
	job *dbJob
	// Redis value page writes (db_redis.go)
	redisEdit  *dbRedisEdit
	redisWrite *dbRedisWrite
}

func (m *Model) dbConsole() *dbConsoleState {
	if m.databases == nil {
		m.databases = newDBState()
	}
	if m.databases.console == nil {
		m.databases.console = &dbConsoleState{attached: map[string]string{}, lens: map[string]string{}, sortCol: -1}
	}
	return m.databases.console
}

func dbConfigPath(parts ...string) string {
	dir, err := configDir()
	if err != nil {
		return ""
	}
	return filepath.Join(append([]string{dir, "db"}, parts...)...)
}

// historyStore is the one history file handle (its mutex orders writes).
func (s *dbConsoleState) historyStore() *dbclient.History {
	if s.hist == nil {
		s.hist = &dbclient.History{Path: dbConfigPath("history.jsonl")}
	}
	s.hist.Max = loadDBConsoleSettings().historyMax()
	return s.hist
}

type dbConsoleSettings struct {
	MaxRows    *int `json:"db_max_rows"`
	PageRows   *int `json:"db_page_rows"`
	Timeout    *int `json:"db_timeout"`
	HistoryMax *int `json:"db_history_max"`
	AIMaxKB    *int `json:"db_ai_max_kb"`
	// Redis (db_redis.go)
	RedisSep     *string `json:"db_redis_separator"`
	RedisMaxKeys *int    `json:"db_redis_max_keys"`
}

func loadDBConsoleSettings() dbConsoleSettings {
	var s dbConsoleSettings
	if p, e := settingsConfigPath(); e == nil {
		if b, e := os.ReadFile(p); e == nil {
			_ = json.Unmarshal(b, &s)
		}
	}
	return s
}

func (s dbConsoleSettings) maxRows() int {
	n := intOr(s.MaxRows, 10000)
	if n < 1 {
		n = 10000
	}
	return min(n, 100000)
}

func (s dbConsoleSettings) pageRows() int {
	n := intOr(s.PageRows, 100)
	if n < 1 {
		n = 100
	}
	return min(n, 1000)
}

func (s dbConsoleSettings) timeout() time.Duration {
	n := intOr(s.Timeout, 30)
	if n < 1 {
		n = 30
	}
	return time.Duration(n) * time.Second
}

func (s dbConsoleSettings) historyMax() int {
	n := intOr(s.HistoryMax, 500)
	if n < 1 {
		n = 500
	}
	return n
}

func (s *dbConsoleState) pageSize() int {
	if s.size < 1 {
		return 100
	}
	return s.size
}

// databaseNewConsole opens (creates on first use) the console of a
// connection: db/consoles/<workspace hash>/<connection hash>.sql, 0600.
func (m *Model) databaseNewConsole(id string) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	m.ensureDatabasesLoaded()
	c, ok := m.databases.connection(id)
	if !ok {
		return nil
	}
	path := m.databaseConsolePath(id)
	if path == "" {
		return nil
	}
	header, filetype := "-- @conn "+id+"\n\n", "sql"
	if c.Driver == "redis" {
		path = strings.TrimSuffix(path, ".sql") + ".redis"
		header, filetype = "# @conn "+id+"\n# One command per line; the send key runs the line at the cursor.\n\n", "redis"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return m.dbConsoleError(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, err = f.WriteString(header)
		_ = f.Close()
	} else if os.IsExist(err) {
		err = nil
	}
	if err != nil {
		return m.dbConsoleError(err)
	}
	m.ensureDBLua()
	m.ensureEditorWindowCurrent()
	var buf int
	_ = m.nvim.ExecLuaResult(`local p,ft=...;vim.cmd('edit '..vim.fn.fnameescape(p)); vim.bo.filetype=ft; vim.cmd('normal! G'); return vim.api.nvim_get_current_buf()`, &buf, path, filetype)
	if buf > 0 {
		m.databaseBufferLens(buf) // the schema is there before the first key
	}
	m.focus = FocusEditor
	return m.fetchStateCmd()
}

// databaseConsolePath is db/consoles/<workspace hash>/<connection hash>.sql
// in the user config ("" when there is no config directory).
func (m *Model) databaseConsolePath(id string) string {
	rootHash := fmt.Sprintf("%x", sha256.Sum256([]byte(m.databaseWorkspaceRoot())))[:16]
	idHash := fmt.Sprintf("%x", sha256.Sum256([]byte(id)))[:12]
	return dbConfigPath("consoles", rootHash, idHash+".sql")
}

func (m *Model) dbConsoleError(err error) tea.Cmd {
	return m.pushToastDetail(toast.Errr, "Database", dbErrorText(err))
}

// dbErrorText is the one line of a driver error worth showing: the
// server's FATAL / ERROR line, or for a multi-line connect error (the
// first line names the target and ends with ":") the reason under it.
func dbErrorText(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "Cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "Stopped: took longer than db_timeout"
	}
	var lines []string
	for _, l := range strings.Split(dbRedact(err.Error()), "\n") {
		if t := strings.TrimSpace(l); t != "" {
			lines = append(lines, t)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	for _, l := range lines {
		for _, tag := range []string{"FATAL:", "ERROR:", "PANIC:"} {
			if i := strings.Index(l, tag); i >= 0 {
				return sanitizeDBText(l[i:])
			}
		}
	}
	if len(lines) > 1 && strings.HasSuffix(lines[0], ":") {
		reason := dbConnectTarget.ReplaceAllString(lines[len(lines)-1], "")
		if mt := dbDialReason.FindStringSubmatch(reason); mt != nil {
			reason = mt[1]
		}
		return sanitizeDBText(reason)
	}
	return sanitizeDBText(lines[0])
}

var (
	// "127.0.0.1:5432 (localhost): dial error: " before a connect reason.
	dbConnectTarget = regexp.MustCompile(`^\S+ \([^)]*\): (?:(?:dial|server|tls) error: )?`)
	// "dial tcp 127.0.0.1:5432: connect: connection refused".
	dbDialReason = regexp.MustCompile(`^dial \S+ \S+: (?:connect: )?(.+)$`)
)

// dbChangeSummary is the outcome of a statement without rows: "3 rows
// changed" for DML, "Done" for anything else (DDL, SET, …).
func dbChangeSummary(r *dbRun) string {
	switch strings.ToUpper(firstSQLWord(r.sql)) {
	case "INSERT", "UPDATE", "DELETE", "MERGE", "REPLACE", "UPSERT":
		return dbPlural(int(r.result.Affected), "row") + " changed"
	}
	if r.result.Affected > 0 {
		return dbPlural(int(r.result.Affected), "row") + " changed"
	}
	return "Done"
}

// databaseSend is the send key in a SQL buffer: the statement under the
// cursor, the visual selection as typed, or every statement (all).
func (m *Model) databaseSend(all bool) tea.Cmd { return m.databaseSendMode(all, nil) }

// databaseSendMode is databaseSend; a non-nil plan makes it an explain of
// the statement at the cursor (db_explain.go).
func (m *Model) databaseSendMode(all bool, plan *dbPlanRun) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	m.ensureDBLua()
	file, lines, ok := m.httpBufText(0)
	redisFile := ok && strings.EqualFold(lastExt(file), ".redis")
	if !ok || !strings.EqualFold(lastExt(file), ".sql") && !redisFile {
		return m.pushToastDetail(toast.Info, "Open a SQL file", "Press n on a database connection to open its console.")
	}
	if redisFile && plan != nil {
		return m.pushToastDetail(toast.Info, "No plan for Redis", "Redis runs each command as it is.")
	}
	m.ensureDatabasesLoaded()
	s := m.dbConsole()
	if s.running != nil {
		return m.pushToastDetail(toast.Warn, "A query is running", "Esc cancels it.")
	}
	var pos struct {
		Line      int    `json:"line"`
		Selection string `json:"selection"`
	}
	var raw string
	_ = m.nvim.ExecLuaResult(dbSelectionLua, &raw)
	_ = json.Unmarshal([]byte(raw), &pos)
	r := &dbRun{file: file, sql: strings.Join(lines, "\n"), line: max(pos.Line, 1), by: "user", plan: plan}
	switch {
	case all:
		r.line = 0
	case strings.TrimSpace(pos.Selection) != "":
		r.sql = pos.Selection
		r.line = -r.line
	}
	r.conn = s.consoleConn(file, lines)
	if _, ok := m.databases.connection(r.conn); !ok {
		r.conn = ""
	}
	if r.conn == "" {
		var items []picker.Item
		for _, c := range m.databases.conns {
			if (c.Driver == "redis") == redisFile && c.Problem == "" {
				items = append(items, picker.Item{ID: c.ID, Title: sanitizeDBText(dbConnName(c)), Hint: c.Driver})
			}
		}
		if len(items) == 0 {
			if redisFile {
				return m.pushToastDetail(toast.Info, "No Redis connection", "Add one in the Databases view first.")
			}
			return m.pushToastDetail(toast.Info, "No SQL connection", "Add one in the Databases view first.")
		}
		s.pending = r
		m.picker = picker.NewItems("Run this file on which connection?", items)
		m.picker.SetSize(m.w, m.h)
		m.pickerKind = pickerKindDBAttach
		m.pickerOpen = true
		return nil
	}
	return m.databasePrepareRun(r)
}

// databasePrepareRun splits r.sql with the connection's dialect and keeps
// the statement under the cursor (line > 0), the selection (line < 0) or
// all of them (line 0).
func (m *Model) databasePrepareRun(r *dbRun) tea.Cmd {
	c, ok := m.databases.connection(r.conn)
	if !ok {
		return m.pushToastDetail(toast.Warn, "Connection not found", sanitizeDBText(r.conn))
	}
	if c.Driver == "redis" {
		return m.databaseRedisPrepare(r)
	}
	if r.line > 0 {
		spans := dbStatementSpans(r.sql, c.Driver)
		if len(spans) == 0 {
			return m.pushToastDetail(toast.Info, "No SQL at the cursor", "")
		}
		chosen := spans[len(spans)-1]
		offset := 0
		for i, l := range strings.Split(r.sql, "\n") {
			if i+1 >= r.line {
				break
			}
			offset += len(l) + 1
		}
		for _, sp := range spans {
			if offset <= sp.end {
				chosen = sp
				break
			}
		}
		r.statements = []string{chosen.sql}
		r.line = chosen.line
	} else {
		sqls, err := dbclient.Split(r.sql, c.Driver)
		if err != nil {
			return m.dbConsoleError(err)
		}
		r.statements = sqls
		if r.line < 0 {
			r.line = -r.line
		} else if spans := dbStatementSpans(r.sql, c.Driver); len(spans) > 0 {
			r.line = spans[0].line
		}
	}
	if len(r.statements) == 0 {
		return m.pushToastDetail(toast.Info, "No SQL to run", "")
	}
	r.line = max(r.line, 1)
	r.index = 0
	r.sql = r.statements[0]
	if r.plan != nil {
		return m.databaseExplainStart(r)
	}
	s := m.dbConsole()
	s.batchSeq++
	r.batch = s.batchSeq
	return m.databaseReviewRun(r)
}

// databaseReviewRun runs a read at once; a write, DDL or an unknown
// statement asks first with the exact SQL (no "always allow").
func (m *Model) databaseReviewRun(r *dbRun) tea.Cmd {
	c, ok := m.databases.connection(r.conn)
	if !ok {
		return nil
	}
	s := m.dbConsole()
	if c.Driver == "redis" {
		return m.databaseRedisReview(r, c)
	}
	if m.databases.manualMode(r.conn) && dbTxControl(r.sql) {
		return m.pushToastDetail(toast.Info, "Use Commit (K) or Roll back (Z)", "Manual transaction mode: "+strings.ToUpper(firstSQLWord(r.sql))+" is not sent.")
	}
	kind, stmts := dbclient.Classify(r.sql, c.Driver)
	if kind == dbclient.KindRead {
		return m.databaseStartRun(r)
	}
	s.pending = r
	title, note := dbGuardText(c, kind, stmts)
	body := dbSafeMultiline(strings.TrimSpace(r.sql))
	if note != "" {
		body += "\n\n" + note
	}
	m.confirm = confirm.New(title, body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "run", Title: "Run", Style: confirm.StyleDestructive},
	}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBRun
	return nil
}

// dbGuardText is the guard card's title and its note under the SQL.
func dbGuardText(c dbclient.Conn, kind dbclient.Kind, stmts []dbclient.Statement) (string, string) {
	name := sanitizeDBText(dbConnName(c))
	var st dbclient.Statement
	if len(stmts) > 0 {
		st = stmts[0]
	}
	verb := strings.ToUpper(firstSQLWord(st.SQL))
	table := st.Table
	if table == "" {
		if mt := dbTargetTable.FindStringSubmatch(dbStripComments(st.SQL)); mt != nil {
			table = mt[1]
		}
	}
	table = sanitizeDBText(table)
	if table == "" {
		table = "the table"
	}
	note := ""
	switch {
	case (verb == "DELETE" || verb == "UPDATE") && !st.HasWhere:
		note = "Every row of " + table + " will change."
		if verb == "DELETE" {
			note = "Every row of " + table + " will go."
		}
	case verb == "DROP" || verb == "TRUNCATE":
		note = "This cannot be undone."
	}
	title := "Run this statement on " + name + "?"
	switch {
	case c.ReadOnly:
		title = name + " is read-only. Run anyway?"
	case c.Environment == "production":
		title = "Write to production (" + name + ")?"
	case (verb == "DELETE" || verb == "UPDATE") && !st.HasWhere:
		title = "Run " + verb + " without WHERE on " + name + "?"
	case verb == "DROP" || verb == "TRUNCATE" || verb == "ALTER":
		title = "Run " + verb + " on " + name + "?"
	case kind == dbclient.KindUnknown:
		title = "Run this statement on " + name + "? It may change data."
	}
	return title, note
}

// dbTargetTable finds the table a DELETE / UPDATE / TRUNCATE names.
var dbTargetTable = regexp.MustCompile("(?is)^\\s*(?:DELETE\\s+FROM|UPDATE|TRUNCATE(?:\\s+TABLE)?)\\s+([\\w.\"`\\[\\]]+)")

func firstSQLWord(sql string) string {
	for _, l := range strings.Split(sql, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		if f := strings.Fields(t); len(f) > 0 {
			return strings.Trim(f[0], "(;")
		}
	}
	return ""
}

// databaseStartRun connects first when needed (the run waits), then runs
// r on the connection's session in the background with a timeout.
func (m *Model) databaseStartRun(r *dbRun) tea.Cmd {
	open := m.revealDatabasesView(false) // sessions live while the view shows
	return tea.Batch(open, m.databaseStartRunRevealed(r))
}

func (m *Model) databaseStartRunRevealed(r *dbRun) tea.Cmd {
	s := m.dbConsole()
	if m.dbSessionBusy() {
		return m.pushToastDetail(toast.Warn, "An import is running", "Esc cancels it first.")
	}
	s.pending = nil
	sess := m.databases.sessions[r.conn]
	if r.sessKey != "" {
		sess = m.databases.sessions[r.sessKey]
	}
	if sess == nil {
		s.waiting = r
		cmd := m.databaseConnect(r.conn)
		if !m.databases.nodes[r.conn].loading {
			s.waiting = nil // no connect started (a card or an error came instead)
		}
		return cmd // onDBConnect starts the run
	}
	s.waiting = nil
	s.gen++
	gen := s.gen
	settings := loadDBConsoleSettings()
	ctx, cancel := context.WithTimeout(context.Background(), settings.timeout())
	s.cancel = cancel
	s.running = r
	old := s.shown
	s.placeRun(r)
	s.page = 0
	s.size = settings.pageRows()
	s.view = dbViewGrid
	s.filter = ""
	s.sortCol = -1
	if r.tv == nil || old == nil || old.tv != r.tv {
		s.trans, s.trow = false, 0
	}
	m.databaseRefreshLens(r.file, r.sql, "running…")
	pageCmd := m.databaseShowPage()
	limit := settings.maxRows()
	if r.maxRows > 0 {
		limit = r.maxRows
	}
	key := r.sessKey
	if key == "" {
		key = r.conn
	}
	inTx := m.databases.txOpen(key)
	// Manual mode: a statement the user runs begins the transaction (or
	// joins it); the data view's reads only look inside an open one.
	manual := m.databases.manualMode(r.conn) && !r.readOnly && r.tv == nil && r.plan == nil
	driver := ""
	if c, ok := m.databases.connection(r.conn); ok {
		driver = c.Driver
	}
	redisCount := settings.pageRows()
	return tea.Batch(pageCmd, func() tea.Msg {
		var result dbclient.Result
		began := false
		switch {
		case r.redis != nil:
			var view *dbRedisView
			result, view = dbRedisLoad(ctx, sess, *r.redis, redisCount)
			return dbRunMsg{gen: gen, run: r, result: result, key: key, redis: view}
		case r.plan != nil:
			result = dbExplainExec(ctx, sess, driver, r.plan, inTx, limit)
		case r.readOnly && inTx:
			result = dbReadInTx(ctx, sess, r.sql, dbclient.RunOptions{MaxRows: limit})
		case r.readOnly:
			result = dbRunReadOnly(ctx, sess, r.sql, dbclient.RunOptions{MaxRows: limit})
		default:
			if manual && !inTx {
				// The transaction outlives this run: never a context that ends.
				if err := sess.Begin(context.Background()); err != nil {
					return dbRunMsg{gen: gen, run: r, result: dbclient.Result{Err: err}, key: key}
				}
				began = true
			}
			result = sess.Run(ctx, r.sql, dbclient.RunOptions{MaxRows: limit})
		}
		return dbRunMsg{gen: gen, run: r, result: result, key: key, began: began, manual: manual}
	})
}

type dbRunMsg struct {
	gen    uint64
	run    *dbRun
	result dbclient.Result
	key    string       // the session it ran on
	began  bool         // it began the manual transaction
	manual bool         // it ran in manual transaction mode
	redis  *dbRedisView // a value page: what it loaded
}

func (m *Model) databaseFinishRun(msg dbRunMsg) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	s := m.dbConsole()
	if s.gen != msg.gen || s.running != msg.run {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.running = nil
	txCmd := m.databaseTrackTx(msg)
	msg.run.result = msg.result
	if msg.redis != nil {
		msg.run.redis = msg.redis
	}
	if pl := msg.run.plan; pl != nil && msg.result.Err == nil {
		driver := ""
		if c, ok := m.databases.connection(msg.run.conn); ok {
			driver = c.Driver
		}
		pl.parsed = dbclient.ParsePlan(driver, msg.result, pl.analyze)
	}
	if len(msg.run.notes) > 0 {
		msg.run.result.Notices = append(append([]string(nil), msg.run.notes...), msg.run.result.Notices...)
	}
	if tv := msg.run.tv; tv != nil {
		tv.finish(&msg.run.result, s.pageSize())
	}
	s.shown = msg.run
	s.cache = nil
	res := msg.run.result
	status := ""
	switch {
	case res.Err != nil:
		status = dbGlyphFail() + " " + dbErrorText(res.Err)
		s.view = dbViewMessages
	case len(res.Columns) == 0:
		status = fmt.Sprintf("%s · %d ms", dbChangeSummary(msg.run), res.Elapsed.Milliseconds())
		s.view = dbViewMessages
	default:
		status = fmt.Sprintf("%s · %d ms", dbPlural(len(res.Rows), "row"), res.Elapsed.Milliseconds())
	}
	if msg.manual {
		status += m.dbTxStatus(msg.run.conn)
	}
	m.databaseRefreshLens(msg.run.file, msg.run.sql, status)
	cmds := []tea.Cmd{m.databaseShowPage(), txCmd}
	if msg.run.tv == nil && msg.run.redis == nil { // a data view or a value page is not something the user typed
		cmds = append(cmds, m.databaseAppendHistory(msg.run))
	}
	if msg.run.index+1 < len(msg.run.statements) {
		next := *msg.run
		next.index++
		next.sql = next.statements[next.index]
		next.result = dbclient.Result{}
		next.tab, next.notes = nil, nil
		if res.Err != nil {
			// Stop at the error; the card offers to go on with the rest.
			s.contRun = &next
			left := len(next.statements) - next.index
			m.confirm = confirm.New(fmt.Sprintf("Statement %d of %d failed", msg.run.index+1, len(msg.run.statements)),
				dbErrorText(res.Err)+"\n\nContinue runs the other "+dbPlural(left, "statement")+".", []confirm.Button{
					{ID: "stop", Title: "Stop"},
					{ID: "continue", Title: "Continue"},
				})
			m.confirm.SetSize(m.w, m.h)
			m.confirmOpen = true
			m.confirmKind = confirmKindDBContinue
			return tea.Sequence(cmds...)
		}
		gen := msg.gen
		cmds = append(cmds, func() tea.Msg { return dbNextMsg{gen: gen, run: &next} })
	}
	return tea.Sequence(cmds...)
}

// databaseTrackTx keeps the transaction state of a finished run: a run
// that began the transaction opens it, a change counts as uncommitted, a
// session that lost its transaction (sqlite3 restarted) closes it.
func (m *Model) databaseTrackTx(msg dbRunMsg) tea.Cmd {
	if msg.key == "" {
		return nil
	}
	var cmd tea.Cmd
	d := m.databases
	if msg.began {
		d.tx(msg.key).open = true
	}
	for _, n := range msg.result.Notices {
		if strings.Contains(n, "transaction was rolled back") && d.txOpen(msg.key) {
			delete(d.txs, msg.key)
			cmd = m.pushToastDetail(toast.Warn, "The open transaction was rolled back", sanitizeDBText(n))
		}
	}
	if msg.manual && msg.result.Err == nil && d.txOpen(msg.key) {
		c, _ := d.connection(msg.run.conn)
		if kind, _ := dbclient.Classify(msg.run.sql, c.Driver); kind != dbclient.KindRead {
			d.tx(msg.key).writes++
		}
	}
	d.touch()
	return cmd
}

// databaseAppendHistory writes the run to db/history.jsonl off the UI
// goroutine (the SQL redacted, capped at 4 KB by the store).
func (m *Model) databaseAppendHistory(r *dbRun) tea.Cmd {
	store := m.dbConsole().historyStore()
	e := dbclient.HistoryEntry{ID: fmt.Sprint(time.Now().UnixNano()), Conn: r.conn, SQL: dbRedact(r.sql), Kind: r.by, Rows: len(r.result.Rows), ElapsedMS: r.result.Elapsed.Milliseconds()}
	if len(r.result.Columns) == 0 {
		e.Rows = int(r.result.Affected)
	}
	return func() tea.Msg {
		if err := store.Append(e); err != nil {
			return dbHistoryErrMsg{err: err}
		}
		return nil
	}
}

type dbHistoryErrMsg struct{ err error }

type dbNextMsg struct {
	gen uint64
	run *dbRun
}

func (m *Model) databaseCancelRun() tea.Cmd {
	if m.databases == nil || m.databases.console == nil {
		return nil
	}
	if s := m.databases.console; s.cancel != nil {
		s.cancel()
	}
	m.databaseJobCancel()
	return nil
}

// stop drops the running and waiting runs: the view hides (conn "") or
// the connection conn closes.
func (s *dbConsoleState) stop(conn string) {
	if s == nil {
		return
	}
	s.tableGen++ // answers still on their way (describe, count, columns) are stale now
	if s.job != nil && (conn == "" || s.job.conn == conn) {
		s.job.cancel() // a transfer ends with the view or its connection
	}
	if conn != "" && (s.running == nil || s.running.conn != conn) && (s.waiting == nil || s.waiting.conn != conn) {
		return
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.gen++
	s.running = nil
	s.waiting = nil
	s.pending = nil
}

func (m *Model) databaseConsoleConfirm(id string) (tea.Cmd, bool) {
	if cmd, ok := m.databaseTransferConfirm(id); ok {
		return cmd, true
	}
	switch m.confirmKind {
	case confirmKindDBExplain:
		return m.databaseExplainConfirm(id), true
	case confirmKindDBRun:
		s := m.dbConsole()
		r := s.pending
		s.pending = nil
		if id == "run" && r != nil {
			return m.databaseStartRun(r), true
		}
		return nil, true
	case confirmKindDBSubmit, confirmKindDBDiscard, confirmKindDBRevertAll, confirmKindDBStale, confirmKindDBTxEnd, confirmKindDBContinue:
		return m.databaseEditConfirm(id)
	case confirmKindDBClearHistory:
		if id == "clear" {
			if e := m.dbConsole().historyStore().Clear(); e != nil {
				return m.dbConsoleError(e), true
			}
			return m.pushToast(toast.Info, "SQL history cleared"), true
		}
		return nil, true
	}
	return nil, false
}

func (m *Model) databaseConsolePicker(id string) (tea.Cmd, bool) {
	if cmd, ok := m.databaseTransferPicker(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseComparePicker(id); ok {
		return cmd, true
	}
	switch m.pickerKind {
	case pickerKindDBAttach, pickerKindDBHistory, pickerKindDBSort:
	case pickerKindDBTxMode:
		return m.databaseTxPicker(id)
	default:
		return nil, false
	}
	s := m.dbConsole()
	switch m.pickerKind {
	case pickerKindDBAttach:
		r := s.pending
		s.pending = nil
		if r == nil {
			return nil, true
		}
		r.conn = id
		s.attached[r.file] = id
		m.databaseRefreshLens(r.file, "", "")
		return m.databasePrepareRun(r), true
	case pickerKindDBHistory:
		if id == "clear" {
			m.confirm = confirm.New("Clear SQL history?", fmt.Sprintf("Delete %d history entries?", len(s.history)), []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "clear", Title: "Delete", Style: confirm.StyleDestructive}})
			m.confirm.SetSize(m.w, m.h)
			m.confirmOpen = true
			m.confirmKind = confirmKindDBClearHistory
			return nil, true
		}
		for _, e := range s.history {
			if e.ID == id {
				return m.databasePrepareRun(&dbRun{conn: e.Conn, sql: e.SQL, by: "user"}), true
			}
		}
		return nil, true
	default: // pickerKindDBSort
		var col int
		if _, err := fmt.Sscan(id, &col); err != nil {
			return nil, true
		}
		if s.shown != nil && s.shown.tv != nil {
			if col < 0 || col >= len(s.shown.result.Columns) {
				return nil, true
			}
			tv, name := s.shown.tv, s.shown.result.Columns[col].Name
			return m.databaseUnlessDirty(func() tea.Cmd { return m.databaseTableSort(tv, name) }), true
		}
		if s.sortCol == col {
			s.desc = !s.desc
		} else {
			s.desc = false
		}
		s.sortCol = col
		s.page = 0
		return m.databaseShowPage(), true
	}
}

// databaseHistory opens the history picker (newest first).
func (m *Model) databaseHistory() tea.Cmd {
	s := m.dbConsole()
	entries, err := s.historyStore().List()
	if err != nil {
		return m.dbConsoleError(err)
	}
	s.history = entries
	items := make([]picker.Item, 0, len(entries)+1)
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		hint := sanitizeDBText(e.Conn) + " · " + e.Time.Format("01-02 15:04") + fmt.Sprintf(" · %d ms", e.ElapsedMS)
		items = append(items, picker.Item{ID: e.ID, Title: dbOneLine(e.SQL), Hint: hint})
	}
	if len(entries) > 0 {
		items = append(items, picker.Item{ID: "clear", Title: "Clear history…"})
	}
	if len(items) == 0 {
		return m.pushToastDetail(toast.Info, "No SQL history yet", "Run a statement from a console first.")
	}
	m.picker = picker.NewItems("SQL history · Enter runs again", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBHistory
	m.pickerOpen = true
	return nil
}
