package dbclient

import (
	"strings"
	"testing"
)

const execText = "SELECT 1;\nSELECT * FROM t WHERE id IN (SELECT id FROM u WHERE x = ')(');\n-- note\nUPDATE t SET a = 2;\n"

func execAt(t *testing.T, s, sub string) int {
	t.Helper()
	i := strings.Index(s, sub)
	if i < 0 {
		t.Fatalf("%q not in text", sub)
	}
	return i
}

func TestStatementSpansCoverTheText(t *testing.T) {
	sp := StatementSpans(execText, "postgres")
	if len(sp) != 3 {
		t.Fatalf("spans = %+v", sp)
	}
	for _, s := range sp {
		if execText[s.Start:s.End] != s.SQL {
			t.Fatalf("span %+v does not match the text", s)
		}
	}
}

func TestExecCandidatesInnermostFirst(t *testing.T) {
	cur := execAt(t, execText, "FROM u") + 2
	c := ExecCandidates(execText, "postgres", cur)
	if len(c) != 2 {
		t.Fatalf("candidates = %+v", c)
	}
	if !strings.HasPrefix(c[0].SQL, "SELECT id FROM u") || strings.HasPrefix(c[0].SQL, "(") || strings.HasSuffix(c[0].SQL, ")") {
		t.Fatalf("smallest = %q", c[0].SQL)
	}
	if !strings.HasPrefix(c[1].SQL, "SELECT * FROM t") {
		t.Fatalf("largest = %q", c[1].SQL)
	}
	// A ( that only groups values is not a statement.
	c = ExecCandidates("SELECT (1 + 2) * 3;", "postgres", 9)
	if len(c) != 1 {
		t.Fatalf("value parens must not be candidates: %+v", c)
	}
	// The caret in the blank line after a statement belongs to it.
	c = ExecCandidates("SELECT 1;\n\n", "postgres", 10)
	if len(c) != 1 || c[0].SQL != "SELECT 1" && c[0].SQL != "SELECT 1;" {
		t.Fatalf("after the last statement: %+v", c)
	}
	if c := ExecCandidates("", "postgres", 0); len(c) != 0 {
		t.Fatalf("empty text: %+v", c)
	}
}

func TestPickStatements(t *testing.T) {
	sub := execAt(t, execText, "FROM u") + 2
	upd := execAt(t, execText, "UPDATE")
	cases := []struct {
		name     string
		mode     ExecMode
		cur      int
		sel      [2]int
		want     []string // prefixes
		wantFail bool
	}{
		{"smallest in subquery", ExecSmallest, sub, [2]int{}, []string{"SELECT id FROM u"}, false},
		{"largest in subquery", ExecLargest, sub, [2]int{}, []string{"SELECT * FROM t"}, false},
		{"smallest outside parens", ExecSmallest, upd, [2]int{}, []string{"UPDATE t"}, false},
		{"from caret", ExecFromCaret, sub, [2]int{}, []string{"SELECT * FROM t", "UPDATE t"}, false},
		{"from caret at start", ExecFromCaret, 0, [2]int{}, []string{"SELECT 1", "SELECT * FROM t", "UPDATE t"}, false},
		{"selection as one", ExecSelectionOne, 0, [2]int{0, upd}, []string{"SELECT 1;\nSELECT * FROM t"}, false},
		{"selection split", ExecSelectionSplit, 0, [2]int{0, upd}, []string{"SELECT 1", "SELECT * FROM t"}, false},
		{"empty selection", ExecSelectionSplit, 0, [2]int{4, 4}, nil, true},
		{"bad selection", ExecSelectionOne, 0, [2]int{5, 999}, nil, true},
	}
	for _, c := range cases {
		got, err := PickStatements(execText, "postgres", c.mode, c.cur, c.sel[0], c.sel[1])
		if c.wantFail {
			if err == nil {
				t.Errorf("%s: want an error, got %q", c.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: got %q", c.name, got)
			continue
		}
		for i := range got {
			if !strings.HasPrefix(got[i], c.want[i]) {
				t.Errorf("%s: [%d] = %q, want prefix %q", c.name, i, got[i], c.want[i])
			}
		}
	}
}

func TestParseExecMode(t *testing.T) {
	for _, s := range []string{"smallest", "largest", "from-caret", "selection-one", "selection-split", "ask"} {
		m, ok := ParseExecMode(s)
		if !ok || m.String() != s {
			t.Errorf("ParseExecMode(%q) = %v %v", s, m, ok)
		}
	}
	if _, ok := ParseExecMode("all"); ok {
		t.Errorf("unknown mode must fail")
	}
}
