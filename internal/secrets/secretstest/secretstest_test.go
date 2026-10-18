package secretstest

import (
	"errors"
	"testing"

	"github.com/amin-jalali/termocode/internal/secrets"
)

func TestMemoryStore(t *testing.T) {
	s := New(t, nil)
	if s.Backend() != "memory" || s.Locked() {
		t.Fatalf("backend %q locked %v", s.Backend(), s.Locked())
	}
	if err := s.Set("ai.key.x", "v1"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.Get("ai.key.x"); v != "v1" || !ok || err != nil {
		t.Fatalf("Get = %q %v %v", v, ok, err)
	}
}

func TestLockedMemory(t *testing.T) {
	m := NewLockedMemory("the passphrase")
	s := New(t, m)
	if !s.Locked() {
		t.Fatal("want locked")
	}
	if _, _, err := s.Get("ai.key.x"); !errors.Is(err, secrets.ErrLocked) {
		t.Fatalf("err = %v", err)
	}
	if err := s.Unlock("nope"); !errors.Is(err, secrets.ErrWrongPassphrase) {
		t.Fatalf("err = %v", err)
	}
	if err := s.Unlock("the passphrase"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("ai.key.x", "v"); err != nil {
		t.Fatal(err)
	}
	s.Lock()
	if !s.Locked() {
		t.Fatal("Lock")
	}
	if got := m.Values()["ai.key.x"]; got != "v" {
		t.Fatalf("Values = %q (the memory keeps data across Lock, like a file)", got)
	}
}
