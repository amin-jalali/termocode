package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func openAge(t *testing.T, dir string, o Options) Store {
	t.Helper()
	o.Dir = dir
	if o.Backend == "" {
		o.Backend = "age"
	}
	s, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreValuesAndReferences(t *testing.T) {
	dir := t.TempDir()
	var registered []string
	s := openAge(t, dir, Options{
		Getenv:   func(k string) string { return map[string]string{"OPENAI_API_KEY": "env-SECRET"}[k] },
		Register: func(v string) { registered = append(registered, v) },
	})
	if s.Backend() != "age" || !s.Locked() {
		t.Fatalf("backend %q locked %v", s.Backend(), s.Locked())
	}
	if _, _, err := s.Get("ai.key.anthropic"); !errors.Is(err, ErrLocked) {
		t.Fatalf("Get while locked: %v", err)
	}
	if err := s.Set("ai.key.anthropic", "v"); !errors.Is(err, ErrLocked) {
		t.Fatalf("Set while locked: %v", err)
	}
	if err := s.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("ai.key.anthropic", "sk-ant-SECRET"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("ai.key.openai", "${env:OPENAI_API_KEY}"); err != nil {
		t.Fatal(err)
	}
	if got := s.Names(); !reflect.DeepEqual(got, []string{"ai.key.anthropic", "ai.key.openai"}) {
		t.Fatalf("Names = %v", got)
	}
	if v, ok, err := s.Get("ai.key.anthropic"); v != "sk-ant-SECRET" || !ok || err != nil {
		t.Fatalf("Get value = %q %v %v", v, ok, err)
	}
	if v, ok, err := s.Get("ai.key.openai"); v != "env-SECRET" || !ok || err != nil {
		t.Fatalf("Get ref = %q %v %v", v, ok, err)
	}
	if !reflect.DeepEqual(registered, []string{"sk-ant-SECRET", "env-SECRET"}) {
		t.Fatalf("every value handed out is registered for redaction: %v", registered)
	}
	if _, ok, err := s.Get("ai.key.none"); ok || err != nil {
		t.Fatalf("unknown name = %v %v", ok, err)
	}

	// The index holds names and references, never values.
	idx := readFile(t, filepath.Join(dir, "secrets.json"))
	if strings.Contains(idx, "SECRET") {
		t.Fatal("index holds a value")
	}
	if !strings.Contains(idx, "${env:OPENAI_API_KEY}") || !strings.Contains(idx, `"backend": "age"`) {
		t.Fatalf("index = %s", idx)
	}

	// A value replaced by a reference leaves the backend.
	if err := s.Set("ai.key.anthropic", "${env:OPENAI_API_KEY}"); err != nil {
		t.Fatal(err)
	}
	s.Lock()
	if !s.Locked() {
		t.Fatal("Lock")
	}
	if _, _, err := s.Get("ai.key.openai"); !errors.Is(err, ErrLocked) {
		t.Fatalf("a locked store gives no references either: %v", err)
	}
	if got := s.Names(); len(got) != 2 {
		t.Fatalf("Names works while locked: %v", got)
	}
	s2 := openAge(t, dir, Options{})
	if err := s2.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	if err := s2.Delete("ai.key.openai"); err != nil {
		t.Fatal(err)
	}
	if got := s2.Names(); !reflect.DeepEqual(got, []string{"ai.key.anthropic"}) {
		t.Fatalf("Names after delete = %v", got)
	}
}

func TestStoreRejectsBadInput(t *testing.T) {
	s := openAge(t, t.TempDir(), Options{})
	if err := s.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"", ".x", "x.", "a..b", "a/b", "../x", "a b", "-x", strings.Repeat("a", 201)} {
		if err := s.Set(n, "v"); !errors.Is(err, ErrBadName) {
			t.Errorf("Set(%q) err = %v, want ErrBadName", n, err)
		}
		if _, _, err := s.Get(n); !errors.Is(err, ErrBadName) {
			t.Errorf("Get(%q) err = %v, want ErrBadName", n, err)
		}
	}
	for _, n := range []string{"ai.key.anthropic", "mcp.secret.GITHUB_PAT", "db.local-pg", "http.secret.a_b"} {
		if err := ValidName(n); err != nil {
			t.Errorf("ValidName(%q) = %v", n, err)
		}
	}
	if err := s.Set("ai.key.x", "${env:1BAD}"); !errors.Is(err, ErrBadRef) {
		t.Fatalf("bad reference: %v", err)
	}
	if got := s.Names(); len(got) != 0 {
		t.Fatalf("a refused Set left a name: %v", got)
	}
}

func TestOpenSelection(t *testing.T) {
	cases := []struct {
		setting  string
		recorded string
		want     string
		err      error
	}{
		{"", "", "age", nil},
		{"auto", "", "age", nil},
		{"auto", "age", "age", nil},
		{"auto", "plain", "age", nil}, // auto never picks plain
		{"age", "", "age", nil},
		{"plain", "", "plain", nil},
		{"plain", "age", "plain", nil},
		{"keyring", "", "", ErrKeyringUnavailable},
		{"vault", "", "", ErrUnknownBackend},
	}
	for _, c := range cases {
		dir := t.TempDir()
		if c.recorded != "" {
			if err := updateIndex(filepath.Join(dir, "secrets.json"), func(i *Index) { i.Backend = c.recorded }); err != nil {
				t.Fatal(err)
			}
		}
		s, err := Open(Options{Dir: dir, Backend: c.setting, KeyringProbe: func() bool { return false }})
		if c.err != nil {
			if !errors.Is(err, c.err) {
				t.Errorf("%q/%q: err = %v, want %v", c.setting, c.recorded, err, c.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q/%q: %v", c.setting, c.recorded, err)
			continue
		}
		if s.Backend() != c.want {
			t.Errorf("%q/%q: backend = %q, want %q", c.setting, c.recorded, s.Backend(), c.want)
		}
		idx, _ := loadIndex(filepath.Join(dir, "secrets.json"))
		if idx.Backend != c.want {
			t.Errorf("%q/%q: recorded %q, want %q", c.setting, c.recorded, idx.Backend, c.want)
		}
	}
}

func TestOpenSelectsAvailableKeyring(t *testing.T) {
	s, err := Open(Options{Dir: t.TempDir(), Backend: "auto", KeyringProbe: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	if s.Backend() != "keyring" {
		t.Fatalf("backend = %q", s.Backend())
	}
}

func TestOpenNeedsDir(t *testing.T) {
	if _, err := Open(Options{}); err == nil {
		t.Fatal("Open without Dir must fail")
	}
}

func TestOpenCorruptIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Options{Dir: dir}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestPlainStoreUsesCredentialsFile(t *testing.T) {
	dir := t.TempDir()
	creds := writeFixture(t)
	s, err := Open(Options{Dir: dir, Backend: "plain", PlainPath: creds})
	if err != nil {
		t.Fatal(err)
	}
	if s.Locked() {
		t.Fatal("plain is never locked")
	}
	// Names in the old file that the index does not list yet still resolve.
	if v, ok, err := s.Get("ai.key.anthropic"); v != "sk-ant-FIXTURE" || !ok || err != nil {
		t.Fatalf("Get = %q %v %v", v, ok, err)
	}
	if err := s.Set("db.local", "pg-VALUE"); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, creds); m != 0o600 {
		t.Errorf("mode = %o", m)
	}
}

func TestPlainStoreDefaultPath(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, Backend: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("ai.key.openai", "sk-VALUE"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ai", "credentials.json")); err != nil {
		t.Fatalf("default plain path: %v", err)
	}
}

func TestCheckPassphrase(t *testing.T) {
	cases := []struct {
		in   string
		weak bool
		err  bool
	}{
		{"", false, true},
		{"1234567", false, true},
		{"ççççççç", false, true}, // 7 runes, more bytes
		{"12345678", true, false},
		{"12345678901", true, false},
		{"123456789012", false, false},
	}
	for _, c := range cases {
		weak, err := CheckPassphrase(c.in)
		if (err != nil) != c.err || weak != c.weak {
			t.Errorf("CheckPassphrase(%q) = %v %v", c.in, weak, err)
		}
		if c.err && !errors.Is(err, ErrWeakPassphrase) {
			t.Errorf("CheckPassphrase(%q) err = %v", c.in, err)
		}
	}
}
