package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Redis, part two (DBX-51): server-side key search by pattern, a new-key
// form, RedisJSON paths, module info (TimeSeries, Bloom, Cuckoo, Top-K,
// Count-Min, Search), MEMORY STATS, pub/sub channels and a bounded
// channel listen, stream consumer groups, CLUSTER NODES and key export.
// Every write shows the Redis card with the exact command (Cancel first);
// the export asks before it writes the file.

const (
	promptKindDBRedisSearch promptKindEnum = 7510 + iota
	promptKindDBRedisNewType
	promptKindDBRedisNewName
	promptKindDBRedisNewValue
	promptKindDBRedisJSONPath
	promptKindDBRedisExportPattern
	promptKindDBRedisExportPath
	promptKindDBRedisListen
)

const confirmKindDBRedisExport confirmKindEnum = 7520

// dbRedisListenFor and dbRedisListenMax bound a channel listen.
const (
	dbRedisListenFor = 10 * time.Second
	dbRedisListenMax = 200
)

// dbRedisMore is the state of a multi-step flow.
type dbRedisMore struct {
	conn    string
	db      int
	typ     string // new key: its type
	key     string // new key: its name; JSON path: the key
	pattern string // export: the key pattern
	path    string // export: the file
	display string
	keys    []string // export: the keys the scan found
	more    bool
}

func dbRedisMorePaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "redis-search-keys", Title: "Databases: Search Redis Keys by Pattern…", Hint: "SCAN MATCH on the server, never KEYS"},
		{ID: "redis-new-key", Title: "Databases: New Redis Key…", Hint: "string, hash, list, set, sorted set, stream or JSON"},
		{ID: "redis-json-path", Title: "Databases: Read RedisJSON Path…", Hint: "JSON.GET on the open or selected key"},
		{ID: "redis-key-module-info", Title: "Databases: Show Redis Key Module Info", Hint: "TS.INFO, BF.INFO, CF.INFO, … for the key's type"},
		{ID: "redis-modules", Title: "Databases: Show Redis Modules", Hint: "MODULE LIST"},
		{ID: "redis-search-indexes", Title: "Databases: Show RediSearch Indexes", Hint: "FT._LIST"},
		{ID: "redis-memory", Title: "Databases: Show Redis Memory Stats", Hint: "MEMORY STATS"},
		{ID: "redis-pubsub-channels", Title: "Databases: Show Redis Pub/Sub Channels", Hint: "PUBSUB CHANNELS"},
		{ID: "redis-listen", Title: "Databases: Listen to Redis Channels…", Hint: "collects messages for 10 seconds"},
		{ID: "redis-stream-groups", Title: "Databases: Show Redis Stream Groups", Hint: "XINFO GROUPS on the stream"},
		{ID: "redis-cluster-nodes", Title: "Databases: Show Redis Cluster Nodes", Hint: "CLUSTER NODES"},
		{ID: "redis-export-keys", Title: "Databases: Export Redis Keys…", Hint: "keys matching a pattern to a JSON file"},
	}
}

func dbRedisMoreID(id string) bool {
	for _, it := range dbRedisMorePaletteItems() {
		if it.ID == id {
			return true
		}
	}
	return false
}

// dbRedisPatternLine is the console line of a pattern search: KEYS, which
// the session runs as SCAN MATCH (never KEYS).
func dbRedisPatternLine(pattern string) (string, error) {
	p := strings.TrimSpace(pattern)
	if p == "" {
		return "", errors.New("type a pattern, such as user:*")
	}
	return dbclient.FormatRedisCommand([]string{"KEYS", p}), nil
}

// dbRedisNewKeyType reads the typed type ("" = string).
func dbRedisNewKeyType(v string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(v))
	switch t {
	case "":
		return "string", nil
	case "sorted set", "sortedset", "zset":
		return "zset", nil
	}
	for _, k := range dbclient.RedisNewKeyTypes {
		if t == k {
			return t, nil
		}
	}
	return "", errors.New("type one of: " + strings.Join(dbclient.RedisNewKeyTypes, ", "))
}

// dbRedisNewValueLabel says what the value prompt of a type wants.
func dbRedisNewValueLabel(typ string) string {
	switch typ {
	case "hash":
		return "Fields as pairs (name Ada role admin; quote values with spaces):"
	case "list":
		return "The first item of the list:"
	case "set":
		return "The first member of the set:"
	case "zset":
		return "A score and a member (1.5 ada):"
	case "stream":
		return "The first entry as field value pairs (event login user 7):"
	case "json":
		return `A JSON document ({"name": "Ada"}):`
	}
	return "The value:"
}

// dbRedisExportDefault is the suggested export file of a connection.
func dbRedisExportDefault(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if r == '.' || r == '_' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "redis"
	}
	return "redis-export-" + slug + ".json"
}

// databaseRedisConn is the current connection when it is a Redis one.
func (m *Model) databaseRedisConn() (dbclient.Conn, tea.Cmd, bool) {
	c, cmd, ok := m.databaseSessionConn()
	if !ok {
		return c, cmd, false
	}
	if c.Driver != "redis" {
		return c, m.pushToastDetail(toast.Info, "Not a Redis connection", "Select a Redis or Valkey connection first."), false
	}
	return c, nil, true
}

// databaseRedisRowDB is the database of the selected tree row of conn,
// else the connection's own.
func (m *Model) databaseRedisRowDB(c dbclient.Conn) int {
	if row, ok := m.databaseCurrentRow(); ok && row.conn == c.ID {
		if n, ok := m.databases.nodes[row.path]; ok && (n.kind == dbNodeDatabase || n.kind == dbNodeRedisKey || n.kind == dbNodeRedisNS) {
			return n.rdb
		}
	}
	return redisConnDB(c)
}

// databaseRedisMorePalette runs the palette commands of this file.
func (m *Model) databaseRedisMorePalette(id string) tea.Cmd {
	switch id {
	case "redis-json-path", "redis-key-module-info", "redis-stream-groups":
		e, ok := m.databaseRedisTarget()
		if !ok {
			return m.pushToastDetail(toast.Info, "Select a Redis key first", "Open a key, or select one in the Databases view.")
		}
		if m.databases.sessions[e.conn] == nil {
			return m.databaseConnect(e.conn)
		}
		switch id {
		case "redis-json-path":
			m.dbConsole().redisMore = &dbRedisMore{conn: e.conn, db: e.db, key: e.key}
			return m.openHTTPPrompt(promptKindDBRedisJSONPath, "RedisJSON path", "JSONPath to read from "+sanitizeDBText(dbclient.RedisDisplay(e.key))+" ($ is the whole document):", "$")
		case "redis-stream-groups":
			if e.typ != "stream" {
				return m.pushToastDetail(toast.Info, "Not a stream", sanitizeDBText(dbclient.RedisDisplay(e.key))+" is a "+sanitizeDBText(e.typ)+".")
			}
		}
		args, ok := dbclient.RedisModuleInfoArgs(e.typ, e.key)
		if !ok {
			return m.pushToastDetail(toast.Info, "No module info for this key", sanitizeDBText(dbclient.RedisDisplay(e.key))+" is a "+sanitizeDBText(e.typ)+"; module info is for TimeSeries, Bloom, Cuckoo, Top-K, Count-Min, JSON and streams.")
		}
		return m.databaseRedisRead(e.conn, e.db, args)
	}
	c, cmd, ok := m.databaseRedisConn()
	if !ok {
		return cmd
	}
	switch id {
	case "redis-modules":
		return m.databaseRunInConsole(c.ID, "MODULE LIST")
	case "redis-search-indexes":
		return m.databaseRunInConsole(c.ID, "FT._LIST")
	case "redis-memory":
		return m.databaseRunInConsole(c.ID, "MEMORY STATS")
	case "redis-pubsub-channels":
		return m.databaseRunInConsole(c.ID, "PUBSUB CHANNELS")
	case "redis-cluster-nodes":
		return m.databaseRunInConsole(c.ID, "CLUSTER NODES")
	}
	if m.databases.sessions[c.ID] == nil {
		return m.databaseConnect(c.ID)
	}
	st := &dbRedisMore{conn: c.ID, db: m.databaseRedisRowDB(c)}
	m.dbConsole().redisMore = st
	switch id {
	case "redis-search-keys":
		return m.openHTTPPrompt(promptKindDBRedisSearch, "Search keys", "Key pattern (* any text, ? one character, [ab] a set); the server scans with SCAN MATCH:", "*")
	case "redis-new-key":
		return m.openHTTPPrompt(promptKindDBRedisNewType, "New key", "Type: "+strings.Join(dbclient.RedisNewKeyTypes, ", ")+":", "string")
	case "redis-listen":
		return m.openHTTPPrompt(promptKindDBRedisListen, "Listen to channels", "Channel pattern (* for every channel); messages are collected for 10 seconds:", "*")
	case "redis-export-keys":
		return m.openHTTPPrompt(promptKindDBRedisExportPattern, "Export keys", "Pattern of the keys to export:", "*")
	}
	return nil
}

// databaseRedisMorePrompt handles the answers of the prompts above.
func (m *Model) databaseRedisMorePrompt(kind promptKindEnum, value string) (tea.Cmd, bool) {
	if kind < promptKindDBRedisSearch || kind > promptKindDBRedisListen {
		return nil, false
	}
	s := m.dbConsole()
	st := s.redisMore
	if st == nil {
		return nil, true
	}
	warn := func(title string, err error) (tea.Cmd, bool) {
		s.redisMore = nil
		return m.pushToastDetail(toast.Warn, title, err.Error()), true
	}
	switch kind {
	case promptKindDBRedisSearch:
		s.redisMore = nil
		line, err := dbRedisPatternLine(value)
		if err != nil {
			return warn("Nothing searched", err)
		}
		return m.databaseRunInConsole(st.conn, line), true
	case promptKindDBRedisNewType:
		typ, err := dbRedisNewKeyType(value)
		if err != nil {
			return warn("No key made", err)
		}
		st.typ = typ
		return m.openHTTPPrompt(promptKindDBRedisNewName, "New "+typ+" key", "Key name (db "+strconv.Itoa(st.db)+"):", ""), true
	case promptKindDBRedisNewName:
		if strings.TrimSpace(value) == "" {
			return warn("No key made", errors.New("type a key name"))
		}
		st.key = value
		return m.openHTTPPrompt(promptKindDBRedisNewValue, "New "+st.typ+" key", dbRedisNewValueLabel(st.typ), ""), true
	case promptKindDBRedisNewValue:
		s.redisMore = nil
		args, err := dbclient.RedisNewKeyArgs(st.typ, st.key, value)
		if err != nil {
			return warn("No key made", err)
		}
		return m.databaseRedisNewKeyCheck(st, args), true
	case promptKindDBRedisJSONPath:
		s.redisMore = nil
		return m.databaseRedisRead(st.conn, st.db, dbclient.RedisJSONGet(st.key, value)), true
	case promptKindDBRedisListen:
		s.redisMore = nil
		p := strings.TrimSpace(value)
		if p == "" {
			p = "*"
		}
		return m.databaseRedisListen(st.conn, p), true
	case promptKindDBRedisExportPattern:
		st.pattern = strings.TrimSpace(value)
		if st.pattern == "" {
			st.pattern = "*"
		}
		name := st.conn
		if c, ok := m.databases.connection(st.conn); ok {
			name = dbConnName(c)
		}
		return m.openHTTPPrompt(promptKindDBRedisExportPath, "Export keys", "File to write (in the workspace unless the path says otherwise):", dbRedisExportDefault(name)), true
	case promptKindDBRedisExportPath:
		abs, display, err := m.dbResolvePath(value)
		if err != nil {
			return warn("Cannot export", err)
		}
		if fi, serr := os.Stat(abs); serr == nil && fi.IsDir() {
			return warn("Cannot export", errors.New(sanitizeDBText(display)+" is a folder."))
		}
		st.path, st.display = abs, display
		return m.databaseRedisExportScan(st), true
	}
	return nil, true
}

// dbRedisReadMsg is the reply of a key read run off the main loop.
type dbRedisReadMsg struct {
	conn  string
	args  []string
	reply any
	err   error
	took  time.Duration
}

// databaseRedisRead runs a read command on database db and shows the
// reply as a grid (the readable grids of RedisAdminGrid when they fit).
func (m *Model) databaseRedisRead(conn string, db int, args []string) tea.Cmd {
	sess, _ := m.databases.sessions[conn].(dbRedis)
	if sess == nil {
		return m.databaseConnect(conn)
	}
	timeout := loadDBConsoleSettings().timeout()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		start := time.Now()
		reply, err := sess.DoDB(ctx, db, args)
		return dbRedisReadMsg{conn: conn, args: args, reply: reply, err: err, took: time.Since(start)}
	}
}

func (m *Model) onDBRedisRead(msg dbRedisReadMsg) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	res := dbclient.RedisReplyResult(msg.reply, loadDBConsoleSettings().pageRows()*100)
	if grid, ok := dbclient.RedisAdminGrid(msg.args, msg.reply); ok && msg.err == nil {
		res = grid
	}
	res.Err, res.Elapsed = msg.err, msg.took
	if msg.err != nil {
		res.Columns, res.Rows = nil, nil
	}
	return m.databaseShowRedisRun(&dbRun{conn: msg.conn, sql: dbclient.FormatRedisCommand(msg.args), by: "user", result: res})
}

// databaseShowRedisRun shows r on the result page and adds it to the
// history.
func (m *Model) databaseShowRedisRun(r *dbRun) tea.Cmd {
	s := m.dbConsole()
	hist := m.databaseAppendHistory(r)
	if s.running != nil {
		return hist
	}
	s.placeRun(r)
	s.page = 0
	s.size = loadDBConsoleSettings().pageRows()
	s.filter = ""
	s.sortCol = -1
	s.trans, s.trow = false, 0
	s.view = dbViewGrid
	if r.result.Err != nil || len(r.result.Columns) == 0 {
		s.view = dbViewMessages
	}
	return tea.Batch(m.databaseShowPage(), hist)
}

// dbRedisNewKeyCheckMsg says whether the new key's name is taken.
type dbRedisNewKeyCheckMsg struct {
	st     *dbRedisMore
	args   []string
	exists bool
	err    error
}

// databaseRedisNewKeyCheck asks the server whether the name is free
// before the card: the form never adds to an existing key.
func (m *Model) databaseRedisNewKeyCheck(st *dbRedisMore, args []string) tea.Cmd {
	sess, _ := m.databases.sessions[st.conn].(dbRedis)
	if sess == nil {
		return m.databaseConnect(st.conn)
	}
	timeout := loadDBConsoleSettings().timeout()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		reply, err := sess.DoDB(ctx, st.db, []string{"EXISTS", st.key})
		n, _ := reply.(int64)
		return dbRedisNewKeyCheckMsg{st: st, args: args, exists: n > 0, err: err}
	}
}

func (m *Model) onDBRedisNewKeyCheck(msg dbRedisNewKeyCheckMsg) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Could not check the key name", dbErrorText(msg.err))
	}
	name := dbOneLine(dbclient.RedisDisplay(msg.st.key))
	if msg.exists {
		return m.pushToastDetail(toast.Warn, "A key named "+name+" already exists", "Nothing changed. Pick another name, or open the key to edit it.")
	}
	return m.databaseRedisAsk(&dbRedisWrite{conn: msg.st.conn, db: msg.st.db, key: msg.st.key, args: msg.args, admin: "new", dst: msg.st.key})
}

// dbRedisListenMsg is the end of a channel listen.
type dbRedisListenMsg struct {
	conn, pattern string
	msgs          []dbclient.RedisMessage
	more          bool
	err           error
	took          time.Duration
}

type dbRedisListener interface {
	Listen(ctx context.Context, pattern string, max int) ([]dbclient.RedisMessage, bool, error)
}

// databaseRedisListen collects the messages of the channels matching
// pattern for dbRedisListenFor on a connection of its own.
func (m *Model) databaseRedisListen(conn, pattern string) tea.Cmd {
	l, _ := m.databases.sessions[conn].(dbRedisListener)
	if l == nil {
		return m.pushToastDetail(toast.Info, "Cannot listen here", "This session cannot subscribe to channels.")
	}
	return tea.Batch(m.pushToast(toast.Info, "Listening to "+sanitizeDBText(dbclient.RedisDisplay(pattern))+" for 10 seconds…"), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbRedisListenFor)
		defer cancel()
		start := time.Now()
		msgs, more, err := l.Listen(ctx, pattern, dbRedisListenMax)
		return dbRedisListenMsg{conn: conn, pattern: pattern, msgs: msgs, more: more, err: err, took: time.Since(start)}
	})
}

func (m *Model) onDBRedisListen(msg dbRedisListenMsg) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	res := dbclient.RedisMessagesResult(msg.msgs, msg.more)
	res.Err, res.Elapsed = msg.err, msg.took
	if msg.more {
		res.Notices = append(res.Notices, "Stopped at "+strconv.Itoa(dbRedisListenMax)+" messages")
	}
	return m.databaseShowRedisRun(&dbRun{conn: msg.conn, sql: dbclient.FormatRedisCommand([]string{"PSUBSCRIBE", msg.pattern}) + " (10 s)", by: "user", result: res})
}

// dbRedisExportScanMsg is the key list of an export.
type dbRedisExportScanMsg struct {
	st   *dbRedisMore
	keys []string
	more bool
	err  error
}

func (m *Model) databaseRedisExportScan(st *dbRedisMore) tea.Cmd {
	sess, _ := m.databases.sessions[st.conn].(dbRedis)
	if sess == nil {
		m.dbConsole().redisMore = nil
		return m.databaseConnect(st.conn)
	}
	settings := loadDBConsoleSettings()
	maxKeys, timeout := settings.redisMaxKeys(), settings.timeout()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		keys, more, err := sess.ScanKeys(ctx, st.db, st.pattern, maxKeys)
		return dbRedisExportScanMsg{st: st, keys: keys, more: more, err: err}
	}
}

// onDBRedisExportScan shows the export card: how many keys, the file,
// Cancel first; Replace when the file exists.
func (m *Model) onDBRedisExportScan(msg dbRedisExportScanMsg) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	s := m.dbConsole()
	if s.redisMore != msg.st {
		return nil
	}
	if msg.err != nil {
		s.redisMore = nil
		return m.pushToastDetail(toast.Errr, "Could not read the keys", dbErrorText(msg.err))
	}
	if len(msg.keys) == 0 {
		s.redisMore = nil
		return m.pushToastDetail(toast.Info, "No keys to export", "No key matches "+sanitizeDBText(dbclient.RedisDisplay(msg.st.pattern))+".")
	}
	msg.st.keys, msg.st.more = msg.keys, msg.more
	body := "Write " + dbRedisCount(int64(len(msg.keys)), msg.more) + " matching " + dbSafeMultiline(dbclient.RedisDisplay(msg.st.pattern)) +
		" (db " + strconv.Itoa(msg.st.db) + ") with their types, TTLs and values to\n" + sanitizeDBText(msg.st.display) + "\n\nThe file holds the values in plain text."
	if msg.more {
		body += " The scan stopped at db_redis_max_keys; the rest is not exported."
	}
	title, run := "Export Redis keys?", confirm.Button{ID: "export", Title: "Export", Style: confirm.StylePrimary}
	if fi, err := os.Stat(msg.st.path); err == nil && !fi.IsDir() {
		title = "Replace the existing file?"
		run = confirm.Button{ID: "export", Title: "Replace", Style: confirm.StyleDestructive}
	}
	m.confirm = confirm.New(title, body, []confirm.Button{{ID: "cancel", Title: "Cancel"}, run}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBRedisExport
	return nil
}

// dbRedisExportDoneMsg is the end of an export.
type dbRedisExportDoneMsg struct {
	display string
	n       int
	err     error
}

func (m *Model) databaseRedisExportConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindDBRedisExport {
		return nil, false
	}
	s := m.dbConsole()
	st := s.redisMore
	s.redisMore = nil
	if id != "export" || st == nil {
		return nil, true
	}
	sess, _ := m.databases.sessions[st.conn].(dbRedis)
	if sess == nil {
		return m.pushToastDetail(toast.Warn, "Not connected", "Connect to "+sanitizeDBText(st.conn)+" first."), true
	}
	timeout := loadDBConsoleSettings().timeout()
	return tea.Batch(m.pushToast(toast.Info, "Exporting "+strconv.Itoa(len(st.keys))+" keys…"), func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		entries, err := dbclient.RedisExportKeys(ctx, func(ctx context.Context, args []string) (any, error) {
			cctx, ccancel := context.WithTimeout(ctx, timeout)
			defer ccancel()
			return sess.DoDB(cctx, st.db, args)
		}, st.keys)
		if err != nil {
			return dbRedisExportDoneMsg{display: st.display, err: err}
		}
		return dbRedisExportDoneMsg{display: st.display, n: len(entries), err: dbRedisWriteExport(st.path, entries)}
	}), true
}

// dbRedisWriteExport writes the entries as indented JSON (0600), through
// a temporary file so a failed export never leaves half a file.
func dbRedisWriteExport(path string, entries []dbclient.RedisExportEntry) error {
	if entries == nil {
		entries = []dbclient.RedisExportEntry{}
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (m *Model) onDBRedisExportDone(msg dbRedisExportDoneMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Export failed", dbErrorText(msg.err))
	}
	return m.pushToastDetail(toast.Info, "Exported "+strconv.Itoa(msg.n)+" keys", sanitizeDBText(msg.display))
}
