package secrets

import "testing"

// Spec (U19 SS-8, the lock timer): every use of an open store — an
// unlock, a Get, a Set, a Delete — calls Options.OnUse, so the app can
// restart its idle count without a timer per use. A refused call on a
// locked store is not a use.
func TestOnUseCalledOnEveryUse(t *testing.T) {
	n := 0
	s := openAge(t, t.TempDir(), Options{OnUse: func() { n++ }})
	if _, _, err := s.Get("ai.key.x"); err == nil {
		t.Fatal("Get on a locked store worked")
	}
	if n != 0 {
		t.Fatalf("a refused Get counted as a use (%d)", n)
	}
	if err := s.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("after Unlock: %d uses, want 1", n)
	}
	if err := s.Set("ai.key.x", "v-SECRET"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("ai.key.x"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("ai.key.x"); err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("after Set, Get, Delete: %d uses, want 4", n)
	}
}
