package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeStore is a store over a fakeBackend with its index in a temp dir.
func fakeStore(t *testing.T) (*store, *fakeBackend, string) {
	t.Helper()
	dir := t.TempDir()
	f := newFakeBackend()
	var seen []string
	s := NewStore(f, Options{Dir: dir, Register: func(v string) { seen = append(seen, v) },
		Getenv: func(string) string { return "" }}).(*store)
	return s, f, dir
}

func corruptIndex(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, IndexFile), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Spec (U19 SS-2): a passphrase the backend refuses keeps the store
// locked and the error is the backend's.
func TestStoreUnlockError(t *testing.T) {
	s, f, _ := fakeStore(t)
	f.locked, f.unlockErr = true, ErrWrongPassphrase
	used := 0
	s.onUse = func() { used++ }
	if err := s.Unlock("nope"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("err = %v", err)
	}
	if !s.Locked() || used != 0 {
		t.Fatalf("locked=%v used=%d after a failed unlock", s.Locked(), used)
	}
}

// Spec (U19 SS-2): a damaged index fails reads and hides the names; it
// never hands out a wrong value.
func TestStoreCorruptIndex(t *testing.T) {
	s, f, dir := fakeStore(t)
	f.data["ai.key.openai"] = canary
	corruptIndex(t, dir)
	if _, _, err := s.Get("ai.key.openai"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Get err = %v", err)
	}
	if n := s.Names(); n != nil {
		t.Fatalf("Names = %v on a damaged index", n)
	}
	if err := s.Set("mcp.account.gh", "amin"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Set public err = %v", err)
	}
	if err := s.setAll(map[string]string{"mcp.account.gh": "amin"}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("setAll err = %v", err)
	}
}

// Spec (U19 SS-2): a reference that can not be read fails with the name
// and the reference, never a value.
func TestStoreBadReferenceInIndex(t *testing.T) {
	s, _, dir := fakeStore(t)
	idx := `{"version":1,"entries":{"mcp.secret.A":{"kind":"ref","ref":"not a ref"},"mcp.secret.B":{"kind":"ref","ref":"${env:EMPTY_VAR}"}}}`
	if err := os.WriteFile(filepath.Join(dir, IndexFile), []byte(idx), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("mcp.secret.A"); err == nil || !strings.HasPrefix(err.Error(), "mcp.secret.A: ") {
		t.Fatalf("bad ref: err = %v", err)
	}
	_, ok, err := s.Get("mcp.secret.B")
	if ok || err == nil || !strings.Contains(err.Error(), "mcp.secret.B: ${env:EMPTY_VAR}: variable is empty") {
		t.Fatalf("empty env: ok=%v err=%v", ok, err)
	}
}

// Spec (U19 SS-2): backend failures reach the caller and leave the index
// as it was.
func TestStoreBackendErrors(t *testing.T) {
	boom := errors.New("backend down")
	t.Run("get", func(t *testing.T) {
		s, f, _ := fakeStore(t)
		f.getErr = boom
		if _, ok, err := s.Get("ai.key.openai"); ok || !errors.Is(err, boom) {
			t.Fatalf("Get = %v %v", ok, err)
		}
	})
	t.Run("set value", func(t *testing.T) {
		s, f, dir := fakeStore(t)
		f.setErr = boom
		if err := s.Set("ai.key.openai", canary); !errors.Is(err, boom) {
			t.Fatalf("Set err = %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, IndexFile)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("a failed Set wrote the index")
		}
	})
	t.Run("set reference", func(t *testing.T) {
		s, f, _ := fakeStore(t)
		f.delErr = boom
		if err := s.Set("mcp.secret.GH", "${env:GH_TOKEN}"); !errors.Is(err, boom) {
			t.Fatalf("Set ref err = %v", err)
		}
		if n := s.Names(); len(n) != 0 {
			t.Fatalf("Names = %v after a failed Set", n)
		}
	})
	t.Run("set many", func(t *testing.T) {
		s, f, _ := fakeStore(t)
		f.setErr = boom
		if err := s.setAll(map[string]string{"ai.key.a": canary, "ai.key.b": canary}); !errors.Is(err, boom) {
			t.Fatalf("setAll err = %v", err)
		}
		if n := s.Names(); len(n) != 0 {
			t.Fatalf("Names = %v after a failed setAll", n)
		}
	})
	t.Run("delete", func(t *testing.T) {
		s, f, _ := fakeStore(t)
		if err := s.Set("ai.key.openai", canary); err != nil {
			t.Fatal(err)
		}
		f.delErr = boom
		if err := s.Delete("ai.key.openai"); !errors.Is(err, boom) {
			t.Fatalf("Delete err = %v", err)
		}
		if n := s.Names(); !slices.Equal(n, []string{"ai.key.openai"}) {
			t.Fatalf("Names = %v; a failed delete dropped the entry", n)
		}
	})
}

// Spec (U19 SS-2): a locked store refuses every change.
func TestStoreLockedRefusesChanges(t *testing.T) {
	s, f, _ := fakeStore(t)
	f.locked = true
	if err := s.setAll(map[string]string{"ai.key.a": canary}); !errors.Is(err, ErrLocked) {
		t.Fatalf("setAll err = %v", err)
	}
	if err := s.Delete("ai.key.a"); !errors.Is(err, ErrLocked) {
		t.Fatalf("Delete err = %v", err)
	}
	if err := s.Delete("bad name!"); err == nil || errors.Is(err, ErrLocked) {
		t.Fatalf("Delete of a bad name: err = %v", err)
	}
}

// Spec (U19 SS-6): an account label an older version kept in the
// backend moves to the index; the backend copy is deleted.
func TestStorePublicNameDropsOldBackendCopy(t *testing.T) {
	for _, many := range []bool{false, true} {
		s, f, dir := fakeStore(t)
		f.data["mcp.account.gh"] = "old"
		idx := `{"version":1,"entries":{"mcp.account.gh":{"kind":"value","backend":"fake"}}}`
		if err := os.WriteFile(filepath.Join(dir, IndexFile), []byte(idx), 0o600); err != nil {
			t.Fatal(err)
		}
		var err error
		if many {
			err = s.setAll(map[string]string{"mcp.account.gh": "amin"})
		} else {
			err = s.Set("mcp.account.gh", "amin")
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := f.data["mcp.account.gh"]; ok || !slices.Contains(f.deleted, "mcp.account.gh") {
			t.Fatalf("many=%v: the old backend copy stayed", many)
		}
		if v, ok, err := s.Get("mcp.account.gh"); v != "amin" || !ok || err != nil {
			t.Fatalf("many=%v: Get = %q %v %v", many, v, ok, err)
		}
	}
}

// Spec (U19 SS-2): SetAll on a Store that is not the package's own
// writes each name through Set (values before references).
func TestSetAllOnOtherStore(t *testing.T) {
	s, f, _ := fakeStore(t)
	if err := SetAll(otherStore{s}, map[string]string{"ai.key.a": canary, "mcp.secret.R": "${env:X}"}); err != nil {
		t.Fatal(err)
	}
	if f.data["ai.key.a"] != canary {
		t.Fatal("the value did not reach the backend")
	}
	if e := Entries(filepath.Dir(s.indexPath))["mcp.secret.R"]; e.Kind != KindRef {
		t.Fatalf("reference entry = %+v", e)
	}
	f.setErr = errors.New("full")
	if err := SetAll(otherStore{s}, map[string]string{"ai.key.b": canary}); err == nil {
		t.Fatal("a failed Set was not reported")
	}
	f.setErr = nil
	if err := SetAll(otherStore{s}, map[string]string{"mcp.secret.R": "${bad:X}"}); err == nil {
		t.Fatal("a bad reference was not reported")
	}
	if err := SetAll(s, map[string]string{"bad name!": "x"}); err == nil {
		t.Fatal("a bad name was not reported")
	}
}
