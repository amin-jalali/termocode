package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBQuickWhere(t *testing.T) {
	for _, tc := range []struct {
		driver, col string
		cell        dbclient.Cell
		old, want   string
	}{
		{"postgres", "plan", dbclient.Cell{Text: "pro"}, "", `"plan" = 'pro'`},
		{"postgres", "id", dbclient.Cell{Text: "42", Kind: "number"}, "", `"id" = 42`},
		{"postgres", "id", dbclient.Cell{Text: "4 OR 1=1", Kind: "number"}, "", `"id" = '4 OR 1=1'`},
		{"postgres", "note", dbclient.Cell{Text: "it's"}, "", `"note" = 'it''s'`},
		{"postgres", "note", dbclient.Cell{Text: `a\b`}, "", `"note" = 'a\b'`},
		{"mysql", "note", dbclient.Cell{Text: `a\'b`}, "", "`note` = 'a\\\\''b'"},
		{"sqlite", "deleted_at", dbclient.Cell{Null: true}, "", `"deleted_at" IS NULL`},
		{"mysql", "plan", dbclient.Cell{Text: "pro"}, "id > 3", "(id > 3) AND `plan` = 'pro'"},
	} {
		got, err := dbQuickWhere(tc.driver, tc.col, tc.cell, tc.old)
		if err != nil || got != tc.want {
			t.Errorf("%s %s %+v: got %s, %v; want %s", tc.driver, tc.col, tc.cell, got, err, tc.want)
			continue
		}
		if err := dbWhereCheck(tc.driver, got); err != nil {
			t.Errorf("%s: the filter check refuses %s: %v", tc.driver, got, err)
		}
	}
}

func TestDBQuickWhereRefusesWhatTheCheckRefuses(t *testing.T) {
	if _, err := dbQuickWhere("postgres", "x", dbclient.Cell{Text: "a"}, "1=1; DROP TABLE t"); err == nil {
		t.Fatal("a bad old filter was combined")
	}
	if _, err := dbQuickWhere("redis", "x", dbclient.Cell{Text: "a"}, ""); err == nil {
		t.Fatal("a driver without SQL got a filter")
	}
}
