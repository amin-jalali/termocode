package dbclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The key pragma quotes the key as one SQL string: quotes are doubled and
// a raw hex key ("x'…'") is passed through as SQLCipher expects.
func TestSQLCipherKeyPragma(t *testing.T) {
	for key, want := range map[string]string{
		"secret":            `PRAGMA key = 'secret';`,
		"it's":              `PRAGMA key = 'it''s';`,
		"x'2DD29CA851E7'":   `PRAGMA key = "x'2DD29CA851E7'";`,
		"x'2DD29CA851E7":    `PRAGMA key = 'x''2DD29CA851E7';`,
		"x'zz'":             `PRAGMA key = 'x''zz''';`,
		"a\nb":              `PRAGMA key = 'a` + "\n" + `b';`,
		"; DROP TABLE t --": `PRAGMA key = '; DROP TABLE t --';`,
	} {
		if got := SQLCipherKeyPragma(key); got != want {
			t.Errorf("SQLCipherKeyPragma(%q) = %q, want %q", key, got, want)
		}
	}
}

// sqlcipher:// URLs make an encrypted SQLite connection; the key is never
// read from the URL.
func TestSQLCipherParseURL(t *testing.T) {
	c, err := ParseURL("sqlcipher:///tmp/vault.db")
	if err != nil {
		t.Fatal(err)
	}
	if c.Driver != "sqlite" || c.Preset != SQLCipherPreset || c.Path != "/tmp/vault.db" {
		t.Fatalf("conn = %+v", c)
	}
	if !IsSQLCipher(c) {
		t.Fatal("IsSQLCipher = false")
	}
	if IsSQLCipher(Conn{Driver: "sqlite", Path: "a.db"}) {
		t.Fatal("plain sqlite reported as SQLCipher")
	}
	if IsSQLCipher(Conn{Driver: "postgres", Preset: SQLCipherPreset}) {
		t.Fatal("postgres reported as SQLCipher")
	}
}

// The sqlcipher preset is valid on the sqlite driver only, and the key in
// a project file must be a reference.
func TestSQLCipherConfigPreset(t *testing.T) {
	root := t.TempDir()
	conns, err := ParseConfig([]byte(`{"connections":[
		{"id":"vault","driver":"sqlite","preset":"sqlcipher","path":"vault.db","password":"${secret:vault-key}"},
		{"id":"bad","driver":"postgres","preset":"sqlcipher","host":"h"},
		{"id":"leak","driver":"sqlite","preset":"SQLCipher","path":"leak.db","password":"plain"}
	]}`), SourceProject, "x", root)
	if err != nil {
		t.Fatal(err)
	}
	if conns[0].Problem != "" || conns[0].Preset != SQLCipherPreset {
		t.Errorf("vault = %+v", conns[0])
	}
	if conns[1].Problem != "preset needs the sqlite driver" {
		t.Errorf("bad problem = %q", conns[1].Problem)
	}
	if conns[2].Problem != "literal password in project config" || conns[2].Preset != SQLCipherPreset {
		t.Errorf("leak = %+v", conns[2])
	}
}

func writeFakeSQLCipher(t *testing.T, log string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sqlcipher")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = \"-version\" ]; then echo '3.46.1 2024-08-13 (SQLCipher 4.6.1 community)'; exit 0; fi\n" +
		"while IFS= read -r line; do\n" +
		"  printf '%s\\n' \"$line\" >> '" + log + "'\n" +
		"  case \"$line\" in\n" +
		"    .print*) printf '%s\\n' \"${line#.print }\" ;;\n" +
		"    *sqlite_master*) printf '[{\"n\":0}]\\n' ;;\n" +
		"  esac\n" +
		"done\n"
	if err := os.WriteFile(path, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

// The key is the first thing sent to sqlcipher, before any other setup,
// and it is never on the command line.
func TestSQLCipherSendsKeyFirst(t *testing.T) {
	log := filepath.Join(t.TempDir(), "stdin.log")
	bin := writeFakeSQLCipher(t, log)
	old := lookupSQLCipher
	lookupSQLCipher = func() (string, error) { return bin, nil }
	t.Cleanup(func() { lookupSQLCipher = old })
	s, err := NewSQLCipherSession(context.Background(), Conn{Driver: "sqlite", Preset: SQLCipherPreset, Path: ":memory:"}, "k'ey")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	if lines[0] != `PRAGMA key = 'k''ey';` {
		t.Fatalf("first line = %q, want the key pragma", lines[0])
	}
	if !strings.Contains(string(b), "sqlite_master") {
		t.Error("the key was not checked with a read of sqlite_master")
	}
}

// Without a key, or without the sqlcipher CLI, the session is refused with
// a clear message.
func TestSQLCipherRefusals(t *testing.T) {
	fakeHost(t, "darwin", "")
	old := lookupSQLCipher
	t.Cleanup(func() { lookupSQLCipher = old })
	lookupSQLCipher = func() (string, error) { return "", os.ErrNotExist }
	_, err := NewSQLCipherSession(context.Background(), Conn{Driver: "sqlite", Preset: SQLCipherPreset, Path: "a.db"}, "k")
	if err == nil || !strings.Contains(err.Error(), "sqlcipher is not installed") || !strings.Contains(err.Error(), "brew install sqlcipher") {
		t.Fatalf("missing CLI err = %v", err)
	}
	lookupSQLCipher = func() (string, error) { return "/bin/true", nil }
	_, err = NewSQLCipherSession(context.Background(), Conn{Driver: "sqlite", Preset: SQLCipherPreset, Path: "a.db"}, "")
	if err == nil || !strings.Contains(err.Error(), "needs its key") {
		t.Fatalf("empty key err = %v", err)
	}
}

// A wrong key shows as a plain message, not sqlite's "file is not a database".
func TestSQLCipherWrongKeyMessage(t *testing.T) {
	err := sqlcipherKeyError(errors.New("Error: file is not a database"))
	if err == nil || !strings.Contains(err.Error(), "wrong key") {
		t.Fatalf("err = %v", err)
	}
	other := errors.New("Error: disk I/O error")
	if got := sqlcipherKeyError(other); got != other {
		t.Fatalf("other error changed: %v", got)
	}
}

// Install hints name the sqlcipher package per OS.
func TestSQLCipherInstallHint(t *testing.T) {
	for goos, want := range map[string]string{
		"darwin":  "brew install sqlcipher",
		"android": "pkg install sqlcipher",
		"linux":   "sqlcipher",
		"plan9":   "your package manager",
	} {
		if got := SQLCipherInstallHintWithRelease(goos, "ID=ubuntu\n"); !strings.Contains(got, want) {
			t.Errorf("%s: %q, want %q", goos, got, want)
		}
	}
	if got := SQLCipherInstallHintWithRelease("linux", "ID=ubuntu\n"); !strings.Contains(got, "apt install sqlcipher") {
		t.Errorf("ubuntu: %q", got)
	}
}

// The $ client of an encrypted file is sqlcipher, and the key is not on
// its command line.
func TestSQLCipherCLICommand(t *testing.T) {
	cmd, err := CLICommand(Conn{Driver: "sqlite", Preset: SQLCipherPreset, Path: "/w/v.db"}, CLIOptions{Password: "topkey"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Tool != "sqlcipher" {
		t.Fatalf("tool = %q", cmd.Tool)
	}
	if strings.Contains(strings.Join(cmd.Args, " ")+strings.Join(cmd.Env, " "), "topkey") {
		t.Fatalf("key leaked: %+v", cmd)
	}
}
