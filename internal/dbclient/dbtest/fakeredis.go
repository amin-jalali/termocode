package dbtest

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FakeRedis is an in-process Redis server speaking RESP2 over net.Pipe. It
// knows the commands the Databases view and the console send, keeps every
// command it received (Commands) and refuses an unknown one like a real
// server. No sockets, no goroutines after Close.
type FakeRedis struct {
	mu       sync.Mutex
	dbs      map[int]map[string]*redisValue
	User     string // an ACL user (empty: the default user)
	Password string // the password AUTH needs (empty: none)
	Version  string
	log      [][]string
	conns    []net.Conn
	closed   bool
	hang     bool
	// NoUnlink makes UNLINK an unknown command, like a Redis before 4.0.
	NoUnlink bool
	// TLS makes the server speak TLS (rediss://) with this config.
	TLS *tls.Config
}

type redisValue struct {
	typ    string
	str    []byte
	hash   map[string][]byte
	list   [][]byte
	set    map[string]bool
	zset   map[string]float64
	stream []redisEntry
	expire time.Time
}

type redisEntry struct {
	id     string
	fields []string
}

func NewFakeRedis() *FakeRedis {
	return &FakeRedis{dbs: map[int]map[string]*redisValue{}, Version: "7.2.4"}
}

// Dial is a redigo DialContextFunc: one net.Pipe end per connection.
func (f *FakeRedis) Dial(_ context.Context, _, _ string) (net.Conn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, errors.New("dial tcp: connection refused")
	}
	client, server := net.Pipe()
	f.conns = append(f.conns, server)
	if f.TLS != nil {
		go f.serve(tls.Server(server, f.TLS))
	} else {
		go f.serve(server)
	}
	return client, nil
}

// SetHang makes every later command wait until its connection closes (a
// stuck server: the client's timeout or cancel must end it).
func (f *FakeRedis) SetHang(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hang = on
}

// Close ends every connection.
func (f *FakeRedis) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	for _, c := range f.conns {
		_ = c.Close()
	}
}

// Commands are the commands received, each as its words joined by spaces.
func (f *FakeRedis) Commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.log))
	for i, l := range f.log {
		out[i] = strings.Join(l, " ")
	}
	return out
}

// Sent reports whether a command with this name (any case) was received.
func (f *FakeRedis) Sent(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.log {
		if len(l) > 0 && strings.EqualFold(l[0], name) {
			return true
		}
	}
	return false
}

func (f *FakeRedis) db(n int) map[string]*redisValue {
	d := f.dbs[n]
	if d == nil {
		d = map[string]*redisValue{}
		f.dbs[n] = d
	}
	return d
}

// SetString, HSet, RPush, SAdd, ZAdd and XAdd fill the store for tests.
func (f *FakeRedis) SetString(db int, key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.db(db)[key] = &redisValue{typ: "string", str: []byte(value)}
}

func (f *FakeRedis) HSet(db int, key string, kv ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.get(db, key, "hash")
	for i := 0; i+1 < len(kv); i += 2 {
		v.hash[kv[i]] = []byte(kv[i+1])
	}
}

func (f *FakeRedis) RPush(db int, key string, items ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.get(db, key, "list")
	for _, it := range items {
		v.list = append(v.list, []byte(it))
	}
}

func (f *FakeRedis) SAdd(db int, key string, members ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.get(db, key, "set")
	for _, m := range members {
		v.set[m] = true
	}
}

func (f *FakeRedis) ZAdd(db int, key string, score float64, member string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.get(db, key, "zset").zset[member] = score
}

func (f *FakeRedis) XAdd(db int, key, id string, fields ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.get(db, key, "stream")
	v.stream = append(v.stream, redisEntry{id: id, fields: fields})
}

// Expire sets a TTL on a key.
func (f *FakeRedis) Expire(db int, key string, d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v := f.db(db)[key]; v != nil {
		v.expire = time.Now().Add(d)
	}
}

// Exists reports whether db holds key.
func (f *FakeRedis) Exists(db int, key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.db(db)[key] != nil
}

// Hash returns a copy of a hash value.
func (f *FakeRedis) Hash(db int, key string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	if v := f.db(db)[key]; v != nil {
		for k, x := range v.hash {
			out[k] = string(x)
		}
	}
	return out
}

// String returns a string value.
func (f *FakeRedis) String(db int, key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v := f.db(db)[key]; v != nil {
		return string(v.str)
	}
	return ""
}

// TTL is the time left on a key (0: none).
func (f *FakeRedis) TTL(db int, key string) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v := f.db(db)[key]; v != nil && !v.expire.IsZero() {
		return time.Until(v.expire)
	}
	return 0
}

func (f *FakeRedis) get(db int, key, typ string) *redisValue {
	v := f.db(db)[key]
	if v == nil || v.typ != typ {
		v = &redisValue{typ: typ, hash: map[string][]byte{}, set: map[string]bool{}, zset: map[string]float64{}}
		f.db(db)[key] = v
	}
	return v
}

type fakeConn struct {
	db     int
	authed bool
}

func (f *FakeRedis) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	w := bufio.NewWriter(c)
	st := &fakeConn{}
	for {
		args, err := readCommand(r)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.log = append(f.log, args)
		hang := f.hang
		f.mu.Unlock()
		if hang {
			_, _ = io.Copy(io.Discard, r)
			return
		}
		f.mu.Lock()
		reply := f.exec(st, args)
		f.mu.Unlock()
		writeReply(w, reply)
		if w.Flush() != nil {
			return
		}
	}
}

func readCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "*") {
		return strings.Fields(line), nil
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		head, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(strings.TrimRight(head, "\r\n")[1:])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:size]))
	}
	return args, nil
}

type status string
type redisErr string

func writeReply(w *bufio.Writer, v any) {
	switch x := v.(type) {
	case nil:
		w.WriteString("$-1\r\n")
	case status:
		w.WriteString("+" + string(x) + "\r\n")
	case redisErr:
		w.WriteString("-" + string(x) + "\r\n")
	case int:
		fmt.Fprintf(w, ":%d\r\n", x)
	case int64:
		fmt.Fprintf(w, ":%d\r\n", x)
	case string:
		fmt.Fprintf(w, "$%d\r\n%s\r\n", len(x), x)
	case []byte:
		fmt.Fprintf(w, "$%d\r\n%s\r\n", len(x), x)
	case []any:
		fmt.Fprintf(w, "*%d\r\n", len(x))
		for _, e := range x {
			writeReply(w, e)
		}
	case []string:
		fmt.Fprintf(w, "*%d\r\n", len(x))
		for _, e := range x {
			writeReply(w, e)
		}
	}
}

func (f *FakeRedis) live(st *fakeConn, key string) *redisValue {
	v := f.db(st.db)[key]
	if v != nil && !v.expire.IsZero() && time.Now().After(v.expire) {
		delete(f.db(st.db), key)
		return nil
	}
	return v
}

func wrongType() redisErr {
	return "WRONGTYPE Operation against a key holding the wrong kind of value"
}

func (f *FakeRedis) exec(st *fakeConn, args []string) any {
	if len(args) == 0 {
		return redisErr("ERR empty command")
	}
	cmd := strings.ToUpper(args[0])
	a := args[1:]
	if f.Password != "" && !st.authed && cmd != "AUTH" && cmd != "HELLO" {
		return redisErr("NOAUTH Authentication required.")
	}
	if cmd == "UNLINK" && f.NoUnlink {
		return redisErr(fmt.Sprintf("ERR unknown command '%s', with args beginning with: ", args[0]))
	}
	arity := func(n int) bool { return len(a) >= n }
	switch cmd {
	case "AUTH":
		user, pass := "default", ""
		switch len(a) {
		case 1:
			pass = a[0]
		case 2:
			user, pass = a[0], a[1]
		default:
			return redisErr("ERR wrong number of arguments for 'auth' command")
		}
		want := f.User
		if want == "" {
			want = "default"
		}
		if pass != f.Password || user != want {
			return redisErr("WRONGPASS invalid username-password pair or user is disabled.")
		}
		st.authed = true
		return status("OK")
	case "PING":
		if len(a) > 0 {
			return a[0]
		}
		return status("PONG")
	case "ECHO":
		if !arity(1) {
			return redisErr("ERR wrong number of arguments")
		}
		return a[0]
	case "CLIENT":
		return status("OK")
	case "SELECT":
		n, err := strconv.Atoi(a[0])
		if err != nil || n < 0 || n > 15 {
			return redisErr("ERR DB index is out of range")
		}
		st.db = n
		return status("OK")
	case "INFO":
		var b strings.Builder
		b.WriteString("# Server\r\nredis_version:" + f.Version + "\r\n# Keyspace\r\n")
		var dbs []int
		for n, d := range f.dbs {
			if len(d) > 0 {
				dbs = append(dbs, n)
			}
		}
		sort.Ints(dbs)
		for _, n := range dbs {
			fmt.Fprintf(&b, "db%d:keys=%d,expires=0,avg_ttl=0\r\n", n, len(f.dbs[n]))
		}
		return b.String()
	case "DBSIZE":
		return len(f.db(st.db))
	case "KEYS":
		var out []string
		for k := range f.db(st.db) {
			if ok, _ := path.Match(a[0], k); ok {
				out = append(out, k)
			}
		}
		sort.Strings(out)
		return out
	case "SCAN":
		keys := make([]string, 0, len(f.db(st.db)))
		for k := range f.db(st.db) {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return scanReply(keys, a, nil)
	case "TYPE":
		v := f.live(st, a[0])
		if v == nil {
			return status("none")
		}
		return status(v.typ)
	case "PTTL", "TTL":
		v := f.live(st, a[0])
		switch {
		case v == nil:
			return -2
		case v.expire.IsZero():
			return -1
		case cmd == "TTL":
			return int64(time.Until(v.expire).Seconds())
		}
		return time.Until(v.expire).Milliseconds()
	case "EXISTS":
		n := 0
		for _, k := range a {
			if f.live(st, k) != nil {
				n++
			}
		}
		return n
	case "MEMORY":
		if len(a) >= 2 && strings.EqualFold(a[0], "USAGE") {
			v := f.live(st, a[1])
			if v == nil {
				return nil
			}
			return 56 + len(a[1]) + f.size(v)
		}
		return redisErr("ERR unknown subcommand")
	case "OBJECT":
		return status("listpack")
	case "CONFIG":
		if len(a) >= 2 && strings.EqualFold(a[0], "GET") {
			return []string{a[1], "16"}
		}
		return status("OK")
	case "STRLEN":
		v := f.live(st, a[0])
		if v == nil {
			return 0
		}
		return len(v.str)
	case "HLEN", "LLEN", "SCARD", "ZCARD", "XLEN":
		v := f.live(st, a[0])
		if v == nil {
			return 0
		}
		switch cmd {
		case "HLEN":
			return len(v.hash)
		case "LLEN":
			return len(v.list)
		case "SCARD":
			return len(v.set)
		case "ZCARD":
			return len(v.zset)
		}
		return len(v.stream)
	case "GET":
		v := f.live(st, a[0])
		if v == nil {
			return nil
		}
		if v.typ != "string" {
			return wrongType()
		}
		return v.str
	case "GETRANGE":
		v := f.live(st, a[0])
		if v == nil {
			return ""
		}
		s, _ := strconv.Atoi(a[1])
		e, _ := strconv.Atoi(a[2])
		if e < 0 {
			e = len(v.str) + e
		}
		e = min(e, len(v.str)-1)
		if s > e || s >= len(v.str) {
			return ""
		}
		return v.str[s : e+1]
	case "SET":
		if !arity(2) {
			return redisErr("ERR wrong number of arguments for 'set' command")
		}
		old := f.live(st, a[0])
		nv := &redisValue{typ: "string", str: []byte(a[1])}
		for i := 2; i < len(a); i++ {
			switch strings.ToUpper(a[i]) {
			case "KEEPTTL":
				if old != nil {
					nv.expire = old.expire
				}
			case "EX":
				if i+1 < len(a) {
					n, _ := strconv.Atoi(a[i+1])
					nv.expire = time.Now().Add(time.Duration(n) * time.Second)
					i++
				}
			}
		}
		f.db(st.db)[a[0]] = nv
		return status("OK")
	case "HSET":
		if len(a) < 3 || len(a)%2 == 0 {
			return redisErr("ERR wrong number of arguments for 'hset' command")
		}
		if v := f.live(st, a[0]); v != nil && v.typ != "hash" {
			return wrongType()
		}
		v := f.get(st.db, a[0], "hash")
		added := 0
		for i := 1; i+1 < len(a); i += 2 {
			if _, ok := v.hash[a[i]]; !ok {
				added++
			}
			v.hash[a[i]] = []byte(a[i+1])
		}
		return added
	case "HGET":
		v := f.live(st, a[0])
		if v == nil || v.hash[a[1]] == nil {
			return nil
		}
		return v.hash[a[1]]
	case "HDEL":
		v := f.live(st, a[0])
		n := 0
		if v != nil {
			for _, k := range a[1:] {
				if _, ok := v.hash[k]; ok {
					delete(v.hash, k)
					n++
				}
			}
			f.dropEmpty(st, a[0], v)
		}
		return n
	case "HGETALL":
		v := f.live(st, a[0])
		var out []any
		if v != nil {
			for _, k := range sortedKeys(v.hash) {
				out = append(out, k, v.hash[k])
			}
		}
		return out
	case "HSCAN", "SSCAN", "ZSCAN":
		v := f.live(st, a[0])
		var keys []string
		if v != nil {
			switch cmd {
			case "HSCAN":
				keys = sortedKeys(v.hash)
			case "SSCAN":
				keys = sortedBool(v.set)
			default:
				for k := range v.zset {
					keys = append(keys, k)
				}
				sort.Strings(keys)
			}
		}
		return scanReply(keys, a[1:], func(k string) []any {
			switch cmd {
			case "HSCAN":
				return []any{k, v.hash[k]}
			case "ZSCAN":
				return []any{k, strconv.FormatFloat(v.zset[k], 'f', -1, 64)}
			}
			return []any{k}
		})
	case "LRANGE":
		v := f.live(st, a[0])
		if v == nil {
			return []any{}
		}
		s, _ := strconv.Atoi(a[1])
		e, _ := strconv.Atoi(a[2])
		if e < 0 {
			e = len(v.list) + e
		}
		e = min(e, len(v.list)-1)
		out := []any{}
		for i := s; i <= e; i++ {
			out = append(out, v.list[i])
		}
		return out
	case "LSET":
		v := f.live(st, a[0])
		if v == nil {
			return redisErr("ERR no such key")
		}
		i, _ := strconv.Atoi(a[1])
		if i < 0 || i >= len(v.list) {
			return redisErr("ERR index out of range")
		}
		v.list[i] = []byte(a[2])
		return status("OK")
	case "RPUSH":
		v := f.get(st.db, a[0], "list")
		for _, x := range a[1:] {
			v.list = append(v.list, []byte(x))
		}
		return len(v.list)
	case "LREM":
		v := f.live(st, a[0])
		if v == nil {
			return 0
		}
		n, _ := strconv.Atoi(a[1])
		removed := 0
		var keep [][]byte
		for _, x := range v.list {
			if string(x) == a[2] && (n == 0 || removed < n) {
				removed++
				continue
			}
			keep = append(keep, x)
		}
		v.list = keep
		f.dropEmpty(st, a[0], v)
		return removed
	case "SADD":
		v := f.get(st.db, a[0], "set")
		n := 0
		for _, m := range a[1:] {
			if !v.set[m] {
				v.set[m] = true
				n++
			}
		}
		return n
	case "SREM":
		v := f.live(st, a[0])
		n := 0
		if v != nil {
			for _, m := range a[1:] {
				if v.set[m] {
					delete(v.set, m)
					n++
				}
			}
			f.dropEmpty(st, a[0], v)
		}
		return n
	case "SMEMBERS":
		v := f.live(st, a[0])
		if v == nil {
			return []any{}
		}
		return sortedBool(v.set)
	case "ZADD":
		v := f.get(st.db, a[0], "zset")
		n := 0
		for i := 1; i+1 < len(a); i += 2 {
			sc, err := strconv.ParseFloat(a[i], 64)
			if err != nil {
				return redisErr("ERR value is not a valid float")
			}
			if _, ok := v.zset[a[i+1]]; !ok {
				n++
			}
			v.zset[a[i+1]] = sc
		}
		return n
	case "ZREM":
		v := f.live(st, a[0])
		n := 0
		if v != nil {
			for _, m := range a[1:] {
				if _, ok := v.zset[m]; ok {
					delete(v.zset, m)
					n++
				}
			}
			f.dropEmpty(st, a[0], v)
		}
		return n
	case "ZRANGE":
		v := f.live(st, a[0])
		if v == nil {
			return []any{}
		}
		type sm struct {
			m string
			s float64
		}
		var all []sm
		for m, s := range v.zset {
			all = append(all, sm{m, s})
		}
		sort.Slice(all, func(i, j int) bool {
			if all[i].s != all[j].s {
				return all[i].s < all[j].s
			}
			return all[i].m < all[j].m
		})
		s, _ := strconv.Atoi(a[1])
		e, _ := strconv.Atoi(a[2])
		if e < 0 {
			e = len(all) + e
		}
		e = min(e, len(all)-1)
		scores := len(a) > 3 && strings.EqualFold(a[3], "WITHSCORES")
		out := []any{}
		for i := s; i <= e; i++ {
			out = append(out, all[i].m)
			if scores {
				out = append(out, strconv.FormatFloat(all[i].s, 'f', -1, 64))
			}
		}
		return out
	case "XADD":
		v := f.get(st.db, a[0], "stream")
		id := a[1]
		if id == "*" {
			id = fmt.Sprintf("%d-0", time.Now().UnixMilli())
		}
		v.stream = append(v.stream, redisEntry{id: id, fields: append([]string(nil), a[2:]...)})
		return id
	case "XDEL":
		v := f.live(st, a[0])
		n := 0
		if v != nil {
			var keep []redisEntry
			for _, e := range v.stream {
				if contains(a[1:], e.id) {
					n++
					continue
				}
				keep = append(keep, e)
			}
			v.stream = keep
		}
		return n
	case "XRANGE":
		v := f.live(st, a[0])
		out := []any{}
		if v == nil {
			return out
		}
		count := len(v.stream)
		if len(a) >= 5 && strings.EqualFold(a[3], "COUNT") {
			count, _ = strconv.Atoi(a[4])
		}
		for _, e := range v.stream {
			if a[1] != "-" && streamLess(e.id, a[1]) {
				continue
			}
			if len(out) == count {
				break
			}
			fields := make([]any, len(e.fields))
			for i, x := range e.fields {
				fields[i] = x
			}
			out = append(out, []any{e.id, fields})
		}
		return out
	case "DEL", "UNLINK":
		n := 0
		for _, k := range a {
			if f.live(st, k) != nil {
				delete(f.db(st.db), k)
				n++
			}
		}
		return n
	case "EXPIRE":
		v := f.live(st, a[0])
		if v == nil {
			return 0
		}
		n, _ := strconv.Atoi(a[1])
		v.expire = time.Now().Add(time.Duration(n) * time.Second)
		return 1
	case "PERSIST":
		v := f.live(st, a[0])
		if v == nil || v.expire.IsZero() {
			return 0
		}
		v.expire = time.Time{}
		return 1
	case "FLUSHDB":
		f.dbs[st.db] = map[string]*redisValue{}
		return status("OK")
	case "FLUSHALL":
		f.dbs = map[int]map[string]*redisValue{}
		return status("OK")
	}
	return redisErr(fmt.Sprintf("ERR unknown command '%s', with args beginning with: ", args[0]))
}

func (f *FakeRedis) size(v *redisValue) int {
	n := len(v.str)
	for k, x := range v.hash {
		n += len(k) + len(x)
	}
	for _, x := range v.list {
		n += len(x)
	}
	return n
}

func (f *FakeRedis) dropEmpty(st *fakeConn, key string, v *redisValue) {
	if len(v.hash) == 0 && len(v.list) == 0 && len(v.set) == 0 && len(v.zset) == 0 && len(v.stream) == 0 && v.typ != "string" {
		delete(f.db(st.db), key)
	}
}

// scanReply pages keys by index: the cursor is the next index. MATCH is
// applied after the COUNT slice, as Redis does (a page can be empty).
func scanReply(keys []string, a []string, expand func(string) []any) any {
	cursor, _ := strconv.Atoi(a[0])
	match, count := "*", 10
	for i := 1; i+1 < len(a); i += 2 {
		switch strings.ToUpper(a[i]) {
		case "MATCH":
			match = a[i+1]
		case "COUNT":
			count, _ = strconv.Atoi(a[i+1])
		}
	}
	end := min(cursor+count, len(keys))
	var out []any
	for _, k := range keys[min(cursor, len(keys)):end] {
		if ok, _ := path.Match(match, k); !ok {
			continue
		}
		if expand != nil {
			out = append(out, expand(k)...)
		} else {
			out = append(out, k)
		}
	}
	next := strconv.Itoa(end)
	if end >= len(keys) {
		next = "0"
	}
	if out == nil {
		out = []any{}
	}
	return []any{next, out}
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedBool(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func streamLess(a, b string) bool {
	am, as, _ := strings.Cut(a, "-")
	bm, bs, _ := strings.Cut(b, "-")
	ai, _ := strconv.ParseUint(am, 10, 64)
	bi, _ := strconv.ParseUint(bm, 10, 64)
	if ai != bi {
		return ai < bi
	}
	x, _ := strconv.ParseUint(as, 10, 64)
	y, _ := strconv.ParseUint(bs, 10, 64)
	return x < y
}
