package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// switchValues are the secrets of the switch fixture: values, a
// reference and a public account label.
var switchValues = map[string]string{
	"ai.key.openai":       "sk-SWITCH-SECRET-1",
	"ai.claude_oauth":     `{"access_token":"at-SWITCH-SECRET-2"}`,
	"mcp.secret.GH":       "${env:GH_TOKEN}",
	"mcp.account.linear":  "amin",
	"db.main.password":    "pw-SWITCH-SECRET-3",
	"mcp.oauth.linear":    `{"token":"tok-SWITCH-SECRET-4"}`,
	"mcp.secret.LITERALX": "lit-SWITCH-SECRET-5",
}

func switchOpts(dir string) Options {
	return Options{
		Dir:          dir,
		PlainPath:    filepath.Join(dir, "ai", "credentials.json"),
		Getenv:       func(k string) string { return map[string]string{"GH_TOKEN": "env-gh"}[k] },
		KeyringProbe: func() bool { return true },
	}
}

// switchFrom opens an open store of backend with the fixture in it.
func switchFrom(t *testing.T, backend string) (Store, string) {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	o := switchOpts(dir)
	o.Backend = backend
	s, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	if err := SetAll(s, switchValues); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

// checkSwitched: every name reads back the same (references resolved)
// from s and from a store opened again with the new setting.
func checkSwitched(t *testing.T, s Store, dir, to string) {
	t.Helper()
	if s.Backend() != to {
		t.Fatalf("backend %s, want %s", s.Backend(), to)
	}
	o := switchOpts(dir)
	o.Backend = to
	again, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	for _, st := range []Store{s, again} {
		for n, want := range switchValues {
			if n == "mcp.secret.GH" {
				want = "env-gh"
			}
			if got, ok, err := st.Get(n); got != want || !ok || err != nil {
				t.Fatalf("%s: Get(%s) = %q %v %v", to, n, got, ok, err)
			}
		}
	}
	idx, err := loadIndex(filepath.Join(dir, IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	if idx.Backend != to {
		t.Fatalf("index backend %q", idx.Backend)
	}
	if e := idx.Entries["mcp.secret.GH"]; e.Kind != KindRef || e.Ref != "${env:GH_TOKEN}" {
		t.Fatalf("the reference moved as %+v", e)
	}
	b, _ := os.ReadFile(filepath.Join(dir, IndexFile))
	if strings.Contains(string(b), "SWITCH-SECRET") {
		t.Fatal("a value is in the index")
	}
}

func keyringHas(name string) bool {
	_, err := keyring.Get(keyringService, name)
	return err == nil
}

func gone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s still exists (%v)", path, err)
	}
}

// Spec (U19 SS-8): Switch copies every name (values as values,
// references as references) to the new backend, checks it, records it
// in the index, and only then removes the old copies.
func TestSwitchBackends(t *testing.T) {
	for _, c := range []struct{ from, to string }{
		{"age", "keyring"}, {"keyring", "age"}, {"age", "plain"}, {"plain", "age"}, {"keyring", "plain"}, {"plain", "keyring"},
	} {
		t.Run(c.from+"-to-"+c.to, func(t *testing.T) {
			from, dir := switchFrom(t, c.from)
			o := switchOpts(dir)
			committed := false
			s, err := Switch(from, SwitchOptions{RemovePlain: true, Options: o, To: c.to, Pass: []byte(pass),
				Commit: func(s Store) error {
					committed = true
					if _, err := os.Stat(filepath.Join(dir, IndexFile)); err != nil {
						return err
					}
					return nil
				}})
			if err != nil {
				t.Fatal(err)
			}
			if !committed {
				t.Fatal("Commit did not run")
			}
			checkSwitched(t, s, dir, c.to)
			switch c.from {
			case "age":
				gone(t, filepath.Join(dir, AgeFile))
				if !from.Locked() {
					t.Error("the old age store is still open")
				}
			case "keyring":
				for n := range switchValues {
					if keyringHas(n) {
						t.Errorf("keyring still holds %s", n)
					}
				}
			case "plain":
				gone(t, o.PlainPath)
				if !Migrated(o.PlainPath) {
					t.Error("no marker after leaving the plain file")
				}
			}
			if c.to == "plain" {
				if b := readFile(t, o.PlainPath); !strings.Contains(b, `"mcp_accounts"`) {
					t.Errorf("the account is not in the plain file:\n%s", b)
				}
			} else if c.to == "keyring" && keyringHas("mcp.account.linear") {
				t.Error("an account label went to the keyring")
			}
			noTemps(t, dir)
		})
	}
}

// Spec (U19 SS-8): a plain file that predates the index moves whole.
func TestSwitchFromPlainFileWithoutIndex(t *testing.T) {
	keyring.MockInit()
	dir := t.TempDir()
	o := switchOpts(dir)
	if err := os.MkdirAll(filepath.Dir(o.PlainPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.PlainPath, []byte(`{"keys":{"openai":"sk-OLD-SECRET"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	o.Backend = "plain"
	from, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Switch(from, SwitchOptions{RemovePlain: true, Options: switchOpts(dir), To: "keyring"})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := s.Get("ai.key.openai"); v != "sk-OLD-SECRET" || !ok {
		t.Fatalf("Get = %q %v", v, ok)
	}
	gone(t, o.PlainPath)
}

// Spec (U19 SS-8): on any failure nothing is deleted, the index is as
// it was and the old store keeps working; a new target file is removed.
func TestSwitchFailuresKeepOldStore(t *testing.T) {
	cases := map[string]struct {
		to   string
		pass string
		prep func(t *testing.T, dir string, o *SwitchOptions) func()
		want string
	}{
		"check fails": {to: "plain", prep: func(t *testing.T, dir string, o *SwitchOptions) func() {
			return setSwitchVerify(func(Backend, map[string]string) error { return errors.New("ai.key.openai differs") })
		}, want: "differs"},
		"commit fails": {to: "plain", prep: func(t *testing.T, dir string, o *SwitchOptions) func() {
			o.Commit = func(Store) error { return errors.New("config.json is read-only") }
			return func() {}
		}, want: "read-only"},
		"no keyring": {to: "keyring", prep: func(t *testing.T, dir string, o *SwitchOptions) func() {
			o.KeyringProbe = func() bool { return false }
			return func() {}
		}, want: "keyring"},
		"same backend": {to: "age", want: "already"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			from, dir := switchFrom(t, "age")
			idxBefore := readFile(t, filepath.Join(dir, IndexFile))
			ageBefore := readFile(t, filepath.Join(dir, AgeFile))
			o := SwitchOptions{RemovePlain: true, Options: switchOpts(dir), To: c.to, Pass: []byte(pass)}
			if c.prep != nil {
				defer c.prep(t, dir, &o)()
			}
			s, err := Switch(from, o)
			if err == nil || s != nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Switch = %v, %v; want an error with %q", s, err, c.want)
			}
			if strings.Contains(err.Error(), "SWITCH-SECRET") {
				t.Fatalf("the error has a value: %v", err)
			}
			if got := readFile(t, filepath.Join(dir, IndexFile)); got != idxBefore {
				t.Fatalf("index changed:\n%s", got)
			}
			if got := readFile(t, filepath.Join(dir, AgeFile)); got != ageBefore {
				t.Fatal("the age file changed")
			}
			gone(t, switchOpts(dir).PlainPath)
			if from.Locked() {
				t.Fatal("the old store locked")
			}
			if v, _, err := from.Get("ai.key.openai"); v != "sk-SWITCH-SECRET-1" || err != nil {
				t.Fatalf("old store Get = %q %v", v, err)
			}
			noTemps(t, dir)
		})
	}
}

// Spec (U19 SS-8): a failure while removing the old copies comes after
// the switch: the new store is returned with ErrOldNotRemoved.
func TestSwitchOldNotRemoved(t *testing.T) {
	from, dir := switchFrom(t, "age")
	t.Cleanup(setRemove(func(string) error { return errors.New("busy") }))
	s, err := Switch(from, SwitchOptions{RemovePlain: true, Options: switchOpts(dir), To: "keyring"})
	if !errors.Is(err, ErrOldNotRemoved) || s == nil || s.Backend() != "keyring" {
		t.Fatalf("Switch = %v, %v", s, err)
	}
	if !from.Locked() {
		t.Fatal("the old store stays open")
	}
}

// Spec (U19 SS-8): the old store must be open first.
func TestSwitchNeedsOpenStore(t *testing.T) {
	from, dir := switchFrom(t, "age")
	from.Lock()
	if _, err := Switch(from, SwitchOptions{RemovePlain: true, Options: switchOpts(dir), To: "keyring"}); !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v", err)
	}
}
