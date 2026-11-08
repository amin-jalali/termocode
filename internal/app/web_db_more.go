package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The rest of the Databases view from the page (web parity, Databases
// area). Reads run in read-only sessions of their own, as db.query does
// (web_db.go): DDL, plans, a connection test, a schema compare, the SQL
// history, the workspace's CSV files and a Redis database's key names.
// Every change to a database or to a saved connection waits for a
// laptop card (Cancel first) showing exactly what runs: the statements
// with their values, the import's file, rows and INSERT, the Redis key,
// the connection entry; and runs only after its yes, in a fresh session
// (never the user's), except commit, roll back, the transaction mode and
// disconnect, which act on the laptop console's own session as its keys
// do. Passwords stay on the laptop: only db.secret takes one (a read asked
// for it; web_db_net.go keeps it in the secret store), a new connection is
// saved without one, and every text the page gets passes the database
// redaction.

func init() {
	webOps[webproto.OpDBDDL] = (*Model).webDBDDL
	webOps[webproto.OpDBExplain] = (*Model).webDBExplain
	webOps[webproto.OpDBTest] = (*Model).webDBTest
	webOps[webproto.OpDBCompare] = (*Model).webDBCompare
	webOps[webproto.OpDBHistory] = (*Model).webDBHistory
	webOps[webproto.OpDBCSVFiles] = (*Model).webDBCSVFiles
	webOps[webproto.OpDBCancel] = (*Model).webDBCancel
	webOps[webproto.OpDBConnect] = (*Model).webDBConnect
	webOps[webproto.OpDBConfig] = (*Model).webDBConfig
	webOps[webproto.OpRedisKeys] = (*Model).webRedisKeys
	webAskLaterOps[webproto.OpDBEdit] = (*Model).webDBEdit
	webAskLaterOps[webproto.OpDBImport] = (*Model).webDBImport
	webAskOps[webproto.OpRedisKey] = (*Model).webRedisKey
	webAskOps[webproto.OpDBAdd] = (*Model).webDBAdd
	webAskOps[webproto.OpDBSSH] = (*Model).webDBSSH
	webAskOps[webproto.OpDBSSL] = (*Model).webDBSSL
	webAskOps[webproto.OpDBDisconnect] = (*Model).webDBDisconnect
	webAskOps[webproto.OpDBTx] = (*Model).webDBTx
}

// webAskLaterOps build their laptop card after a read off the UI thread
// (a table's key, a CSV file's rows): the handler calls call.later and
// its goroutine returns open(card), which web_call.go turns into the card.
// The op's row says LaptopCard; nothing changes before the card's yes.
var webAskLaterOps = map[string]func(m *Model, call *webCall, open func(*webAsk) tea.Msg) tea.Cmd{}

// webDBState is the hub's state of the Databases ops (Update only, but
// runs, which the read goroutines end, has its own lock).
type webDBState struct {
	views     map[string]map[string]bool // the views among the tables db.tables listed, by connection
	redisKeys map[string]map[string]bool // the keys redis.keys listed last, by "conn/db"
	runs      webDBRuns
}

func (m *Model) webDB() *webDBState {
	if m.web.more.db == nil {
		m.web.more.db = &webDBState{}
	}
	return m.web.more.db
}

// webDBRuns are the page's running reads, by web connection and database
// connection, so db.cancel stops a page's own reads only.
type webDBRuns struct {
	mu   sync.Mutex
	next int
	runs map[string]map[int]context.CancelFunc
}

// start is a context for one read; stop ends it and forgets it.
func (r *webDBRuns) start(web, conn string, parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(parent, loadDBConsoleSettings().timeout())
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runs == nil {
		r.runs = map[string]map[int]context.CancelFunc{}
	}
	key := web + "\x00" + conn
	if r.runs[key] == nil {
		r.runs[key] = map[int]context.CancelFunc{}
	}
	r.next++
	id := r.next
	r.runs[key][id] = cancel
	return ctx, func() {
		cancel()
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.runs[key], id)
	}
}

// cancel stops the reads web runs on conn and says how many.
func (r *webDBRuns) cancel(web, conn string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := web + "\x00" + conn
	n := len(r.runs[key])
	for id, c := range r.runs[key] {
		c()
		delete(r.runs[key], id)
	}
	return n
}

// webDBCtx is the context of a read of call on conn (db.cancel stops it).
func (m *Model) webDBCtx(call *webCall, conn string) (context.Context, func()) {
	return m.webDB().runs.start(call.conn.id, conn, call.ctx)
}

// webDBAny is a saved connection of any driver by id.
func (m *Model) webDBAny(call *webCall, id string) (string, dbclient.Conn, bool) {
	root, err := m.rootDir()
	if err != nil {
		call.fail(webproto.CodeNotFound, "No workspace is open.")
		return "", dbclient.Conn{}, false
	}
	_, _, list := dbLoadConfigs(root)
	for _, c := range list {
		if c.ID == id {
			return root, c, true
		}
	}
	call.fail(webproto.CodeNotFound, "That connection does not exist.")
	return "", dbclient.Conn{}, false
}

// webDBListed is the table of conn that db.tables listed last.
func (m *Model) webDBListed(call *webCall, conn, table string) (view, ok bool) {
	if !m.web.more.dbTables[conn][table] {
		call.fail(webproto.CodeNotFound, "That table is not in the list. Open the list again.")
		return false, false
	}
	return m.webDB().views[conn][table], true
}

// webDBTableOf is the dbTable of a listed table (schema as db.rows reads it).
func webDBTableOf(c dbclient.Conn, name string, view bool) dbTable {
	t := dbTable{conn: c.ID, driver: c.Driver, name: name, view: view}
	if c.Driver == "postgres" {
		t.schema = webDBSchema(c)
	}
	return t
}

// webDBWhere is "postgres, app, production" for a card.
func webDBWhere(c dbclient.Conn) string {
	where := c.Driver
	if db := webDBDatabase(c); db != "" {
		where += ", " + db
	}
	if c.Environment != "" {
		where += ", " + c.Environment
	}
	return sanitizeDBText(where)
}

// webDBReadOnce opens a read-only session, runs read and closes it; an
// error fails call (redacted).
func (m *Model) webDBReadOnce(call *webCall, w webDBConn, read func(ctx context.Context, s dbclient.Session) (any, error)) tea.Cmd {
	ctx, stop := m.webDBCtx(call, w.c.ID)
	call.later()
	return func() tea.Msg {
		defer stop()
		s, err := w.open(ctx)
		if err != nil {
			w.fail(call, err)
			return nil
		}
		defer s.Close()
		out, err := read(ctx, s)
		if err != nil {
			w.fail(call, err)
			return nil
		}
		call.reply(out)
		return nil
	}
}

func (m *Model) webDBDDL(call *webCall) tea.Cmd {
	a := call.args.(*webproto.DBTableArgs)
	w, ok := m.webDBFind(call, a.Conn)
	if !ok {
		return nil
	}
	view, ok := m.webDBListed(call, a.Conn, a.Table)
	if !ok {
		return nil
	}
	t := webDBTableOf(w.c, a.Table, view)
	return m.webDBReadOnce(call, w, func(ctx context.Context, s dbclient.Session) (any, error) {
		meta, err := dbLoadTableMeta(ctx, s, t, t.request())
		if err != nil {
			return nil, err
		}
		return webproto.DBText{Text: dbAIClean(dbBuildDDL(t, meta), w.snap.password)}, nil
	})
}

func (m *Model) webDBExplain(call *webCall) tea.Cmd {
	a := call.args.(*webproto.DBExplainArgs)
	w, ok := m.webDBFind(call, a.Conn)
	if !ok {
		return nil
	}
	sql, kind, err := webDBStatement(w.c, a.SQL)
	if err != nil {
		call.fail(webproto.CodeBadRequest, err.Error())
		return nil
	}
	if a.Analyze && kind != dbclient.KindRead {
		call.fail(webproto.CodeForbidden, "Explain Analyze runs the statement. A change needs the laptop.")
		return nil
	}
	m.webAuditOp(call, w.c.ID+" "+strconv.Itoa(len(sql))+" bytes", "read")
	return m.webDBReadOnce(call, w, func(ctx context.Context, s dbclient.Session) (any, error) {
		start := time.Now()
		pl := &dbPlanRun{analyze: a.Analyze, target: sql}
		res := dbExplainExec(ctx, s, w.c.Driver, pl, false, 5000)
		if res.Err != nil {
			return nil, res.Err
		}
		p := dbclient.ParsePlan(w.c.Driver, res, a.Analyze)
		out := webproto.DBPlan{Lines: []webproto.DBPlanLine{}, Analyze: a.Analyze, Ms: time.Since(start).Milliseconds()}
		for i, l := range p.Lines {
			if i == webproto.MaxDBPlanLines {
				out.Truncated = true
				break
			}
			out.Lines = append(out.Lines, webproto.DBPlanLine{Depth: l.Depth, Text: dbAIClean(l.Text, w.snap.password), Detail: l.Detail, Costly: l.Costly})
		}
		for _, s := range p.Summary {
			out.Summary = append(out.Summary, dbAIClean(s, w.snap.password))
		}
		return out, nil
	})
}

func (m *Model) webDBTest(call *webCall) tea.Cmd {
	w, ok := m.webDBFind(call, call.args.(*webproto.DBConnArgs).Conn)
	if !ok {
		return nil
	}
	w.snap.user = nil // a fresh login, never a clone of the open session
	m.webAuditOp(call, w.c.ID, "test")
	return m.webDBReadOnce(call, w, func(ctx context.Context, s dbclient.Session) (any, error) {
		start := time.Now()
		if err := s.Ping(ctx); err != nil {
			return nil, err
		}
		return webproto.DBDone{Text: "The connection works.", Ms: time.Since(start).Milliseconds()}, nil
	})
}

func (m *Model) webDBCompare(call *webCall) tea.Cmd {
	a := call.args.(*webproto.DBCompareArgs)
	var sides [2]webDBConn
	for i, id := range []string{a.Conn, a.Target} {
		_, c, ok := m.webDBAny(call, id)
		if !ok {
			return nil
		}
		if !webDBSQL(c.Driver) {
			call.fail(webproto.CodeBadRequest, "Schema compare needs two SQL databases.")
			return nil
		}
		w, ok := m.webDBFind(call, id)
		if !ok {
			return nil
		}
		sides[i] = w
	}
	if dbDriverFamily(sides[0].c.Driver) != dbDriverFamily(sides[1].c.Driver) {
		call.fail(webproto.CodeBadRequest, "Schema compare needs two databases of the same type.")
		return nil
	}
	ctx, stop := m.webDBCtx(call, a.Conn)
	call.later()
	return func() tea.Msg {
		defer stop()
		var canon [2]dbclient.CanonSchema
		for i := range sides {
			s, err := sides[i].open(ctx)
			if err != nil {
				sides[i].fail(call, err)
				return nil
			}
			canon[i], err = dbclient.ReadCanonSchema(ctx, s, sides[i].c.Driver, webDBSchema(sides[i].c))
			_ = s.Close()
			if err != nil {
				sides[i].fail(call, err)
				return nil
			}
			canon[i].Label = sides[i].c.ID
		}
		cmp := dbclient.CompareSchemas(canon[0], canon[1])
		script := dbAIClean(dbAIClean(cmp.Script, sides[0].snap.password), sides[1].snap.password)
		out := webproto.DBCompare{Script: script, Changes: cmp.Changes, Destructive: cmp.Destructive, Truncated: cmp.Truncated}
		if len(out.Script) > webproto.MaxDBScript {
			out.Script, out.Truncated = webCut(out.Script, webproto.MaxDBScript), true
		}
		call.reply(out)
		return nil
	}
}

// webCut is s cut at n bytes on a rune start.
func webCut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && (s[n]&0xC0) == 0x80 {
		n--
	}
	return s[:n]
}

func (m *Model) webDBHistory(call *webCall) tea.Cmd {
	conn := call.args.(*webproto.DBHistoryArgs).Conn
	call.later()
	return func() tea.Msg {
		list, err := (&dbclient.History{Path: dbConfigPath("history.jsonl")}).List()
		if err != nil {
			call.fail(webproto.CodeInternal, "Could not read the SQL history.")
			return nil
		}
		out := webproto.DBHistory{Entries: []webproto.DBHistoryEntry{}}
		for _, e := range list { // newest first
			if len(out.Entries) == webproto.MaxDBHistory {
				break
			}
			if conn != "" && e.Conn != conn {
				continue
			}
			sql := e.SQL
			if len(sql) > webproto.MaxDBSQL {
				sql = webCut(sql, webproto.MaxDBSQL)
			}
			out.Entries = append(out.Entries, webproto.DBHistoryEntry{Conn: webName(e.Conn), SQL: dbSafeMultiline(sql), At: e.Time.UnixMilli(), Ms: e.ElapsedMS, Rows: e.Rows})
		}
		call.reply(out)
		return nil
	}
}

func (m *Model) webDBCSVFiles(call *webCall) tea.Cmd {
	root, err := m.rootDir()
	if err != nil {
		call.fail(webproto.CodeNotFound, "No workspace is open.")
		return nil
	}
	call.later()
	return func() tea.Msg {
		out := webproto.DBFiles{Files: []string{}}
		for _, rel := range dbFindCSV(root) {
			rel = filepath.ToSlash(rel)
			if webSecretFile(filepath.Join(root, rel)) {
				continue
			}
			if len(out.Files) == webproto.MaxDBCSVFiles {
				out.More++
				continue
			}
			out.Files = append(out.Files, rel)
		}
		call.reply(out)
		return nil
	}
}

func (m *Model) webDBCancel(call *webCall) tea.Cmd {
	n := m.webDB().runs.cancel(call.conn.id, call.args.(*webproto.DBConnArgs).Conn)
	if n == 0 {
		call.reply(webproto.DBDone{Text: "Nothing runs."})
		return nil
	}
	call.reply(webproto.DBDone{Text: dbPlural(n, "query") + " stopped."})
	return nil
}

func (m *Model) webDBConnect(call *webCall) tea.Cmd {
	id := call.args.(*webproto.DBConnArgs).Conn
	if _, _, ok := m.webDBAny(call, id); !ok {
		return nil
	}
	m.ensureDatabasesLoaded()
	if m.databases.sessions[id] != nil {
		call.reply(webproto.DBDone{Text: "Already connected."})
		return nil
	}
	m.webAuditOp(call, id, "connect")
	cmd := m.databaseConnect(id)
	call.reply(webproto.DBDone{Text: "Connecting on the laptop. A password, if one is needed, is typed there."})
	return cmd
}

func (m *Model) webDBConfig(call *webCall) tea.Cmd {
	root, err := m.rootDir()
	if err != nil {
		call.fail(webproto.CodeNotFound, "No workspace is open.")
		return nil
	}
	path := filepath.Join(root, ".termocode", "databases.json")
	out := webproto.DBConfigFile{Path: ".termocode/databases.json"}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			err = os.WriteFile(path, []byte(dbclient.ConfigTemplate()), 0o644)
		}
		if err != nil {
			call.fail(webproto.CodeInternal, "Could not write the connections file.")
			return nil
		}
		out.Created = true
	}
	m.webAuditOp(call, out.Path, "config")
	call.reply(out)
	return nil
}

// webRedisOpen opens a Redis session (a seam for tests).
var webRedisOpen = redisAIOpen

// webRedisFind is a Redis connection by id with its password snapshot.
func (m *Model) webRedisFind(call *webCall, id string) (webDBConn, bool) {
	root, c, ok := m.webDBAny(call, id)
	if !ok {
		return webDBConn{}, false
	}
	if c.Driver != "redis" {
		call.fail(webproto.CodeBadRequest, "That connection is not Redis.")
		return webDBConn{}, false
	}
	if c.Problem != "" {
		call.fail(webproto.CodeConflict, "This connection cannot be read here. Open it on the laptop.")
		return webDBConn{}, false
	}
	w := webDBConn{root: root, c: c}
	if d := m.databases; d != nil {
		w.snap.password, w.snap.hasPW = d.passwords[id]
		if c.SSH != nil {
			w.snap.dial, _ = d.dbDialFor(c)
		}
	}
	m.webDBRoute(&w)
	return w, true
}

func webRedisDB(c dbclient.Conn, db int) int {
	if db > 0 {
		return db
	}
	return redisConnDB(c)
}

func (m *Model) webRedisKeys(call *webCall) tea.Cmd {
	a := call.args.(*webproto.RedisKeysArgs)
	w, ok := m.webRedisFind(call, a.Conn)
	if !ok {
		return nil
	}
	db := webRedisDB(w.c, a.DB)
	pattern := a.Pattern
	if pattern == "" {
		pattern = "*"
	}
	ctx, stop := m.webDBCtx(call, w.c.ID)
	call.later()
	return func() tea.Msg {
		defer stop()
		if err := w.route(ctx); err != nil {
			w.fail(call, err)
			return nil
		}
		s, err := webRedisOpen(ctx, w.root, w.c, &w.snap, true)
		if err != nil {
			w.fail(call, err)
			return nil
		}
		defer s.Close()
		keys, more, err := s.ScanKeys(ctx, db, pattern, webproto.MaxRedisKeys)
		var metas []dbclient.RedisKeyMeta
		if err == nil {
			sort.Strings(keys)
			metas, err = s.Types(ctx, db, keys)
		}
		if err != nil {
			w.fail(call, err)
			return nil
		}
		out := webproto.RedisKeys{DB: db, Keys: []webproto.RedisKey{}, More: more}
		names := map[string]bool{}
		for i, k := range keys {
			mt := dbclient.RedisKeyMeta{Type: "none", TTL: -1}
			if i < len(metas) {
				mt = metas[i]
			}
			if mt.Type == "none" {
				continue
			}
			names[k] = true
			out.Keys = append(out.Keys, webproto.RedisKey{Key: k, Type: mt.Type, TTL: mt.TTL})
		}
		return applyMsg(func(m *Model) tea.Cmd {
			if m.web != nil {
				st := m.webDB()
				if st.redisKeys == nil {
					st.redisKeys = map[string]map[string]bool{}
				}
				st.redisKeys[w.c.ID+"/"+strconv.Itoa(db)] = names
			}
			call.reply(out)
			return nil
		})
	}
}

// ── Changes: each after a laptop card ───────────────────────────────────

// webDBWritable is a SQL connection a change may run on.
func (m *Model) webDBWritable(call *webCall, id string) (webDBConn, bool) {
	w, ok := m.webDBFind(call, id)
	if !ok {
		return w, false
	}
	if w.c.ReadOnly {
		call.fail(webproto.CodeForbidden, "This connection is read-only.")
		return w, false
	}
	return w, true
}

// webDBKeyOf is the key of a table: the primary key, else the first
// unique key ("" names none).
func webDBKeyOf(ir dbclient.IntrospectionResult) []string {
	keys := append([]string(nil), ir.Keys...)
	if len(keys) == 0 {
		for _, c := range ir.Columns {
			if c.PK {
				keys = append(keys, c.Name)
			}
		}
	}
	if len(keys) == 0 {
		if u := dbUniqueSets(ir); len(u) > 0 {
			keys = u[0]
		}
	}
	return keys
}

// webDBCell is a page value as a cell.
func webDBCell(v webproto.DBColValue) dbclient.Cell {
	if v.N {
		return dbclient.Cell{Null: true}
	}
	return dbclient.Cell{Text: v.V}
}

// webDBRowEdits turns the page's changes into row edits: the key must be
// the table's key, a value may not be a masked one, and a secret column
// is never set.
func webDBRowEdits(a *webproto.DBEditArgs, key []string) ([]dbclient.RowEdit, webproto.ErrCode, error) {
	if len(key) == 0 {
		return nil, webproto.CodeBadRequest, errors.New("This table has no primary or unique key: change it on the laptop.")
	}
	var out []dbclient.RowEdit
	for _, e := range a.Edits {
		var re dbclient.RowEdit
		switch e.Kind {
		case "update":
			re.Kind = dbclient.EditUpdate
		case "delete":
			re.Kind = dbclient.EditDelete
		default:
			re.Kind = dbclient.EditInsert
		}
		if e.Kind != "insert" {
			var got []string
			for _, k := range e.Key {
				got = append(got, k.Col)
			}
			sort.Strings(got)
			sorted := append([]string(nil), key...)
			sort.Strings(sorted)
			if strings.Join(got, "\x00") != strings.Join(sorted, "\x00") {
				return nil, webproto.CodeBadRequest, errors.New("A row is found by the table's key (" + sanitizeDBText(strings.Join(key, ", ")) + "). Open the table again.")
			}
			byCol := map[string]webproto.DBColValue{}
			for _, k := range e.Key {
				byCol[k.Col] = k
			}
			for _, k := range key {
				v := byCol[k]
				if v.V == webproto.DBMaskedValue {
					return nil, webproto.CodeBadRequest, errors.New("A hidden value cannot find a row.")
				}
				re.Key = append(re.Key, dbclient.EditValue{Column: k, Value: webDBCell(v)})
			}
		}
		old := map[string]webproto.DBColValue{}
		for _, o := range e.Old {
			old[o.Col] = o
		}
		for _, v := range e.Set {
			if webDBSecretColumn(v.Col) {
				return nil, webproto.CodeForbidden, errors.New("The column " + sanitizeDBText(v.Col) + " holds secrets. Change it on the laptop.")
			}
			if v.V == webproto.DBMaskedValue {
				return nil, webproto.CodeBadRequest, errors.New("A hidden value cannot be saved.")
			}
			ev := dbclient.EditValue{Column: v.Col, Value: webDBCell(v)}
			if o, ok := old[v.Col]; ok && re.Kind == dbclient.EditUpdate && o.V != webproto.DBMaskedValue {
				ev.Old, ev.Check = webDBCell(o), true
			}
			re.Values = append(re.Values, ev)
		}
		out = append(out, re)
	}
	return out, "", nil
}

func (m *Model) webDBEdit(call *webCall, open func(*webAsk) tea.Msg) tea.Cmd {
	a := call.args.(*webproto.DBEditArgs)
	w, ok := m.webDBWritable(call, a.Conn)
	if !ok {
		return nil
	}
	view, ok := m.webDBListed(call, a.Conn, a.Table)
	if !ok {
		return nil
	}
	if view {
		call.fail(webproto.CodeBadRequest, "A view's rows are not edited here.")
		return nil
	}
	t := webDBTableOf(w.c, a.Table, false)
	ctx, stop := m.webDBCtx(call, w.c.ID)
	call.later()
	return func() tea.Msg {
		defer stop()
		s, err := w.open(ctx)
		if err != nil {
			w.fail(call, err)
			return nil
		}
		ir, err := s.Introspect(ctx, t.request())
		_ = s.Close()
		if err != nil {
			w.fail(call, err)
			return nil
		}
		edits, code, err := webDBRowEdits(a, webDBKeyOf(ir))
		if err != nil {
			call.fail(code, err.Error())
			return nil
		}
		stmts, err := dbclient.EditStatements(dbAIDialect(w.c.Driver), t.schema, t.name, edits)
		if err != nil {
			call.fail(webproto.CodeBadRequest, sanitizeDBText(err.Error()))
			return nil
		}
		name := sanitizeDBText(dbConnName(w.c))
		n := dbPlural(len(stmts), "change")
		ask := &webAsk{
			summary: w.c.ID + " " + a.Table + " " + n,
			title:   "Save row changes?",
			body: "Save " + n + " to " + sanitizeDBText(a.Table) + " on " + name + " (" + webDBWhere(w.c) + "):\n\n" + dbPreviewSQL(stmts) +
				"\n\nThey run in one transaction. A statement that changes another number of rows than one rolls everything back.",
			action: "Save",
			run: func(m *Model) tea.Cmd {
				fresh, ok := m.webDBFindQuiet(w.c.ID)
				if !ok {
					return m.pushToastDetail(toast.Warn, "Nothing saved", "The connection changed.")
				}
				return func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), loadDBConsoleSettings().timeout())
					defer cancel()
					s, err := fresh.session(ctx, false)
					var out dbclient.EditOutcome
					if err == nil {
						out = dbclient.ApplyEdits(ctx, s, stmts, false)
						_ = s.Close()
						err = out.Err
					}
					return applyMsg(func(m *Model) tea.Cmd {
						if err != nil {
							return m.pushToastDetail(toast.Errr, "Nothing saved on "+name, dbAIError(err, fresh.snap.password).Error())
						}
						return m.pushToastDetail(toast.Info, n+" saved", sanitizeDBText(a.Table)+" on "+name)
					})
				}
			},
		}
		return open(ask)
	}
}

func (m *Model) webDBImport(call *webCall, open func(*webAsk) tea.Msg) tea.Cmd {
	a := call.args.(*webproto.DBImportArgs)
	w, ok := m.webDBWritable(call, a.Conn)
	if !ok {
		return nil
	}
	view, ok := m.webDBListed(call, a.Conn, a.Table)
	if !ok {
		return nil
	}
	if view {
		call.fail(webproto.CodeBadRequest, "Rows are imported into a table, not a view.")
		return nil
	}
	if !strings.EqualFold(filepath.Ext(a.Path), ".csv") {
		call.fail(webproto.CodeBadRequest, "Pick a .csv file.")
		return nil
	}
	abs, err := m.webResolve(a.Path)
	if err != nil {
		webFailErr(call, err, "")
		return nil
	}
	if real, _ := filepath.EvalSymlinks(abs); webSecretFile(abs) || webSecretFile(real) {
		call.fail(webproto.CodeForbidden, "This file may hold secrets. It stays on the laptop.")
		return nil
	}
	t := webDBTableOf(w.c, a.Table, false)
	rel := webName(a.Path)
	ctx, stop := m.webDBCtx(call, w.c.ID)
	call.later()
	return func() tea.Msg {
		defer stop()
		sn, err := dbclient.SniffCSV(abs)
		if err != nil {
			call.fail(webproto.CodeBadRequest, sanitizeDBText(err.Error()))
			return nil
		}
		sn = sn.WithHeader(!a.NoHeader)
		rows, err := dbclient.CountCSVRows(ctx, abs, sn)
		if err != nil {
			call.fail(webproto.CodeBadRequest, sanitizeDBText(err.Error()))
			return nil
		}
		s, err := w.open(ctx)
		if err != nil {
			w.fail(call, err)
			return nil
		}
		ir, err := s.Introspect(ctx, t.request())
		_ = s.Close()
		if err != nil {
			w.fail(call, err)
			return nil
		}
		var targets []string
		for _, c := range ir.Columns {
			targets = append(targets, c.Name)
		}
		sources := dbclient.AutoMap(targets, sn)
		var pairs, cols []string
		used := map[int]bool{}
		d := dbAIDialect(w.c.Driver)
		for i, src := range sources {
			if src >= 0 {
				used[src] = true
				pairs = append(pairs, sanitizeDBText(targets[i])+" ← "+sanitizeDBText(sn.Columns[src]))
				cols = append(cols, d.QuoteIdent(targets[i]))
			}
		}
		if len(cols) == 0 {
			call.fail(webproto.CodeBadRequest, "No file column matches a column of "+sanitizeDBText(a.Table)+".")
			return nil
		}
		var skipped []string
		for j, c := range sn.Columns {
			if !used[j] {
				skipped = append(skipped, sanitizeDBText(c))
			}
		}
		spec := dbclient.ImportSpec{Path: abs, Sniff: sn, Dialect: d, Schema: t.schema, Table: t.name, Targets: targets, Sources: sources}
		name := sanitizeDBText(dbConnName(w.c))
		body := "Import " + dbPlural(rows, "row") + " from " + rel + " into " + sanitizeDBText(a.Table) + " on " + name + " (" + webDBWhere(w.c) + ")?\n\n" +
			"Columns: " + strings.Join(pairs, ", ")
		if len(skipped) > 0 {
			body += "\nNot imported: " + strings.Join(skipped, ", ")
		}
		body += "\n\nINSERT INTO " + t.qualified() + " (" + strings.Join(cols, ", ") + ") VALUES (…)\n\n" +
			"The values are bound, in INSERTs of up to 1000 rows, all in one transaction: the first row that fails rolls everything back."
		ask := &webAsk{
			summary: w.c.ID + " " + a.Table + " " + rel,
			title:   "Import CSV?",
			body:    dbSafeMultiline(body),
			action:  "Import",
			run: func(m *Model) tea.Cmd {
				fresh, ok := m.webDBFindQuiet(w.c.ID)
				if !ok {
					return m.pushToastDetail(toast.Warn, "Nothing imported", "The connection changed.")
				}
				return func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 10*loadDBConsoleSettings().timeout())
					defer cancel()
					s, err := fresh.session(ctx, false)
					var res dbclient.ImportResult
					if err == nil {
						res = dbclient.ImportCSV(ctx, s, spec, nil)
						_ = s.Close()
						err = res.Err
					}
					return applyMsg(func(m *Model) tea.Cmd {
						if err != nil {
							detail := dbAIError(err, fresh.snap.password).Error()
							if res.FailedLine > 0 {
								detail = "Line " + strconv.Itoa(res.FailedLine) + ": " + detail
							}
							return m.pushToastDetail(toast.Errr, "Nothing imported into "+sanitizeDBText(a.Table), detail)
						}
						return m.pushToastDetail(toast.Info, "Imported "+dbPlural(int(res.Rows), "row"), sanitizeDBText(a.Table)+" on "+name)
					})
				}
			},
		}
		return open(ask)
	}
}

func (m *Model) webRedisKey(call *webCall) *webAsk {
	a := call.args.(*webproto.RedisKeyArgs)
	w, ok := m.webRedisFind(call, a.Conn)
	if !ok {
		return nil
	}
	if w.c.ReadOnly {
		call.fail(webproto.CodeForbidden, "This connection is read-only.")
		return nil
	}
	db := webRedisDB(w.c, a.DB)
	if !m.webDB().redisKeys[w.c.ID+"/"+strconv.Itoa(db)][a.Key] {
		call.fail(webproto.CodeNotFound, "That key is not in the list. List the keys again.")
		return nil
	}
	key := dbSafeMultiline(a.Key)
	name := sanitizeDBText(dbConnName(w.c))
	where := " on " + name + " (" + webDBWhere(w.c) + ", db " + strconv.Itoa(db) + ")"
	ask := &webAsk{summary: w.c.ID + " db " + strconv.Itoa(db) + " " + a.Verb}
	var args []string
	switch {
	case a.Verb == "delete":
		ask.title, ask.action = "Delete Redis key?", "Delete"
		ask.body = "Delete the key " + key + where + "?\n\nIts value is gone for good."
		args = []string{"DEL", a.Key}
	case a.TTL == 0:
		ask.title, ask.action = "Remove the TTL?", "Remove TTL"
		ask.body = "Keep the key " + key + where + " for ever (remove its TTL)?"
		args = []string{"PERSIST", a.Key}
	default:
		ask.title, ask.action = "Set the TTL?", "Set TTL"
		ask.body = "Expire the key " + key + where + " in " + strconv.FormatInt(a.TTL, 10) + " s?"
		args = []string{"EXPIRE", a.Key, strconv.FormatInt(a.TTL, 10)}
	}
	ask.run = func(m *Model) tea.Cmd {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), loadDBConsoleSettings().timeout())
			defer cancel()
			s, err := webRedisOpen(ctx, w.root, w.c, &w.snap, false)
			if err == nil {
				_, err = s.DoDB(ctx, db, args)
				_ = s.Close()
			}
			return applyMsg(func(m *Model) tea.Cmd {
				if err != nil {
					return m.pushToastDetail(toast.Errr, ask.action+" failed on "+name, dbAIClean(dbOneLine(err.Error()), w.snap.password))
				}
				return m.pushToastDetail(toast.Info, ask.action+" done", key+where)
			})
		}
	}
	return ask
}

// webDBDefaultPort is the usual port of a driver.
func webDBDefaultPort(driver string) int {
	switch driver {
	case "postgres":
		return 5432
	case "mysql":
		return 3306
	case "redis":
		return 6379
	case "cassandra":
		return 9042
	}
	return 0
}

// webDBFileInside checks a sqlite file a browser names: it, or (a new
// file) its folder, resolves inside the workspace with links followed.
func (m *Model) webDBFileInside(rel string) error {
	_, err := m.webResolve(rel)
	var we *webError
	if err == nil || !errors.As(err, &we) || we.code != webproto.CodeNotFound {
		return err
	}
	dir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(webCleanRel(rel))))
	if dir == "." {
		dir = ""
	}
	_, err = m.webResolve(dir)
	return err
}

func (m *Model) webDBAdd(call *webCall) *webAsk {
	a := call.args.(*webproto.DBAddArgs)
	root, err := m.rootDir()
	if err != nil {
		call.fail(webproto.CodeNotFound, "No workspace is open.")
		return nil
	}
	_, _, list := dbLoadConfigs(root)
	for _, c := range list {
		if c.ID == a.ID {
			call.fail(webproto.CodeConflict, "A connection with that id exists.")
			return nil
		}
	}
	c := dbclient.Conn{ID: a.ID, Name: a.Name, Driver: a.Driver, Host: a.Host, Port: a.Port, User: a.User, Database: a.Database, Path: a.Path,
		DB: a.DB, Environment: a.Env, ReadOnly: a.ReadOnly, Source: dbclient.SourceProject}
	if c.Driver != "sqlite" && c.Port == 0 {
		c.Port = webDBDefaultPort(c.Driver)
	}
	if err := dbclient.Validate(c); err != nil {
		call.fail(webproto.CodeBadRequest, sanitizeDBText(err.Error()))
		return nil
	}
	if c.Driver == "sqlite" {
		if err := m.webDBFileInside(c.Path); err != nil {
			webFailErr(call, err, "The database file cannot be used.")
			return nil
		}
	}
	var b strings.Builder
	b.WriteString("Save this connection to .termocode/databases.json?\n\n")
	line := func(k, v string) {
		if v != "" {
			b.WriteString(k + ": " + sanitizeDBText(v) + "\n")
		}
	}
	line("Name", dbConnName(c))
	line("Id", c.ID)
	line("Type", c.Driver)
	if c.Driver == "sqlite" {
		line("File", c.Path)
	} else {
		line("Server", c.Host+":"+strconv.Itoa(c.Port))
	}
	line("User", c.User)
	if c.Driver == "cassandra" {
		line("Keyspace", c.Database)
	} else {
		line("Database", c.Database)
	}
	if c.Driver == "redis" {
		line("Database number", strconv.Itoa(c.DB))
	}
	line("Environment", c.Environment)
	if c.ReadOnly {
		line("Read-only", "yes")
	}
	if c.Driver != "sqlite" {
		b.WriteString("\nNo password is saved. The laptop asks for it when you connect.")
	}
	return &webAsk{
		summary: c.ID + " " + c.Driver,
		title:   "Add a connection?",
		body:    strings.TrimRight(b.String(), "\n"),
		action:  "Save",
		run: func(m *Model) tea.Cmd {
			m.ensureDatabasesLoaded()
			if _, dup := m.databases.connection(c.ID); dup {
				return m.pushToastDetail(toast.Warn, "Not saved", "A connection with that id exists.")
			}
			m.databases.pendingConn = c
			m.databases.pendingPassword = ""
			if cmd := m.databaseSavePending(true); cmd != nil {
				return cmd
			}
			return m.pushToastDetail(toast.Info, "Connection added", sanitizeDBText(dbConnName(c))+" · connect to type its password")
		},
	}
}

// webDBSaved is a saved connection a setting may change (not SQLite).
func (m *Model) webDBSaved(call *webCall, id string) (dbclient.Conn, bool) {
	_, c, ok := m.webDBAny(call, id)
	if !ok {
		return c, false
	}
	if c.Driver == "sqlite" {
		call.fail(webproto.CodeBadRequest, "A SQLite database is a local file: it has no tunnel or SSL.")
		return c, false
	}
	return c, true
}

// webDBUpdate changes a saved connection after the card.
func webDBUpdate(id, what string, edit func(*dbclient.Conn)) func(m *Model) tea.Cmd {
	return func(m *Model) tea.Cmd {
		m.ensureDatabasesLoaded()
		if err := m.databaseUpdateConn(id, edit); err != nil {
			return m.pushToastDetail(toast.Errr, "Not saved", sanitizeDBText(err.Error()))
		}
		detail := "Saved for " + sanitizeDBText(id) + "."
		if m.databases.sessions[id] != nil {
			detail += " Connect again to use it."
		}
		return m.pushToastDetail(toast.Info, what+" saved", detail)
	}
}

func (m *Model) webDBSSH(call *webCall) *webAsk {
	a := call.args.(*webproto.DBSSHArgs)
	c, ok := m.webDBSaved(call, a.Conn)
	if !ok {
		return nil
	}
	name := sanitizeDBText(dbConnName(c))
	if a.Remove {
		if c.SSH == nil {
			call.fail(webproto.CodeBadRequest, "This connection has no SSH tunnel.")
			return nil
		}
		return &webAsk{summary: c.ID + " remove", title: "Remove the SSH tunnel?", action: "Remove",
			body: "Remove the SSH tunnel of " + name + "? It connects to " + sanitizeDBText(c.Host) + " directly after that.",
			run:  webDBUpdate(c.ID, "SSH tunnel", func(c *dbclient.Conn) { c.SSH = nil })}
	}
	ssh := dbclient.SSHConfig{Host: a.Host, Port: a.Port, User: a.User, Auth: a.Auth, KeyFile: a.KeyFile, Jump: a.Jump}
	if c.SSH != nil {
		ssh.KnownHosts = c.SSH.KnownHosts
	}
	target := a.Host
	if a.User != "" {
		target = a.User + "@" + target
	}
	if a.Port != 0 {
		target += ":" + strconv.Itoa(a.Port)
	}
	body := "Connect to " + name + " through the SSH server " + sanitizeDBText(target) + "?"
	if len(a.Jump) > 0 {
		body += "\nJump hosts: " + sanitizeDBText(strings.Join(a.Jump, ", "))
	}
	if a.Auth != "" {
		body += "\nSign-in: " + a.Auth
	}
	if a.KeyFile != "" {
		body += "\nKey file: " + sanitizeDBText(a.KeyFile)
	}
	body += "\n\nA password or key passphrase, if one is needed, is typed on the laptop when you connect."
	return &webAsk{summary: c.ID + " " + webName(a.Host), title: "Use an SSH tunnel?", action: "Save", body: body,
		run: webDBUpdate(c.ID, "SSH tunnel", func(c *dbclient.Conn) { s := ssh; c.SSH = &s })}
}

func (m *Model) webDBSSL(call *webCall) *webAsk {
	a := call.args.(*webproto.DBSSLArgs)
	c, ok := m.webDBSaved(call, a.Conn)
	if !ok {
		return nil
	}
	ssl := dbclient.SSLConfig{Mode: a.Mode, CA: a.CA, Cert: a.Cert, Key: a.Key}
	body := "Set SSL of " + sanitizeDBText(dbConnName(c)) + " to " + a.Mode + "?"
	for _, f := range [][2]string{{"CA", a.CA}, {"Client certificate", a.Cert}, {"Client key", a.Key}} {
		if f[1] != "" {
			body += "\n" + f[0] + ": " + sanitizeDBText(f[1])
		}
	}
	if a.Mode == "disable" {
		body += "\n\nThe connection is not encrypted."
	}
	return &webAsk{summary: c.ID + " " + a.Mode, title: "Change SSL settings?", action: "Save", body: body,
		run: webDBUpdate(c.ID, "SSL settings", func(c *dbclient.Conn) { s := ssl; c.SSL = &s })}
}

// webDBSession is a connection the laptop console has open.
func (m *Model) webDBSession(call *webCall, id string) (dbclient.Conn, bool) {
	_, c, ok := m.webDBAny(call, id)
	if !ok {
		return c, false
	}
	if m.databases == nil || m.databases.sessions[id] == nil {
		call.fail(webproto.CodeBadRequest, "The laptop is not connected to it.")
		return c, false
	}
	return c, true
}

func (m *Model) webDBDisconnect(call *webCall) *webAsk {
	id := call.args.(*webproto.DBConnArgs).Conn
	c, ok := m.webDBSession(call, id)
	if !ok {
		return nil
	}
	body := "Disconnect " + sanitizeDBText(dbConnName(c)) + " on the laptop?"
	if n := m.databases.txWrites(id); n > 0 {
		body += "\n\n" + dbPlural(n, "uncommitted change") + " are rolled back."
	}
	return &webAsk{summary: id, title: "Disconnect?", action: "Disconnect", body: body,
		run: func(m *Model) tea.Cmd {
			cmd := m.databaseDisconnectNow(id)
			return tea.Batch(cmd, m.pushToast(toast.Info, "Disconnected "+sanitizeDBText(dbConnName(c))))
		}}
}

func (m *Model) webDBTx(call *webCall) *webAsk {
	a := call.args.(*webproto.DBTxArgs)
	_, c, ok := m.webDBAny(call, a.Conn)
	if !ok {
		return nil
	}
	m.ensureDatabasesLoaded()
	s := m.databases
	name := sanitizeDBText(dbConnName(c))
	switch a.Verb {
	case "manual", "auto":
		if !webDBSQL(c.Driver) {
			call.fail(webproto.CodeBadRequest, "Redis has no transactions here.")
			return nil
		}
		manual := a.Verb == "manual"
		if s.manualMode(a.Conn) == manual {
			call.fail(webproto.CodeBadRequest, "It is in that mode already.")
			return nil
		}
		body := "Switch " + name + " to manual commit? Changes in the laptop's console wait for Commit or Roll back."
		title, action := "Manual commit?", "Switch"
		if !manual {
			body = "Switch " + name + " to auto-commit? Each statement commits by itself."
			if n := s.txWrites(a.Conn); n > 0 {
				body += "\n\nThe laptop asks first what to do with " + dbPlural(n, "uncommitted change") + "."
			}
			title = "Auto-commit?"
		}
		return &webAsk{summary: a.Conn + " " + a.Verb, title: title, action: action, body: body,
			run: func(m *Model) tea.Cmd { return m.databaseSetTxMode(a.Conn, manual) }}
	}
	if len(s.txKeys(a.Conn)) == 0 {
		call.fail(webproto.CodeBadRequest, "No transaction is open on it.")
		return nil
	}
	commit := a.Verb == "commit"
	n := dbPlural(s.txWrites(a.Conn), "uncommitted change")
	ask := &webAsk{summary: a.Conn + " " + a.Verb, run: func(m *Model) tea.Cmd { return m.databaseEndTx(a.Conn, commit, nil) }}
	if commit {
		ask.title, ask.action, ask.body = "Commit?", "Commit", "Commit the open transaction on "+name+" ("+n+")? They are saved."
	} else {
		ask.title, ask.action, ask.body = "Roll back?", "Roll back", "Roll back the open transaction on "+name+" ("+n+")? They are dropped."
	}
	return ask
}
