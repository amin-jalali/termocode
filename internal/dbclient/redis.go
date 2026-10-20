package dbclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gomodule/redigo/redis"
)

// Redis (plan F12, DB-10): one redigo connection per session, no pool, no
// keepalive, no background goroutine while idle. Keys are listed with SCAN
// (never KEYS), values are read a page at a time (HSCAN, SSCAN, ZRANGE,
// LRANGE, XRANGE, GETRANGE), never whole. The console sends what the user
// typed after the guard in the app; a read-only session (the AI's) sends
// only allow-listed read commands.

// RedisDialFunc opens the network connection (tests use net.Pipe; DB-11
// passes an SSH tunnel dialer).
type RedisDialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// RedisStringMax is the most bytes of a string value read for a page.
const RedisStringMax = 64 * 1024

// RedisScanCount is the COUNT hint of every SCAN.
const RedisScanCount = 1000

// RedisSession implements Session for Redis and Valkey.
type RedisSession struct {
	mu       sync.Mutex // one command (or pipeline) at a time
	conn     redis.Conn
	cfg      Conn
	password string
	dial     RedisDialFunc
	target   redisTarget
	selected int // the database the connection is on
	readonly bool
	closed   bool

	cmu    sync.Mutex
	cancel context.CancelFunc
}

type redisTarget struct {
	addr, user, host string
	tls              bool
	db               int
}

// redisTargetOf reads the address, user, TLS and database index of c
// (its URL first, then its fields).
func redisTargetOf(c Conn) redisTarget {
	t := redisTarget{host: c.Host, user: c.User, tls: c.TLS, db: c.DB}
	port := c.Port
	if c.URL != "" {
		if u, err := url.Parse(c.URL); err == nil {
			t.host = u.Hostname()
			if p, err := strconv.Atoi(u.Port()); err == nil {
				port = p
			} else {
				port = 0
			}
			if u.User != nil && t.user == "" {
				t.user = u.User.Username()
			}
			if u.Scheme == "rediss" {
				t.tls = true
			}
			if n, err := strconv.Atoi(strings.TrimPrefix(u.Path, "/")); err == nil {
				t.db = n
			}
		}
	}
	if port == 0 {
		port = 6379
	}
	t.addr = net.JoinHostPort(t.host, strconv.Itoa(port))
	return t
}

// NewRedisSession dials c (dial nil: plain TCP without keepalive).
func NewRedisSession(ctx context.Context, c Conn, password string, dial RedisDialFunc) (*RedisSession, error) {
	s := &RedisSession{cfg: c, password: password, dial: dial, target: redisTargetOf(c)}
	if err := s.connect(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *RedisSession) tlsConfig() (*tls.Config, error) {
	cfg := &tls.Config{ServerName: s.target.host, MinVersion: tls.VersionTLS12}
	if ssl := s.cfg.SSL; ssl != nil {
		if ssl.CA != "" {
			pem, err := os.ReadFile(ssl.CA)
			if err != nil {
				return nil, fmt.Errorf("could not read the CA file: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, errors.New("the CA file holds no certificate")
			}
			cfg.RootCAs = pool
		}
		if ssl.Cert != "" || ssl.Key != "" {
			cert, err := tls.LoadX509KeyPair(ssl.Cert, ssl.Key)
			if err != nil {
				return nil, fmt.Errorf("could not read the client certificate: %w", err)
			}
			cfg.Certificates = []tls.Certificate{cert}
		}
	}
	return cfg, nil
}

// connect dials a new connection (the caller holds mu or owns s).
func (s *RedisSession) connect(ctx context.Context) error {
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
	dial := s.dial
	if dial == nil {
		d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: -1}
		dial = d.DialContext
	}
	opts := []redis.DialOption{redis.DialContextFunc(dial), redis.DialDatabase(s.target.db)}
	if s.password != "" {
		opts = append(opts, redis.DialPassword(s.password))
		if s.target.user != "" {
			opts = append(opts, redis.DialUsername(s.target.user))
		}
	}
	if s.target.tls {
		cfg, err := s.tlsConfig()
		if err != nil {
			return err
		}
		opts = append(opts, redis.DialUseTLS(true), redis.DialTLSConfig(cfg))
	}
	conn, err := redis.DialContext(ctx, "tcp", s.target.addr, opts...)
	if err != nil {
		return redisError(err, s.password)
	}
	s.conn = conn
	s.selected = s.target.db
	return nil
}

// redisError keeps the server's reason and never the password.
func redisError(err error, password string) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	if password != "" {
		text = strings.ReplaceAll(text, password, "[redacted]")
	}
	text = Redact(text)
	if strings.Contains(text, "MOVED ") || strings.Contains(text, "CLUSTERDOWN") {
		text = "this is a cluster node; connect to each node (cluster browsing is not supported yet): " + text
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", text, err)
	}
	return errors.New(text)
}

// op runs f with the connection on database db (-1: the session's own),
// reconnecting first when the last command broke the connection.
func (s *RedisSession) op(ctx context.Context, db int, f func(ctx context.Context, c redis.Conn) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("the session is closed")
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cmu.Lock()
	s.cancel = cancel
	s.cmu.Unlock()
	defer func() {
		s.cmu.Lock()
		s.cancel = nil
		s.cmu.Unlock()
		cancel()
	}()
	if s.conn == nil || s.conn.Err() != nil {
		if err := s.connect(ctx); err != nil {
			return err
		}
	}
	if db < 0 {
		db = s.target.db
	}
	if db != s.selected {
		if _, err := redis.DoContext(s.conn, ctx, "SELECT", db); err != nil {
			return redisError(err, s.password)
		}
		s.selected = db
	}
	return f(ctx, s.conn)
}

func (s *RedisSession) do(ctx context.Context, c redis.Conn, args []string) (any, error) {
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	rest := make([]any, len(args)-1)
	for i, a := range args[1:] {
		rest[i] = a
	}
	reply, err := redis.DoContext(c, ctx, args[0], rest...)
	return reply, redisError(err, s.password)
}

func (s *RedisSession) Ping(ctx context.Context) error {
	return s.op(ctx, -1, func(ctx context.Context, c redis.Conn) error {
		_, err := s.do(ctx, c, []string{"PING"})
		return err
	})
}

// Cancel ends the running command (the connection is redialled on the
// next one).
func (s *RedisSession) Cancel() {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *RedisSession) Close() error {
	s.Cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.conn != nil {
		err := s.conn.Close()
		s.conn = nil
		return err
	}
	return nil
}

var errRedisNoTx = errors.New("Redis has no transactions in the console")

func (s *RedisSession) Begin(context.Context) error    { return errRedisNoTx }
func (s *RedisSession) Commit(context.Context) error   { return errRedisNoTx }
func (s *RedisSession) Rollback(context.Context) error { return errRedisNoTx }

// NewRedisReadOnlySession is a session that sends allow-listed reads only;
// it dials on its first command.
func NewRedisReadOnlySession(c Conn, password string, dial RedisDialFunc) *RedisSession {
	return &RedisSession{cfg: c, password: password, dial: dial, target: redisTargetOf(c), readonly: true}
}

// ReadOnlySession is a new connection that sends allow-listed read
// commands only (it dials on its first command).
func (s *RedisSession) ReadOnlySession() (Session, error) {
	return &RedisSession{cfg: s.cfg, password: s.password, dial: s.dial, target: s.target, readonly: true}, nil
}

// ReadOnly reports whether the session sends read commands only.
func (s *RedisSession) ReadOnly() bool { return s.readonly }

// DB is the database index of the connection.
func (s *RedisSession) DB() int { return s.target.db }

// Introspect lists the databases (db0, db3, …) for callers that only
// know Session.
func (s *RedisSession) Introspect(ctx context.Context, _ IntrospectionRequest) (IntrospectionResult, error) {
	dbs, err := s.Keyspace(ctx)
	var out IntrospectionResult
	for _, d := range dbs {
		out.Databases = append(out.Databases, "db"+strconv.Itoa(d.Index))
	}
	return out, err
}

// RedisDB is one database of the keyspace.
type RedisDB struct {
	Index int
	Keys  int64
}

// Keyspace is INFO keyspace: the databases that hold keys, plus the
// connection's own database (listed even when empty).
func (s *RedisSession) Keyspace(ctx context.Context) ([]RedisDB, error) {
	var out []RedisDB
	err := s.op(ctx, -1, func(ctx context.Context, c redis.Conn) error {
		reply, err := redis.String(s.do(ctx, c, []string{"INFO", "keyspace"}))
		if err != nil {
			return err
		}
		seen := map[int]bool{}
		for _, line := range strings.Split(reply, "\n") {
			name, rest, ok := strings.Cut(strings.TrimSpace(line), ":")
			if !ok || !strings.HasPrefix(name, "db") {
				continue
			}
			n, err := strconv.Atoi(name[2:])
			if err != nil {
				continue
			}
			d := RedisDB{Index: n}
			for _, kv := range strings.Split(rest, ",") {
				if v, ok := strings.CutPrefix(kv, "keys="); ok {
					d.Keys, _ = strconv.ParseInt(v, 10, 64)
				}
			}
			seen[n] = true
			out = append(out, d)
		}
		if !seen[s.target.db] {
			out = append(out, RedisDB{Index: s.target.db})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
		return nil
	})
	return out, err
}

// RedisGlobEscape quotes the glob characters of a key prefix for MATCH.
func RedisGlobEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '*', '?', '[', ']', '\\', '^':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ScanKeys runs SCAN … MATCH pattern COUNT 1000 on db until the cursor is
// back at 0 or max keys are found (more: the cap stopped it).
func (s *RedisSession) ScanKeys(ctx context.Context, db int, pattern string, max int) (keys []string, more bool, err error) {
	if max <= 0 {
		max = 10000
	}
	err = s.op(ctx, db, func(ctx context.Context, c redis.Conn) error {
		cursor := "0"
		seen := map[string]bool{}
		for {
			reply, err := redis.Values(s.do(ctx, c, []string{"SCAN", cursor, "MATCH", pattern, "COUNT", strconv.Itoa(RedisScanCount)}))
			if err != nil {
				return err
			}
			if len(reply) != 2 {
				return errors.New("unexpected SCAN reply")
			}
			cursor, _ = redis.String(reply[0], nil)
			batch, _ := redis.Strings(reply[1], nil)
			for _, k := range batch {
				if seen[k] { // SCAN may return a key twice
					continue
				}
				if len(keys) == max {
					more = true
					return nil
				}
				seen[k] = true
				keys = append(keys, k)
			}
			if cursor == "0" {
				return nil
			}
			if len(keys) == max {
				more = true
				return nil
			}
		}
	})
	sort.Strings(keys)
	return keys, more, err
}

// RedisKeyMeta is the type and TTL of a key in a list.
type RedisKeyMeta struct {
	Type string // "none": the key is gone
	TTL  int64  // milliseconds; -1 no TTL
}

// Types pipelines TYPE and PTTL for keys, 500 at a time.
func (s *RedisSession) Types(ctx context.Context, db int, keys []string) ([]RedisKeyMeta, error) {
	out := make([]RedisKeyMeta, 0, len(keys))
	err := s.op(ctx, db, func(ctx context.Context, c redis.Conn) error {
		for start := 0; start < len(keys); start += 500 {
			end := min(start+500, len(keys))
			for _, k := range keys[start:end] {
				if err := c.Send("TYPE", k); err != nil {
					return redisError(err, s.password)
				}
				if err := c.Send("PTTL", k); err != nil {
					return redisError(err, s.password)
				}
			}
			replies, err := redis.Values(redis.DoContext(c, ctx, ""))
			if err != nil {
				return redisError(err, s.password)
			}
			for i := 0; i+1 < len(replies); i += 2 {
				t, _ := redis.String(replies[i], nil)
				ttl, _ := redis.Int64(replies[i+1], nil)
				out = append(out, RedisKeyMeta{Type: t, TTL: ttl})
			}
		}
		return nil
	})
	return out, err
}

// RedisKeyInfo is what the value page header shows.
type RedisKeyInfo struct {
	Type  string
	TTL   int64 // milliseconds; -1 no TTL, -2 the key is gone
	Len   int64 // fields, items, members, entries or bytes
	Bytes int64 // MEMORY USAGE (-1: unknown)
}

var redisLenCommand = map[string]string{"string": "STRLEN", "hash": "HLEN", "list": "LLEN", "set": "SCARD", "zset": "ZCARD", "stream": "XLEN"}

// KeyInfo reads the type, TTL, length and memory of key in one pipeline.
func (s *RedisSession) KeyInfo(ctx context.Context, db int, key string) (RedisKeyInfo, error) {
	info := RedisKeyInfo{Bytes: -1, TTL: -2}
	err := s.op(ctx, db, func(ctx context.Context, c redis.Conn) error {
		typ, err := redis.String(s.do(ctx, c, []string{"TYPE", key}))
		if err != nil {
			return err
		}
		info.Type = typ
		if typ == "none" {
			return nil
		}
		_ = c.Send("PTTL", key)
		if cmd := redisLenCommand[typ]; cmd != "" {
			_ = c.Send(cmd, key)
		}
		_ = c.Send("MEMORY", "USAGE", key)
		replies, err := redis.Values(redis.DoContext(c, ctx, ""))
		if err != nil {
			return redisError(err, s.password)
		}
		at := 0
		next := func() any {
			if at < len(replies) {
				at++
				return replies[at-1]
			}
			return nil
		}
		info.TTL, _ = redis.Int64(next(), nil)
		if redisLenCommand[typ] != "" {
			info.Len, _ = redis.Int64(next(), nil)
		}
		if n, err := redis.Int64(next(), nil); err == nil {
			info.Bytes = n // an ACL may refuse MEMORY: the size stays unknown
		}
		return nil
	})
	return info, err
}

// RedisValuePage is one page of a value: raw (unescaped) cells.
type RedisValuePage struct {
	Columns   []string
	Rows      [][]string
	Next      string // where the next page starts ("": the last page)
	Truncated bool   // a string longer than RedisStringMax
}

// ValuePage reads count items of key from from ("": the start).
func (s *RedisSession) ValuePage(ctx context.Context, db int, key, typ, from string, count int) (RedisValuePage, error) {
	if count <= 0 {
		count = 100
	}
	var p RedisValuePage
	err := s.op(ctx, db, func(ctx context.Context, c redis.Conn) error {
		scan := func(cmd string, pairs bool) error {
			cursor := from
			if cursor == "" {
				cursor = "0"
			}
			reply, err := redis.Values(s.do(ctx, c, []string{cmd, key, cursor, "COUNT", strconv.Itoa(count)}))
			if err != nil {
				return err
			}
			if len(reply) != 2 {
				return errors.New("unexpected " + cmd + " reply")
			}
			next, _ := redis.String(reply[0], nil)
			items, _ := redis.Strings(reply[1], nil)
			if pairs {
				for i := 0; i+1 < len(items); i += 2 {
					p.Rows = append(p.Rows, []string{items[i], items[i+1]})
				}
			} else {
				for _, it := range items {
					p.Rows = append(p.Rows, []string{it})
				}
			}
			if next != "0" {
				p.Next = next
			}
			return nil
		}
		index := func() int {
			n, _ := strconv.Atoi(from)
			return max(n, 0)
		}
		switch typ {
		case "string":
			p.Columns = []string{"value"}
			n, err := redis.Int64(s.do(ctx, c, []string{"STRLEN", key}))
			if err != nil {
				return err
			}
			v, err := redis.String(s.do(ctx, c, []string{"GETRANGE", key, "0", strconv.Itoa(RedisStringMax - 1)}))
			if err != nil {
				return err
			}
			p.Rows = [][]string{{v}}
			p.Truncated = n > RedisStringMax
		case "hash":
			p.Columns = []string{"field", "value"}
			return scan("HSCAN", true)
		case "set":
			p.Columns = []string{"member"}
			return scan("SSCAN", false)
		case "list":
			p.Columns = []string{"index", "value"}
			start := index()
			items, err := redis.Strings(s.do(ctx, c, []string{"LRANGE", key, strconv.Itoa(start), strconv.Itoa(start + count - 1)}))
			if err != nil {
				return err
			}
			for i, it := range items {
				p.Rows = append(p.Rows, []string{strconv.Itoa(start + i), it})
			}
			if len(items) == count {
				p.Next = strconv.Itoa(start + count)
			}
		case "zset":
			p.Columns = []string{"score", "member"}
			start := index()
			items, err := redis.Strings(s.do(ctx, c, []string{"ZRANGE", key, strconv.Itoa(start), strconv.Itoa(start + count - 1), "WITHSCORES"}))
			if err != nil {
				return err
			}
			for i := 0; i+1 < len(items); i += 2 {
				p.Rows = append(p.Rows, []string{items[i+1], items[i]})
			}
			if len(items)/2 == count {
				p.Next = strconv.Itoa(start + count)
			}
		case "stream":
			p.Columns = []string{"id", "fields"}
			start := from
			if start == "" {
				start = "-"
			}
			entries, err := redis.Values(s.do(ctx, c, []string{"XRANGE", key, start, "+", "COUNT", strconv.Itoa(count)}))
			if err != nil {
				return err
			}
			last := ""
			for _, e := range entries {
				parts, _ := redis.Values(e, nil)
				if len(parts) != 2 {
					continue
				}
				id, _ := redis.String(parts[0], nil)
				fields, _ := redis.Strings(parts[1], nil)
				var kv []string
				for i := 0; i+1 < len(fields); i += 2 {
					kv = append(kv, fields[i]+"="+fields[i+1])
				}
				p.Rows = append(p.Rows, []string{id, strings.Join(kv, ", ")})
				last = id
			}
			if len(entries) == count && last != "" {
				p.Next = redisNextStreamID(last)
			}
		default:
			return fmt.Errorf("the value of a %s key cannot be shown", typ)
		}
		return nil
	})
	return p, err
}

// redisNextStreamID is the smallest id after id (XRANGE start is
// inclusive; "(" needs Redis 6.2).
func redisNextStreamID(id string) string {
	ms, seq, ok := strings.Cut(id, "-")
	if !ok {
		return id
	}
	n, err := strconv.ParseUint(seq, 10, 64)
	if err != nil {
		return id
	}
	return ms + "-" + strconv.FormatUint(n+1, 10)
}

// Do sends one command (args as typed) on the session's database and
// returns the raw reply. A read-only session refuses anything but an
// allow-listed read before sending; the console's refused commands are
// never sent.
func (s *RedisSession) Do(ctx context.Context, args []string) (any, error) {
	return s.DoDB(ctx, -1, args)
}

// DoDB is Do on database db (-1: the session's own).
func (s *RedisSession) DoDB(ctx context.Context, db int, args []string) (any, error) {
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	if kind, why := ClassifyRedis(args); kind == RedisRefused {
		return nil, errors.New(why)
	}
	if s.readonly && !RedisAIRead(args) {
		return nil, fmt.Errorf("read-only session: %s is not an allowed read command", strings.ToUpper(args[0]))
	}
	var reply any
	err := s.op(ctx, db, func(ctx context.Context, c redis.Conn) error {
		var err error
		reply, err = s.do(ctx, c, args)
		return err
	})
	return reply, err
}

// Run parses line (one command) and returns its reply as a grid. KEYS is
// never sent: it runs as SCAN up to opts.MaxRows keys, with a note.
func (s *RedisSession) Run(ctx context.Context, line string, opts RunOptions) Result {
	start := time.Now()
	args, err := ParseRedisCommand(line)
	if err == nil && len(args) == 0 {
		err = errors.New("no command")
	}
	if err != nil {
		return Result{Err: err, Elapsed: time.Since(start)}
	}
	if strings.EqualFold(args[0], "KEYS") {
		if len(args) != 2 {
			return Result{Err: errors.New("KEYS takes one pattern"), Elapsed: time.Since(start)}
		}
		limit := opts.MaxRows
		if limit <= 0 {
			limit = 10000
		}
		keys, more, err := s.ScanKeys(ctx, -1, args[1], limit)
		res := Result{Columns: []Column{{Name: "#"}, {Name: "key"}}, Truncated: more, Err: err}
		for i, k := range keys {
			res.Rows = append(res.Rows, []Cell{{Text: strconv.Itoa(i + 1), Kind: "number"}, {Text: RedisDisplay(k)}})
		}
		res.Notices = []string{fmt.Sprintf("KEYS is never sent (it blocks the server); SCAN MATCH %s found %d keys", RedisDisplay(args[1]), len(keys))}
		res.Elapsed = time.Since(start)
		return res
	}
	reply, err := s.Do(ctx, args)
	res := RedisReplyResult(reply, opts.MaxRows)
	res.Err = err
	if err != nil {
		res.Columns, res.Rows = nil, nil
	}
	res.Elapsed = time.Since(start)
	return res
}

// RedisReplyResult turns a reply into a grid: a scalar is one "reply"
// cell, an array is "#" | "value" rows (nested arrays numbered 1.2.3).
func RedisReplyResult(reply any, maxRows int) Result {
	if maxRows <= 0 {
		maxRows = 10000
	}
	switch reply.(type) {
	case []any:
	default:
		return Result{Columns: []Column{{Name: "reply"}}, Rows: [][]Cell{{redisCell(reply)}}}
	}
	res := Result{Columns: []Column{{Name: "#"}, {Name: "value"}}}
	var walk func(prefix string, v []any)
	walk = func(prefix string, v []any) {
		if len(v) == 0 && prefix != "" {
			res.Rows = append(res.Rows, []Cell{{Text: prefix}, {Text: "(empty array)", Kind: "nil"}})
		}
		for i, e := range v {
			if len(res.Rows) >= maxRows {
				res.Truncated = true
				return
			}
			idx := strconv.Itoa(i + 1)
			if prefix != "" {
				idx = prefix + "." + idx
			}
			if sub, ok := e.([]any); ok {
				walk(idx, sub)
				continue
			}
			res.Rows = append(res.Rows, []Cell{{Text: idx, Kind: "number"}, redisCell(e)})
		}
	}
	walk("", reply.([]any))
	if len(res.Rows) == 0 {
		res.Notices = []string{"(empty array)"}
	}
	return res
}

func redisCell(v any) Cell {
	switch x := v.(type) {
	case nil:
		return Cell{Null: true}
	case int64:
		return Cell{Text: strconv.FormatInt(x, 10), Kind: "number"}
	case []byte:
		return Cell{Text: RedisDisplay(string(x))}
	case string:
		return Cell{Text: RedisDisplay(x)}
	case redis.Error:
		return Cell{Text: "(error) " + RedisDisplay(string(x))}
	default:
		return Cell{Text: RedisDisplay(fmt.Sprint(x))}
	}
}

// RedisDisplay makes a value safe to show: bytes that are not valid UTF-8
// and control characters (terminal escapes) become \xHH, bidi controls
// \u{…}; tab and newline stay (the grid folds them, the value view keeps
// them).
func RedisDisplay(s string) string {
	clean := true
	for _, r := range s {
		if r == utf8.RuneError || (unicode.IsControl(r) && r != '\n' && r != '\t') || redisBidi(r) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x80 && unicode.IsControl(r):
			fmt.Fprintf(&b, `\x%02x`, r)
		case unicode.IsControl(r) || redisBidi(r):
			fmt.Fprintf(&b, `\u{%x}`, r)
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

func redisBidi(r rune) bool {
	return (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0x200e || r == 0x200f || r == 0x061c
}

// ParseRedisCommand splits a console line like redis-cli: words split by
// spaces; "double quotes" take \n \r \t \b \a \\ \" \xHH; 'single quotes'
// take \' only; a closing quote must end the word.
func ParseRedisCommand(line string) ([]string, error) {
	var out []string
	i := 0
	for {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t' || line[i] == '\r' || line[i] == '\n') {
			i++
		}
		if i >= len(line) {
			return out, nil
		}
		var b strings.Builder
		switch line[i] {
		case '"':
			i++
			closed := false
			for i < len(line) && !closed {
				ch := line[i]
				switch {
				case ch == '\\' && i+1 < len(line):
					i++
					switch line[i] {
					case 'n':
						b.WriteByte('\n')
					case 'r':
						b.WriteByte('\r')
					case 't':
						b.WriteByte('\t')
					case 'b':
						b.WriteByte('\b')
					case 'a':
						b.WriteByte('\a')
					case 'x':
						if i+2 < len(line) {
							if v, err := strconv.ParseUint(line[i+1:i+3], 16, 8); err == nil {
								b.WriteByte(byte(v))
								i += 2
								break
							}
						}
						b.WriteByte('x')
					default:
						b.WriteByte(line[i])
					}
					i++
				case ch == '"':
					closed = true
					i++
				default:
					b.WriteByte(ch)
					i++
				}
			}
			if !closed {
				return nil, errors.New("unbalanced quotes")
			}
		case '\'':
			i++
			closed := false
			for i < len(line) && !closed {
				ch := line[i]
				switch {
				case ch == '\\' && i+1 < len(line) && line[i+1] == '\'':
					b.WriteByte('\'')
					i += 2
				case ch == '\'':
					closed = true
					i++
				default:
					b.WriteByte(ch)
					i++
				}
			}
			if !closed {
				return nil, errors.New("unbalanced quotes")
			}
		default:
			for i < len(line) && line[i] != ' ' && line[i] != '\t' && line[i] != '\r' && line[i] != '\n' {
				b.WriteByte(line[i])
				i++
			}
			out = append(out, b.String())
			continue
		}
		if i < len(line) && line[i] != ' ' && line[i] != '\t' && line[i] != '\r' && line[i] != '\n' {
			return nil, errors.New("closing quote must be followed by a space")
		}
		out = append(out, b.String())
	}
}

// FormatRedisCommand is args as one console line that ParseRedisCommand
// reads back exactly: plain words as they are, others double-quoted with
// escapes (no control character reaches the screen).
func FormatRedisCommand(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = redisQuote(a)
	}
	return strings.Join(parts, " ")
}

func redisQuote(a string) string {
	plain := a != ""
	for _, r := range a {
		if r <= ' ' || r == '"' || r == '\'' || r == '\\' || r == 0x7f || r == utf8.RuneError || unicode.IsControl(r) || redisBidi(r) {
			plain = false
			break
		}
	}
	if plain {
		return a
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(a); {
		r, size := utf8.DecodeRuneInString(a[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			fmt.Fprintf(&b, `\x%02x`, a[i])
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x80 && (unicode.IsControl(r) || r == 0x7f):
			fmt.Fprintf(&b, `\x%02x`, r)
		case unicode.IsControl(r) || redisBidi(r):
			for _, c := range []byte(string(r)) {
				fmt.Fprintf(&b, `\x%02x`, c)
			}
		default:
			b.WriteString(a[i : i+size])
		}
		i += size
	}
	b.WriteByte('"')
	return b.String()
}

// Write builders: the exact commands the value page sends (and shows).
func RedisSetString(key, value string, keepTTL bool) []string {
	if keepTTL {
		return []string{"SET", key, value, "KEEPTTL"}
	}
	return []string{"SET", key, value}
}
func RedisHSet(key, field, value string) []string { return []string{"HSET", key, field, value} }
func RedisHDel(key, field string) []string        { return []string{"HDEL", key, field} }
func RedisLSet(key string, index int, value string) []string {
	return []string{"LSET", key, strconv.Itoa(index), value}
}
func RedisRPush(key, value string) []string { return []string{"RPUSH", key, value} }
func RedisLRem(key, value string) []string  { return []string{"LREM", key, "1", value} }
func RedisSAdd(key, member string) []string { return []string{"SADD", key, member} }
func RedisSRem(key, member string) []string { return []string{"SREM", key, member} }
func RedisZAdd(key, score, member string) []string {
	return []string{"ZADD", key, score, member}
}
func RedisZRem(key, member string) []string { return []string{"ZREM", key, member} }
func RedisXAdd(key string, fields []string) []string {
	return append([]string{"XADD", key, "*"}, fields...)
}
func RedisXDel(key, id string) []string { return []string{"XDEL", key, id} }
func RedisDel(key string) []string      { return []string{"DEL", key} }

// RedisSetTTL is EXPIRE key seconds, or PERSIST when seconds is 0.
func RedisSetTTL(key string, seconds int64) []string {
	if seconds <= 0 {
		return []string{"PERSIST", key}
	}
	return []string{"EXPIRE", key, strconv.FormatInt(seconds, 10)}
}

// ── allow list and never list ───────────────────────────────────────────

// RedisKind is how the console and the AI treat a command.
type RedisKind int

const (
	RedisRead      RedisKind = iota // runs at once
	RedisWrite                      // asks with the command shown
	RedisDangerous                  // the never-list: a strong card for the user, refused for the AI
	RedisRefused                    // never sent from the console (streams, session state)
)

func (k RedisKind) String() string {
	return [...]string{"read", "write", "dangerous", "refused"}[k]
}

// redisReads are read commands (no side effect, bounded reply).
var redisReads = map[string]bool{
	"GET": true, "MGET": true, "STRLEN": true, "GETRANGE": true, "SUBSTR": true, "EXISTS": true, "TYPE": true, "TTL": true, "PTTL": true,
	"EXPIRETIME": true, "PEXPIRETIME": true, "HGET": true, "HMGET": true, "HGETALL": true, "HKEYS": true, "HVALS": true, "HLEN": true,
	"HEXISTS": true, "HSTRLEN": true, "HSCAN": true, "HRANDFIELD": true, "LRANGE": true, "LLEN": true, "LINDEX": true, "LPOS": true,
	"SMEMBERS": true, "SISMEMBER": true, "SMISMEMBER": true, "SCARD": true, "SSCAN": true, "SRANDMEMBER": true, "SINTER": true,
	"SUNION": true, "SDIFF": true, "SINTERCARD": true, "ZRANGE": true, "ZRANGEBYSCORE": true, "ZREVRANGE": true,
	"ZREVRANGEBYSCORE": true, "ZRANGEBYLEX": true, "ZREVRANGEBYLEX": true, "ZSCORE": true, "ZMSCORE": true, "ZRANK": true,
	"ZREVRANK": true, "ZCARD": true, "ZCOUNT": true, "ZLEXCOUNT": true, "ZSCAN": true, "ZRANDMEMBER": true, "ZINTER": true,
	"ZUNION": true, "ZDIFF": true, "ZINTERCARD": true, "XRANGE": true, "XREVRANGE": true, "XLEN": true, "XPENDING": true,
	"SCAN": true, "DBSIZE": true, "INFO": true, "PING": true, "ECHO": true, "TIME": true, "RANDOMKEY": true, "BITCOUNT": true,
	"BITPOS": true, "GETBIT": true, "BITFIELD_RO": true, "PFCOUNT": true, "GEOPOS": true, "GEODIST": true, "GEOHASH": true,
	"GEOSEARCH": true, "GEORADIUS_RO": true, "GEORADIUSBYMEMBER_RO": true, "LASTSAVE": true, "ROLE": true, "LCS": true,
	"SORT_RO": true, "DUMP": true, "LOLWUT": true, "KEYS": true, "JSON.GET": true, "JSON.TYPE": true,
	"JSON.MGET": true, "JSON.STRLEN": true, "JSON.ARRLEN": true, "JSON.OBJKEYS": true, "JSON.OBJLEN": true,
}

// redisReadSub are read subcommands of container commands.
var redisReadSub = map[string]map[string]bool{
	"OBJECT":   {"ENCODING": true, "FREQ": true, "IDLETIME": true, "REFCOUNT": true, "HELP": true},
	"MEMORY":   {"USAGE": true, "STATS": true, "DOCTOR": true, "MALLOC-STATS": true, "HELP": true},
	"CLIENT":   {"LIST": true, "INFO": true, "GETNAME": true, "ID": true, "HELP": true},
	"COMMAND":  {"COUNT": true, "INFO": true, "DOCS": true, "LIST": true, "GETKEYS": true, "HELP": true},
	"SLOWLOG":  {"GET": true, "LEN": true, "HELP": true},
	"LATENCY":  {"LATEST": true, "HISTORY": true, "DOCTOR": true, "GRAPH": true, "HELP": true},
	"XINFO":    {"STREAM": true, "GROUPS": true, "CONSUMERS": true, "HELP": true},
	"CLUSTER":  {"INFO": true, "NODES": true, "SLOTS": true, "SHARDS": true, "MYID": true, "KEYSLOT": true, "COUNTKEYSINSLOT": true},
	"PUBSUB":   {"CHANNELS": true, "NUMSUB": true, "NUMPAT": true, "SHARDCHANNELS": true, "SHARDNUMSUB": true},
	"FUNCTION": {"LIST": true, "STATS": true},
	"SCRIPT":   {"EXISTS": true},
	"CONFIG":   {"GET": true},
	"ACL":      {"WHOAMI": true, "CAT": true},
}

// redisAIHidden are reads the AI never runs: they can show secrets
// (CONFIG GET requirepass, the user list) or the server's clients.
var redisAIHidden = map[string]bool{"CONFIG": true, "ACL": true, "CLIENT": true, "DUMP": true, "LOLWUT": true}

// redisNever is the never-list: commands that wipe data, stop or rewire
// the server, load code or change users.
var redisNever = map[string]bool{
	"FLUSHALL": true, "FLUSHDB": true, "SHUTDOWN": true, "DEBUG": true, "MODULE": true, "EVAL": true, "EVALSHA": true,
	"EVAL_RO": true, "EVALSHA_RO": true, "FCALL": true, "FCALL_RO": true, "MIGRATE": true, "REPLICAOF": true, "SLAVEOF": true,
	"FAILOVER": true, "SWAPDB": true, "SAVE": true, "BGSAVE": true, "BGREWRITEAOF": true, "RESTORE": true, "RESTORE-ASKING": true,
}

// redisNeverSub are never-list subcommands.
var redisNeverSub = map[string]map[string]bool{
	"CONFIG":   {"SET": true, "REWRITE": true, "RESETSTAT": true},
	"SCRIPT":   {"FLUSH": true, "LOAD": true, "KILL": true},
	"FUNCTION": {"LOAD": true, "DELETE": true, "FLUSH": true, "RESTORE": true, "KILL": true},
	"ACL":      {"SETUSER": true, "DELUSER": true, "LOAD": true, "SAVE": true, "LOG": true, "GENPASS": true, "DRYRUN": true},
	"CLIENT":   {"KILL": true, "PAUSE": true, "UNPAUSE": true, "NO-EVICT": true, "NO-TOUCH": true},
	"CLUSTER":  {"RESET": true, "FAILOVER": true, "FORGET": true, "MEET": true, "ADDSLOTS": true, "ADDSLOTSRANGE": true, "DELSLOTS": true, "DELSLOTSRANGE": true, "SETSLOT": true, "FLUSHSLOTS": true, "REPLICATE": true, "SAVECONFIG": true, "BUMPEPOCH": true, "SET-CONFIG-EPOCH": true},
	"MEMORY":   {"PURGE": true},
}

// redisRefused are never sent from the console: they stream forever,
// change the session's protocol or state, or replicate.
var redisRefused = map[string]string{
	"MONITOR":    "MONITOR streams every command forever; it is not sent from the console",
	"SUBSCRIBE":  "SUBSCRIBE keeps the connection busy; it is not sent from the console",
	"PSUBSCRIBE": "PSUBSCRIBE keeps the connection busy; it is not sent from the console",
	"SSUBSCRIBE": "SSUBSCRIBE keeps the connection busy; it is not sent from the console",
	"SYNC":       "SYNC is for replicas; it is not sent from the console",
	"PSYNC":      "PSYNC is for replicas; it is not sent from the console",
	"HELLO":      "HELLO changes the protocol of the session; it is not sent from the console",
	"SELECT":     "SELECT is not sent: the console runs on the connection's database (set \"db\" or open the database in the tree)",
	"MULTI":      "MULTI / EXEC are not sent: each console line runs on its own",
	"EXEC":       "MULTI / EXEC are not sent: each console line runs on its own",
	"DISCARD":    "MULTI / EXEC are not sent: each console line runs on its own",
	"WATCH":      "WATCH is not sent: each console line runs on its own",
	"UNWATCH":    "WATCH is not sent: each console line runs on its own",
	"QUIT":       "QUIT is not sent: Disconnect closes the session",
	"RESET":      "RESET is not sent: Disconnect closes the session",
	"REPLCONF":   "REPLCONF is for replicas; it is not sent from the console",
}

// ClassifyRedis sorts a command (args as typed) into read, write,
// dangerous (the never-list) or refused, with the reason for the last two.
func ClassifyRedis(args []string) (RedisKind, string) {
	if len(args) == 0 {
		return RedisRefused, "no command"
	}
	cmd := strings.ToUpper(args[0])
	sub := ""
	if len(args) > 1 {
		sub = strings.ToUpper(args[1])
	}
	if why, ok := redisRefused[cmd]; ok {
		return RedisRefused, why
	}
	if cmd == "CLIENT" && sub == "REPLY" {
		return RedisRefused, "CLIENT REPLY would hide the replies; it is not sent from the console"
	}
	if redisNever[cmd] || redisNeverSub[cmd][sub] {
		name := cmd
		if redisNeverSub[cmd][sub] {
			name += " " + sub
		}
		return RedisDangerous, name + " can wipe data, stop the server or run code"
	}
	if cmd == "ACL" && !redisReadSub["ACL"][sub] {
		return RedisDangerous, "ACL " + sub + " changes users"
	}
	if subs, ok := redisReadSub[cmd]; ok {
		if subs[sub] {
			return RedisRead, ""
		}
		return RedisWrite, ""
	}
	if cmd == "XREAD" {
		for _, a := range args[1:] {
			if strings.EqualFold(a, "BLOCK") {
				return RedisWrite, "" // blocks the connection
			}
		}
		return RedisRead, ""
	}
	if redisReads[cmd] {
		return RedisRead, ""
	}
	return RedisWrite, ""
}

// RedisAIRead reports whether the AI (and any read-only session) may send
// args without asking: an allow-listed read that cannot show a secret.
func RedisAIRead(args []string) bool {
	kind, _ := ClassifyRedis(args)
	return kind == RedisRead && !redisAIHidden[strings.ToUpper(args[0])]
}
