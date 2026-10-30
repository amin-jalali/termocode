package webdevices

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return Open(filepath.Join(t.TempDir(), "web"))
}

func dev(id, role string) Device {
	return Device{ID: id, Name: "Phone " + id, Role: role, Created: time.Unix(1700000000, 0).UTC()}
}

// The store is 0600 in a 0700 folder, written atomically; it holds no
// secret, only public keys and names (WEB-32).
func TestStoreMode0600(t *testing.T) {
	s := testStore(t)
	if err := s.Add(dev("dev-1", RoleViewer)); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	fi, err := os.Stat(s.Path())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file %v %v", fi.Mode(), err)
	}
	di, err := os.Stat(filepath.Dir(s.Path()))
	if err != nil || di.Mode().Perm() != 0o700 {
		t.Fatalf("dir %v %v", di.Mode(), err)
	}
	ents, _ := os.ReadDir(filepath.Dir(s.Path()))
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".devices-") {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
}

func TestStoreAddListRemove(t *testing.T) {
	s := testStore(t)
	if ds, err := s.List(); err != nil || len(ds) != 0 {
		t.Fatalf("empty store: %v %v", ds, err)
	}
	for _, d := range []Device{dev("dev-1", RoleViewer), dev("dev-2", RoleControl)} {
		if err := s.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Add(dev("dev-1", RoleControl)); err == nil {
		t.Fatal("a second device with the same id was added")
	}
	ds, _ := s.List()
	if len(ds) != 2 || ds[0].ID != "dev-1" || ds[1].Role != RoleControl {
		t.Fatalf("list %+v", ds)
	}
	got, ok, err := s.Remove("dev-1")
	if err != nil || !ok || got.ID != "dev-1" {
		t.Fatalf("remove: %+v %v %v", got, ok, err)
	}
	if _, ok, _ := s.Remove("dev-1"); ok {
		t.Fatal("removed twice")
	}
	if _, ok, _ := s.Get("dev-1"); ok {
		t.Fatal("a removed device is still there (revoke, T30)")
	}
	if d, ok, _ := s.Get("dev-2"); !ok || d.Role != RoleControl {
		t.Fatal("the other device went too")
	}
}

// A damaged file is an empty list, never a crash or a refusal to pair;
// the damaged bytes are kept aside for the user.
func TestCorruptFileIsEmptyNotCrash(t *testing.T) {
	s := testStore(t)
	if err := os.MkdirAll(filepath.Dir(s.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ds, err := s.List(); err != nil || len(ds) != 0 {
		t.Fatalf("corrupt: %v %v", ds, err)
	}
	if err := s.Add(dev("dev-1", RoleViewer)); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(s.Path() + ".bad"); err != nil || string(b) != "{not json" {
		t.Fatalf("damaged file not kept: %q %v", b, err)
	}
}

// Two processes (the gateway pairs, the UI revokes) change the file at
// the same time: no change is lost (flock around read-change-write).
func TestStoreConcurrentUpdates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "web")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := Open(dir).Add(dev("dev-"+string(rune('a'+i)), RoleViewer)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if ds, _ := Open(dir).List(); len(ds) != 16 {
		t.Fatalf("%d devices, want 16", len(ds))
	}
}

// Names come from a browser: control and bidi characters go, the length
// is capped, and an empty one gets a plain default.
func TestDeviceNameCleaned(t *testing.T) {
	for in, want := range map[string]string{
		"iPhone · Safari":        "iPhone · Safari",
		"evil‮gnp.exe":           "evilgnp.exe",
		"a\x1b[31mred":           "a[31mred",
		"":                       "Browser",
		"   ":                    "Browser",
		strings.Repeat("x", 100): strings.Repeat("x", 47) + "…",
		"tab\tname\nline":        "tab name line",
	} {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

// The store has room for MaxDevices; one more is refused, not dropped.
func TestStoreCap(t *testing.T) {
	s := testStore(t)
	for i := 0; i < MaxDevices; i++ {
		if err := s.Add(dev("dev-"+strings.Repeat("x", i+1), RoleViewer)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Add(dev("dev-last", RoleViewer)); err != ErrFull {
		t.Fatalf("err = %v, want ErrFull", err)
	}
}

func TestForgetAll(t *testing.T) {
	s := testStore(t)
	_ = s.Add(dev("dev-1", RoleViewer))
	if err := s.ForgetAll(); err != nil {
		t.Fatal(err)
	}
	if ds, _ := s.List(); len(ds) != 0 {
		t.Fatal("devices left after forget-all")
	}
}
