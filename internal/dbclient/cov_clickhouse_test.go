package dbclient

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func chSession(t *testing.T, h http.HandlerFunc, mod func(*Conn)) *ClickHouseSession {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	c := Conn{Driver: "clickhouse", Host: host, Port: p, User: "u"}
	if mod != nil {
		mod(&c)
	}
	s, err := NewClickHouseSession(context.Background(), c, "pw")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestClickHouseOddResults(t *testing.T) {
	body := "SELECT 1"
	s := chSession(t, func(w http.ResponseWriter, r *http.Request) {
		q, _ := io.ReadAll(r.Body)
		if string(q) == "SELECT 1" {
			_, _ = io.WriteString(w, "[\"1\"]\n[\"UInt8\"]\n[1]\n")
			return
		}
		_, _ = io.WriteString(w, body)
	}, func(c *Conn) { c.Database = "db1" })
	ctx := context.Background()
	for name, c := range map[string]struct{ body, want string }{
		"bad names":  {"[1,\n", "could not read"},
		"bad types":  {"[\"a\"]\n[bad\n", "could not read"},
		"bad row":    {"[\"a\"]\n[\"UInt8\"]\n[1,\n", "could not read"},
		"error line": {"[\"a\"]\n[\"UInt8\"]\n[1]\nCode: 1. Boom\n", "Boom"},
	} {
		body = c.body
		if r := s.Run(ctx, "X", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), c.want) {
			t.Errorf("%s = %v, want %q", name, r.Err, c.want)
		}
	}
	// Blank lines are skipped, extra types are tolerated, and the rows are cut.
	body = "\n[\"a\",\"b\"]\n[\"UInt8\"]\n\n[1,{\"k\":[1, 2]}]\n[2,null]\n[3,\"x\"]\n"
	r := s.Run(ctx, "X", RunOptions{MaxRows: 2})
	if r.Err != nil || len(r.Rows) != 2 || !r.Truncated || r.Columns[1].Type != "" || r.Rows[0][1].Kind != "json" || !r.Rows[1][1].Null {
		t.Errorf("result = %+v", r)
	}
	for in, want := range map[string]string{`"x"`: "x", `12`: "12", `[1, 2]`: "[1,2]", `null`: "", `"bad`: `"bad`} {
		c := clickHouseCell([]byte(in))
		if (in == "null" && !c.Null) || (in != "null" && c.Text != want) {
			t.Errorf("clickHouseCell(%s) = %+v", in, c)
		}
	}
	s.Cancel()
	_ = s.Close()
	if r := s.Run(ctx, "X", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "closed") {
		t.Errorf("closed = %v", r.Err)
	}
	for _, f := range []func(context.Context) error{s.Begin, s.Commit, s.Rollback} {
		if f(ctx) == nil {
			t.Error("ClickHouse has no transactions")
		}
	}
}

func TestClickHouseStatusAndTransportFailures(t *testing.T) {
	status := 500
	s := chSession(t, func(w http.ResponseWriter, r *http.Request) {
		q, _ := io.ReadAll(r.Body)
		if string(q) == "SELECT 1" {
			_, _ = io.WriteString(w, "[\"1\"]\n[\"UInt8\"]\n[1]\n")
			return
		}
		w.WriteHeader(status)
	}, nil)
	if r := s.Run(context.Background(), "X", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "500") {
		t.Errorf("an empty error body gives the status = %v", r.Err)
	}
	ro, _ := s.ReadOnlySession()
	if ro == nil {
		t.Error("read-only session")
	}
	gone := chSession(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "[\"1\"]\n[\"UInt8\"]\n[1]\n")
	}, nil)
	gone.base.Host = "127.0.0.1:1"
	if r := gone.Run(context.Background(), "X", RunOptions{}); r.Err == nil {
		t.Error("a server that is gone: want error")
	}
}

func TestClickHouseIntrospectFailuresAndDefaults(t *testing.T) {
	mode := "ok"
	s := chSession(t, func(w http.ResponseWriter, r *http.Request) {
		q, _ := io.ReadAll(r.Body)
		qs := string(q)
		if mode == "fail" && qs != "SELECT 1" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, "denied")
			return
		}
		switch {
		case qs == "SELECT 1":
			_, _ = io.WriteString(w, "[\"1\"]\n[\"UInt8\"]\n[1]\n")
		case qs == "SELECT currentDatabase()":
			if mode == "nodb" {
				_, _ = io.WriteString(w, "[\"c\"]\n[\"String\"]\n")
			} else {
				_, _ = io.WriteString(w, "[\"c\"]\n[\"String\"]\n[\"cur\"]\n")
			}
		case strings.Contains(qs, "system.tables"):
			_, _ = io.WriteString(w, "[\"name\",\"kind\"]\n[\"String\",\"String\"]\n[\"t\",\"BASE TABLE\"]\n[\"short\"]\n")
		case strings.Contains(qs, "system.columns"):
			_, _ = io.WriteString(w, "[\"name\",\"type\",\"pk\"]\n[\"String\",\"String\",\"UInt8\"]\n[\"id\",\"UInt64\",1]\n[\"short\"]\n")
		case strings.HasPrefix(qs, "SHOW CREATE"):
			_, _ = io.WriteString(w, "[\"s\"]\n[\"String\"]\n[\"CREATE TABLE x\"]\n")
		default:
			_, _ = io.WriteString(w, "[\"n\"]\n[\"String\"]\n[\"a\"]\n[null]\n")
		}
	}, nil)
	ctx := context.Background()
	objs, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || len(objs.Databases) != 1 {
		t.Errorf("databases = %+v %v", objs, err)
	}
	cols, err := s.Introspect(ctx, IntrospectionRequest{Object: "t"})
	if err != nil || len(cols.Columns) != 1 || !cols.Columns[0].PK || cols.DDL == "" {
		t.Errorf("columns = %+v %v", cols, err)
	}
	if top, err := s.Introspect(ctx, IntrospectionRequest{Database: "x", Object: ""}); err != nil || len(top.Objects) != 1 {
		t.Errorf("objects = %+v %v", top, err)
	}
	mode = "nodb"
	if _, err := s.Introspect(ctx, IntrospectionRequest{Object: "t"}); err != nil {
		t.Errorf("no current database = %v", err)
	}
	mode = "fail"
	for name, req := range map[string]IntrospectionRequest{"cur": {Object: "t"}, "objects": {Schema: "s"}, "columns": {Schema: "s", Object: "t"}} {
		if _, err := s.Introspect(ctx, req); err == nil {
			t.Errorf("%s failing: want error", name)
		}
	}
	var dst []string
	if err := s.readList(ctx, "SELECT 2", &dst); err == nil {
		t.Error("readList of a failing query: want error")
	}
}
