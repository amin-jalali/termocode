package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (web parity, Databases area): the rest of the Databases view from
// the page. Reads (a table's DDL, a plan, a connection test, a schema
// compare, the SQL history, the workspace's CSV files, the Redis keys)
// run in read-only sessions; cancel stops the page's own reads; connect
// starts the laptop's own connect (a password is typed there); the
// config op names the connections file. Every change to a database or
// to a saved connection (row edits, a CSV import, a Redis key's delete
// or TTL, add, SSH tunnel, SSL, disconnect, commit, roll back and the
// transaction mode) waits for a laptop card. No arg carries a password
// or a URL.
func TestDBMoreReadOps(t *testing.T) {
	checkArgs(t, OpDBDDL, []string{`{"conn":"local"}`, `{"conn":"","table":"users"}`}, []string{`{"conn":"local","table":"users"}`})
	checkRow(t, OpDBDDL, RoleViewer, false, AuditNo)
	checkArgs(t, OpDBExplain, []string{`{"conn":"local","sql":""}`, `{"conn":"local","sql":"` + strings.Repeat("x", MaxDBSQL+1) + `"}`},
		[]string{`{"conn":"local","sql":"SELECT 1"}`, `{"conn":"local","sql":"SELECT 1","analyze":true}`})
	checkRow(t, OpDBExplain, RoleController, false, AuditYes)
	checkArgs(t, OpDBTest, []string{`{}`}, []string{`{"conn":"local"}`})
	checkRow(t, OpDBTest, RoleController, false, AuditYes)
	checkArgs(t, OpDBCompare, []string{`{"conn":"local"}`, `{"conn":"local","target":"local"}`}, []string{`{"conn":"local","target":"prod"}`})
	checkRow(t, OpDBCompare, RoleViewer, false, AuditNo)
	checkArgs(t, OpDBHistory, []string{`{"conn":"\u0007"}`}, []string{`{}`, `{"conn":"local"}`})
	checkRow(t, OpDBHistory, RoleController, false, AuditNo)
	checkArgs(t, OpDBCSVFiles, nil, []string{`{}`})
	checkRow(t, OpDBCSVFiles, RoleController, false, AuditNo)
	checkArgs(t, OpDBCancel, []string{`{}`}, []string{`{"conn":"local"}`})
	checkRow(t, OpDBCancel, RoleController, false, AuditNo)
	checkArgs(t, OpDBConnect, []string{`{}`}, []string{`{"conn":"local"}`})
	checkRow(t, OpDBConnect, RoleController, false, AuditYes)
	checkArgs(t, OpDBConfig, nil, []string{`{}`})
	checkRow(t, OpDBConfig, RoleController, false, AuditYes)
	checkArgs(t, OpRedisKeys, []string{`{"conn":"cache","db":-1}`, `{"conn":"cache","db":16}`, `{"conn":"cache","pattern":"` + strings.Repeat("a", MaxRedisPattern+1) + `"}`},
		[]string{`{"conn":"cache"}`, `{"conn":"cache","db":3,"pattern":"session:*"}`})
	checkRow(t, OpRedisKeys, RoleViewer, false, AuditNo)
}

func TestDBMoreLaptopOps(t *testing.T) {
	checkArgs(t, OpDBEdit,
		[]string{`{"conn":"local","table":"users","edits":[]}`, `{"conn":"local","table":"users","edits":[{"kind":"merge"}]}`,
			`{"conn":"local","table":"users","edits":[{"kind":"update","set":[{"col":"email","v":"a"}]}]}`,
			`{"conn":"local","table":"users","edits":[{"kind":"delete"}]}`,
			`{"conn":"local","table":"users","edits":[{"kind":"insert"}]}`,
			`{"conn":"local","table":"users","edits":[{"kind":"insert","set":[{"col":"","v":"a"}]}]}`},
		[]string{`{"conn":"local","table":"users","edits":[{"kind":"update","key":[{"col":"id","v":"7"}],"set":[{"col":"email","v":"a@b"}],"old":[{"col":"email","v":"x@y"}]}]}`,
			`{"conn":"local","table":"users","edits":[{"kind":"delete","key":[{"col":"id","v":"7"}]},{"kind":"insert","set":[{"col":"email","n":true}]}]}`})
	checkRow(t, OpDBEdit, RoleController, true, AuditYes)
	checkArgs(t, OpDBImport, []string{`{"conn":"local","table":"users"}`, `{"conn":"local","table":"users","path":"../x.csv"}`, `{"conn":"local","table":"users","path":"/etc/x.csv"}`},
		[]string{`{"conn":"local","table":"users","path":"data/users.csv"}`, `{"conn":"local","table":"users","path":"u.csv","noheader":true}`})
	checkRow(t, OpDBImport, RoleController, true, AuditYes)
	checkArgs(t, OpRedisKey, []string{`{"conn":"cache","key":"k","verb":"flush"}`, `{"conn":"cache","key":"","verb":"delete"}`, `{"conn":"cache","key":"k","verb":"ttl","ttl":-5}`},
		[]string{`{"conn":"cache","key":"session:ab12","verb":"delete"}`, `{"conn":"cache","db":2,"key":"k","verb":"ttl","ttl":3600}`, `{"conn":"cache","key":"k","verb":"ttl","ttl":0}`})
	checkRow(t, OpRedisKey, RoleController, true, AuditYes)
	checkArgs(t, OpDBAdd, []string{`{"id":"x","driver":"oracle"}`, `{"id":"","driver":"postgres","host":"h"}`, `{"id":"a b","driver":"postgres","host":"h"}`,
		`{"id":"pg","driver":"postgres","host":"h","port":70000}`, `{"id":"lite","driver":"sqlite"}`, `{"id":"lite","driver":"sqlite","path":"../a.db"}`, `{"id":"ring","driver":"cassandra","host":"h","db":1}`},
		[]string{`{"id":"pg","name":"App","driver":"postgres","host":"localhost","port":5432,"user":"app","database":"app","env":"development"}`,
			`{"id":"lite","driver":"sqlite","path":"data/app.db","readonly":true}`, `{"id":"cache","driver":"redis","host":"localhost","db":2}`,
			`{"id":"ring","driver":"cassandra","host":"localhost","port":9042,"database":"shop"}`})
	checkRow(t, OpDBAdd, RoleController, true, AuditYes)
	checkArgs(t, OpDBSSH, []string{`{"conn":"local","host":""}`, `{"conn":"local","host":"bastion","auth":"magic"}`, `{"conn":"local","host":"-oProxy","auth":"agent"}`},
		[]string{`{"conn":"local","host":"bastion","user":"me","port":22,"auth":"agent"}`, `{"conn":"local","host":"bastion","auth":"key","keyfile":"~/.ssh/id_ed25519","jump":["jump.example"]}`, `{"conn":"local","remove":true}`})
	checkRow(t, OpDBSSH, RoleController, true, AuditYes)
	checkArgs(t, OpDBSSL, []string{`{"conn":"local","mode":"maybe"}`, `{"conn":"local"}`}, []string{`{"conn":"local","mode":"verify-full","ca":"certs/ca.pem"}`, `{"conn":"local","mode":"disable"}`})
	checkRow(t, OpDBSSL, RoleController, true, AuditYes)
	checkArgs(t, OpDBDisconnect, []string{`{}`}, []string{`{"conn":"local"}`})
	checkRow(t, OpDBDisconnect, RoleController, true, AuditYes)
	checkArgs(t, OpDBTx, []string{`{"conn":"local","verb":"savepoint"}`, `{"conn":"local"}`},
		[]string{`{"conn":"local","verb":"commit"}`, `{"conn":"local","verb":"rollback"}`, `{"conn":"local","verb":"manual"}`, `{"conn":"local","verb":"auto"}`})
	checkRow(t, OpDBTx, RoleController, true, AuditYes)
	// No arg carries a password or a URL: such args are refused.
	for _, c := range [][2]string{{OpDBAdd, `{"id":"pg","driver":"postgres","host":"h","password":"x"}`}, {OpDBAdd, `{"id":"pg","driver":"postgres","url":"postgres://u:p@h/db"}`},
		{OpDBSSH, `{"conn":"local","host":"bastion","password":"x"}`}, {OpDBSSH, `{"conn":"local","host":"bastion","passphrase":"x"}`}} {
		if _, err := DecodeArgs(c[0], json.RawMessage(c[1])); err == nil {
			t.Errorf("%s %s was taken", c[0], c[1])
		}
	}
}

// Spec (web DB export): db.export_parquet turns the rows the page shows
// into a Parquet file on the laptop; it reads nothing from a database, so
// it is a viewer's read with no card. Every row has as many cells as there
// are columns (null is NULL).
func TestDBExportParquetOp(t *testing.T) {
	checkArgs(t, OpDBExportParquet,
		[]string{`{}`, `{"columns":[],"rows":[]}`, `{"columns":["a"],"rows":[["x","y"]]}`, `{"columns":["a\u0007"],"rows":[]}`, `{"columns":["a"]}`},
		[]string{`{"columns":["a","b"],"rows":[["1",null],["x","y"]]}`, `{"columns":["a"],"rows":[]}`})
	checkRow(t, OpDBExportParquet, RoleViewer, false, AuditNo)
	if p, _ := Lookup(OpDBExportParquet); p.MaxBytes < 2*mib {
		t.Errorf("max bytes %d: one answer's rows (%d B of text) must fit", p.MaxBytes, MaxDBBytes)
	}
}
