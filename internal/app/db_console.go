package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/nvim"
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
	pickerKindDBHistoryDelete
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
	bound               bool         // its query parameters are filled (db_params.go)
	boundSQL            string       // sql with the driver's placeholders
	args                []any        // the values bound to them
	cursor              int          // caret byte offset + 1 in sql (0: unknown) (DBX-25)
	mode                string       // execution mode (dbclient.ExecMode names)
	selOne              bool         // send the selection as one text
	reviewed            bool         // its exact SQL was shown on a card already (the table designer)
}

type dbConsoleState struct {
	lens       map[string]string // file + "\x00" + SQL → the lens status
	attached   map[string]string // project .sql file → connection (saved ones from .termocode/sql-connections.json)
	switchFile string            // the file Switch File Connection… is for (db_sqllang.go)
	intent     *dbIntentTarget   // the statement SQL Intentions… was opened on (db_sqlintent.go)
	pending    *dbRun            // waits for the attach picker or the guard card
	waiting    *dbRun            // waits for its connection
	running    *dbRun
	shown      *dbRun // the result on the page
	stopConn   string // the connection Cancel / End Session asks an id for
	stopEnd    bool   // End Session (else Cancel Session Query)
	aiConn     string // the connection AI Generate SQL asks about (db_ai.go)
	cancel     context.CancelFunc
	gen        uint64         // bumps on each run; stale results are dropped
	pageGen    uint64         // bumps on each page write; stale batches are dropped
	tableGen   uint64         // bumps on each table command; stale answers are dropped
	genTable   *dbTable       // Generate Test Data: the table while the row count is asked
	pushed     map[int]string // SQL buffer -> the schema stamp last pushed to it
	view       int
	page       int
	size       int // rows per page (db_page_rows, read at run time)
	filter     string
	sortCol    int
	desc       bool
	trans      bool // the Transposed view (one row as column | value)
	trow       int  // the row it shows, on the page
	cache      *dbclient.Result
	cacheIdx   []int // the loaded row of each displayed row
	cacheKey   dbViewKey
	history    []dbclient.HistoryEntry
	hist       *dbclient.History
	rename     *dbRenameState      // the SQL rename being asked for (db_rename.go)
	askCands   []dbclient.StmtSpan // the Run… choices (db_execmode.go)

	// Search Data (db_datasearch.go): the schema while the value is asked,
	// then the last search's hits (the hit picker opens them).
	search *dbSearchTarget
	hits   *dbSearchResult
	heat   bool            // M: the grid colours numeric cells by value (db_visualize.go)
	folder *dbFolderExport // Export Tables to Folder while the folder is asked (db_export_more.go)

	// DBX-37 (db_dump.go): a SQL dump and an Import as New Table while
	// their file and card are open.
	dump     *dbDump
	newTable *dbNewTable

	roles    dbRolesState // the open users-and-roles flow (DBX-18)
	dumpTool *dbDumpState // the dump / restore being prepared (DBX-19)

	tabs         []*dbTab // result tabs (db_tabs.go)
	tab          int      // the tab on the page
	batchSeq     uint64
	keepCursor   bool                // the next page write keeps the cursor where it is
	cursorNewRow bool                // the next page write puts the cursor on the last new row
	gotoRow      int                 // the next page write puts the cursor on this page row + 1 (0: no)
	rowNums      *bool               // I: row numbers on/off (nil: db_row_numbers) (db_result_extra.go)
	colTypes     *bool               // T: column types in the header (nil: db_column_types)
	inline       map[string][]string // file + "\x00" + SQL → the inline result lines (db_inline_result)
	inlineSeen   bool                // an inline result was drawn once (the lens refresh then redraws them)
	beside       *dbTab              // the tab drawn next to the page's grid (db_result_beside.go)
	menuRow      int                 // the cell the cell menu is for (m)
	menuCol      int
	// editing (db_edit.go)
	editTab                         *dbTab
	editAt                          dbEditPos
	editMore                        []dbEditPos // V then e: the other cells that get the same value
	editArray                       bool        // the buffer holds an array, one element per line (DBX-55)
	lobTab                          *dbTab      // gS / gL: the cell whose value goes to or comes from a file (db_edit_lob.go)
	lobAt                           dbEditPos
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
	// copy table data between connections (db_copy_table.go)
	copyTbl *dbCopyTable
	// Redis value page writes (db_redis.go)
	redisEdit  *dbRedisEdit
	redisWrite *dbRedisWrite
	redisMore  *dbRedisMore // a new-key / export / listen flow in progress (DBX-51)
	pgtapConn  string       // the connection a pgTAP pattern prompt is for (DBX-52)
	pgtapRun   *dbPgTAPRun  // the pgTAP run waiting for its card
	// query parameters (db_params.go)
	paramAsk *dbParamAsk
	params   map[string]map[string]string // console file → name → last value
	// the grid's column layouts (db_columns.go)
	colStore *dbColStore
	// saved queries (db_saved.go, DBX-6)
	saved     *dbclient.SavedQueries
	savedList []dbclient.SavedQuery // what the saved-queries picker shows
	saveDraft *dbSaveDraft          // the query waiting for its name
	// foreign key navigation (db_fknav.go)
	fk dbFKState
	// the filter builder (db_where_builder.go)
	whereDraft *dbWhereDraft
	// data compare (db_data_compare.go)
	dataCmp dbDataCompareState
}

func (m *Model) dbConsole() *dbConsoleState {
	if m.databases == nil {
		m.databases = newDBState()
	}
	if m.databases.console == nil {
		m.databases.console = &dbConsoleState{attached: map[string]string{}, lens: map[string]string{}, sortCol: -1}
		m.databases.console.mergeSQLAttach(m.databases.root)
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
	// Display formats (db_display.go, DBX-27)
	NumberFormat *string `json:"db_number_format"`
	DateFormat   *string `json:"db_date_format"`
	TimeFormat   *string `json:"db_time_format"`
	TimeZone     *string `json:"db_time_zone"`
	BoolStyle    *string `json:"db_bool_style"`
	AltRows      *bool   `json:"db_alternate_rows"`
	LOBBytes     *int    `json:"db_lob_bytes"`
	GeometryFmt  *string `json:"db_geometry_format"`
	// Execution modes (db_execmode.go, DBX-25)
	ExecMode       *string `json:"db_exec_mode"`
	SelectionSplit *bool   `json:"db_selection_split"`
	// Result page (db_result_more.go, DBX-26)
	ResultNewTab  *bool `json:"db_result_new_tab"`
	AutoTranspose *bool `json:"db_auto_transpose"`
	RowNumbers    *bool `json:"db_row_numbers"`
	ColumnTypes   *bool `json:"db_column_types"`
	InlineResult  *bool `json:"db_inline_result"`
	// Introspection (db_introspect.go, DBX-29)
	IntrospectLevel  *string `json:"db_introspect_level"`
	CompleteCatalogs *bool   `json:"db_complete_catalogs"`
	// Export settings (db_export_more.go, DBX-36)
	CSVSeparator *string  `json:"db_csv_separator"`
	CSVHeader    *bool    `json:"db_csv_header"`
	CSVNull      *string  `json:"db_csv_null"`
	ExportMask   []string `json:"db_export_mask"`
	// SQL files (db_sqllang_more.go, DBX-41)
	SyntaxOff []string `json:"db_sql_syntax_off"`
}

// exportMask is the column name patterns whose values exports and copies hide.
func (s dbConsoleSettings) exportMask() []string {
	var out []string
	for _, p := range s.ExportMask {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
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
		return loadDBConsoleSettings().pageRows()
	}
	return s.size
}

// databaseNewConsole opens (creates on first use) the console of a
// connection: db/consoles/<workspace hash>/<connection hash>.sql, 0600.
func (m *Model) databaseNewConsole(id string) tea.Cmd {
	return m.databaseNewConsoleAt(id, "")
}

// databaseNewConsoleAt opens the console file at path ("" = the
// connection's first console; DBX-6 opens more).
func (m *Model) databaseNewConsoleAt(id, path string) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	m.ensureDatabasesLoaded()
	c, ok := m.databases.connection(id)
	if !ok {
		return nil
	}
	if path == "" {
		path = m.databaseConsolePath(id)
	}
	if path == "" {
		return nil
	}
	ext, header, filetype := dbConsoleFile(c.Driver, id)
	path = strings.TrimSuffix(path, ".sql") + ext
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

// dbConsoleFile is the extension, first lines and Neovim filetype of a
// new console for a connection of driver: .redis for Redis, .cql for
// Cassandra / ScyllaDB (CQL has SQL's comments, so the sql filetype
// colors it), else .sql.
func dbConsoleFile(driver, id string) (ext, header, filetype string) {
	switch driver {
	case "redis":
		return ".redis", "# @conn " + id + "\n# One command per line; the send key runs the line at the cursor.\n\n", "redis"
	case "cassandra":
		return ".cql", "-- @conn " + id + "\n-- CQL: the send key runs the statement at the cursor; results come a page at a time.\n\n", "sql"
	case "mongodb":
		// Shell lines in the SQL console file (DBX-68): one command per
		// statement, ";" between them.
		return ".sql", "-- @conn " + id + "\n-- One shell command per statement, for example: db.users.find({age: {$gt: 30}}).limit(20);\n\n", "sql"
	}
	return ".sql", "-- @conn " + id + "\n\n", "sql"
}

// isDBConsoleExt reports a file the database send key runs: .sql, .cql
// or .redis.
func isDBConsoleExt(p string) bool {
	switch strings.ToLower(lastExt(p)) {
	case ".sql", ".cql", ".redis":
		return true
	}
	return false
}

// databaseConsolePath is db/consoles/<workspace hash>/<connection hash>.sql
// in the user config ("" when there is no config directory).
func (m *Model) databaseConsolePath(id string) string {
	rootHash := fmt.Sprintf("%x", sha256.Sum256([]byte(m.databaseWorkspaceRoot())))[:16]
	idHash := fmt.Sprintf("%x", sha256.Sum256([]byte(id)))[:12]
	return dbConfigPath("consoles", rootHash, idHash+".sql")
}

// databaseConsoleCrumbs names a console file in the breadcrumbs: "SQL console ›
// local-pg" instead of the two hashes its path is made of.
func (m *Model) databaseConsoleCrumbs(path string) ([]string, bool) {
	dir := dbConfigPath("consoles")
	if dir == "" || !strings.HasPrefix(path, dir+string(filepath.Separator)) {
		return nil, false
	}
	kind := "SQL console"
	if strings.HasSuffix(path, ".redis") {
		kind = "Redis console"
	} else if strings.HasSuffix(path, ".cql") {
		kind = "CQL console"
	}
	base, num := dbConsoleNumber(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if num > 1 {
		kind += " " + strconv.Itoa(num)
	}
	if m.databases != nil {
		for _, c := range m.databases.conns {
			if fmt.Sprintf("%x", sha256.Sum256([]byte(c.ID)))[:12] == base {
				return []string{kind, sanitizeDBText(dbConnName(c))}, true
			}
		}
	}
	return []string{kind}, true
}

// databaseConsoleTitle is the tab name of a console file: "SQL console ·
// local-pg" instead of the hash its file name is made of ("" = not a console).
func (m *Model) databaseConsoleTitle(path string) string {
	segs, ok := m.databaseConsoleCrumbs(path)
	if !ok {
		return ""
	}
	return strings.Join(segs, " · ")
}

// titledBufs gives the buffers whose file name is a hash a readable tab name.
// The list is a copy: m.bufs keeps the plain paths.
func (m *Model) titledBufs(bufs []nvim.BufferInfo) []nvim.BufferInfo {
	var out []nvim.BufferInfo
	for i, b := range bufs {
		if isRunsPagePath(b.Path) && m.runs != nil {
			if out == nil {
				out = append([]nvim.BufferInfo(nil), bufs...)
			}
			out[i].Title = m.runsPageTitle()
			continue
		}
		if isWfxPagePath(b.Path) && m.wfx != nil {
			if out == nil {
				out = append([]nvim.BufferInfo(nil), bufs...)
			}
			out[i].Title = m.wfxPageTitle()
			continue
		}
		if isItermocPagePath(b.Path) && m.itermocPage != nil {
			if out == nil {
				out = append([]nvim.BufferInfo(nil), bufs...)
			}
			out[i].Title = m.itermocPageTitle()
			continue
		}
		if t := m.databaseConsoleTitle(b.Path); t != "" {
			if out == nil {
				out = append([]nvim.BufferInfo(nil), bufs...)
			}
			out[i].Title = t
		}
	}
	if out == nil {
		return bufs
	}
	return out
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
	if t := dbclient.ReachText(err); t != "" {
		return sanitizeDBText(t) // one plain line: what to check
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
	dbConnectTarget = lazyre.New(`^\S+ \([^)]*\): (?:(?:dial|server|tls) error: )?`)
	// "dial tcp 127.0.0.1:5432: connect: connection refused".
	dbDialReason = lazyre.New(`^dial \S+ \S+: (?:connect: )?(.+)$`)
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
	return m.databaseSendExec(all, plan, "")
}

// databaseSendExec is databaseSendMode with an execution mode (DBX-25);
// "" uses db_exec_mode.
func (m *Model) databaseSendExec(all bool, plan *dbPlanRun, mode string) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	m.ensureDBLua()
	file, lines, ok := m.httpBufText(0)
	redisFile := ok && strings.EqualFold(lastExt(file), ".redis")
	cqlFile := ok && strings.EqualFold(lastExt(file), ".cql")
	if !ok || !isDBConsoleExt(file) {
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
		Col       int    `json:"col"`
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
		r.selOne = !loadDBConsoleSettings().selectionSplit()
	default:
		r.cursor = dbCursorOffset(lines, r.line, pos.Col) + 1
		r.mode = mode
		if r.mode == "" {
			r.mode = loadDBConsoleSettings().execMode()
		}
	}
	r.conn = s.consoleConn(file, lines)
	if _, ok := m.databases.connection(r.conn); !ok {
		r.conn = ""
	}
	r.sessKey = m.databases.consoleSessionKey(file, r.conn) // its own session (DBX-40)
	if r.conn == "" {
		var items []picker.Item
		for _, c := range m.databases.conns {
			if (c.Driver == "redis") == redisFile && (!cqlFile || c.Driver == "cassandra") && c.Problem == "" {
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
	m.databaseConsoleVersion(r.file) // db_query_log.go
	c, ok := m.databases.connection(r.conn)
	if !ok {
		return m.pushToastDetail(toast.Warn, "Connection not found", sanitizeDBText(r.conn))
	}
	if c.Driver == "redis" {
		return m.databaseRedisPrepare(r)
	}
	if e, ok := dbclient.HTTPEngineByID(c.Driver); ok && e.LineBased {
		// etcd and Memcached (DBX-69): one command per line.
		r.statements = dbLineCommands(r.sql, r.line)
		if r.line < 0 {
			r.line = -r.line
		}
		if len(r.statements) == 0 {
			return m.pushToastDetail(toast.Info, "No command at the cursor", "")
		}
		r.line = max(r.line, 1)
		r.index = 0
		r.sql = r.statements[0]
		s := m.dbConsole()
		s.batchSeq++
		r.batch = s.batchSeq
		return m.databaseReviewRun(r)
	}
	if r.line > 0 && r.cursor > 0 && r.plan == nil {
		if r.mode == dbclient.ExecAsk.String() {
			return m.databaseExecAsk(r, c.Driver)
		}
		if dbPickByMode(r, c.Driver) {
			return m.databasePrepareTail(r)
		}
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
	} else if r.selOne && r.line < 0 {
		r.statements = []string{strings.TrimSpace(r.sql)}
		r.line = -r.line
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
	return m.databasePrepareTail(r)
}

// databasePrepareTail starts r once its statements are chosen.
func (m *Model) databasePrepareTail(r *dbRun) tea.Cmd {
	if len(r.statements) == 0 || strings.TrimSpace(r.statements[0]) == "" {
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
	if refs := dbNeedsParams(r, c.Driver); len(refs) > 0 {
		return m.databaseAskParams(r, refs)
	}
	kind, stmts := dbclient.Classify(r.sql, c.Driver)
	if kind == dbclient.KindRead || r.reviewed {
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
var dbTargetTable = lazyre.New("(?is)^\\s*(?:DELETE\\s+FROM|UPDATE|TRUNCATE(?:\\s+TABLE)?)\\s+([\\w.\"`\\[\\]]+)")

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
	if sess == nil && dbIsExtraSession(r.sessKey) && m.databases.sessions[r.conn] != nil {
		s.waiting = r // the console's own session opens first (DBX-40)
		return m.databaseOpenExtraSession(r.sessKey)
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
	m.databaseRefreshLens(r.file, r.sql, "running… · Esc cancels")
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
		execSQL, args := r.execSQL()
		switch {
		case r.redis != nil:
			var view *dbRedisView
			result, view = dbRedisLoad(ctx, sess, *r.redis, redisCount)
			return dbRunMsg{gen: gen, run: r, result: result, key: key, redis: view}
		case r.plan != nil:
			result = dbExplainExec(ctx, sess, driver, r.plan, inTx, limit)
		case r.readOnly && inTx:
			result = dbReadInTx(ctx, sess, execSQL, dbclient.RunOptions{MaxRows: limit, Args: args})
		case r.readOnly:
			result = dbRunReadOnly(ctx, sess, execSQL, dbclient.RunOptions{MaxRows: limit, Args: args})
		default:
			if manual && !inTx {
				// The transaction outlives this run: never a context that ends.
				if err := sess.Begin(context.Background()); err != nil {
					return dbRunMsg{gen: gen, run: r, result: dbclient.Result{Err: err}, key: key}
				}
				began = true
			}
			result = sess.Run(ctx, execSQL, dbclient.RunOptions{MaxRows: limit, Args: args})
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
		if len(res.Rows) == 1 && len(res.Columns) > 1 && msg.run.tv == nil && msg.run.plan == nil && msg.run.redis == nil && loadDBConsoleSettings().AutoTranspose != nil && *loadDBConsoleSettings().AutoTranspose {
			s.trans, s.trow = true, 0 // one row reads better as name/value lines (DBX-26)
		}
	}
	if msg.manual {
		status += m.dbTxStatus(msg.run.conn)
	}
	m.databaseSetInline(msg.run) // before the lens refresh draws it (DBX-26)
	m.databaseRefreshLens(msg.run.file, msg.run.sql, status)
	cmds := []tea.Cmd{m.databaseShowPage(), txCmd, m.databaseTempTableHint(msg)}
	if msg.run.tv == nil && msg.run.redis == nil { // a data view or a value page is not something the user typed
		cmds = append(cmds, m.databaseAppendHistory(msg.run))
	}
	if msg.run.index+1 < len(msg.run.statements) {
		next := *msg.run
		next.index++
		next.sql = next.statements[next.index]
		next.result = dbclient.Result{}
		next.tab, next.notes = nil, nil
		next.bound, next.boundSQL, next.args = false, "", nil
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
	if cmd, ok := m.databaseSchemaFilesConfirm(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseImportConnConfirm(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseDumpToolConfirm(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseSessionsConfirm(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseSavepointConfirm(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseTransferConfirm(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseSourceConfirm(id); ok { // db_objsource.go
		return cmd, true
	}
	if cmd, ok := m.databaseObjActionConfirm(id); ok { // db_objactions.go
		return cmd, true
	}
	switch m.confirmKind {
	case confirmKindDBRename:
		return m.databaseRenameApply(id), true
	case confirmKindDBDesign:
		return m.databaseDesignConfirm(id), true
	case confirmKindDBExplain:
		return m.databaseExplainConfirm(id), true
	case confirmKindDBPlanStep:
		return m.databasePlanStepConfirm(id), true
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
	if cmd, ok := m.databaseSchemaFilesPicker(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseImportConnPick(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseRolesPick(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseGeneratePicker(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseSQLRunPicked(id); ok { // db_sqlruns.go
		return cmd, true
	}
	if cmd, ok := m.databaseSessionsPick(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseConsoleSessionPick(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseOpenCSVPicked(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseSavepointPicked(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseSQLPicked(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseSQLMorePicked(id); ok { // db_sqllang_more.go
		return cmd, true
	}
	if cmd, ok := m.databaseTransferPicker(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseComparePicker(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseUsagesPicker(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseSavedPicker(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseColumnsPicker(id); ok {
		return cmd, true
	}
	if cmd, ok := m.databaseFKPicker(id); ok { // db_fknav.go
		return cmd, true
	}
	if cmd, ok := m.databaseWherePicker(id); ok { // db_where_builder.go
		return cmd, true
	}
	if cmd, ok := m.databaseERPicker(id); ok { // db_er.go
		return cmd, true
	}
	if cmd, ok := m.databaseSourcePicker(id); ok { // db_objsource.go
		return cmd, true
	}
	if cmd, ok := m.databaseObjActionPicker(id); ok { // db_objactions.go
		return cmd, true
	}
	if cmd, ok := m.databaseERPathPicker(id); ok { // db_er_path.go
		return cmd, true
	}
	if cmd, ok := m.databaseCloudPicker(id); ok { // db_cloud.go
		return cmd, true
	}
	if cmd, ok := m.databaseManagePicker(id); ok { // db_connmanage.go
		return cmd, true
	}
	if cmd, ok := m.databaseFindObjectPicker(id); ok { // db_findobj.go
		return cmd, true
	}
	switch m.pickerKind {
	case pickerKindDBPageSize:
		return m.databaseSetPageSize(id), true
	case pickerKindDBBeside:
		return m.databaseSetBeside(id), true
	case pickerKindDBCellMenu:
		return m.databaseCellMenuPick(id), true
	case pickerKindDBExecMode:
		return m.databaseExecModeSet(id), true
	case pickerKindDBExecAsk:
		return m.databaseExecAskPick(id), true
	case pickerKindDBAttach, pickerKindDBHistory, pickerKindDBSort:
	case pickerKindDBHistoryDelete:
		return m.databaseHistoryDelete(id), true
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
		if id == "delete-one" {
			return m.databaseHistoryDeletePicker(), true
		}
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
func (m *Model) databaseHistory() tea.Cmd { return m.databaseHistoryFor("") }

// databaseHistoryFor is the history of one connection ("" = all; DBX-6).
// Typing in the picker searches it.
func (m *Model) databaseHistoryFor(conn string) tea.Cmd {
	s := m.dbConsole()
	entries, err := s.historyStore().Find(conn, "")
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
		items = append(items, picker.Item{ID: "delete-one", Title: "Delete an entry…"})
		items = append(items, picker.Item{ID: "clear", Title: "Clear history…"})
	}
	if len(items) == 0 {
		return m.pushToastDetail(toast.Info, "No SQL history yet", "Run a statement from a console first.")
	}
	title := "SQL history · Enter runs again"
	if conn != "" {
		title = "SQL history of " + sanitizeDBText(conn) + " · Enter runs again"
	}
	m.picker = picker.NewItems(title, items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBHistory
	m.pickerOpen = true
	return nil
}

// databaseHistoryDeletePicker lists the history again; the entry picked
// is deleted (DBX-48).
func (m *Model) databaseHistoryDeletePicker() tea.Cmd {
	s := m.dbConsole()
	items := make([]picker.Item, 0, len(s.history))
	for i := len(s.history) - 1; i >= 0; i-- {
		e := s.history[i]
		hint := sanitizeDBText(e.Conn) + " · " + e.Time.Format("01-02 15:04")
		items = append(items, picker.Item{ID: e.ID, Title: dbOneLine(e.SQL), Hint: hint})
	}
	if len(items) == 0 {
		return m.pushToast(toast.Info, "No SQL history yet")
	}
	m.picker = picker.NewItems("Delete a history entry · Enter deletes it", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBHistoryDelete
	m.pickerOpen = true
	return nil
}

// databaseHistoryDelete removes one history entry.
func (m *Model) databaseHistoryDelete(id string) tea.Cmd {
	s := m.dbConsole()
	if id == "" {
		return nil
	}
	if err := s.historyStore().Delete(id); err != nil {
		return m.dbConsoleError(err)
	}
	kept := s.history[:0]
	for _, e := range s.history {
		if e.ID != id {
			kept = append(kept, e)
		}
	}
	s.history = kept
	return m.pushToast(toast.Info, "History entry deleted")
}

// dbLineCommands is the command on line (> 0) of text, or every command
// line (line <= 0), without blank and comment lines.
func dbLineCommands(text string, line int) []string {
	lines := strings.Split(text, "\n")
	keep := func(l string) bool {
		t := strings.TrimSpace(l)
		return t != "" && !strings.HasPrefix(t, "--") && !strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "//")
	}
	if line > 0 {
		if line <= len(lines) && keep(lines[line-1]) {
			return []string{strings.TrimSpace(lines[line-1])}
		}
		return nil
	}
	var out []string
	for _, l := range lines {
		if keep(l) {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}
