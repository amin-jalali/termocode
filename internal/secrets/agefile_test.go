package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pass = "correct horse battery"

func newAge(t *testing.T, dir string) *ageFile {
	t.Helper()
	return newAgeFile(filepath.Join(dir, "secrets.age"), 0)
}

func TestAgeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a := newAge(t, dir)
	if !a.Locked() {
		t.Fatal("a new age backend must start locked")
	}
	if err := a.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	if err := a.Set("ai.key.anthropic", "sk-ant-SECRET-1"); err != nil {
		t.Fatal(err)
	}
	if err := a.Set("db.local", "pg-SECRET-2"); err != nil {
		t.Fatal(err)
	}
	if err := a.Delete("db.local"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "secrets.age")
	if m := mode(t, path); m != 0o600 {
		t.Errorf("age file mode = %o, want 600", m)
	}
	if strings.Contains(readFile(t, path), "SECRET") {
		t.Fatal("age file holds a plain value")
	}
	noTemps(t, dir)

	// A new process: unlock again and read back.
	b := newAge(t, dir)
	if err := b.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	v, ok, err := b.Get("ai.key.anthropic")
	if err != nil || !ok || v != "sk-ant-SECRET-1" {
		t.Fatalf("Get = %q %v %v", v, ok, err)
	}
	if _, ok, _ := b.Get("db.local"); ok {
		t.Fatal("deleted name came back")
	}
}

func TestAgeWorkFactorFloor(t *testing.T) {
	if a := newAgeFile("x", 0); a.work != defaultWorkFactor {
		t.Errorf("0 → %d, want the default", a.work)
	}
	if a := newAgeFile("x", 1); a.work != minWorkFactor {
		t.Errorf("1 → %d, want the floor", a.work)
	}
}

func TestAgeLockForgets(t *testing.T) {
	a := newAge(t, t.TempDir())
	if err := a.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	if err := a.Set("x", "v"); err != nil {
		t.Fatal(err)
	}
	held := a.pass
	a.Lock()
	if !a.Locked() || a.data != nil || a.pass != nil {
		t.Fatal("Lock must drop the passphrase and the values")
	}
	for _, c := range held {
		if c != 0 {
			t.Fatal("Lock must zero the passphrase bytes")
		}
	}
	if _, _, err := a.Get("x"); !errors.Is(err, ErrLocked) {
		t.Fatalf("Get err = %v, want ErrLocked", err)
	}
	if err := a.Set("x", "w"); !errors.Is(err, ErrLocked) {
		t.Fatalf("Set err = %v, want ErrLocked", err)
	}
	if err := a.Delete("x"); !errors.Is(err, ErrLocked) {
		t.Fatalf("Delete err = %v, want ErrLocked", err)
	}
}

func TestAgeWrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	a := newAge(t, dir)
	if err := a.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	if err := a.Set("x", "v"); err != nil {
		t.Fatal(err)
	}
	b := newAge(t, dir)
	err := b.Unlock("wrong passphrase!")
	if !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("err = %v, want ErrWrongPassphrase", err)
	}
	if !b.Locked() {
		t.Fatal("still locked after a wrong passphrase")
	}
	if err := b.Unlock(pass); err != nil {
		t.Fatalf("the right passphrase after a wrong one: %v", err)
	}
}

func TestAgeNewFileNeedsStrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	a := newAge(t, dir)
	if err := a.Unlock("short"); !errors.Is(err, ErrWeakPassphrase) {
		t.Fatalf("err = %v, want ErrWeakPassphrase", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets.age")); !os.IsNotExist(err) {
		t.Fatal("no file may be written for a refused passphrase")
	}
	// The first unlock creates the (empty) file, so the passphrase is fixed.
	if err := a.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets.age")); err != nil {
		t.Fatalf("first unlock must create the file: %v", err)
	}
}

func TestAgeCorruptFile(t *testing.T) {
	cases := map[string]func(t *testing.T, path string){
		"garbage": func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("this is not age"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"truncated": func(t *testing.T, path string) {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, b[:len(b)-20], 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"flipped byte": func(t *testing.T, path string) {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			b[len(b)-5] ^= 0xff
			if err := os.WriteFile(path, b, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, damage := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			a := newAge(t, dir)
			if err := a.Unlock(pass); err != nil {
				t.Fatal(err)
			}
			if err := a.Set("x", strings.Repeat("v", 200)); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "secrets.age")
			damage(t, path)
			err := newAge(t, dir).Unlock(pass)
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("err = %v, want ErrCorrupt", err)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error %q must name the file", err)
			}
		})
	}
}

func TestAgeMergesTwoWriters(t *testing.T) {
	dir := t.TempDir()
	a := newAge(t, dir)
	if err := a.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	b := newAge(t, dir)
	if err := b.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	if err := a.Set("one", "1"); err != nil {
		t.Fatal(err)
	}
	if err := b.Set("two", "2"); err != nil {
		t.Fatal(err)
	}
	// a sees b's write too (the file changed on disk).
	for _, n := range []string{"one", "two"} {
		if _, ok, err := a.Get(n); !ok || err != nil {
			t.Fatalf("a.Get(%s) = %v %v", n, ok, err)
		}
	}
}
