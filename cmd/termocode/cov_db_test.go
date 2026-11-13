package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func dbRun(stdin string, args ...string) (int, string, string) {
	var out, errw bytes.Buffer
	code := runDBIn(args, strings.NewReader(stdin), &out, &errw)
	return code, out.String(), errw.String()
}

func TestDBCommandMoreErrors(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	if code, _, errw := dbRun(""); code != 2 || !strings.Contains(errw, "manage your database") {
		t.Errorf("no args: %d %q", code, errw)
	}
	if code, out, _ := dbRun("", "--help"); code != 0 || out == "" {
		t.Errorf("help: %d", code)
	}
	if code, out, _ := dbRun("", "list"); code != 0 || !strings.Contains(out, "no connections") {
		t.Errorf("empty list: %d %q", code, out)
	}
	for _, a := range [][]string{{"show"}, {"show", ""}, {"remove"}, {"rm", "a", "b"}} {
		if code, _, errw := dbRun("", a...); code != 2 || !strings.Contains(errw, "give one connection id") {
			t.Errorf("%v: %d %q", a, code, errw)
		}
	}
	if code, _, errw := dbRun("", "show", "ghost"); code != 1 || !strings.Contains(errw, "no connection") {
		t.Errorf("show ghost: %d %q", code, errw)
	}
	if code, _, _ := dbRun("not json", "add"); code != 1 {
		t.Errorf("add bad json: %d", code)
	}
	// A damaged connections file is reported for every command that reads it.
	path := filepath.Join(cfg, "termocode", "db", "connections.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"list"}, {"show", "x"}, {"remove", "x"}} {
		if code, _, errw := dbRun("{}", a...); code != 1 || !strings.Contains(errw, "connections.json") {
			t.Errorf("%v on broken file: %d %q", a, code, errw)
		}
	}
	if code, _, _ := dbRun(`{"id":"a","driver":"mysql","host":"h","port":1}`, "add"); code != 1 {
		t.Errorf("add on broken file: %d", code)
	}
}

func TestDBPathAndTargets(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/h")
	if p, err := dbConnectionsPath(); err != nil || p != "/h/.config/termocode/db/connections.json" {
		t.Fatalf("%q %v", p, err)
	}
	t.Setenv("HOME", "")
	if _, err := dbConnectionsPath(); err == nil {
		t.Skip("platform finds a home anyway")
	}
	for _, c := range []struct {
		c    dbclient.Conn
		want string
	}{
		{dbclient.Conn{Path: "/x.db"}, "/x.db"},
		{dbclient.Conn{Kube: &dbclient.KubeConfig{Pod: "p"}}, "kube:p"},
		{dbclient.Conn{URL: "u"}, "url"},
		{dbclient.Conn{Host: "h", Port: 5}, "h:5"},
	} {
		if got := dbTarget(c.c); got != c.want {
			t.Errorf("%+v: %q", c.c, got)
		}
	}
}

func TestDBWriteConnectionsErrors(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "f")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if dbWriteConnections(filepath.Join(blocker, "sub", "c.json"), nil) == nil {
		t.Fatal("wrote under a file")
	}
	if _, err := dbReadConnections(dir); err == nil {
		t.Fatal("read a folder")
	}
}
