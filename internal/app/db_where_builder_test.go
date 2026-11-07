package app

import "testing"

func TestDBBuildCondition(t *testing.T) {
	for _, tc := range []struct {
		driver, col, op, value, old string
		or                          bool
		want                        string
	}{
		{"postgres", "plan", "=", "pro", "", false, `"plan" = 'pro'`},
		{"postgres", "id", ">=", "10", "", false, `"id" >= '10'`},
		{"postgres", "note", "LIKE", "it's%", "", false, `"note" LIKE 'it''s%'`},
		{"mysql", "note", "<>", `a\b`, "", false, "`note` <> 'a\\\\b'"},
		{"sqlite", "deleted_at", "IS NULL", "ignored", "", false, `"deleted_at" IS NULL`},
		{"sqlite", "id", "IN", " 1, 2 ,,3", "", false, `"id" IN ('1', '2', '3')`},
		{"postgres", "plan", "=", "pro", "id > 3", false, `(id > 3) AND "plan" = 'pro'`},
		{"postgres", "plan", "=", "pro", "id > 3", true, `(id > 3) OR "plan" = 'pro'`},
	} {
		got, err := dbBuildCondition(tc.driver, tc.col, tc.op, tc.value, tc.old, tc.or)
		if err != nil || got != tc.want {
			t.Errorf("%s %s %s %q: got %s, %v; want %s", tc.driver, tc.col, tc.op, tc.value, got, err, tc.want)
		}
	}
	for _, bad := range []struct{ driver, col, op, value, old string }{
		{"postgres", "a", "DROP", "x", ""},         // not an operator
		{"postgres", "a", "IN", " , ", ""},         // IN without values
		{"postgres", "a", "=", "x", "1=1; DROP t"}, // a bad filter already set
		{"redis", "a", "=", "x", ""},               // no SQL
		{"postgres", "", "=", "x", ""},             // no column
	} {
		if got, err := dbBuildCondition(bad.driver, bad.col, bad.op, bad.value, bad.old, false); err == nil {
			t.Errorf("%+v: built %s", bad, got)
		}
	}
}

func TestDBWhereBuilderFlow(t *testing.T) {
	withConfigDir(t)
	m := dbColumnsModel()
	tv := m.dbConsole().shown.tv
	m.databasePageKey("b", 0, 0)
	if !m.pickerOpen || m.pickerKind != pickerKindDBWhereMenu {
		t.Fatalf("b opened %v %v", m.pickerOpen, m.pickerKind)
	}
	m.databaseConsolePicker("and")
	if m.pickerKind != pickerKindDBWhereCol {
		t.Fatalf("no column picker: %v", m.pickerKind)
	}
	m.databaseConsolePicker("1") // b
	if m.pickerKind != pickerKindDBWhereOp {
		t.Fatalf("no operator picker: %v", m.pickerKind)
	}
	m.databaseConsolePicker("=")
	if !m.promptOpen || m.promptKind != promptKindDBWhereValue {
		t.Fatal("no value prompt")
	}
	m.databaseWhereApply("b1")
	if tv.where != `"b" = 'b1'` {
		t.Fatalf("where = %s", tv.where)
	}
	// An alternative, then a NULL test that needs no value.
	m.databasePageKey("b", 0, 0)
	m.databaseConsolePicker("or")
	m.databaseConsolePicker("0")
	m.databaseConsolePicker("IS NULL")
	if tv.where != `("b" = 'b1') OR "a" IS NULL` {
		t.Fatalf("where = %s", tv.where)
	}
	// Sort, then clear.
	m.databasePageKey("b", 0, 0)
	m.databaseConsolePicker("sort")
	m.databaseConsolePicker("2")
	m.databaseConsolePicker("desc")
	if tv.orderCol != "c" || !tv.desc {
		t.Fatalf("sort = %s %v", tv.orderCol, tv.desc)
	}
	m.databasePageKey("b", 0, 0)
	m.databaseConsolePicker("clear")
	if tv.where != "" {
		t.Fatalf("not cleared: %s", tv.where)
	}
}

func TestDBPageKeyRowsListBuilderAndQuickFilter(t *testing.T) {
	keys := map[string]bool{}
	for _, r := range dbPageKeyRows() {
		keys[r.Key] = true
	}
	if !keys["b"] || !keys["*"] {
		t.Fatalf("the key card misses b or *: %v", keys)
	}
}
