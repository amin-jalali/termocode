package dbclient

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

const tnsSample = `# sample
PROD, PROD.WORLD =
  (DESCRIPTION =
    (ADDRESS = (PROTOCOL = TCPS)(HOST = db.example.com)(PORT = 2484))
    (CONNECT_DATA = (SERVICE_NAME = prod.example.com))
  )
dev=(DESCRIPTION=(ADDRESS=(PROTOCOL=TCP)(HOST=localhost)(PORT=1521))(CONNECT_DATA=(SID=XE)))
broken = (DESCRIPTION = (ADDRESS = (HOST = x)
`

func TestParseTNSNames(t *testing.T) {
	es := ParseTNSNames(tnsSample)
	if len(es) != 3 {
		t.Fatalf("entries = %+v", es)
	}
	p := es[0]
	if p.Alias != "PROD" || p.Host != "db.example.com" || p.Port != 2484 || p.Service != "prod.example.com" || !p.TLS {
		t.Fatalf("prod = %+v", p)
	}
	if es[1].Alias != "PROD.WORLD" || es[1].Host != "db.example.com" {
		t.Fatalf("second alias = %+v", es[1])
	}
	d := es[2]
	if d.Alias != "DEV" || d.SID != "XE" || d.Port != 1521 || d.TLS {
		t.Fatalf("dev = %+v", d)
	}
	if _, ok := LookupTNS(es, "dev"); !ok {
		t.Fatal("lookup is not case-blind")
	}
}

func TestTNSNamesFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tnsnames.ora"), []byte(tnsSample), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TNS_ADMIN", dir)
	if got := TNSNamesPath(""); got != filepath.Join(dir, "tnsnames.ora") {
		t.Fatalf("path = %q", got)
	}
}

func TestOracleDSN(t *testing.T) {
	u, err := url.Parse(OracleDSN(Conn{Driver: "oracle", Host: "h", Port: 1521, User: "scott", Database: "ORCLPDB1"}, "tiger", nil))
	if err != nil || u.Scheme != "oracle" || u.Host != "h:1521" || u.Path != "/ORCLPDB1" || u.User.Username() != "scott" {
		t.Fatalf("dsn = %v %v", u, err)
	}
	es := ParseTNSNames(tnsSample)
	got := OracleDSN(Conn{Driver: "oracle", User: "app", Params: map[string]string{"tns": "prod", "wallet": "/w"}}, "", es)
	u, _ = url.Parse(got)
	if u.Host != "db.example.com:2484" || u.Path != "/prod.example.com" || u.Query().Get("SSL") != "true" || u.Query().Get("WALLET") != "/w" || u.Query().Has("tns") {
		t.Fatalf("tns dsn = %s", got)
	}
	got = OracleDSN(Conn{Driver: "oracle", Params: map[string]string{"tns": "dev"}}, "", es)
	if u, _ = url.Parse(got); u.Query().Get("SID") != "XE" {
		t.Fatalf("sid dsn = %s", got)
	}
}

func TestOracleWired(t *testing.T) {
	if normDriver("ora") != "oracle" || defaultPort("oracle") != 1521 {
		t.Fatal("oracle driver name/port")
	}
	if _, ok := DialectFor("oracle").(OracleDialect); !ok {
		t.Fatal("no Oracle dialect")
	}
	if err := DriverMissing("oracle"); err != nil {
		t.Fatalf("go-ora is built in now: %v", err)
	}
}
