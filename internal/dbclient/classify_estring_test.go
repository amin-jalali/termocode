package dbclient

import "testing"

func TestPostgresEStringBackslashQuoteDoesNotHideCode(t *testing.T) {
	for _, q := range []string{
		`SELECT * FROM t WHERE (x = E'\'' or lo_export(16400,'/tmp/pwn') > 0 or x = E'\'')`,
		`SELECT * FROM t WHERE (x = e'\'' or lo_export(16400,'/tmp/pwn') > 0 or x = e'\'')`,
		`SELECT * FROM t WHERE (x = E'\'' UNION SELECT setval('s', 1) --)`,
		`SELECT * FROM t WHERE (x = E'\'' UNION SELECT pg_sleep(100) WHERE E'\'' = '')`,
	} {
		if k, _ := Classify(q, "postgres"); k == KindRead {
			t.Errorf("classified as a plain read: %s", q)
		}
	}
}

func TestPostgresPlainStringContentIsIgnored(t *testing.T) {
	if k, _ := Classify(`SELECT 'lo_export DELETE; x' AS a, E'it\'s ok' AS b`, "postgres"); k != KindRead {
		t.Errorf("words inside strings changed the class: %v", k)
	}
}

func TestPostgresSplitKnowsEStrings(t *testing.T) {
	got, err := Split(`SELECT E'\';' ; SELECT 2`, "postgres")
	if err != nil || len(got) != 2 {
		t.Fatalf("Split = %q, %v", got, err)
	}
	if got, _ = Split(`SELECT E'a\'; DROP TABLE t; --'`, "postgres"); len(got) != 1 {
		t.Fatalf("a ; inside an E string split the statement: %q", got)
	}
}

func TestPostgresNotifyAndStatsResetAreNotReads(t *testing.T) {
	for _, q := range []string{`SELECT pg_notify('c','x')`, `SELECT pg_stat_reset()`, `SELECT pg_stat_reset_shared('bgwriter')`, `SELECT pg_stat_statements_reset()`} {
		if k, _ := Classify(q, "postgres"); k == KindRead {
			t.Errorf("%s classified as a read", q)
		}
	}
}
