package dbclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// EtcdSession browses etcd v3 (DBX-69) through its JSON gateway (POST
// /v3/kv/range, /v3/kv/put, /v3/kv/deleterange), port 2379. With a user
// it signs in once (/v3/auth/authenticate) and sends the token. The
// console takes etcdctl-style lines:
//
//	get KEY [--prefix] [--limit N]
//	put KEY VALUE
//	del KEY [--prefix]
//	members | status
//
// The tree lists the first path segments ("/app/") as databases and their
// keys as tables. Read-only refuses put and del in the session.
type EtcdSession struct {
	*httpEngine
	readOnly bool
	token    string
}

func init() {
	registerHTTPEngine(HTTPEngine{
		ID: "etcd", Title: "etcd", Port: 2379,
		Hint:      "keys over the v3 JSON gateway, default port 2379",
		Schemes:   []string{"etcd", "etcds"},
		LineBased: true,
		Classify:  ClassifyEtcd,
		Open: func(c Conn, password string) (Session, error) {
			return NewEtcdSession(c, password)
		},
	})
}

// NewEtcdSession makes a session of c; it does not connect.
func NewEtcdSession(c Conn, password string) (*EtcdSession, error) {
	e, err := newHTTPEngine("etcd", c, password)
	if err != nil {
		return nil, err
	}
	return &EtcdSession{httpEngine: e}, nil
}

// ClassifyEtcd judges one console line.
func ClassifyEtcd(line string) Kind {
	f := strings.Fields(stripLineComments(line))
	if len(f) == 0 {
		return KindUnknown
	}
	switch strings.ToLower(f[0]) {
	case "get", "members", "status":
		return KindRead
	case "put":
		return KindWrite
	case "del", "delete":
		return KindDDL // can remove a whole prefix
	}
	return KindUnknown
}

// etcdPrefixEnd is the range_end that covers every key starting with p.
func etcdPrefixEnd(p string) string {
	b := []byte(p)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < 0xff {
			b[i]++
			return string(b[:i+1])
		}
	}
	return "\x00"
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func unb64(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return s
	}
	return string(b)
}

type etcdKV struct {
	Key            string `json:"key"`
	Value          string `json:"value"`
	CreateRevision string `json:"create_revision"`
	ModRevision    string `json:"mod_revision"`
	Version        string `json:"version"`
	Lease          string `json:"lease"`
}

// call signs in when needed and posts one gateway request.
func (s *EtcdSession) call(ctx context.Context, path string, in, out any) error {
	if s.user != "" && s.token == "" {
		var auth struct {
			Token string `json:"token"`
		}
		if err := s.doJSON(ctx, "POST", "/v3/auth/authenticate", map[string]string{"name": s.user, "password": s.password}, &auth); err != nil {
			return err
		}
		s.token = auth.Token
		s.header.Set("Authorization", s.token)
	}
	return s.doJSON(ctx, "POST", path, in, out)
}

// rangeKeys reads key (or every key under the prefix) up to limit.
func (s *EtcdSession) rangeKeys(ctx context.Context, key string, prefix, keysOnly bool, limit int) ([]etcdKV, bool, error) {
	req := map[string]any{"key": b64(key)}
	if prefix {
		if key == "" {
			req["key"], req["range_end"] = b64("\x00"), b64("\x00")
		} else {
			req["range_end"] = b64(etcdPrefixEnd(key))
		}
	}
	if limit > 0 {
		req["limit"] = limit
	}
	if keysOnly {
		req["keys_only"] = true
	}
	var out struct {
		KVs   []etcdKV `json:"kvs"`
		More  bool     `json:"more"`
		Count string   `json:"count"`
	}
	err := s.call(ctx, "/v3/kv/range", req, &out)
	return out.KVs, out.More, err
}

func (s *EtcdSession) Ping(ctx context.Context) error {
	return s.call(ctx, "/v3/maintenance/status", map[string]any{}, nil)
}

// EtcdTreeMax is the most keys one tree row loads.
const EtcdTreeMax = 10000

func (s *EtcdSession) Run(ctx context.Context, line string, opts RunOptions) Result {
	start := time.Now()
	res := s.run(ctx, stripLineComments(line), opts)
	res.Elapsed = time.Since(start)
	return res
}

func (s *EtcdSession) run(ctx context.Context, line string, opts RunOptions) Result {
	args, err := ParseRedisCommand(line)
	if err != nil || len(args) == 0 {
		return Result{Err: fmt.Errorf("etcd: write get, put, del, members or status")}
	}
	cmd := strings.ToLower(args[0])
	prefix, limit := false, opts.MaxRows
	var rest []string
	for i := 1; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--prefix":
			prefix = true
		case a == "--limit" && i+1 < len(args):
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 1 {
				return Result{Err: fmt.Errorf("etcd: --limit wants a number")}
			}
			limit = n
			i++
		default:
			rest = append(rest, a)
		}
	}
	if s.readOnly && ClassifyEtcd(cmd) != KindRead {
		return Result{Err: fmt.Errorf("etcd: the session is read-only")}
	}
	switch cmd {
	case "get":
		if len(rest) != 1 {
			return Result{Err: fmt.Errorf("etcd: get KEY [--prefix] [--limit N]")}
		}
		kvs, more, err := s.rangeKeys(ctx, rest[0], prefix, false, limit)
		if err != nil {
			return Result{Err: err}
		}
		res := Result{Columns: []Column{{Name: "key", PK: true}, {Name: "value"}, {Name: "version"}, {Name: "mod_revision"}, {Name: "lease"}}, Truncated: more}
		for _, kv := range kvs {
			res.Rows = append(res.Rows, []Cell{{Text: RedisDisplay(unb64(kv.Key))}, {Text: RedisDisplay(unb64(kv.Value))}, {Text: kv.Version}, {Text: kv.ModRevision}, {Text: kv.Lease}})
		}
		return res
	case "put":
		if len(rest) != 2 {
			return Result{Err: fmt.Errorf("etcd: put KEY VALUE")}
		}
		if err := s.call(ctx, "/v3/kv/put", map[string]any{"key": b64(rest[0]), "value": b64(rest[1])}, nil); err != nil {
			return Result{Err: err}
		}
		return Result{Affected: 1}
	case "del", "delete":
		if len(rest) != 1 {
			return Result{Err: fmt.Errorf("etcd: del KEY [--prefix]")}
		}
		req := map[string]any{"key": b64(rest[0])}
		if prefix {
			req["range_end"] = b64(etcdPrefixEnd(rest[0]))
		}
		var out struct {
			Deleted string `json:"deleted"`
		}
		if err := s.call(ctx, "/v3/kv/deleterange", req, &out); err != nil {
			return Result{Err: err}
		}
		n, _ := strconv.ParseInt(out.Deleted, 10, 64)
		return Result{Affected: n}
	case "members":
		var out struct {
			Members []struct {
				ID         string   `json:"ID"`
				Name       string   `json:"name"`
				PeerURLs   []string `json:"peerURLs"`
				ClientURLs []string `json:"clientURLs"`
			} `json:"members"`
		}
		if err := s.call(ctx, "/v3/cluster/member/list", map[string]any{}, &out); err != nil {
			return Result{Err: err}
		}
		res := Result{Columns: []Column{{Name: "id"}, {Name: "name"}, {Name: "peer_urls"}, {Name: "client_urls"}}}
		for _, m := range out.Members {
			res.Rows = append(res.Rows, []Cell{{Text: m.ID}, {Text: m.Name}, {Text: strings.Join(m.PeerURLs, ",")}, {Text: strings.Join(m.ClientURLs, ",")}})
		}
		return res
	case "status":
		var out map[string]any
		if err := s.call(ctx, "/v3/maintenance/status", map[string]any{}, &out); err != nil {
			return Result{Err: err}
		}
		res := Result{Columns: []Column{{Name: "field"}, {Name: "value"}}}
		keys := make([]string, 0, len(out))
		for k := range out {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			res.Rows = append(res.Rows, []Cell{{Text: k}, {Text: fmt.Sprint(out[k])}})
		}
		return res
	}
	return Result{Err: fmt.Errorf("etcd: unknown command %q (get, put, del, members, status)", args[0])}
}

// Introspect: the connection lists the first path segments, a segment
// lists its keys (keys only, never values).
func (s *EtcdSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	db := req.Database
	if db == "" {
		db = req.Schema
	}
	if db == "" && req.Object == "" {
		kvs, _, err := s.rangeKeys(ctx, "", true, true, EtcdTreeMax)
		if err != nil {
			return out, err
		}
		seen := map[string]bool{}
		for _, kv := range kvs {
			seg := etcdSegment(unb64(kv.Key))
			if !seen[seg] {
				seen[seg] = true
				out.Databases = append(out.Databases, seg)
			}
		}
		sort.Strings(out.Databases)
		return out, nil
	}
	if req.Object == "" {
		kvs, _, err := s.rangeKeys(ctx, db, true, true, EtcdTreeMax)
		if err != nil {
			return out, err
		}
		for _, kv := range kvs {
			out.Objects = append(out.Objects, SchemaObject{Name: unb64(kv.Key), Kind: "key"})
		}
		return out, nil
	}
	out.Columns = []Column{{Name: "key", PK: true, Table: req.Object}, {Name: "value", Table: req.Object}, {Name: "version", Table: req.Object}, {Name: "mod_revision", Table: req.Object}, {Name: "lease", Table: req.Object}}
	return out, nil
}

// etcdSegment is the first path segment of a key ("/app/a" → "/app/"),
// or the whole key without a slash after the first byte.
func etcdSegment(k string) string {
	if i := strings.IndexByte(k[min(1, len(k)):], '/'); i >= 0 {
		return k[:i+min(1, len(k))+1]
	}
	return k
}

func (s *EtcdSession) Begin(context.Context) error    { return errNoTx("etcd") }
func (s *EtcdSession) Commit(context.Context) error   { return errNoTx("etcd") }
func (s *EtcdSession) Rollback(context.Context) error { return errNoTx("etcd") }

func (s *EtcdSession) ReadOnlySession() (Session, error) {
	return &EtcdSession{httpEngine: s.clone(), readOnly: true, token: s.token}, nil
}
