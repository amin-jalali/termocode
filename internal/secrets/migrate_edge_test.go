package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// Spec (U19 SS-4): FromPlain refuses what it can not move, so Remove
// never deletes a secret that was not copied.
func TestFromPlainRefusesWhatItCanNotMove(t *testing.T) {
	cases := map[string]struct{ content, want string }{
		"unknown field": {`{"keys":{"a":"` + canary + `"},"weird":{}}`, `unknown field "weird"`},
		"field not obj": {`{"keys":"` + canary + `"}`, "keys"},
		"bad name":      {`{"keys":{"bad name":"` + canary + `"}}`, "credentials.json"},
		"value not str": {`{"keys":{"a":42}}`, "is not a string"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, plain := migrateDir(t, c.content)
			_, err := FromPlain(plain)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			noValueIn(t, err)
		})
	}
}

// Spec (U19 SS-4): a null field is empty, not an error.
func TestFromPlainSkipsNullFields(t *testing.T) {
	_, plain := migrateDir(t, `{"keys":{"openai":"`+canary+`"},"claude_oauth":null,"mcp_secrets":null}`)
	p, err := FromPlain(plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Values) != 1 || p.Values["ai.key.openai"] != canary {
		t.Fatalf("Values = %v", keys(p.Values))
	}
}

// Spec (U19 SS-4): Migrate refuses bad input before it changes anything.
func TestMigrateRejectsBadInput(t *testing.T) {
	dir, plain := migrateDir(t, migrateFixture)
	if _, err := Migrate(MigrateOptions{RemovePlain: true, To: "keyring"}); err == nil || !strings.Contains(err.Error(), "no config dir") {
		t.Fatalf("no dir: err = %v", err)
	}
	if _, err := Migrate(MigrateOptions{RemovePlain: true, Options: Options{Dir: dir}, To: "plain"}); !errors.Is(err, ErrUnknownBackend) {
		t.Fatalf("to plain: err = %v", err)
	}
	if got := readFile(t, plain); got != migrateFixture {
		t.Fatal("a refused move changed the plain file")
	}
	_, bad := migrateDir(t, `{"weird":1}`)
	if _, err := Migrate(MigrateOptions{RemovePlain: true, Options: Options{Dir: filepath.Dir(filepath.Dir(bad)), PlainPath: bad,
		KeyringProbe: func() bool { return true }}, To: "keyring"}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("bad plain file: err = %v", err)
	}
}

// Spec (U19 SS-4): with no PlainPath, Migrate reads Dir/ai/credentials.json.
func TestMigrateDefaultPlainPath(t *testing.T) {
	keyring.MockInit()
	dir, plain := migrateDir(t, migrateFixture)
	s, err := Migrate(MigrateOptions{RemovePlain: true, Options: Options{Dir: dir, KeyringProbe: func() bool { return true }}, To: "keyring"})
	if err != nil {
		t.Fatal(err)
	}
	checkValues(t, s)
	gone(t, plain)
}

// Spec (U19 SS-4): every failure before the delete step leaves the plain
// file, the index (or its absence) and the age file as they were.
func TestMigrateFailuresRollBack(t *testing.T) {
	cases := map[string]struct {
		prep func(t *testing.T, dir string, o *MigrateOptions)
		want string
	}{
		"damaged index": {func(t *testing.T, dir string, o *MigrateOptions) {
			corruptIndex(t, dir)
		}, "not valid JSON"},
		"wrong passphrase": {func(t *testing.T, dir string, o *MigrateOptions) {
			a := newAgeFile(filepath.Join(dir, AgeFile), 10)
			if err := a.Unlock(pass); err != nil {
				t.Fatal(err)
			}
			a.Lock()
			o.To, o.Pass = "age", []byte("another long passphrase")
		}, "passphrase"},
		"keyring refuses": {func(t *testing.T, dir string, o *MigrateOptions) {
			keyring.MockInitWithError(errors.New("keyring refused"))
			t.Cleanup(keyring.MockInit)
		}, "keyring refused"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			keyring.MockInit()
			dir, plain := migrateDir(t, migrateFixture)
			o := MigrateOptions{RemovePlain: true, Options: Options{Dir: dir, PlainPath: plain, KeyringProbe: func() bool { return true }}, To: "keyring"}
			c.prep(t, dir, &o)
			idxBefore, idxErr := os.ReadFile(filepath.Join(dir, IndexFile))
			ageBefore, ageErr := os.ReadFile(filepath.Join(dir, AgeFile))
			s, err := Migrate(o)
			if s != nil || err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Migrate = %v, %v; want %q", s, err, c.want)
			}
			noValueIn(t, err)
			if strings.Contains(err.Error(), "FIXTURE") {
				t.Fatalf("the error shows a value: %v", err)
			}
			if got := readFile(t, plain); got != migrateFixture {
				t.Fatal("the plain file changed")
			}
			idxAfter, idxErr2 := os.ReadFile(filepath.Join(dir, IndexFile))
			if (idxErr == nil) != (idxErr2 == nil) || string(idxAfter) != string(idxBefore) {
				t.Fatalf("index changed: %q (%v) -> %q (%v)", idxBefore, idxErr, idxAfter, idxErr2)
			}
			ageAfter, ageErr2 := os.ReadFile(filepath.Join(dir, AgeFile))
			if (ageErr == nil) != (ageErr2 == nil) || string(ageAfter) != string(ageBefore) {
				t.Fatal("the age file changed")
			}
		})
	}
}

// Spec (U19 SS-4): a plain file that can not be removed after the copy
// gives the open store and ErrNotRemoved.
func TestMigrateNotRemoved(t *testing.T) {
	keyring.MockInit()
	dir, plain := migrateDir(t, migrateFixture)
	t.Cleanup(setRemove(func(string) error { return errors.New("busy") }))
	s, err := Migrate(MigrateOptions{RemovePlain: true, Options: Options{Dir: dir, PlainPath: plain, KeyringProbe: func() bool { return true }}, To: "keyring"})
	if !errors.Is(err, ErrNotRemoved) || s == nil {
		t.Fatalf("Migrate = %v, %v", s, err)
	}
	checkValues(t, s)
}

// Spec (U19 SS-4): Finish does nothing without an open safe store or a
// readable plain file, and reports a delete that fails.
func TestFinishEdgeCases(t *testing.T) {
	keyring.MockInit()
	dir, plain := migrateDir(t, migrateFixture)
	if done, err := Finish(nil, plain); done || err != nil {
		t.Fatalf("nil store: %v %v", done, err)
	}
	locked := NewStore(newAgeFile(filepath.Join(dir, AgeFile), 10), Options{Dir: dir})
	if done, err := Finish(locked, plain); done || err != nil {
		t.Fatalf("locked store: %v %v", done, err)
	}
	s, err := Open(Options{Dir: dir, Backend: "keyring", KeyringProbe: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	if err := SetAll(s, wantMigrated); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(setRemove(func(string) error { return errors.New("busy") }))
	if done, err := Finish(s, plain); done || err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("failed delete: %v %v", done, err)
	}
	if err := os.WriteFile(plain, []byte(`{"weird":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if done, err := Finish(s, plain); done || err == nil {
		t.Fatalf("bad plain file: %v %v", done, err)
	}
}

// Spec (U19 SS-4): Remove of a missing file is an error and writes no
// marker.
func TestRemoveMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := Remove(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v", err)
	}
	if Migrated(path) {
		t.Fatal("a marker was written for a file that was not there")
	}
}

// Spec (U19 SS-4): the "later" count reads 0 from a damaged index and
// refuses to overwrite it.
func TestLaterCountDamagedIndex(t *testing.T) {
	dir := t.TempDir()
	corruptIndex(t, dir)
	if n := LaterCount(dir); n != 0 {
		t.Fatalf("LaterCount = %d", n)
	}
	if _, err := AddLater(dir); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("AddLater err = %v", err)
	}
	if got := readFile(t, filepath.Join(dir, IndexFile)); got != "{oops" {
		t.Fatal("the damaged index was overwritten")
	}
}
