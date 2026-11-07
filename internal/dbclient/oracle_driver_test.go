package dbclient

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// DBX-63: go-ora is built in, so an Oracle connection opens a pool.

func TestOracleOpensAPool(t *testing.T) {
	db, release, err := openSQLDB(Conn{Driver: "oracle", Host: "127.0.0.1", Port: 1, Database: "FREEPDB1", User: "app"}, "pw")
	if err != nil || db == nil {
		t.Fatalf("open = %v", err)
	}
	_ = db.Close()
	release()
}

func TestOracleTNSAliasFromWallet(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tnsnames.ora"), []byte(tnsSample), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TNS_ADMIN", "")
	t.Setenv("ORACLE_HOME", "")
	c := Conn{Driver: "oracle", User: "app", Params: map[string]string{"tns": "prod", "wallet": dir}}
	dsn, err := oracleConnString(c, "pw")
	if err != nil || !strings.Contains(dsn, "db.example.com:2484") || !strings.Contains(dsn, "WALLET=") {
		t.Fatalf("dsn = %q, %v", dsn, err)
	}
	c.Params["tns"] = "nope"
	if _, err := oracleConnString(c, "pw"); err == nil || !strings.Contains(err.Error(), "NOPE") {
		t.Fatalf("unknown alias = %v", err)
	}
	c.Params = map[string]string{"tns": "prod"}
	if _, err := oracleConnString(c, "pw"); err == nil || !strings.Contains(err.Error(), "tnsnames.ora") {
		t.Fatalf("no tnsnames.ora = %v", err)
	}
}

func TestOracleConfigIsSupported(t *testing.T) {
	cs, err := ParseConfig([]byte(`{"connections":[{"id":"ora","driver":"oracle","host":"h","database":"FREEPDB1"}]}`), SourceUser, "u", t.TempDir())
	if err != nil || len(cs) != 1 || cs[0].Problem != "" || cs[0].Port != 1521 {
		t.Fatalf("parse = %v %+v", err, cs)
	}
	if err := Validate(cs[0]); err != nil {
		t.Fatalf("validate = %v", err)
	}
}

func TestOracleIntrospectHelpers(t *testing.T) {
	if q := currentSchemaQuery("oracle"); !strings.Contains(q, "CURRENT_SCHEMA") || !strings.Contains(q, "dual") {
		t.Errorf("current schema = %s", q)
	}
	if currentSchemaQuery("mysql") != "SELECT DATABASE()" || currentSchemaQuery("postgres") != "SELECT current_schema()" {
		t.Error("pg/mysql current schema changed")
	}
	for q, want := range map[string]bool{"WHERE owner = :1": true, "WHERE s=$1": true, "WHERE s=?": true, "WHERE s = @p1": true, "SELECT 1": false, "": false} {
		if catalogTakesSchema(q) != want {
			t.Errorf("catalogTakesSchema(%q) != %v", q, want)
		}
	}
	q := OracleDialect{}.CatalogQueries()
	if !strings.Contains(q["databases"], "DB_NAME") || !strings.Contains(q["triggers"], ":1") || !strings.Contains(q["packages"], "'PACKAGE'") {
		t.Errorf("catalog = %v", q)
	}
	if got := splitIndexColumns("A\x1fB C", OracleDialect{}); !reflect.DeepEqual(got, []string{"A", "B C"}) {
		t.Errorf("index columns = %v", got)
	}
}

func TestOracleObjectDDL(t *testing.T) {
	q, err := ObjectDDLQuery("oracle", ObjectFunction, "HR", "PAY_PKG")
	if err != nil || !strings.Contains(q.SQL, "DBMS_METADATA.GET_DDL") || !strings.Contains(q.SQL, "'PACKAGE'") || !strings.Contains(q.SQL, "'PROCEDURE'") {
		t.Fatalf("routine ddl = %+v %v", q, err)
	}
	if !reflect.DeepEqual(q.Args, []any{"PAY_PKG", "HR"}) || strings.Contains(q.SQL, "PAY_PKG") {
		t.Fatalf("names must be bound: %+v", q)
	}
	q, err = ObjectDDLQuery("oracle", ObjectSequence, "", "S1")
	if err != nil || !strings.Contains(q.SQL, "'SEQUENCE'") || !strings.Contains(q.SQL, "CURRENT_SCHEMA") || !reflect.DeepEqual(q.Args, []any{"S1"}) {
		t.Fatalf("sequence ddl = %+v %v", q, err)
	}
	if _, err := ObjectDDLQuery("oracle", ObjectTrigger, "HR", "T1"); err != nil {
		t.Fatal(err)
	}
}
