package dbclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSSHConfigParsingEdges(t *testing.T) {
	cfg := []byte(`# top
User fallback
Host *.internal !secret.internal
  HostName %h.example
  Port 2200
  IdentityFile "~/keys/my key"
  IdentityFile ~/keys/b
  ProxyJump = jump1,jump2
  ProxyCommand ssh -W %h:%p x
Host other
  Port abc
Match all
  User ignored
Host *
  Port 22
  User late
  Compression yes
  ProxyJump none
`)
	got := parseSSHConfig(cfg, "db.internal")
	if got.HostName != "db.internal.example" || got.Port != 2200 || len(got.IdentityFiles) != 2 || got.IdentityFiles[0] != "~/keys/my key" || got.ProxyJump != "jump1,jump2" || got.ProxyCommand != "" {
		t.Errorf("db.internal = %+v", got)
	}
	if secret := parseSSHConfig(cfg, "secret.internal"); secret.HostName != "" || secret.Port != 22 || secret.User != "fallback" {
		t.Errorf("a negated pattern = %+v", secret)
	}
	if other := parseSSHConfig(cfg, "other"); other.Port != 0 {
		t.Errorf("the first value counts, as in ssh: a bad port is no port = %+v", other)
	}
	if none := parseSSHConfig([]byte("Host a\n ProxyCommand none\n"), "a"); none.ProxyCommand != "" {
		t.Errorf("ProxyCommand none = %+v", none)
	}
	for in, want := range map[string][]string{`a "b c" d`: {"a", "b c", "d"}, `"unclosed arg`: {"unclosed arg"}, "": nil, `""`: {""}} {
		got := sshConfigArgs(in)
		if len(got) != len(want) {
			t.Errorf("sshConfigArgs(%q) = %q, want %q", in, got, want)
		}
	}
	if k, v := splitSSHConfigLine("onlykey"); k != "onlykey" || v != "" {
		t.Errorf("a line without a value = %q %q", k, v)
	}
	if k, v := splitSSHConfigLine("Port=22"); k != "port" || v != "22" {
		t.Errorf("key=value = %q %q", k, v)
	}
	if !sshHostMatches([]string{"a,,b"}, "B") || sshHostMatches([]string{"[bad"}, "x") || sshHostMatches(nil, "x") {
		t.Error("sshHostMatches")
	}
}

func TestSchemaFolderFilesAndFailures(t *testing.T) {
	dir := t.TempDir()
	if got, err := ReadSchemaFolder(filepath.Join(dir, "none")); err != nil || len(got) != 0 {
		t.Errorf("a missing folder is empty = %v %v", got, err)
	}
	if _, err := WriteSchemaFolder(dir, []CanonFile{{Name: "tables/a.sql", Text: "CREATE TABLE a (x int);"}, {Name: "views/.hidden.sql", Text: "x"}, {Name: "tables/notes.txt", Text: "n"}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := WriteSchemaFolder(dir, []CanonFile{{Name: "tables/a.sql", Text: "CREATE TABLE a (x int);"}}); n != 0 {
		t.Errorf("an unchanged file is not written: %d", n)
	}
	got, err := ReadSchemaFolder(dir)
	if err != nil || len(got) != 1 || got["tables/a.sql"] == "" {
		t.Errorf("read = %v %v", got, err)
	}
	big := filepath.Join(dir, "tables", "big.sql")
	if err := os.WriteFile(big, make([]byte, schemaFolderMaxFile+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSchemaFolder(dir); err == nil || !strings.Contains(err.Error(), "larger than 4 MB") {
		t.Errorf("a big file = %v", err)
	}
	_ = os.Remove(big)
	// Names that leave the folder are refused before anything is written.
	for _, name := range []string{"", "../x.sql", "/abs.sql", "..", "."} {
		if _, err := WriteSchemaFolder(dir, []CanonFile{{Name: name, Text: "x"}}); err == nil {
			t.Errorf("%q should be refused", name)
		}
		if _, _, err := ApplySchemaFolder(dir, []SchemaFileDiff{{Name: name, State: SchemaFileChanged}}); err == nil {
			t.Errorf("apply %q should be refused", name)
		}
	}
	// A folder that cannot be made, and a file that cannot be replaced.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteSchemaFolder(blocker, []CanonFile{{Name: "tables/x.sql", Text: "x"}}); err == nil {
		t.Error("a folder under a file: want error")
	}
	if _, err := os.Stat(filepath.Join(dir, "tables", "x.sql")); err == nil {
		t.Error("nothing is written after a refusal")
	}
	// Apply: write, replace and remove.
	if err := os.WriteFile(filepath.Join(dir, "tables", "old.sql"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	wrote, removed, err := ApplySchemaFolder(dir, []SchemaFileDiff{
		{Name: "tables/a.sql", State: SchemaFileChanged, Live: "CREATE TABLE a (y int);"},
		{Name: "tables/new.sql", State: SchemaFileLiveOnly, Live: "n"},
		{Name: "tables/old.sql", State: SchemaFileFolderOnly},
		{Name: "tables/gone.sql", State: SchemaFileFolderOnly},
		{Name: "tables/same.sql", State: SchemaFileIdentical},
	})
	if err != nil || wrote != 2 || removed != 2 {
		t.Errorf("apply = %d %d %v", wrote, removed, err)
	}
	// Removing something that is a non-empty folder fails and says how far it got.
	if err := os.MkdirAll(filepath.Join(dir, "tables", "d", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ApplySchemaFolder(dir, []SchemaFileDiff{{Name: "tables/d", State: SchemaFileFolderOnly}}); err == nil {
		t.Error("removing a non-empty folder: want error")
	}
	for file, want := range map[string]string{
		"flyway.toml": "Flyway", "conf/flyway.conf": "Flyway", "liquibase.properties": "Liquibase", "db/changelog/a.xml": "Liquibase",
		"alembic.ini": "Alembic", "prisma/migrations/x": "Prisma", "db/migrate/1.rb": "Rails", "migrations/1.up.sql": "golang-migrate",
		"db/migrations/1.up.sql": "golang-migrate", "db/migrations/1.sql": "dbmate", "dbmate": "dbmate", "README": "",
	} {
		d := t.TempDir()
		if err := os.MkdirAll(filepath.Join(d, filepath.Dir(file)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, file), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := DetectMigrationTool(d); got != want {
			t.Errorf("DetectMigrationTool with %s = %q, want %q", file, got, want)
		}
	}
}
