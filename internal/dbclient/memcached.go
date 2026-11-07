package dbclient

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MemcachedSession speaks the memcached text protocol over TCP (DBX-69),
// port 11211, with Go's net package only. The console takes:
//
//	get KEY [KEY…]     stats [items|slabs]
//	set KEY VALUE [TTL]  delete KEY   touch KEY TTL   keys [PATTERN]
//
// "keys" and the tree list keys with "lru_crawler metadump all" (memcached
// 1.5.19+), never by guessing. flush_all is refused: there is no undo.
// The read-only twin refuses set, delete and touch.
type MemcachedSession struct {
	addr     string
	host     string
	tlsOn    bool
	dial     DialFunc
	readOnly bool

	mu     sync.Mutex // one command at a time
	conn   net.Conn
	rd     *bufio.Reader
	closed bool

	cmu    sync.Mutex // guards active (Cancel must not wait for mu)
	active net.Conn
}

func init() {
	registerHTTPEngine(HTTPEngine{
		ID: "memcached", Title: "Memcached", Port: 11211,
		Hint:      "the text protocol, default port 11211",
		Schemes:   []string{"memcached", "memcache"},
		LineBased: true,
		Classify:  ClassifyMemcached,
		Open: func(c Conn, _ string) (Session, error) {
			return NewMemcachedSession(c)
		},
	})
}

// NewMemcachedSession makes a session of c; it does not connect.
func NewMemcachedSession(c Conn) (*MemcachedSession, error) {
	host, port := c.Host, c.Port
	if u, err := url.Parse(c.URL); err == nil && u.Host != "" {
		host = u.Hostname()
		port, _ = strconv.Atoi(u.Port())
	}
	if host == "" {
		return nil, errors.New("memcached: no server address")
	}
	if port == 0 {
		port = 11211
	}
	return &MemcachedSession{addr: net.JoinHostPort(host, strconv.Itoa(port)), host: host, tlsOn: c.TLS, dial: c.Dial}, nil
}

// ClassifyMemcached judges one console line.
func ClassifyMemcached(line string) Kind {
	f := strings.Fields(stripLineComments(line))
	if len(f) == 0 {
		return KindUnknown
	}
	switch strings.ToLower(f[0]) {
	case "get", "gets", "stats", "keys", "version":
		return KindRead
	case "set", "add", "replace", "append", "prepend", "delete", "touch", "incr", "decr":
		return KindWrite
	}
	return KindUnknown
}

func (s *MemcachedSession) connect(ctx context.Context) error {
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
	dial := s.dial
	if dial == nil {
		d := &net.Dialer{Timeout: 10 * time.Second}
		dial = d.DialContext
	}
	c, err := dial(ctx, "tcp", s.addr)
	if err != nil {
		return err
	}
	if s.tlsOn {
		tc := tls.Client(c, &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12})
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = c.Close()
			return err
		}
		c = tc
	}
	s.conn, s.rd = c, bufio.NewReader(c)
	return nil
}

// MemcachedMaxValue is the most bytes of one value shown.
const MemcachedMaxValue = 64 * 1024

// exchange sends one request and reads lines until done says the answer
// is complete (bulk values are read by length).
func (s *MemcachedSession) exchange(ctx context.Context, req string, done func(line string) bool, bulk func(line string, data []byte)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("memcached: session closed")
	}
	if s.conn == nil {
		if err := s.connect(ctx); err != nil {
			return err
		}
	}
	conn := s.conn
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()
	s.cmu.Lock()
	s.active = conn
	s.cmu.Unlock()
	defer func() {
		s.cmu.Lock()
		s.active = nil
		s.cmu.Unlock()
	}()
	if dl, ok := ctx.Deadline(); ok {
		_ = s.conn.SetDeadline(dl)
	} else {
		_ = s.conn.SetDeadline(time.Now().Add(30 * time.Second))
	}
	fail := func(err error) error {
		_ = s.conn.Close()
		s.conn = nil
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if _, err := s.conn.Write([]byte(req)); err != nil {
		return fail(err)
	}
	for {
		line, err := s.rd.ReadString('\n')
		if err != nil {
			return fail(err)
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, "VALUE ") && bulk != nil {
			f := strings.Fields(line)
			if len(f) < 4 {
				return fail(errors.New("memcached: bad VALUE line"))
			}
			n, err := strconv.Atoi(f[3])
			if err != nil || n < 0 || n > 1<<30 {
				return fail(errors.New("memcached: bad value length"))
			}
			data := make([]byte, n+2)
			if _, err := readFull(s.rd, data); err != nil {
				return fail(err)
			}
			bulk(line, data[:n])
			continue
		}
		if line == "ERROR" || strings.HasPrefix(line, "CLIENT_ERROR") || strings.HasPrefix(line, "SERVER_ERROR") {
			return errors.New("memcached: " + line)
		}
		if done(line) {
			return nil
		}
	}
}

func readFull(r *bufio.Reader, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := r.Read(b[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func (s *MemcachedSession) Ping(ctx context.Context) error {
	return s.exchange(ctx, "version\r\n", func(l string) bool { return strings.HasPrefix(l, "VERSION") }, nil)
}

// memcachedKey checks one key (no spaces or control bytes, at most 250).
func memcachedKey(k string) error {
	if k == "" || len(k) > 250 {
		return fmt.Errorf("memcached: a key is 1 to 250 bytes")
	}
	for i := 0; i < len(k); i++ {
		if k[i] <= ' ' || k[i] == 0x7f {
			return fmt.Errorf("memcached: a key cannot hold spaces or control bytes")
		}
	}
	return nil
}

func (s *MemcachedSession) Run(ctx context.Context, line string, opts RunOptions) Result {
	start := time.Now()
	res := s.run(ctx, stripLineComments(line), opts)
	res.Elapsed = time.Since(start)
	return res
}

func (s *MemcachedSession) run(ctx context.Context, line string, opts RunOptions) Result {
	args, err := ParseRedisCommand(line)
	if err != nil {
		return Result{Err: err}
	}
	if len(args) == 0 {
		return Result{Err: errors.New("memcached: write get, set, delete, touch, stats or keys")}
	}
	cmd := strings.ToLower(args[0])
	if s.readOnly && ClassifyMemcached(cmd) != KindRead {
		return Result{Err: errors.New("memcached: the session is read-only")}
	}
	for _, k := range keyArgs(cmd, args[1:]) {
		if err := memcachedKey(k); err != nil {
			return Result{Err: err}
		}
	}
	switch cmd {
	case "get", "gets":
		if len(args) < 2 {
			return Result{Err: errors.New("memcached: get KEY [KEY…]")}
		}
		res := Result{Columns: []Column{{Name: "key", PK: true}, {Name: "value"}, {Name: "flags"}, {Name: "bytes"}}}
		err := s.exchange(ctx, "get "+strings.Join(args[1:], " ")+"\r\n", func(l string) bool { return l == "END" }, func(l string, data []byte) {
			f := strings.Fields(l)
			v := string(data)
			if len(v) > MemcachedMaxValue {
				v = v[:MemcachedMaxValue]
			}
			res.Rows = append(res.Rows, []Cell{{Text: RedisDisplay(f[1])}, {Text: RedisDisplay(v)}, {Text: f[2]}, {Text: f[3]}})
		})
		res.Err = err
		return res
	case "set", "add", "replace":
		if len(args) < 3 || len(args) > 4 {
			return Result{Err: fmt.Errorf("memcached: %s KEY VALUE [TTL]", cmd)}
		}
		ttl := "0"
		if len(args) == 4 {
			if _, err := strconv.Atoi(args[3]); err != nil {
				return Result{Err: errors.New("memcached: TTL is seconds")}
			}
			ttl = args[3]
		}
		return s.simple(ctx, fmt.Sprintf("%s %s 0 %s %d\r\n%s\r\n", cmd, args[1], ttl, len(args[2]), args[2]), "STORED")
	case "delete":
		if len(args) != 2 {
			return Result{Err: errors.New("memcached: delete KEY")}
		}
		return s.simple(ctx, "delete "+args[1]+"\r\n", "DELETED")
	case "touch":
		if len(args) != 3 {
			return Result{Err: errors.New("memcached: touch KEY TTL")}
		}
		if _, err := strconv.Atoi(args[2]); err != nil {
			return Result{Err: errors.New("memcached: TTL is seconds")}
		}
		return s.simple(ctx, "touch "+args[1]+" "+args[2]+"\r\n", "TOUCHED")
	case "stats":
		req := "stats\r\n"
		if len(args) == 2 && (args[1] == "items" || args[1] == "slabs" || args[1] == "settings") {
			req = "stats " + args[1] + "\r\n"
		} else if len(args) > 1 {
			return Result{Err: errors.New("memcached: stats [items|slabs|settings]")}
		}
		res := Result{Columns: []Column{{Name: "stat"}, {Name: "value"}}}
		res.Err = s.exchange(ctx, req, func(l string) bool {
			if f := strings.Fields(l); len(f) >= 3 && f[0] == "STAT" {
				res.Rows = append(res.Rows, []Cell{{Text: f[1]}, {Text: strings.Join(f[2:], " ")}})
			}
			return l == "END"
		}, nil)
		return res
	case "keys":
		pat := ""
		if len(args) > 1 {
			pat = args[1]
		}
		keys, more, err := s.keys(ctx, opts.MaxRows)
		res := Result{Columns: []Column{{Name: "key", PK: true}, {Name: "exp"}, {Name: "size"}}, Err: err, Truncated: more}
		for _, k := range keys {
			if pat == "" || strings.Contains(k.key, pat) {
				res.Rows = append(res.Rows, []Cell{{Text: RedisDisplay(k.key)}, {Text: k.exp}, {Text: k.size}})
			}
		}
		return res
	case "flush_all":
		return Result{Err: errors.New("memcached: flush_all is not sent; it empties the whole server with no undo")}
	}
	return Result{Err: fmt.Errorf("memcached: unknown command %q", args[0])}
}

func keyArgs(cmd string, rest []string) []string {
	switch cmd {
	case "get", "gets":
		return rest
	case "set", "add", "replace", "delete", "touch":
		if len(rest) > 0 {
			return rest[:1]
		}
	}
	return nil
}

func (s *MemcachedSession) simple(ctx context.Context, req, ok string) Result {
	var got string
	err := s.exchange(ctx, req, func(l string) bool { got = l; return true }, nil)
	if err != nil {
		return Result{Err: err}
	}
	if got != ok {
		return Result{Err: errors.New("memcached: " + got)}
	}
	return Result{Affected: 1}
}

type memcachedKeyInfo struct{ key, exp, size string }

// MemcachedTreeMax is the most keys one listing reads.
const MemcachedTreeMax = 10000

// keys reads "lru_crawler metadump all" (key=… exp=… size=… lines, then
// END), at most max keys (0: MemcachedTreeMax), sorted.
func (s *MemcachedSession) keys(ctx context.Context, max int) ([]memcachedKeyInfo, bool, error) {
	if max <= 0 || max > MemcachedTreeMax {
		max = MemcachedTreeMax
	}
	var out []memcachedKeyInfo
	more := false
	err := s.exchange(ctx, "lru_crawler metadump all\r\n", func(l string) bool {
		if l == "END" {
			return true
		}
		if strings.HasPrefix(l, "BUSY") {
			return true
		}
		var k memcachedKeyInfo
		for _, f := range strings.Fields(l) {
			name, v, _ := strings.Cut(f, "=")
			switch name {
			case "key":
				if u, err := url.QueryUnescape(v); err == nil {
					v = u
				}
				k.key = v
			case "exp":
				k.exp = v
			case "size":
				k.size = v
			}
		}
		if k.key != "" {
			if len(out) < max {
				out = append(out, k)
			} else {
				more = true
			}
		}
		return false
	}, nil)
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out, more, err
}

func (s *MemcachedSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	if req.Database == "" && req.Schema == "" && req.Object == "" {
		out.Databases = []string{"keys"}
		return out, nil
	}
	if req.Object == "" {
		keys, _, err := s.keys(ctx, MemcachedTreeMax)
		for _, k := range keys {
			out.Objects = append(out.Objects, SchemaObject{Name: k.key, Kind: "key"})
		}
		return out, err
	}
	out.Columns = []Column{{Name: "key", PK: true, Table: req.Object}, {Name: "value", Table: req.Object}, {Name: "flags", Table: req.Object}, {Name: "bytes", Table: req.Object}}
	return out, nil
}

func (s *MemcachedSession) Begin(context.Context) error    { return errNoTx("Memcached") }
func (s *MemcachedSession) Commit(context.Context) error   { return errNoTx("Memcached") }
func (s *MemcachedSession) Rollback(context.Context) error { return errNoTx("Memcached") }

func (s *MemcachedSession) ReadOnlySession() (Session, error) {
	return &MemcachedSession{addr: s.addr, host: s.host, tlsOn: s.tlsOn, dial: s.dial, readOnly: true}, nil
}

// Cancel breaks a waiting command (the connection is redialed next time).
func (s *MemcachedSession) Cancel() {
	s.cmu.Lock()
	c := s.active
	s.cmu.Unlock()
	if c != nil {
		_ = c.SetDeadline(time.Unix(1, 0))
	}
}

func (s *MemcachedSession) Close() error {
	s.Cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
	return nil
}
