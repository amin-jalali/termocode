package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Redis in the Databases view (plan F12, DB-10): the tree lists the
// databases of the keyspace, then the keys of a database grouped into
// namespaces by db_redis_separator (":"), loaded with SCAN only when a row
// is expanded (never KEYS, at most db_redis_max_keys). Enter on a key opens
// its value page on the SQL Result page: one server page at a time
// (HSCAN, SSCAN, ZRANGE, LRANGE, XRANGE, GETRANGE). Every write (edit,
// add, delete field, TTL, delete key) asks first with the exact command.
// Nothing runs while the view is hidden: the session closes with it.

const (
	confirmKindDBRedisWrite confirmKindEnum = 1450 + iota
)

const (
	promptKindDBRedisValue promptKindEnum = 1450 + iota
	promptKindDBRedisAdd
	promptKindDBRedisAddValue
	promptKindDBRedisTTL
)

// dbRedisDial is the dialer of new Redis sessions (nil: TCP; tests: a
// fake server over net.Pipe; DB-11: the SSH tunnel).
var dbRedisDial dbclient.RedisDialFunc

// dbRedisSessionFactory opens a Redis session (the view's and the AI's).
var dbRedisSessionFactory = func(ctx context.Context, c dbclient.Conn, password string) (dbclient.Session, error) {
	s, err := dbclient.NewRedisSession(ctx, c, password, dbRedisDial)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// dbRedis is what the view needs from a Redis session.
type dbRedis interface {
	dbclient.Session
	Keyspace(context.Context) ([]dbclient.RedisDB, error)
	ScanKeys(ctx context.Context, db int, pattern string, max int) ([]string, bool, error)
	Types(ctx context.Context, db int, keys []string) ([]dbclient.RedisKeyMeta, error)
	KeyInfo(ctx context.Context, db int, key string) (dbclient.RedisKeyInfo, error)
	ValuePage(ctx context.Context, db int, key, typ, from string, count int) (dbclient.RedisValuePage, error)
	DoDB(ctx context.Context, db int, args []string) (any, error)
}

func (s dbConsoleSettings) redisSeparator() string {
	if s.RedisSep == nil {
		return ":"
	}
	return *s.RedisSep
}

func (s dbConsoleSettings) redisMaxKeys() int {
	n := intOr(s.RedisMaxKeys, 10000)
	if n < 1 {
		n = 10000
	}
	return min(max(n, 100), 1000000)
}

func (s *dbState) isRedis(conn string) bool {
	c, ok := s.connection(conn)
	return ok && c.Driver == "redis"
}

// ── the tree ────────────────────────────────────────────────────────────

type dbRedisTreeMsg struct {
	path       string
	connection string
	epoch      uint64
	generation uint64
	dbs        []dbclient.RedisDB
	keys       []string
	meta       map[string]dbclient.RedisKeyMeta
	more       bool
	max        int
	err        error
}

// dbRedisGroup splits the keys under prefix into namespaces (the next
// separator after prefix; with their key counts) and plain keys.
func dbRedisGroup(keys []string, prefix, sep string) (ns []string, counts map[string]int, leaves []string) {
	counts = map[string]int{}
	for _, k := range keys {
		rest, ok := strings.CutPrefix(k, prefix)
		if !ok {
			continue
		}
		if sep != "" {
			if i := strings.Index(rest, sep); i >= 0 {
				name := prefix + rest[:i+len(sep)]
				if name != k {
					if counts[name] == 0 {
						ns = append(ns, name)
					}
					counts[name]++
					continue
				}
			}
		}
		leaves = append(leaves, k)
	}
	sort.Strings(ns)
	sort.Strings(leaves)
	return ns, counts, leaves
}

// databaseRedisExpand loads the children of a Redis row: the databases
// of a connection, or the namespaces and keys of a database / namespace.
func (m *Model) databaseRedisExpand(path string) tea.Cmd {
	s := m.databases
	n, ok := s.nodes[path]
	if !ok || n.loading {
		return nil
	}
	sess, _ := s.sessions[n.conn].(dbRedis)
	if sess == nil {
		return m.databaseConnect(n.conn)
	}
	n.loading = true
	s.nodes[path] = n
	s.touch()
	epoch, generation := s.connectionEpochs[n.conn], s.generation
	settings := loadDBConsoleSettings()
	maxKeys := settings.redisMaxKeys()
	timeout := settings.timeout()
	kind, db, prefix, conn := n.kind, n.rdb, n.key, n.conn
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		msg := dbRedisTreeMsg{path: path, connection: conn, epoch: epoch, generation: generation, max: maxKeys}
		if kind == dbNodeConnection {
			msg.dbs, msg.err = sess.Keyspace(ctx)
			return msg
		}
		msg.keys, msg.more, msg.err = sess.ScanKeys(ctx, db, dbclient.RedisGlobEscape(prefix)+"*", maxKeys)
		if msg.err != nil {
			return msg
		}
		_, _, leaves := dbRedisGroup(msg.keys, prefix, settings.redisSeparator())
		meta, err := sess.Types(ctx, db, leaves)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.meta = map[string]dbclient.RedisKeyMeta{}
		for i, k := range leaves {
			if i < len(meta) {
				msg.meta[k] = meta[i]
			}
		}
		return msg
	}
}

func (m *Model) onDBRedisTree(msg dbRedisTreeMsg) tea.Cmd {
	s := m.databases
	if s == nil || msg.generation != s.generation || !m.databasesVisible() || msg.epoch != s.connectionEpochs[msg.connection] {
		return nil
	}
	n, ok := s.nodes[msg.path]
	if !ok {
		return nil
	}
	n.loading = false
	if msg.err != nil {
		n.problem = dbErrorText(msg.err)
		s.nodes[msg.path] = n
		s.touch()
		return m.pushToastDetail(toast.Errr, "Could not read the keys", dbErrorText(msg.err))
	}
	for p, old := range s.nodes {
		if old.conn == n.conn && strings.HasPrefix(p, msg.path+"/") {
			delete(s.nodes, p)
		}
	}
	n.children, n.problem, n.loaded = nil, "", true
	add := func(child dbNode) {
		s.nodes[child.path] = child
		n.children = append(n.children, child.path)
	}
	if n.kind == dbNodeConnection {
		for _, d := range msg.dbs {
			name := "db" + strconv.Itoa(d.Index)
			add(dbNode{kind: dbNodeDatabase, name: name, path: msg.path + "/db/" + strconv.Itoa(d.Index), conn: n.conn, rdb: d.Index, detail: dbRedisCount(d.Keys, false)})
		}
		s.nodes[msg.path] = n
		s.touch()
		return nil
	}
	sep := loadDBConsoleSettings().redisSeparator()
	ns, counts, leaves := dbRedisGroup(msg.keys, n.key, sep)
	for _, name := range ns {
		label := sanitizeDBText(dbclient.RedisDisplay(strings.TrimPrefix(name, n.key)))
		add(dbNode{kind: dbNodeRedisNS, name: label, path: msg.path + "/ns/" + name, conn: n.conn, rdb: n.rdb, key: name, detail: dbRedisCount(int64(counts[name]), msg.more)})
	}
	for _, k := range leaves {
		meta := msg.meta[k]
		label := sanitizeDBText(dbclient.RedisDisplay(strings.TrimPrefix(k, n.key)))
		if label == "" {
			label = sanitizeDBText(dbclient.RedisDisplay(k))
		}
		add(dbNode{kind: dbNodeRedisKey, name: label, path: msg.path + "/key/" + k, conn: n.conn, rdb: n.rdb, key: k, rtype: meta.Type, detail: dbRedisTypeName(meta.Type), ttl: meta.TTL})
	}
	switch {
	case msg.more:
		p := msg.path + "/more"
		s.nodes[p] = dbNode{kind: dbNodeRedisMore, name: fmt.Sprintf("… first %d keys; open a namespace", msg.max), path: p, conn: n.conn}
		n.children = append(n.children, p)
	case len(msg.keys) == 0:
		p := msg.path + "/empty"
		s.nodes[p] = dbNode{kind: dbNodeRedisMore, name: "no keys", path: p, conn: n.conn}
		n.children = append(n.children, p)
	}
	s.nodes[msg.path] = n
	s.touch()
	return nil
}

// dbRedisCount is "1 234 keys" ("+" when the scan stopped at the cap).
func dbRedisCount(n int64, more bool) string {
	s := strconv.FormatInt(n, 10)
	if more {
		s += "+"
	}
	if n == 1 && !more {
		return s + " key"
	}
	return s + " keys"
}

func dbRedisTypeName(t string) string {
	if t == "none" {
		return "gone"
	}
	return t
}

// dbRedisTypeGlyph is the one-cell type mark of a key row (no ambiguous
// width; ASCII letters in the ASCII tier).
func dbRedisTypeGlyph(t string) string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		switch t {
		case "string":
			return "s"
		case "hash":
			return "h"
		case "list":
			return "l"
		case "set":
			return "S"
		case "zset":
			return "z"
		case "stream":
			return "x"
		}
		return "?"
	}
	switch t {
	case "string":
		return `"`
	case "hash":
		return "{"
	case "list":
		return "["
	case "set":
		return "("
	case "zset":
		return "⇅"
	case "stream":
		return "≋"
	}
	return "?"
}

// dbRedisTTL is a TTL in milliseconds as "23 min" ("" without one).
func dbRedisTTL(ms int64) string {
	if ms < 0 {
		return ""
	}
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}

func dbRedisSize(n int64) string {
	switch {
	case n < 0:
		return ""
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
}

// ── the value page ──────────────────────────────────────────────────────

// dbRedisView is the key a value page shows and where its page starts.
type dbRedisView struct {
	db    int
	key   string
	info  dbclient.RedisKeyInfo
	from  string   // where this page starts ("": the first page)
	prev  []string // where the earlier pages start ([)
	next  string   // where the next page starts ("": the last one)
	raw   [][]string
	cols  []string
	trunc bool
}

// databaseRedisOpen shows key on the value page (a new tab, or the same
// tab for a page change or a reload).
func (m *Model) databaseRedisOpen(conn string, db int, key, from string, prev []string, tab *dbTab) tea.Cmd {
	v := &dbRedisView{db: db, key: key, from: from, prev: prev}
	r := &dbRun{conn: conn, sql: "Redis: " + key, by: "user", redis: v, tab: tab, line: 0}
	r.statements = []string{r.sql}
	return m.databaseStartRun(r)
}

// dbRedisLoad reads the header and one page of a value (off the UI).
func dbRedisLoad(ctx context.Context, sess dbclient.Session, v dbRedisView, count int) (dbclient.Result, *dbRedisView) {
	start := time.Now()
	rs, ok := sess.(dbRedis)
	if !ok {
		return dbclient.Result{Err: fmt.Errorf("not a Redis session")}, nil
	}
	info, err := rs.KeyInfo(ctx, v.db, v.key)
	if err != nil {
		return dbclient.Result{Err: err, Elapsed: time.Since(start)}, nil
	}
	v.info = info
	if info.Type == "none" {
		return dbclient.Result{Err: fmt.Errorf("the key %s does not exist (deleted or expired)", dbclient.RedisDisplay(v.key)), Elapsed: time.Since(start)}, &v
	}
	p, err := rs.ValuePage(ctx, v.db, v.key, info.Type, v.from, count)
	if err != nil {
		return dbclient.Result{Err: err, Elapsed: time.Since(start)}, &v
	}
	v.raw, v.cols, v.next, v.trunc = p.Rows, p.Columns, p.Next, p.Truncated
	res := dbclient.Result{Elapsed: time.Since(start)}
	for _, c := range p.Columns {
		res.Columns = append(res.Columns, dbclient.Column{Name: c})
	}
	for _, row := range p.Rows {
		cells := make([]dbclient.Cell, len(row))
		for i, x := range row {
			cells[i] = dbclient.Cell{Text: dbclient.RedisDisplay(x)}
			if x == "" {
				cells[i] = dbclient.Cell{Text: "<empty>", Kind: "nil"}
			}
			if (p.Columns[i] == "index" || p.Columns[i] == "score") && x != "" {
				cells[i].Kind = "number"
			}
		}
		res.Rows = append(res.Rows, cells)
	}
	return res, &v
}

// dbRedisSummary is row 2 of a value page, longest first.
func dbRedisSummary(c dbclient.Conn, v *dbRedisView) []string {
	unit := map[string]string{"string": "byte", "hash": "field", "list": "item", "set": "member", "zset": "member", "stream": "entry"}[v.info.Type]
	parts := []string{v.info.Type}
	if unit != "" {
		word := dbPlural(int(v.info.Len), unit)
		if unit == "entry" && v.info.Len != 1 {
			word = fmt.Sprintf("%d entries", v.info.Len)
		}
		parts = append(parts, word)
	}
	if ttl := dbRedisTTL(v.info.TTL); ttl != "" {
		parts = append(parts, "TTL "+ttl)
	}
	if size := dbRedisSize(v.info.Bytes); size != "" {
		parts = append(parts, size)
	}
	if v.trunc {
		parts = append(parts, "first 64 KB")
	}
	pageNo := ""
	if len(v.prev) > 0 || v.next != "" {
		pageNo = fmt.Sprintf("page %d", len(v.prev)+1)
	}
	prod := ""
	if c.Environment == "production" {
		prod = " · prod"
	}
	join := func(p []string) string {
		if pageNo != "" {
			p = append(append([]string(nil), p...), pageNo)
		}
		return strings.Join(p, " · ") + prod
	}
	return []string{join(parts), join(parts[:min(3, len(parts))]), join(parts[:min(2, len(parts))]), join(parts[:1])}
}

// dbRedisHints is row 3 of a value page.
func dbRedisHints(width int, v *dbRedisView) string {
	var keys []string
	switch v.info.Type {
	case "string":
		keys = []string{"e edit", "t ttl", "D delete key", "y copy", "Tab json", "r reload", "q close"}
	case "hash":
		keys = []string{"e edit field", "+ add", "- delete field", "t ttl", "D delete key", "]/[ page", "y copy", "Tab json", "q close"}
	case "list":
		keys = []string{"e edit", "+ push", "- remove", "t ttl", "D delete key", "]/[ page", "y copy", "q close"}
	case "set":
		keys = []string{"+ add", "- remove", "t ttl", "D delete key", "]/[ page", "y copy", "q close"}
	case "zset":
		keys = []string{"e score", "+ add", "- remove", "t ttl", "D delete key", "]/[ page", "y copy", "q close"}
	case "stream":
		keys = []string{"+ add entry", "- delete entry", "t ttl", "D delete key", "]/[ page", "y copy", "q close"}
	default:
		keys = []string{"r reload", "D delete key", "q close"}
	}
	return dbFitHints(width, keys...)
}

// databaseRedisStringView shows a string value as text (JSON pretty).
func databaseRedisStringView(p *dbPage, v *dbRedisView) {
	text := ""
	if len(v.raw) > 0 && len(v.raw[0]) > 0 {
		text = v.raw[0][0]
	}
	var pretty bytes.Buffer
	if json.Valid([]byte(text)) && json.Indent(&pretty, []byte(text), "", "  ") == nil {
		text = pretty.String()
	}
	text = dbclient.RedisDisplay(text)
	lines := strings.Split(text, "\n")
	if len(lines) > 2000 {
		lines = append(lines[:2000], "…")
	}
	p.first = len(p.lines) + 1
	p.bounds = []int{0}
	for _, l := range lines {
		p.add(" " + sanitizeOutputLine(l))
	}
	if text == "" {
		n := p.add(" <empty string>")
		p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpSep")
	}
	if v.trunc {
		n := p.add(" " + dbGlyphMore.String() + " the value is longer than 64 KB; only its start is shown (GETRANGE)")
		p.hl(n, 0, len(p.lines[n-1]), "TermocodeHttpHint")
	}
}

// databaseRedisRow is the raw row of the page under the cursor.
func (m *Model) databaseRedisRow(v *dbRedisView, row int) ([]string, bool) {
	s := m.dbConsole()
	idx := s.pageIdx()
	if row < 0 || row >= len(idx) || idx[row] >= len(v.raw) {
		return nil, false
	}
	return v.raw[idx[row]], true
}

// dbRedisEdit is a write waiting for its prompt.
type dbRedisEdit struct {
	conn  string
	db    int
	key   string
	typ   string
	ttl   int64
	what  string // "value" | "add" | "ttl"
	field string // the field / index / member edited, or the first add answer
	tab   *dbTab
}

// dbRedisWrite is a command waiting for its card, or running.
type dbRedisWrite struct {
	conn    string
	db      int
	key     string
	args    []string
	deleted bool // DEL: the key leaves the tree and the page
	reload  *dbRedisEdit
	ns      *dbRedisNSDelete // delete namespace: every key under a prefix
}

// dbRedisNSDelete is a namespace delete: the keys the SCAN found under the
// prefix (shown on the card, deleted in batches).
type dbRedisNSDelete struct {
	path   string // the tree row of the namespace
	prefix string
	keys   []string
	more   bool // the scan stopped at db_redis_max_keys: more keys may remain
}

type dbRedisNSScanMsg struct {
	conn   string
	db     int
	path   string
	prefix string
	keys   []string
	more   bool
	err    error
}

// dbRedisNSSample is how many keys the delete card lists.
const dbRedisNSSample = 8

// databaseRedisNSDelete scans the keys under a namespace (SCAN, never KEYS)
// and then asks, with the count and a sample, before anything is deleted.
func (m *Model) databaseRedisNSDelete(n dbNode, path string) tea.Cmd {
	s := m.databases
	sess, _ := s.sessions[n.conn].(dbRedis)
	if sess == nil {
		return m.databaseConnect(n.conn)
	}
	settings := loadDBConsoleSettings()
	maxKeys, timeout := settings.redisMaxKeys(), settings.timeout()
	conn, db, prefix := n.conn, n.rdb, n.key
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		keys, more, err := sess.ScanKeys(ctx, db, dbclient.RedisGlobEscape(prefix)+"*", maxKeys)
		return dbRedisNSScanMsg{conn: conn, db: db, path: path, prefix: prefix, keys: keys, more: more, err: err}
	}
}

func (m *Model) onDBRedisNSScan(msg dbRedisNSScanMsg) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Could not read the keys", dbErrorText(msg.err))
	}
	if len(msg.keys) == 0 {
		return m.pushToastDetail(toast.Info, "No keys to delete", "Nothing is stored under "+sanitizeDBText(dbclient.RedisDisplay(msg.prefix))+"*.")
	}
	c, ok := m.databases.connection(msg.conn)
	if !ok {
		return nil
	}
	w := &dbRedisWrite{conn: msg.conn, db: msg.db, key: msg.prefix, deleted: true, ns: &dbRedisNSDelete{path: msg.path, prefix: msg.prefix, keys: msg.keys, more: msg.more}}
	m.dbConsole().redisWrite = w
	count := dbRedisCount(int64(len(msg.keys)), msg.more)
	var b strings.Builder
	b.WriteString("Every key that starts with " + dbSafeMultiline(dbclient.RedisDisplay(msg.prefix)) + " (" + count + ").\n\n")
	for i, k := range msg.keys {
		if i == dbRedisNSSample {
			b.WriteString("… and " + strconv.Itoa(len(msg.keys)-dbRedisNSSample) + " more\n")
			break
		}
		b.WriteString(dbSafeMultiline(dbclient.RedisDisplay(k)) + "\n")
	}
	if msg.more {
		b.WriteString("\nThe scan stopped at the key limit: more keys may remain. Run it again for the rest.\n")
	}
	b.WriteString("\nThey are removed with UNLINK in batches of " + strconv.Itoa(dbclient.RedisDeleteBatch) + ". This cannot be undone.")
	if msg.db != redisConnDB(c) {
		b.WriteString("\n\non database " + strconv.Itoa(msg.db))
	}
	name := sanitizeDBText(dbConnName(c))
	title := "Delete " + count + " from " + name + "?"
	switch {
	case c.ReadOnly:
		title = name + " is read-only. Run anyway?"
	case c.Environment == "production":
		title = "Write to production (" + name + ")?"
	}
	m.confirm = confirm.New(title, b.String(), []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "run", Title: "Delete", Style: confirm.StyleDestructive},
	}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBRedisWrite
	return nil
}

type dbRedisWriteMsg struct {
	w     *dbRedisWrite
	reply any
	err   error
}

// databaseRedisPageKey handles the keys of a value page. ok=false: the
// generic result page handles it (Tab, v, y, o, f, q, …).
func (m *Model) databaseRedisPageKey(key string, row, col int) (tea.Cmd, bool) {
	s := m.dbConsole()
	r := s.shown
	v := r.redis
	edit := &dbRedisEdit{conn: r.conn, db: v.db, key: v.key, typ: v.info.Type, ttl: v.info.TTL, tab: s.curTab()}
	switch key {
	case "]":
		if v.next == "" {
			return nil, true
		}
		return m.databaseRedisOpen(r.conn, v.db, v.key, v.next, append(append([]string(nil), v.prev...), v.from), s.curTab()), true
	case "[":
		if len(v.prev) == 0 {
			return nil, true
		}
		return m.databaseRedisOpen(r.conn, v.db, v.key, v.prev[len(v.prev)-1], v.prev[:len(v.prev)-1], s.curTab()), true
	case "r":
		return m.databaseRedisOpen(r.conn, v.db, v.key, v.from, v.prev, s.curTab()), true
	case "x", "X", "K", "Z", "#", "u", "w", "E", "t", "D", "e", "+", "-":
	default:
		return nil, false
	}
	if r.result.Err != nil && key != "D" {
		return nil, true
	}
	switch key {
	case "t":
		return m.databaseRedisTTLPrompt(edit), true
	case "D":
		return m.databaseRedisAsk(&dbRedisWrite{conn: r.conn, db: v.db, key: v.key, args: dbclient.RedisDel(v.key), deleted: true}), true
	case "e", "E":
		return m.databaseRedisEditKey(edit, v, row), true
	case "+":
		return m.databaseRedisAddKey(edit), true
	case "-":
		raw, ok := m.databaseRedisRow(v, row)
		if !ok {
			return m.pushToastDetail(toast.Info, "Put the cursor on a row", "- deletes the row under the cursor."), true
		}
		var args []string
		switch v.info.Type {
		case "hash":
			args = dbclient.RedisHDel(v.key, raw[0])
		case "list":
			args = dbclient.RedisLRem(v.key, raw[1])
		case "set":
			args = dbclient.RedisSRem(v.key, raw[0])
		case "zset":
			args = dbclient.RedisZRem(v.key, raw[1])
		case "stream":
			args = dbclient.RedisXDel(v.key, raw[0])
		default:
			return m.pushToastDetail(toast.Info, "Nothing to delete here", "D deletes the whole key."), true
		}
		return m.databaseRedisAsk(&dbRedisWrite{conn: r.conn, db: v.db, key: v.key, args: args, reload: edit}), true
	}
	return nil, true // x, K, Z, #, u, w: no plan, no transaction on Redis
}

func (m *Model) databaseRedisEditKey(edit *dbRedisEdit, v *dbRedisView, row int) tea.Cmd {
	s := m.dbConsole()
	edit.what = "value"
	switch v.info.Type {
	case "string":
		if v.trunc {
			return m.pushToastDetail(toast.Info, "This value is too long to edit here", "It is longer than 64 KB; use the console (n) to change it.")
		}
		text := ""
		if len(v.raw) > 0 {
			text = v.raw[0][0]
		}
		s.redisEdit = edit
		_ = m.nvim.ExecLuaArgs(`_G._termocode_db_edit(...)`, "Redis Value: "+sanitizeDBText(dbclient.RedisDisplay(v.key)), strings.Split(text, "\n"))
		return tea.Batch(m.fetchStateCmd(), m.pushToastDetail(toast.Info, "Edit the value, then :w", "The card shows the SET command before it runs. q leaves without a change."))
	case "set", "stream":
		return m.pushToastDetail(toast.Info, "A "+v.info.Type+" entry cannot be changed in place", "Add a new one with + and delete the old one with -.")
	}
	raw, ok := m.databaseRedisRow(v, row)
	if !ok {
		return m.pushToastDetail(toast.Info, "Put the cursor on a row", "e edits the row under the cursor.")
	}
	label, value := "", ""
	switch v.info.Type {
	case "hash":
		edit.field, value = raw[0], raw[1]
		label = "New value of " + sanitizeDBText(dbclient.RedisDisplay(raw[0])) + ":"
	case "list":
		edit.field, value = raw[0], raw[1]
		label = "New value of item " + raw[0] + ":"
	case "zset":
		edit.field, value = raw[1], raw[0]
		label = "New score of " + sanitizeDBText(dbclient.RedisDisplay(raw[1])) + ":"
	}
	s.redisEdit = edit
	if strings.ContainsAny(value, "\n\r") || len(value) > 2000 {
		_ = m.nvim.ExecLuaArgs(`_G._termocode_db_edit(...)`, "Redis Value: "+sanitizeDBText(dbclient.RedisDisplay(v.key)), strings.Split(value, "\n"))
		return m.fetchStateCmd()
	}
	cmd := m.openHTTPPrompt(promptKindDBRedisValue, "Edit "+v.info.Type+" value", label, value)
	m.prompt.SetAllowEmpty(true)
	return cmd
}

func (m *Model) databaseRedisAddKey(edit *dbRedisEdit) tea.Cmd {
	s := m.dbConsole()
	edit.what = "add"
	s.redisEdit = edit
	switch edit.typ {
	case "hash":
		return m.openHTTPPrompt(promptKindDBRedisAdd, "Add a field", "New field:", "")
	case "zset":
		return m.openHTTPPrompt(promptKindDBRedisAdd, "Add a member", "New member:", "")
	case "list":
		cmd := m.openHTTPPrompt(promptKindDBRedisAddValue, "Push an item", "Value (RPUSH):", "")
		m.prompt.SetAllowEmpty(true)
		return cmd
	case "set":
		return m.openHTTPPrompt(promptKindDBRedisAddValue, "Add a member", "New member:", "")
	case "stream":
		return m.openHTTPPrompt(promptKindDBRedisAddValue, "Add an entry", "Fields (field value …):", "")
	}
	s.redisEdit = nil
	return m.pushToastDetail(toast.Info, "Nothing to add to a "+edit.typ, "e edits the value.")
}

func (m *Model) databaseRedisTTLPrompt(edit *dbRedisEdit) tea.Cmd {
	edit.what = "ttl"
	m.dbConsole().redisEdit = edit
	initial := ""
	if edit.ttl > 0 {
		initial = strconv.FormatInt((edit.ttl+999)/1000, 10)
	}
	cmd := m.openHTTPPrompt(promptKindDBRedisTTL, "Time to live", "Time to live: seconds or 30m, 2h, 1d (empty: no TTL):", initial)
	m.prompt.SetAllowEmpty(true)
	return cmd
}

// dbRedisParseTTL reads "90", "30m", "2h", "1d" ("": no TTL).
func dbRedisParseTTL(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n < 1 {
			return 0, fmt.Errorf("a TTL is at least 1 second")
		}
		return n, nil
	}
	if d, ok := strings.CutSuffix(s, "d"); ok {
		if n, err := strconv.ParseInt(d, 10, 64); err == nil && n > 0 {
			return n * 86400, nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < time.Second {
		return 0, fmt.Errorf("type seconds, or a duration such as 30m, 2h or 1d")
	}
	return int64(d / time.Second), nil
}

// databaseRedisPrompt handles the answers of the value page prompts.
func (m *Model) databaseRedisPrompt(kind promptKindEnum, value string) (tea.Cmd, bool) {
	switch kind {
	case promptKindDBRedisValue, promptKindDBRedisAdd, promptKindDBRedisAddValue, promptKindDBRedisTTL:
	default:
		return nil, false
	}
	s := m.dbConsole()
	e := s.redisEdit
	if e == nil {
		return nil, true
	}
	w := &dbRedisWrite{conn: e.conn, db: e.db, key: e.key, reload: e}
	switch kind {
	case promptKindDBRedisValue:
		s.redisEdit = nil
		return m.databaseRedisValueWrite(e, value), true
	case promptKindDBRedisAdd:
		if strings.TrimSpace(value) == "" {
			s.redisEdit = nil
			return nil, true
		}
		e.field = value
		if e.typ == "zset" {
			return m.openHTTPPrompt(promptKindDBRedisAddValue, "Add a member", "Score of "+sanitizeDBText(dbclient.RedisDisplay(value))+":", "0"), true
		}
		cmd := m.openHTTPPrompt(promptKindDBRedisAddValue, "Add a field", "Value of "+sanitizeDBText(dbclient.RedisDisplay(value))+":", "")
		m.prompt.SetAllowEmpty(true)
		return cmd, true
	case promptKindDBRedisAddValue:
		s.redisEdit = nil
		switch e.typ {
		case "hash":
			w.args = dbclient.RedisHSet(e.key, e.field, value)
		case "zset":
			if _, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err != nil {
				return m.pushToastDetail(toast.Warn, "A score is a number", "For example 1, 2.5 or -3."), true
			}
			w.args = dbclient.RedisZAdd(e.key, strings.TrimSpace(value), e.field)
		case "list":
			w.args = dbclient.RedisRPush(e.key, value)
		case "set":
			w.args = dbclient.RedisSAdd(e.key, value)
		case "stream":
			fields, err := dbclient.ParseRedisCommand(value)
			if err != nil || len(fields) == 0 || len(fields)%2 != 0 {
				return m.pushToastDetail(toast.Warn, "Give field value pairs", `For example: event login user "Ada L"`), true
			}
			w.args = dbclient.RedisXAdd(e.key, fields)
		}
	case promptKindDBRedisTTL:
		s.redisEdit = nil
		secs, err := dbRedisParseTTL(value)
		if err != nil {
			return m.pushToastDetail(toast.Warn, "That TTL is not valid", err.Error()), true
		}
		w.args = dbclient.RedisSetTTL(e.key, secs)
	}
	if len(w.args) == 0 {
		return nil, true
	}
	return m.databaseRedisAsk(w), true
}

// databaseRedisValueWrite is the command of an edited value.
func (m *Model) databaseRedisValueWrite(e *dbRedisEdit, value string) tea.Cmd {
	w := &dbRedisWrite{conn: e.conn, db: e.db, key: e.key, reload: e}
	switch e.typ {
	case "string":
		w.args = dbclient.RedisSetString(e.key, value, e.ttl > 0)
	case "hash":
		w.args = dbclient.RedisHSet(e.key, e.field, value)
	case "list":
		n, _ := strconv.Atoi(e.field)
		w.args = dbclient.RedisLSet(e.key, n, value)
	case "zset":
		if _, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err != nil {
			return m.pushToastDetail(toast.Warn, "A score is a number", "For example 1, 2.5 or -3.")
		}
		w.args = dbclient.RedisZAdd(e.key, strings.TrimSpace(value), e.field)
	default:
		return nil
	}
	return m.databaseRedisAsk(w)
}

// databaseRedisBufferDone is :w in the value editor of a Redis value.
func (m *Model) databaseRedisBufferDone(text string) tea.Cmd {
	s := m.dbConsole()
	e := s.redisEdit
	s.redisEdit = nil
	if m.nvim != nil {
		_ = m.nvim.ExecLua(`_G._termocode_db_edit_close()`)
	}
	if e == nil {
		return nil
	}
	return m.databaseRedisValueWrite(e, text)
}

// dbRedisCardTitle is the title of a write card: production and
// read-only connections say so (the SQL guard's rules).
func dbRedisCardTitle(c dbclient.Conn, w *dbRedisWrite) string {
	name := sanitizeDBText(dbConnName(c))
	switch {
	case c.ReadOnly:
		return name + " is read-only. Run anyway?"
	case c.Environment == "production":
		return "Write to production (" + name + ")?"
	case w.deleted:
		return "Delete this key from " + name + "?"
	}
	return "Run this command on " + name + "?"
}

// databaseRedisAsk shows the card with the exact command.
func (m *Model) databaseRedisAsk(w *dbRedisWrite) tea.Cmd {
	c, ok := m.databases.connection(w.conn)
	if !ok {
		return nil
	}
	m.dbConsole().redisWrite = w
	body := dbSafeMultiline(dbclient.FormatRedisCommand(w.args))
	if w.db != redisConnDB(c) {
		body += "\n\non database " + strconv.Itoa(w.db)
	}
	style := confirm.StylePrimary
	if w.deleted {
		body += "\n\nThis cannot be undone."
		style = confirm.StyleDestructive
	}
	m.confirm = confirm.New(dbRedisCardTitle(c, w), body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "run", Title: "Run", Style: style},
	}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBRedisWrite
	return nil
}

func redisConnDB(c dbclient.Conn) int {
	if c.URL != "" {
		if p, err := dbclient.ParseURL(c.URL); err == nil {
			return p.DB
		}
	}
	return c.DB
}

func (m *Model) databaseRedisConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindDBRedisWrite {
		return nil, false
	}
	s := m.dbConsole()
	w := s.redisWrite
	s.redisWrite = nil
	if id != "run" || w == nil {
		return nil, true
	}
	sess, _ := m.databases.sessions[w.conn].(dbRedis)
	if sess == nil {
		return m.pushToastDetail(toast.Warn, "Not connected", "Connect to "+sanitizeDBText(w.conn)+" first."), true
	}
	timeout := loadDBConsoleSettings().timeout()
	if w.ns != nil {
		keys := w.ns.keys
		// Each batch is one command; the time limit is for one of them.
		return func() tea.Msg {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			n, err := dbclient.RedisDeleteKeys(ctx, func(ctx context.Context, args []string) (any, error) {
				cctx, ccancel := context.WithTimeout(ctx, timeout)
				defer ccancel()
				return sess.DoDB(cctx, w.db, args)
			}, keys)
			return dbRedisWriteMsg{w: w, reply: n, err: err}
		}, true
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		reply, err := sess.DoDB(ctx, w.db, w.args)
		return dbRedisWriteMsg{w: w, reply: reply, err: err}
	}, true
}

func (m *Model) onDBRedisWrite(msg dbRedisWriteMsg) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	w := msg.w
	if w.ns != nil {
		return m.onDBRedisNSDeleted(msg)
	}
	line := dbclient.FormatRedisCommand(w.args)
	r := &dbRun{conn: w.conn, sql: line, by: "user"}
	r.result = dbclient.RedisReplyResult(msg.reply, 10)
	hist := m.databaseAppendHistory(r)
	if msg.err != nil {
		return tea.Batch(hist, m.pushToastDetail(toast.Errr, "The command failed", dbErrorText(msg.err)))
	}
	done := m.pushToastDetail(toast.Info, "Done", dbOneLine(line))
	if w.deleted {
		s := m.databases
		for p, n := range s.nodes {
			if n.kind == dbNodeRedisKey && n.conn == w.conn && n.rdb == w.db && n.key == w.key {
				delete(s.nodes, p)
				parent := p[:strings.LastIndex(p, "/key/")]
				if pn, ok := s.nodes[parent]; ok {
					kept := pn.children[:0]
					for _, ch := range pn.children {
						if ch != p {
							kept = append(kept, ch)
						}
					}
					pn.children = kept
					s.nodes[parent] = pn
				}
			}
		}
		s.touch()
		cs := m.dbConsole()
		if cs.shown != nil && cs.shown.redis != nil && cs.shown.redis.key == w.key && cs.shown.conn == w.conn {
			cs.shown.result = dbclient.Result{Notices: []string{"Deleted " + dbOneLine(dbclient.RedisDisplay(w.key))}}
			cs.shown.redis.info.Type = "none"
			cs.view = dbViewMessages
			return tea.Batch(hist, done, m.databaseShowPage())
		}
		return tea.Batch(hist, done)
	}
	if e := w.reload; e != nil {
		cs := m.dbConsole()
		if cs.shown != nil && cs.shown.redis != nil && cs.shown.redis.key == w.key && cs.shown.conn == w.conn {
			v := cs.shown.redis
			return tea.Batch(hist, done, m.databaseRedisOpen(w.conn, w.db, w.key, v.from, v.prev, cs.curTab()))
		}
	}
	return tea.Batch(hist, done)
}

// onDBRedisNSDeleted reports a namespace delete and drops the namespace's
// rows from the tree (a partly failed delete says how far it got).
func (m *Model) onDBRedisNSDeleted(msg dbRedisWriteMsg) tea.Cmd {
	w := msg.w
	n, _ := msg.reply.(int64)
	line := "Delete " + dbclient.RedisDisplay(w.ns.prefix) + "* (" + strconv.FormatInt(n, 10) + " keys)"
	r := &dbRun{conn: w.conn, sql: line, by: "user", result: dbclient.Result{Affected: n}}
	hist := m.databaseAppendHistory(r)
	s := m.databases
	if n > 0 && !w.ns.more && msg.err == nil {
		for p := range s.nodes {
			if p == w.ns.path || strings.HasPrefix(p, w.ns.path+"/") {
				delete(s.nodes, p)
			}
		}
		if i := strings.LastIndex(w.ns.path, "/ns/"); i >= 0 {
			if pn, ok := s.nodes[w.ns.path[:i]]; ok {
				kept := pn.children[:0]
				for _, ch := range pn.children {
					if ch != w.ns.path {
						kept = append(kept, ch)
					}
				}
				pn.children = kept
				s.nodes[w.ns.path[:i]] = pn
			}
		}
		s.touch()
	}
	if msg.err != nil {
		return tea.Batch(hist, m.pushToastDetail(toast.Errr, "Deleted "+strconv.FormatInt(n, 10)+" of "+strconv.Itoa(len(w.ns.keys))+" keys", dbErrorText(msg.err)))
	}
	detail := dbOneLine(dbclient.RedisDisplay(w.ns.prefix)) + "*"
	if w.ns.more {
		detail += ": more keys may remain; refresh and run it again."
	}
	return tea.Batch(hist, m.pushToastDetail(toast.Info, "Deleted "+strconv.FormatInt(n, 10)+" keys", detail))
}

// ── the sidebar ─────────────────────────────────────────────────────────

// databaseRedisSidebarKey handles the keys on Redis rows. ok=false: the
// generic handler runs.
func (m *Model) databaseRedisSidebarKey(key string, row dbTreeRow) (tea.Cmd, bool) {
	s := m.databases
	if !s.isRedis(row.conn) {
		return nil, false
	}
	n := s.nodes[row.path]
	switch key {
	case "enter", " ", "right", "l", "e":
		if n.kind == dbNodeRedisKey {
			return m.databaseRedisOpen(n.conn, n.rdb, n.key, "", nil, nil), true
		}
	case "t":
		if n.kind == dbNodeRedisKey {
			return m.databaseRedisTTLPrompt(&dbRedisEdit{conn: n.conn, db: n.rdb, key: n.key, typ: n.rtype, ttl: n.ttl}), true
		}
		return nil, true // no transaction mode on Redis
	case "D":
		if n.kind == dbNodeRedisKey {
			if s.sessions[n.conn] == nil {
				return m.databaseConnect(n.conn), true
			}
			return m.databaseRedisAsk(&dbRedisWrite{conn: n.conn, db: n.rdb, key: n.key, args: dbclient.RedisDel(n.key), deleted: true}), true
		}
		if n.kind == dbNodeRedisNS {
			return m.databaseRedisNSDelete(n, row.path), true
		}
		return nil, true
	case "y":
		if n.kind == dbNodeRedisKey || n.kind == dbNodeRedisNS {
			m.copyToClipboard(n.key)
			return m.pushToastDetail(toast.Info, "Copied key", sanitizeDBText(dbclient.RedisDisplay(n.key))), true
		}
		return nil, true
	case "o", "d", "s", "#", "i":
		return nil, true // table keys
	case "?":
		return m.pushToastDetail(toast.Info, "Redis keys", "j/k move · Enter connect, expand or open a key · n console · on a key: t TTL · D delete · on a namespace: D delete all its keys · y copy key · R refresh · / filter"), true
	}
	return nil, false
}

// dbRedisFooter is the hint row of a Redis tree row.
func (s *dbState) dbRedisFooter(n dbNode) string {
	switch n.kind {
	case dbNodeRedisKey:
		parts := []string{dbRedisTypeName(n.rtype)}
		if ttl := dbRedisTTL(n.ttl); ttl != "" {
			parts = append(parts, "TTL "+ttl)
		}
		return strings.Join(parts, " · ") + " · Enter open · t ttl · D delete"
	case dbNodeRedisNS:
		return n.detail + " · Enter expand · y copy · D delete all"
	case dbNodeRedisMore:
		return "db_redis_max_keys stops the scan"
	case dbNodeDatabase:
		return n.detail + " · Enter keys · n console"
	}
	return ""
}

// ── the console ─────────────────────────────────────────────────────────

// dbRedisConsoleLines are the commands of a .redis console: one per line,
// "#" and "//" lines are comments.
func dbRedisConsoleLine(l string) bool {
	t := strings.TrimSpace(l)
	return t != "" && !strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "//")
}

// databaseRedisPrepare keeps the command at the cursor line (line > 0),
// every command of the selection (line < 0) or of the file (line 0).
func (m *Model) databaseRedisPrepare(r *dbRun) tea.Cmd {
	lines := strings.Split(r.sql, "\n")
	r.statements = nil
	switch {
	case r.line > 0:
		at := r.line - 1
		if at >= len(lines) || !dbRedisConsoleLine(lines[at]) {
			return m.pushToastDetail(toast.Info, "No command on this line", "Put the cursor on a command line (one command per line).")
		}
		r.statements = []string{strings.TrimSpace(lines[at])}
	default:
		first := 0
		for i, l := range lines {
			if dbRedisConsoleLine(l) {
				if len(r.statements) == 0 {
					first = i + 1
				}
				r.statements = append(r.statements, strings.TrimSpace(l))
			}
		}
		if r.line < 0 {
			r.line = -r.line
		} else {
			r.line = first
		}
	}
	if len(r.statements) == 0 {
		return m.pushToastDetail(toast.Info, "No command to run", "")
	}
	r.line = max(r.line, 1)
	r.index = 0
	r.sql = r.statements[0]
	s := m.dbConsole()
	s.batchSeq++
	r.batch = s.batchSeq
	return m.databaseReviewRun(r)
}

// databaseRedisReview is the console guard: a read runs at once, a
// write asks, the never-list asks with a strong card, a refused command
// is never sent.
func (m *Model) databaseRedisReview(r *dbRun, c dbclient.Conn) tea.Cmd {
	args, err := dbclient.ParseRedisCommand(r.sql)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "That command could not be read", err.Error())
	}
	if len(args) == 0 {
		return nil
	}
	kind, why := dbclient.ClassifyRedis(args)
	switch kind {
	case dbclient.RedisRefused:
		return m.pushToastDetail(toast.Warn, "Not sent", why)
	case dbclient.RedisRead:
		return m.databaseStartRun(r)
	}
	s := m.dbConsole()
	s.pending = r
	name := sanitizeDBText(dbConnName(c))
	title := "Run this command on " + name + "?"
	body := dbSafeMultiline(r.sql)
	style := confirm.StyleDestructive
	switch {
	case kind == dbclient.RedisDangerous:
		title = "Dangerous command on " + name
		if c.Environment == "production" {
			title = "Dangerous command on production (" + name + ")"
		}
		body += "\n\n" + why + ". This cannot be undone."
	case c.ReadOnly:
		title = name + " is read-only. Run anyway?"
	case c.Environment == "production":
		title = "Write to production (" + name + ")?"
	default:
		style = confirm.StylePrimary
	}
	m.confirm = confirm.New(title, body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "run", Title: "Run", Style: style},
	}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBRun
	return nil
}

// databaseRedisPaletteKey runs Set TTL / Delete Key on the key of the
// value page, else on the key selected in the tree.
func (m *Model) databaseRedisPaletteKey(id string) tea.Cmd {
	key := "t"
	if id == "redis-delete-key" {
		key = "D"
	}
	if m.databases != nil && m.databases.console != nil {
		if s := m.databases.console; s.shown != nil && s.shown.redis != nil && s.running == nil && lastPathPart(m.editor.Path()) == "SQL Result" {
			cmd, _ := m.databaseRedisPageKey(key, -1, -1)
			return cmd
		}
	}
	m.ensureDatabasesLoaded()
	if row, ok := m.databaseCurrentRow(); ok && row.kind == dbNodeRedisKey {
		cmd, _ := m.databaseRedisSidebarKey(key, row)
		return cmd
	}
	return m.pushToastDetail(toast.Info, "Select a Redis key first", "Open a key, or select one in the Databases view.")
}
