package dbclient

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func realSQLite(t *testing.T) *SQLiteSession {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is not installed")
	}
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "t.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// Bound values keep their type: numbers stay numbers, booleans are 1/0,
// bytes are a BLOB, text with quotes and unicode arrives unchanged.
func TestSQLiteBoundArgumentTypes(t *testing.T) {
	s := realSQLite(t)
	r := s.Run(context.Background(), `SELECT typeof(?1) AS t1, ?1 AS v1, ?2 AS v2, typeof(?3) AS t3, hex(?3) AS v3, ?4 AS v4, ?5 AS v5, ?6 IS NULL AS v6, ?7 AS v7`,
		RunOptions{Args: []any{int64(-42), true, []byte{0x41, 0xff}, 2.5, "it's 'ü' 🙂", nil, uint8(7)}})
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	got := map[string]string{}
	for i, c := range r.Columns {
		got[c.Name] = r.Rows[0][i].Text
	}
	want := map[string]string{"t1": "integer", "v1": "-42", "v2": "1", "t3": "blob", "v3": "41FF", "v4": "2.5", "v5": "it's 'ü' 🙂", "v6": "1", "v7": "7"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if b := sqliteBindLiteral(false); b != "0" {
		t.Errorf("false = %q", b)
	}
}

// Several statements at once, or an unfinished one, are refused before
// anything is sent.
func TestSQLiteRunRefusesScriptsAndBrokenInput(t *testing.T) {
	s := realSQLite(t)
	if r := s.Run(context.Background(), "SELECT 1; SELECT 2", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "one SQL statement at a time") {
		t.Errorf("two statements = %v", r.Err)
	}
	if r := s.Run(context.Background(), "SELECT 'open", RunOptions{}); r.Err == nil {
		t.Error("unterminated string was accepted")
	}
	if r := s.Run(context.Background(), "SELEC 1", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "syntax error") {
		t.Errorf("syntax error = %v", r.Err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close = %v", err)
	}
}

// A sqlite3 that cannot tell its version is not used.
func TestSQLiteBrokenCLIVersion(t *testing.T) {
	fakeHost(t, "plan9", "")
	for body, want := range map[string]string{
		"#!/bin/sh\nexit 3\n":                      "could not check sqlite3 version",
		"#!/bin/sh\necho ''\n":                     "could not read sqlite3 version",
		"#!/bin/sh\necho 'SQLite version three'\n": "could not read sqlite3 version",
		"#!/bin/sh\necho '3.x.1 2024'\n":           "could not read sqlite3 version",
		"#!/bin/sh\necho '3.40.1.2 2024'\n":        "could not read sqlite3 version",
	} {
		bin := writeFakeSQLite(t, body)
		old := lookupSQLite
		lookupSQLite = func() (string, error) { return bin, nil }
		_, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"})
		lookupSQLite = old
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", body, err, want)
		}
		if want != "could not check sqlite3 version" && !strings.Contains(err.Error(), "your package manager") {
			t.Errorf("%q: no install hint in %v", body, err)
		}
	}
}

func TestSQLiteInstallHintOtherSystems(t *testing.T) {
	for _, tc := range []struct{ goos, release string }{{"linux", "ID=arch\n"}, {"freebsd", ""}, {"windows", ""}} {
		if got := SQLiteInstallHintWithRelease(tc.goos, "amd64", tc.release); got != "Install sqlite3 with your package manager" {
			t.Errorf("%s = %q", tc.goos, got)
		}
	}
	// The real host lookup always gives some hint.
	if got := sqliteHostInstallHint(); !strings.HasPrefix(got, "Install sqlite3 with") {
		t.Errorf("host hint = %q", got)
	}
}

// Output that is not the JSON sqlite3 writes is an error, never rows.
func TestParseSQLiteJSONRejectsOddOutput(t *testing.T) {
	for _, lines := range [][]string{
		{`{"a":1}`},
		{`[1,2]`},
		{`[{"a":1`},
		{`[{"a":tru}]`},
	} {
		if rows, _, err := parseSQLiteJSON(lines); err == nil {
			t.Errorf("%q parsed as %v", lines, rows)
		}
	}
}
