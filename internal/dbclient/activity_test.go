package dbclient

import (
	"strings"
	"testing"
)

// DBX-17: the sessions and server info queries are plain reads (they run
// without a card, in a read-only session); stopping a session is not.
func TestSessionsSQLIsARead(t *testing.T) {
	for _, d := range []string{"postgres", "mysql"} {
		for name, f := range map[string]func(string) (string, bool){"sessions": SessionsSQL, "server info": ServerInfoSQL} {
			sql, ok := f(d)
			if !ok || sql == "" {
				t.Fatalf("%s %s: no query", d, name)
			}
			if strings.Contains(sql, "\n") {
				t.Errorf("%s %s: the console statement must be one line: %q", d, name, sql)
			}
			if k, st := Classify(sql, d); k != KindRead || len(st) != 1 {
				t.Errorf("%s %s: Classify = %s (%d statements), want one read: %s", d, name, k, len(st), sql)
			}
		}
	}
	if sql, _ := SessionsSQL("postgres"); !strings.Contains(sql, "pg_stat_activity") || !strings.Contains(sql, "pg_blocking_pids") {
		t.Errorf("postgres sessions: %s", sql)
	}
	if sql, _ := SessionsSQL("mysql"); !strings.Contains(sql, "PROCESSLIST") {
		t.Errorf("mysql sessions: %s", sql)
	}
	if _, ok := SessionsSQL("sqlite"); ok {
		t.Error("sqlite has no sessions")
	}
	if sql, ok := ServerInfoSQL("sqlite"); !ok || !strings.Contains(sql, "sqlite_version()") {
		t.Errorf("sqlite server info: %q %v", sql, ok)
	}
	if _, ok := SessionsSQL("redis"); ok {
		t.Error("redis has no SQL sessions")
	}
}

func TestStopSessionSQL(t *testing.T) {
	for _, c := range []struct {
		driver string
		end    bool
		want   string
	}{
		{"postgres", false, "SELECT pg_cancel_backend(42);"},
		{"postgres", true, "SELECT pg_terminate_backend(42);"},
		{"mysql", false, "KILL QUERY 42;"},
		{"mysql", true, "KILL 42;"},
	} {
		got, err := StopSessionSQL(c.driver, 42, c.end)
		if err != nil || got != c.want {
			t.Errorf("StopSessionSQL(%s, 42, %v) = %q, %v; want %q", c.driver, c.end, got, err, c.want)
		}
		// Never a plain read: the run card always asks first.
		if k, _ := Classify(got, c.driver); k == KindRead {
			t.Errorf("%s classifies as a read", got)
		}
	}
	for _, c := range []struct {
		driver string
		id     int64
	}{{"postgres", 0}, {"postgres", -1}, {"sqlite", 1}, {"redis", 1}} {
		if _, err := StopSessionSQL(c.driver, c.id, false); err == nil {
			t.Errorf("StopSessionSQL(%s, %d) = nil error", c.driver, c.id)
		}
	}
}

func TestParseSessionID(t *testing.T) {
	for in, want := range map[string]int64{"42": 42, " 7 ": 7, "#9": 9} {
		if got, err := ParseSessionID(in); err != nil || got != want {
			t.Errorf("ParseSessionID(%q) = %d, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "0", "-3", "1; DROP TABLE x", "abc", "99999999999999999999"} {
		if _, err := ParseSessionID(in); err == nil {
			t.Errorf("ParseSessionID(%q) = nil error", in)
		}
	}
}
