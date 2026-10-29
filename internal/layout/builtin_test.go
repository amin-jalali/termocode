package layout

import "testing"

func TestBuiltinsAreListedAndValid(t *testing.T) {
	es, _ := testStore(t).List()
	var got []string
	for _, e := range es {
		if e.Source == Builtin {
			got = append(got, e.Name)
		}
		if err := e.Validate(); err != nil {
			t.Errorf("%s: %v", e.Name, err)
		}
		if e.Source == Builtin && e.Description == "" {
			t.Errorf("%s has no description", e.Name)
		}
	}
	want := []string{"debug", "default", "focus", "review"}
	if len(got) != len(want) {
		t.Fatalf("built-ins = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] || !IsBuiltin(want[i]) {
			t.Fatalf("built-ins = %v, want %v", got, want)
		}
	}
}

func TestBuiltinCannotBeDeletedAndIsNotShared(t *testing.T) {
	s := testStore(t)
	e, ok := s.Find("focus")
	if !ok {
		t.Fatal("focus missing")
	}
	if err := s.Delete(e); err == nil {
		t.Error("built-in deleted")
	}
	e.Sidebar.Open = true // a caller edits its copy ...
	e2, _ := s.Find("focus")
	if e2.Sidebar.Open { // ... and the table stays as it was
		t.Error("the built-in table was changed through a copy")
	}
}

func TestFileWithABuiltinNameWinsOverTheBuiltin(t *testing.T) {
	s := testStore(t)
	s.Save(User, Layout{Name: "debug", Description: "my debug"})
	e, _ := s.Find("debug")
	if e.Source != User || e.Description != "my debug" {
		t.Fatalf("find = %+v", e)
	}
	if err := s.Delete(e); err != nil {
		t.Fatal(err)
	}
	if e, _ := s.Find("debug"); e.Source != Builtin {
		t.Fatalf("after delete: %+v", e)
	}
}
