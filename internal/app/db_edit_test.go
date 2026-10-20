package app

import (
	"reflect"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBResolveKeyPrefersThePrimaryKey(t *testing.T) {
	cols := []dbclient.Column{{Name: "id"}, {Name: "Email"}, {Name: "org"}, {Name: "slug"}}
	if idx, why, _ := dbResolveKey(cols, []string{"org", "id"}, [][]string{{"email"}}); why != "" || !reflect.DeepEqual(idx, []int{2, 0}) {
		t.Fatalf("pk = %v %q", idx, why)
	}
	// No primary key: the first unique key whose columns are all there.
	if idx, why, _ := dbResolveKey(cols, nil, [][]string{{"missing"}, {"email"}}); why != "" || !reflect.DeepEqual(idx, []int{1}) {
		t.Fatalf("unique = %v %q", idx, why)
	}
	if _, why, detail := dbResolveKey(cols, []string{"uid"}, nil); why != "read-only: no key column" || detail == "" {
		t.Fatalf("missing key column: %q %q", why, detail)
	}
	if _, why, _ := dbResolveKey(cols, nil, nil); why != "read-only: no key" {
		t.Fatalf("no key: %q", why)
	}
	// A column twice in the result is not a key.
	dup := []dbclient.Column{{Name: "id"}, {Name: "id"}}
	if _, why, _ := dbResolveKey(dup, []string{"id"}, nil); why == "" {
		t.Fatal("an ambiguous key column was accepted")
	}
}

func TestDBComparableSkipsValuesThatDoNotRoundTrip(t *testing.T) {
	for _, c := range []struct {
		driver string
		col    dbclient.Column
		old    dbclient.Cell
		want   bool
	}{
		{"postgres", dbclient.Column{Type: "TEXT"}, dbclient.Cell{Text: "a"}, true},
		{"postgres", dbclient.Column{Type: "INT8"}, dbclient.Cell{Text: "1", Kind: "number"}, true},
		{"postgres", dbclient.Column{Type: "JSON"}, dbclient.Cell{Text: "{}", Kind: "json"}, false},
		{"postgres", dbclient.Column{Type: "FLOAT8"}, dbclient.Cell{Text: "0.1"}, false},
		{"postgres", dbclient.Column{Type: "BYTEA"}, dbclient.Cell{Text: "0x00", Kind: "binary"}, false},
		{"postgres", dbclient.Column{Type: "TIMESTAMPTZ"}, dbclient.Cell{Text: "2026-10-18T09:12:33Z", Kind: "time"}, true},
		{"mysql", dbclient.Column{Type: "DATETIME"}, dbclient.Cell{Text: "2026-10-18T09:12:33Z", Kind: "time"}, false},
		{"mysql", dbclient.Column{Type: "JSON"}, dbclient.Cell{Null: true}, true}, // IS NULL always works
	} {
		if got := dbComparable(c.driver, c.col, c.old); got != c.want {
			t.Errorf("%s %s %+v = %v, want %v", c.driver, c.col.Type, c.old, got, c.want)
		}
	}
}

func TestDBPlaceRunKeepsPinnedAndEditedTabs(t *testing.T) {
	s := &dbConsoleState{sortCol: -1}
	a := &dbRun{sql: "a"}
	s.placeRun(a)
	s.curTab().pinned = true
	b := &dbRun{sql: "b"}
	s.placeRun(b)
	s.curTab().edits = &dbEdits{cells: map[dbCellRef]dbclient.Cell{{0, 0}: {Text: "x"}}, deleted: map[int]bool{}}
	c := &dbRun{sql: "c"}
	s.placeRun(c)
	if len(s.tabs) != 3 || s.tabStrip() != "1p 2* [3]" {
		t.Fatalf("tabs = %q", s.tabStrip())
	}
	d := &dbRun{sql: "d"}
	s.placeRun(d) // replaces c only
	if len(s.tabs) != 3 || s.tabs[2].run != d {
		t.Fatalf("tabs = %q", s.tabStrip())
	}
	// A page of the same tab replaces it in place and drops its edits.
	s.useTab(1)
	again := &dbRun{sql: "b2", tab: s.tabs[1]}
	s.placeRun(again)
	if s.tab != 1 || s.tabs[1].run != again || s.tabs[1].dirty() || s.shown != again {
		t.Fatalf("in place: tab %d %q", s.tab, s.tabStrip())
	}
	// Never more than nine tabs.
	for i := 0; i < 12; i++ {
		s.placeRun(&dbRun{sql: "x"})
		s.curTab().pinned = true
	}
	if len(s.tabs) != dbMaxTabs {
		t.Fatalf("%d tabs", len(s.tabs))
	}
}
