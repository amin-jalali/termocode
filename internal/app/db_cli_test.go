package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-45: $ on a connection opens its command line client (psql, mysql,
// sqlite3, redis-cli) in a terminal tab; the password goes in the
// environment, never on the command line.

func TestDBCLIJobKeepsPasswordOutOfTheCommand(t *testing.T) {
	c := dbclient.Conn{ID: "local", Name: "Local PG", Driver: "postgres", Host: "db.example", Port: 5433, User: "ann", Database: "shop"}
	j, err := dbCLIJob(c, "s3cret'pw", "/work")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(j.Cmdline, "s3cret") {
		t.Errorf("password on the command line: %s", j.Cmdline)
	}
	if !strings.HasPrefix(j.Cmdline, "'psql' ") || !strings.Contains(j.Cmdline, "'-h' 'db.example'") || !strings.Contains(j.Cmdline, "'-d' 'shop'") {
		t.Errorf("cmdline: %s", j.Cmdline)
	}
	if j.Env["PGPASSWORD"] != "s3cret'pw" {
		t.Errorf("env: %v", j.Env)
	}
	if j.Dir != "/work" || !strings.Contains(j.Label, "psql") || !strings.Contains(j.Label, "Local PG") {
		t.Errorf("job: %+v", j)
	}
}

func TestDBCLIJobRefusals(t *testing.T) {
	if _, err := dbCLIJob(dbclient.Conn{ID: "m", Driver: "mongodb"}, "", "/w"); err == nil {
		t.Error("a driver without a client should be refused")
	}
	ssh := dbclient.Conn{ID: "s", Driver: "postgres", Host: "h", SSH: &dbclient.SSHConfig{Host: "bastion"}}
	if _, err := dbCLIJob(ssh, "", "/w"); err == nil || !strings.Contains(err.Error(), "SSH") {
		t.Errorf("an SSH connection should be refused: %v", err)
	}
}

func TestDBCLIJobSQLiteRelativePath(t *testing.T) {
	j, err := dbCLIJob(dbclient.Conn{ID: "f", Driver: "sqlite", Path: "data/app.db", ReadOnly: true}, "", "/work")
	if err != nil {
		t.Fatal(err)
	}
	if j.Cmdline != "'sqlite3' '-readonly' '/work/data/app.db'" {
		t.Errorf("cmdline: %s", j.Cmdline)
	}
}
