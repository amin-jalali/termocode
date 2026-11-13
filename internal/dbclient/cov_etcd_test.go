package dbclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func etcdWithMembers(t *testing.T) (*EtcdSession, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch r.URL.Path {
		case "/v3/maintenance/status":
			_, _ = w.Write([]byte(`{"version":"3.5.9","dbSize":"20480","leader":"1"}`))
		case "/v3/cluster/member/list":
			_, _ = w.Write([]byte(`{"members":[{"ID":"a1","name":"n1","peerURLs":["http://p1","http://p2"],"clientURLs":["http://c1"]}]}`))
		case "/v3/kv/range":
			if req["limit"] == float64(2) {
				_, _ = w.Write([]byte(`{"kvs":[{"key":"` + b64("/a") + `"},{"key":"` + b64("/b") + `"}],"more":true}`))
				return
			}
			_, _ = w.Write([]byte(`{"kvs":[{"key":"` + b64("k") + `","value":"` + b64("v") + `"}]}`))
		case "/v3/kv/deleterange":
			_, _ = w.Write([]byte(`{"deleted":"3"}`))
		default:
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		}
	}))
	t.Cleanup(srv.Close)
	s, err := NewEtcdSession(Conn{Driver: "etcd", URL: srv.URL}, "")
	if err != nil {
		t.Fatal(err)
	}
	return s, srv
}

func TestEtcdConsoleOptionsAndRefusals(t *testing.T) {
	s, _ := etcdWithMembers(t)
	ctx := context.Background()
	for line, why := range map[string]string{
		"":                   "write get",
		`get "unterminated`:  "write get",
		"get":                "get KEY",
		"get a b":            "get KEY",
		"get a --limit":      "get KEY",
		"get a --limit soon": "--limit wants a number",
		"get a --limit 0":    "--limit wants a number",
		"put k":              "put KEY VALUE",
		"put k v extra":      "put KEY VALUE",
		"del":                "del KEY",
		"del a b":            "del KEY",
		"frobnicate":         "unknown command",
	} {
		if r := s.Run(ctx, line, RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), why) {
			t.Errorf("%q = %v, want %q", line, r.Err, why)
		}
	}
	// --limit reaches the server and a cut listing says so.
	r := s.Run(ctx, "get /a --prefix --limit 2", RunOptions{})
	if r.Err != nil || len(r.Rows) != 2 || !r.Truncated {
		t.Errorf("limited get = %+v", r)
	}
	if r := s.Run(ctx, "del /a --prefix", RunOptions{}); r.Err != nil || r.Affected != 3 {
		t.Errorf("del prefix = %+v", r)
	}
	if r := s.Run(ctx, "delete k", RunOptions{}); r.Err != nil {
		t.Errorf("delete = %+v", r)
	}
	m := s.Run(ctx, "members", RunOptions{})
	if m.Err != nil || len(m.Rows) != 1 || m.Rows[0][2].Text != "http://p1,http://p2" {
		t.Errorf("members = %+v", m)
	}
	st := s.Run(ctx, "status", RunOptions{})
	if st.Err != nil || len(st.Rows) != 3 || st.Rows[0][0].Text != "dbSize" {
		t.Errorf("status = %+v", st)
	}
	if ClassifyEtcd("") != KindUnknown || ClassifyEtcd("members") != KindRead || ClassifyEtcd("put a b") != KindWrite || ClassifyEtcd("delete a") != KindDDL || ClassifyEtcd("zzz") != KindUnknown {
		t.Error("ClassifyEtcd")
	}
	for _, f := range []func(context.Context) error{s.Begin, s.Commit, s.Rollback} {
		if f(ctx) == nil {
			t.Error("etcd has no transactions")
		}
	}
}

func TestEtcdFailuresAreReportedPerCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":"etcdserver: no leader"}`))
	}))
	defer srv.Close()
	s, _ := NewEtcdSession(Conn{Driver: "etcd", URL: srv.URL}, "")
	ctx := context.Background()
	for _, line := range []string{"get a", "put a b", "del a", "members", "status"} {
		if r := s.Run(ctx, line, RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "no leader") {
			t.Errorf("%s = %v", line, r.Err)
		}
	}
	for name, req := range map[string]IntrospectionRequest{"top": {}, "keys": {Database: "/a/"}} {
		if _, err := s.Introspect(ctx, req); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if got := etcdSegment("plain"); got != "plain" {
		t.Errorf("etcdSegment(plain) = %q", got)
	}
	if got := etcdSegment("/a/b"); got != "/a/" {
		t.Errorf("etcdSegment(/a/b) = %q", got)
	}
	if etcdPrefixEnd("\xff\xff") != "\x00" {
		t.Error("a prefix of 0xff bytes has no end: the whole rest")
	}
	if unb64("not base64!") != "not base64!" {
		t.Error("text that is no base64 stays text")
	}
}
