package secrets

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// migrateFixture is fixture plus a name only the plain backend's
// "secrets" field holds (a database password saved under SS-3).
const migrateFixture = `{
  "keys": {"anthropic": "sk-ant-FIXTURE", "openai": "sk-oai-FIXTURE"},
  "claude_oauth": {"access_token": "at-FIXTURE", "refresh_token": "rt-FIXTURE", "expires_at_ms": 42},
  "mcp_secrets": {"GITHUB_PAT": "ghp_FIXTURE"},
  "mcp_oauth": {"linear": {"issuer": "https://linear.app", "access_token": "lin-FIXTURE"}},
  "mcp_accounts": {"github": "amin"},
  "secrets": {"db.main.password": "pw-FIXTURE"}
}`

// migrateDir is a config dir with ai/credentials.json = content.
func migrateDir(t *testing.T, content string) (dir, plain string) {
	t.Helper()
	dir = t.TempDir()
	plain = filepath.Join(dir, "ai", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(plain), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plain, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, plain
}

// wantMigrated are the names and values FromPlain gives for
// migrateFixture (blobs compacted as the plain backend reads them).
var wantMigrated = map[string]string{
	"ai.key.anthropic":      "sk-ant-FIXTURE",
	"ai.key.openai":         "sk-oai-FIXTURE",
	"ai.claude_oauth":       `{"access_token":"at-FIXTURE","refresh_token":"rt-FIXTURE","expires_at_ms":42}`,
	"mcp.secret.GITHUB_PAT": "ghp_FIXTURE",
	"mcp.oauth.linear":      `{"issuer":"https://linear.app","access_token":"lin-FIXTURE"}`,
	"mcp.account.github":    "amin",
	"db.main.password":      "pw-FIXTURE",
}

func TestFromPlainAllKinds(t *testing.T) {
	_, plain := migrateDir(t, migrateFixture)
	p, err := FromPlain(plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Values) != len(wantMigrated) {
		t.Errorf("names = %v", keys(p.Values))
	}
	for n, w := range wantMigrated {
		if p.Values[n] != w {
			t.Errorf("%s = %q, want %q", n, p.Values[n], w)
		}
	}
	c := p.Counts()
	if c != (Counts{AIKeys: 2, ClaudeOAuth: 1, MCPSecrets: 1, MCPOAuth: 1, Other: 1, Accounts: 1}) {
		t.Errorf("counts = %+v", c)
	}
	if c.Secrets() != 6 {
		t.Errorf("Secrets() = %d", c.Secrets())
	}
	if got := c.String(); got != "2 AI keys · Claude sign-in · 1 MCP secret · 1 sign-in · 1 other secret" {
		t.Errorf("String() = %q", got)
	}
}

func TestFromPlainMissingOrEmpty(t *testing.T) {
	p, err := FromPlain(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || len(p.Values) != 0 || p.Counts().Secrets() != 0 {
		t.Fatalf("missing file: %+v %v", p, err)
	}
	_, plain := migrateDir(t, `{"mcp_accounts": {"github": "amin"}, "keys": {"openai": ""}}`)
	p, err = FromPlain(plain)
	if err != nil {
		t.Fatal(err)
	}
	if p.Counts().Secrets() != 0 || len(p.Values) != 1 {
		t.Fatalf("accounts only: %+v", p.Values)
	}
	_, bad := migrateDir(t, `{not json`)
	if _, err := FromPlain(bad); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("damaged file: %v", err)
	}
}

// removeSpy hooks the delete step: it records what the file held at
// that moment.
func removeSpy(t *testing.T) *[]byte {
	t.Helper()
	var seen []byte
	restore := setRemove(func(path string) error {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		seen = b
		return os.Remove(path)
	})
	t.Cleanup(restore)
	return &seen
}

// checkRemoved: the plain file is gone, was all zeros of its old length
// when it was deleted, and the empty 0600 marker is there.
func checkRemoved(t *testing.T, plain string, oldLen int, seen []byte) {
	t.Helper()
	if _, err := os.Stat(plain); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("plain file still there: %v", err)
	}
	if len(seen) != oldLen || !bytes.Equal(seen, make([]byte, oldLen)) {
		t.Errorf("deleted file was not zeroed: %d bytes, want %d zeros", len(seen), oldLen)
	}
	m := MarkerPath(plain)
	if m != plain+".migrated" {
		t.Errorf("marker path %q", m)
	}
	fi, err := os.Stat(m)
	if err != nil {
		t.Fatalf("no marker: %v", err)
	}
	if fi.Size() != 0 || fi.Mode().Perm() != 0o600 {
		t.Errorf("marker size %d mode %v", fi.Size(), fi.Mode().Perm())
	}
	if !Migrated(plain) {
		t.Error("Migrated = false")
	}
}

func checkValues(t *testing.T, s Store) {
	t.Helper()
	for n, w := range wantMigrated {
		v, ok, err := s.Get(n)
		if err != nil || !ok || v != w {
			t.Errorf("Get(%s) = %q %v %v, want %q", n, v, ok, err, w)
		}
	}
}

func TestMigrateToAge(t *testing.T) {
	dir, plain := migrateDir(t, migrateFixture)
	seen := removeSpy(t)
	var committed Store
	s, err := Migrate(MigrateOptions{RemovePlain: true,
		Options: Options{Dir: dir, PlainPath: plain, ScryptWorkFactor: 10},
		To:      "age", Pass: []byte("correct horse battery"),
		Commit: func(s Store) { committed = s },
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Backend() != "age" || s.Locked() || committed != s {
		t.Fatalf("store %s locked=%v committed=%v", s.Backend(), s.Locked(), committed == s)
	}
	checkValues(t, s)
	checkRemoved(t, plain, len(migrateFixture), *seen)
	idx, _ := loadIndex(filepath.Join(dir, IndexFile))
	if idx.Backend != "age" {
		t.Errorf("recorded backend %q", idx.Backend)
	}
	// mcp_accounts are not secret: in the index, not the encrypted file.
	if e := idx.Entries["mcp.account.github"]; e.Kind != KindPublic || e.Value != "amin" {
		t.Errorf("account entry %+v", e)
	}
	if strings.Contains(readFile(t, filepath.Join(dir, IndexFile)), "FIXTURE") {
		t.Error("a secret value reached secrets.json")
	}
	// The next start under auto (with the SS-3 plain rule) opens the age
	// file: the plain rule no longer applies.
	again, err := Open(Options{Dir: dir, PlainPath: plain, KeepPlainFile: true, ScryptWorkFactor: 10,
		KeyringProbe: func() bool { return false }})
	if err != nil {
		t.Fatal(err)
	}
	if again.Backend() != "age" || !again.Locked() {
		t.Fatalf("restart: %s locked=%v", again.Backend(), again.Locked())
	}
	if err := again.Unlock("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	checkValues(t, again)
}

func TestMigrateToKeyring(t *testing.T) {
	keyring.MockInit()
	dir, plain := migrateDir(t, migrateFixture)
	seen := removeSpy(t)
	s, err := Migrate(MigrateOptions{RemovePlain: true,
		Options: Options{Dir: dir, PlainPath: plain, KeyringProbe: func() bool { return true }},
		To:      "keyring",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Backend() != "keyring" {
		t.Fatalf("backend %s", s.Backend())
	}
	checkValues(t, s)
	checkRemoved(t, plain, len(migrateFixture), *seen)
	if v, err := keyring.Get(keyringService, "mcp.account.github"); err == nil {
		t.Errorf("account went to the keyring: %q", v)
	}
	again, err := Open(Options{Dir: dir, PlainPath: plain, KeepPlainFile: true, KeyringProbe: func() bool { return false }})
	if err != nil {
		t.Fatal(err)
	}
	if again.Backend() != "keyring" {
		t.Fatalf("restart: %s", again.Backend())
	}
	checkValues(t, again)
}

// Any failure before the delete step leaves the plain file as it was,
// the index as it was (plain stays the backend) and no new age file.
func TestMigrateVerifyFailureKeepsPlainFile(t *testing.T) {
	dir, plain := migrateDir(t, migrateFixture)
	if err := updateIndex(filepath.Join(dir, IndexFile), func(i *Index) { i.Backend = "plain" }); err != nil {
		t.Fatal(err)
	}
	idxBefore := readFile(t, filepath.Join(dir, IndexFile))
	removed := false
	t.Cleanup(setRemove(func(string) error { removed = true; return nil }))
	t.Cleanup(setVerify(func(Store, map[string]string) error { return errors.New("ai.key.openai differs") }))
	committed := false
	_, err := Migrate(MigrateOptions{RemovePlain: true,
		Options: Options{Dir: dir, PlainPath: plain, ScryptWorkFactor: 10},
		To:      "age", Pass: []byte("correct horse battery"),
		Commit: func(Store) { committed = true },
	})
	if err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "FIXTURE") {
		t.Errorf("error carries a value: %v", err)
	}
	if removed || committed {
		t.Errorf("removed=%v committed=%v", removed, committed)
	}
	if got := readFile(t, plain); got != migrateFixture {
		t.Errorf("plain file changed: %q", got)
	}
	if Migrated(plain) {
		t.Error("marker written")
	}
	if got := readFile(t, filepath.Join(dir, IndexFile)); got != idxBefore {
		t.Errorf("index not restored:\n%s\nwant\n%s", got, idxBefore)
	}
	if _, err := os.Stat(filepath.Join(dir, AgeFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("new age file left behind: %v", err)
	}
	s, err := Open(Options{Dir: dir, PlainPath: plain, KeepPlainFile: true, KeyringProbe: func() bool { return false }})
	if err != nil || s.Backend() != "plain" {
		t.Fatalf("after failure: %v %v", s, err)
	}
}

func TestMigrateWeakPassphraseKeepsPlainFile(t *testing.T) {
	dir, plain := migrateDir(t, migrateFixture)
	_, err := Migrate(MigrateOptions{RemovePlain: true, Options: Options{Dir: dir, PlainPath: plain, ScryptWorkFactor: 10}, To: "age", Pass: []byte("short")})
	if !errors.Is(err, ErrWeakPassphrase) {
		t.Fatalf("err = %v", err)
	}
	if readFile(t, plain) != migrateFixture || Migrated(plain) {
		t.Fatal("plain file touched")
	}
	if _, err := os.Stat(filepath.Join(dir, IndexFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("index left behind: %v", err)
	}
}

func TestMigrateNothingToMove(t *testing.T) {
	dir, plain := migrateDir(t, `{}`)
	if _, err := Migrate(MigrateOptions{RemovePlain: true, Options: Options{Dir: dir, PlainPath: plain}, To: "age", Pass: []byte("correct horse battery")}); !errors.Is(err, ErrNothingToMove) {
		t.Fatalf("err = %v", err)
	}
	if readFile(t, plain) != `{}` {
		t.Fatal("plain file touched")
	}
}

func TestVerifyNamesWithoutValues(t *testing.T) {
	s := NewStore(newPlain(filepath.Join(t.TempDir(), "c.json")), Options{Dir: t.TempDir()})
	if err := s.Set("ai.key.openai", "sk-STORED"); err != nil {
		t.Fatal(err)
	}
	if err := Verify(s, map[string]string{"ai.key.openai": "sk-STORED"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []map[string]string{{"ai.key.openai": "sk-OTHER"}, {"ai.key.anthropic": "sk-MISSING"}} {
		err := Verify(s, want)
		if err == nil {
			t.Fatalf("Verify(%v) passed", keys(want))
		}
		if strings.Contains(err.Error(), "sk-") {
			t.Errorf("error carries a value: %v", err)
		}
	}
}

// Interrupted after the writes: the plain file and a filled store with
// the same names. Finish verifies and does the delete step.
func TestFinishInterruptedMigration(t *testing.T) {
	keyring.MockInit()
	dir, plain := migrateDir(t, migrateFixture)
	seen := removeSpy(t)
	s, err := Open(Options{Dir: dir, PlainPath: plain, Backend: "keyring", KeyringProbe: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	if done, err := Finish(s, plain); done || err != nil {
		t.Fatalf("empty store: done=%v err=%v", done, err)
	}
	if readFile(t, plain) != migrateFixture {
		t.Fatal("plain file touched while the store was empty")
	}
	p, _ := FromPlain(plain)
	if err := SetAll(s, p.Values); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("ai.key.openai", "sk-CHANGED"); err != nil {
		t.Fatal(err)
	}
	if done, err := Finish(s, plain); done || err == nil {
		t.Fatalf("different value: done=%v err=%v", done, err)
	}
	if readFile(t, plain) != migrateFixture {
		t.Fatal("plain file removed although a value differs")
	}
	if err := s.Set("ai.key.openai", "sk-oai-FIXTURE"); err != nil {
		t.Fatal(err)
	}
	done, err := Finish(s, plain)
	if !done || err != nil {
		t.Fatalf("done=%v err=%v", done, err)
	}
	checkRemoved(t, plain, len(migrateFixture), *seen)
}

func TestStoreKeepsAccountsInIndex(t *testing.T) {
	keyring.MockInit()
	dir := t.TempDir()
	var registered []string
	s, err := Open(Options{Dir: dir, Backend: "keyring", KeyringProbe: func() bool { return true },
		Register: func(v string) { registered = append(registered, v) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("mcp.account.github", "amin"); err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Get(keyringService, "mcp.account.github"); err == nil {
		t.Fatal("account stored in the keyring")
	}
	if v, ok, err := s.Get("mcp.account.github"); v != "amin" || !ok || err != nil {
		t.Fatalf("Get = %q %v %v", v, ok, err)
	}
	if len(registered) != 0 {
		t.Errorf("account registered as a secret: %v", registered)
	}
	if e := Entries(dir)["mcp.account.github"]; e.Kind != KindPublic || e.Value != "amin" {
		t.Fatalf("entry %+v", e)
	}
	if err := s.Delete("mcp.account.github"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get("mcp.account.github"); ok {
		t.Fatal("account still there")
	}
	// The plain backend keeps the old file shape (older binaries read it).
	plain := filepath.Join(dir, "c.json")
	p := NewStore(newPlain(plain), Options{Dir: t.TempDir()})
	if err := p.Set("mcp.account.github", "amin"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, plain), `"mcp_accounts"`) {
		t.Fatal("plain backend lost the mcp_accounts field")
	}
}

func TestMigrateLaterCount(t *testing.T) {
	dir := t.TempDir()
	if n := LaterCount(dir); n != 0 {
		t.Fatalf("start %d", n)
	}
	for want := 1; want <= 3; want++ {
		n, err := AddLater(dir)
		if err != nil || n != want {
			t.Fatalf("AddLater = %d %v, want %d", n, err, want)
		}
	}
	if LaterCount(dir) != 3 {
		t.Fatal("count not kept")
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
