package dbclient

import "testing"

func TestDialectLimitIsLimitThenOffset(t *testing.T) {
	for name, d := range map[string]Dialect{"postgres": PostgresDialect{}, "mysql": MySQLDialect{}, "sqlite": SQLiteDialect{}} {
		if got := d.Limit(100, 200); got != " LIMIT 100 OFFSET 200" {
			t.Errorf("%s Limit(100, 200) = %q", name, got)
		}
		if got := d.Limit(0, 5); got != "" {
			t.Errorf("%s Limit(0, 5) = %q, want empty", name, got)
		}
	}
}

func TestDialectForDriver(t *testing.T) {
	for driver, quoted := range map[string]string{"postgres": `"a""b"`, "postgresql": `"a""b"`, "mysql": "`a\"b`", "mariadb": "`a\"b`", "sqlite": `"a""b"`} {
		d := DialectFor(driver)
		if d == nil {
			t.Fatalf("DialectFor(%q) = nil", driver)
		}
		if got := d.QuoteIdent(`a"b`); got != quoted {
			t.Errorf("%s QuoteIdent = %q, want %q", driver, got, quoted)
		}
	}
	if DialectFor("redis") != nil {
		t.Fatal("redis has no SQL dialect")
	}
}
