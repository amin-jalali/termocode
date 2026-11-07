package dbclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeEtcd is a v3 JSON gateway over an in-memory map.
func fakeEtcd(t *testing.T, kv map[string]string, paths *[]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.URL.Path)
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		str := func(k string) string { s, _ := req[k].(string); return unb64(s) }
		switch r.URL.Path {
		case "/v3/auth/authenticate":
			if req["name"] != "root" || req["password"] != "pw-1" {
				w.WriteHeader(401)
				_, _ = w.Write([]byte(`{"error":"etcdserver: authentication failed, invalid user ID or password","code":3}`))
				return
			}
			_, _ = w.Write([]byte(`{"token":"tok"}`))
		case "/v3/maintenance/status":
			_, _ = w.Write([]byte(`{"version":"3.5.9","dbSize":"20480"}`))
		case "/v3/kv/range":
			key, end := str("key"), str("range_end")
			var kvs []map[string]any
			for k, v := range kv {
				if k == key || (end != "" && k >= key && (end == "\x00" || k < end)) {
					e := map[string]any{"key": b64(k), "version": "1", "mod_revision": "7"}
					if req["keys_only"] != true {
						e["value"] = b64(v)
					}
					kvs = append(kvs, e)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"kvs": kvs})
		case "/v3/kv/put":
			kv[str("key")] = str("value")
			_, _ = w.Write([]byte(`{}`))
		case "/v3/kv/deleterange":
			n := 0
			key, end := str("key"), str("range_end")
			for k := range kv {
				if k == key || (end != "" && k >= key && k < end) {
					delete(kv, k)
					n++
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"deleted": strings.Repeat("1", min(n, 1))})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestEtcdSessionConsoleAndTree(t *testing.T) {
	kv := map[string]string{"/app/a": "1", "/app/b": "2", "/db/x": "3"}
	var paths []string
	srv := fakeEtcd(t, kv, &paths)
	defer srv.Close()
	s, err := NewEtcdSession(Conn{Driver: "etcd", URL: srv.URL, User: "root"}, "pw-1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil || paths[0] != "/v3/auth/authenticate" {
		t.Fatalf("ping %v paths %v", err, paths)
	}
	res := s.Run(ctx, "get /app/ --prefix", RunOptions{})
	if res.Err != nil || len(res.Rows) != 2 {
		t.Fatalf("get = %+v", res)
	}
	if w := s.Run(ctx, `put /app/c "hello world"`, RunOptions{}); w.Err != nil || kv["/app/c"] != "hello world" {
		t.Fatalf("put = %+v %v", w, kv)
	}
	in, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(in.Databases, ",") != "/app/,/db/" {
		t.Fatalf("segments = %v %v", in.Databases, err)
	}
	in, err = s.Introspect(ctx, IntrospectionRequest{Database: "/app/"})
	if err != nil || len(in.Objects) != 3 {
		t.Fatalf("keys = %+v %v", in.Objects, err)
	}
	if d := s.Run(ctx, "del /app/a", RunOptions{}); d.Err != nil || d.Affected != 1 {
		t.Fatalf("del = %+v", d)
	}
	ro, _ := s.ReadOnlySession()
	if r := ro.Run(ctx, "put k v", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "read-only") {
		t.Fatalf("read-only put = %+v", r)
	}
}

func TestEtcdWrongPasswordNeverShowsIt(t *testing.T) {
	var paths []string
	srv := fakeEtcd(t, map[string]string{}, &paths)
	defer srv.Close()
	s, _ := NewEtcdSession(Conn{Driver: "etcd", URL: srv.URL, User: "root"}, "secret-pw")
	err := s.Ping(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret-pw") || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestClassifyEtcdAndPrefixEnd(t *testing.T) {
	for in, want := range map[string]Kind{"get k": KindRead, "members": KindRead, "put k v": KindWrite, "del k --prefix": KindDDL, "compact 5": KindUnknown, "": KindUnknown} {
		if got := ClassifyEtcd(in); got != want {
			t.Errorf("%q = %v, want %v", in, got, want)
		}
	}
	if etcdPrefixEnd("/app/") != "/app0" || etcdPrefixEnd("a\xff") != "b" {
		t.Fatal("prefix end")
	}
	if k, _ := Classify("put k v", "etcd"); k != KindWrite {
		t.Fatalf("Classify uses the engine: %v", k)
	}
}
