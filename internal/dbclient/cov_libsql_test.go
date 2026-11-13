package dbclient

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLibSQLArgsAndCellsOfEveryType(t *testing.T) {
	when := time.Date(2024, 1, 2, 3, 4, 5, 6, time.UTC)
	for _, c := range []struct {
		in       any
		typ, val string
	}{
		{nil, "null", ""}, {true, "integer", `"1"`}, {false, "integer", `"0"`}, {7, "integer", `"7"`}, {int32(8), "integer", `"8"`},
		{int64(9), "integer", `"9"`}, {uint(10), "integer", `"10"`}, {uint64(11), "integer", `"11"`}, {float32(1.5), "float", "1.5"},
		{2.5, "float", "2.5"}, {math.NaN(), "null", ""}, {math.Inf(1), "null", ""}, {"s", "text", `"s"`},
		{when, "text", `"2024-01-02T03:04:05.000000006Z"`}, {struct{ A int }{1}, "text", `"{1}"`},
	} {
		got := libsqlArg(c.in)
		if got.Type != c.typ || (c.val != "" && string(got.Value) != c.val) {
			t.Errorf("libsqlArg(%v) = %+v, want %s %s", c.in, got, c.typ, c.val)
		}
	}
	if b := libsqlArg([]byte{1, 2}); b.Type != "blob" || b.Base64 != "AQI=" {
		t.Errorf("blob = %+v", b)
	}
	for _, c := range []struct {
		in   libsqlValue
		text string
		null bool
		kind string
	}{
		{libsqlValue{Type: "null"}, "", true, ""}, {libsqlValue{}, "", true, ""},
		{libsqlValue{Type: "blob", Base64: "AQI="}, "0x0102", false, "binary"},
		{libsqlValue{Type: "blob", Base64: "AQI"}, "0x0102", false, "binary"},
		{libsqlValue{Type: "text", Value: json.RawMessage(`"x"`)}, "x", false, ""},
		{libsqlValue{Type: "text", Value: json.RawMessage(`12`)}, "12", false, ""},
		{libsqlValue{Type: "float", Value: json.RawMessage(`2.50`)}, "2.5", false, ""},
		{libsqlValue{Type: "float", Value: json.RawMessage(`"nan"`)}, `"nan"`, false, ""},
		{libsqlValue{Type: "weird", Value: json.RawMessage(`1`)}, "1", false, ""},
	} {
		got := libsqlCell(c.in)
		if got.Text != c.text || got.Null != c.null || got.Kind != c.kind {
			t.Errorf("libsqlCell(%+v) = %+v", c.in, got)
		}
	}
}

func TestLibSQLBaseURLRules(t *testing.T) {
	for _, c := range []struct {
		conn Conn
		want string
		err  string
	}{
		{Conn{Host: "db.example", Port: 8080}, "https://db.example:8080", ""},
		{Conn{URL: "libsql://x.turso.io?tls=0"}, "http://x.turso.io", ""},
		{Conn{URL: "wss://x/y"}, "https://x/y", ""},
		{Conn{URL: "ws://x"}, "http://x", ""},
		{Conn{}, "", "no server URL"},
		{Conn{URL: "ftp://x"}, "", "unsupported URL scheme"},
		{Conn{URL: "https://"}, "", "no host"},
		{Conn{URL: "http://%zz"}, "", "bad URL"},
	} {
		got, err := libsqlHTTPBase(c.conn)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%+v: %v, want %q", c.conn, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%+v = %q, %v, want %q", c.conn, got, err, c.want)
		}
	}
	if _, err := NewLibSQLSession(Conn{}, ""); err == nil {
		t.Error("no URL: want error")
	}
	if _, err := NewLibSQLSession(Conn{URL: "https://h", Dial: func(context.Context, string, string) (net.Conn, error) { return nil, io.EOF }}, ""); err != nil {
		t.Error(err)
	}
}

func TestLibSQLServerFailures(t *testing.T) {
	mode := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		switch mode {
		case "500":
			w.WriteHeader(500)
			_, _ = w.Write([]byte(strings.Repeat("x", 300)))
		case "503empty":
			w.WriteHeader(503)
		case "notjson":
			_, _ = w.Write([]byte("<html>"))
		case "fewer":
			_, _ = w.Write([]byte(`{"baton":"","results":[]}`))
		case "ok":
			_, _ = w.Write([]byte(`{"baton":"b","base_url":"` + "http://" + r.Host + `/","results":[` + strings.TrimSuffix(strings.Repeat(`{"type":"ok","response":{"type":"execute","result":{"cols":[],"rows":[]}}},`, 3), ",") + `]}`))
		}
	}))
	defer srv.Close()
	s, _ := NewLibSQLSession(Conn{URL: srv.URL}, "tok")
	defer s.Close()
	ctx := context.Background()
	for m, want := range map[string]string{"500": "…", "503empty": "503", "notjson": "not a pipeline result", "fewer": "fewer results"} {
		mode = m
		r := s.Run(ctx, "SELECT 1", RunOptions{})
		if r.Err == nil || !strings.Contains(r.Err.Error(), want) {
			t.Errorf("%s = %v, want %q", m, r.Err, want)
		}
	}
	// A stream whose server names a new base URL keeps using it.
	mode = "ok"
	if err := s.Begin(ctx); err != nil {
		t.Fatalf("begin = %v", err)
	}
	if err := s.Begin(ctx); err == nil || !strings.Contains(err.Error(), "already open") {
		t.Errorf("second begin = %v", err)
	}
	s.Cancel()
	if err := s.Rollback(ctx); err != nil {
		t.Errorf("rollback = %v", err)
	}
	if err := s.Commit(ctx); err == nil || !strings.Contains(err.Error(), "no open transaction") {
		t.Errorf("commit without a transaction = %v", err)
	}
	ro, _ := s.ReadOnlySession()
	if err := ro.Begin(ctx); err != nil {
		t.Errorf("read-only begin = %v", err)
	}
	mode = "500"
	if err := s.Begin(ctx); err == nil {
		t.Error("a failing begin: want error")
	}
	srv.Close()
	if err := s.Begin(ctx); err == nil {
		t.Error("a server that is gone: want error")
	}
	if r := s.Run(ctx, "SELECT 1", RunOptions{}); r.Err == nil {
		t.Error("a server that is gone: want error")
	}
}
