package dbclient

import (
	"strings"
	"testing"
)

func TestBlankLiteralsKeepsLengthAndLines(t *testing.T) {
	for _, src := range []string{
		"SELECT 'it''s' -- tail", "SELECT 'unterminated", "/* a\nb */ SELECT 1", "SELECT /* open", "SELECT 1 /* x */ -- y\nFROM t",
	} {
		got := blankLiterals(src)
		if len(got) != len(src) || strings.Count(got, "\n") != strings.Count(src, "\n") {
			t.Errorf("blankLiterals(%q) = %q changed the shape", src, got)
		}
		if strings.Contains(got, "it") && strings.Contains(src, "it''s") {
			t.Errorf("a string is blanked: %q", got)
		}
	}
	if strings.TrimSpace(blankLiterals("/* a\nb */")) != "" {
		t.Error("a comment is blanked")
	}
}

func TestNameUsagesQuotedAndUnclosed(t *testing.T) {
	src := `SELECT a, "A", [a], ` + "`a`" + ` FROM t WHERE x = 'a' -- a`
	if got := NameUsages(src, "a"); len(got) != 4 {
		t.Errorf("usages = %v", got)
	}
	if got := NameUsages(`SELECT "unclosed a`, "a"); len(got) != 0 {
		t.Errorf("an unclosed quote ends the scan: %v", got)
	}
	if NameUsages("SELECT 1", "") != nil {
		t.Error("no name, no usages")
	}
}

func TestOutlineAndLeadingComments(t *testing.T) {
	src := "-- head\n/* c */ SELECT 1;\n\nCREATE TABLE t (\n  a int\n);\n-- only a comment"
	items := Outline(src, "")
	if len(items) < 2 || items[0].Line != 2 || items[1].Line != 4 || items[1].Label != "CREATE TABLE t (" {
		t.Errorf("outline = %+v", items)
	}
	if Outline("SELECT 'open", "") != nil {
		t.Error("a file that does not split has no outline")
	}
	for in, want := range map[string]int{"  x": 2, "-- a\nx": 5, "-- only": 7, "/* a */x": 7, "/* open": 7, "": 0} {
		if got := skipLeadingComments(in); got != want {
			t.Errorf("skipLeadingComments(%q) = %d, want %d", in, got, want)
		}
	}
	if got := shortLabel(strings.Repeat("word ", 30)); len([]rune(got)) > outlineLabelMax+1 {
		t.Errorf("label not cut: %q", got)
	}
}

func TestDetectDialectScores(t *testing.T) {
	if d, ok := DetectDialect("-- @dialect mysql\nSELECT 1"); !ok || d != DialectMySQL {
		t.Errorf("dialect line = %v %v", d, ok)
	}
	if _, ok := DetectDialect("SELECT 1"); ok {
		t.Error("nothing to go on")
	}
	if d, ok := DetectDialect("SELECT * FROM t WHERE x ILIKE 'a' RETURNING id"); ok && d != DialectPostgres {
		t.Errorf("postgres hints = %v", d)
	}
}

func TestFindEmbeddedSQLPerLanguage(t *testing.T) {
	goSrc := "package x\n// SELECT 1 FROM ignored\nvar q = `SELECT a\nFROM t`\nvar r = 'x'\nvar s = \"DELETE FROM t WHERE id = 1\"\n/* SELECT 2 FROM z */\nvar u = \"unclosed\n"
	got := FindEmbeddedSQL(goSrc, "go")
	if len(got) != 2 || got[0].Line != 3 || !strings.Contains(got[0].Text, "FROM t") || got[1].Kind != KindWrite {
		t.Errorf("go = %+v", got)
	}
	py := "# SELECT 1 FROM c\nq = \"\"\"SELECT a FROM t\"\"\"\nr = 'UPDATE t SET a = 1'\nz = '''unclosed"
	if got := FindEmbeddedSQL(py, "python"); len(got) != 2 {
		t.Errorf("python = %+v", got)
	}
	js := "// SELECT 1 FROM c\nconst a = `SELECT x FROM y`;\nconst b = 'SELECT z FROM w';\n/* open"
	if got := FindEmbeddedSQL(js, "js"); len(got) != 2 {
		t.Errorf("js = %+v", got)
	}
	if FindEmbeddedSQL("x", "") != nil {
		t.Error("no language, no scan")
	}
	if FindEmbeddedSQL("var a = `open", "go") != nil {
		t.Error("an unclosed raw string ends the scan")
	}
	if FindEmbeddedSQL("// open comment", "go") != nil {
		t.Error("a comment to the end")
	}
	if got := FindEmbeddedSQL(`x = "SELECT 1 \" FROM t"`, "python"); len(got) != 1 {
		t.Errorf("an escaped quote stays inside: %+v", got)
	}
	if LooksLikeSQL("hello world") || !LooksLikeSQL("SELECT a FROM t") {
		t.Error("LooksLikeSQL")
	}
}

func TestStatementPickingByMode(t *testing.T) {
	text := "SELECT 1;\nSELECT (2);\n-- c\nSELECT 'a;b';\n/* x */ SELECT 3"
	if len(StatementSpans(text, "")) != 4 {
		t.Fatalf("spans = %+v", StatementSpans(text, ""))
	}
	if _, err := PickStatements(text, "", ExecAsk, 0, 0, 0); err == nil || !strings.Contains(err.Error(), "ask mode") {
		t.Errorf("ask = %v", err)
	}
	if _, err := PickStatements(text, "", ExecMode(99), 0, 0, 0); err == nil || !strings.Contains(err.Error(), "unknown execution mode") {
		t.Errorf("unknown mode = %v", err)
	}
	if ExecMode(99).String() != "unknown" || ExecMode(-1).String() != "unknown" {
		t.Error("String of a bad mode")
	}
	for _, m := range []ExecMode{ExecSmallest, ExecLargest, ExecFromCaret} {
		if _, err := PickStatements("   ", "", m, 1, 0, 0); err == nil || !strings.Contains(err.Error(), "no statement at the caret") {
			t.Errorf("%s on empty text = %v", m, err)
		}
	}
	if got, err := PickStatements(text, "", ExecLargest, 3, 0, 0); err != nil || len(got) != 1 || got[0] != "SELECT 1" {
		t.Errorf("largest = %v, %v", got, err)
	}
	if got, err := PickStatements(text, "", ExecSmallest, 20, 0, 0); err != nil || len(got) != 1 {
		t.Errorf("smallest = %v, %v", got, err)
	}
	if got, err := PickStatements(text, "", ExecFromCaret, 12, 0, 0); err != nil || len(got) != 3 {
		t.Errorf("from caret = %v, %v", got, err)
	}
	for _, m := range []ExecMode{ExecSelectionOne, ExecSelectionSplit} {
		if _, err := PickStatements(text, "", m, 0, 5, 2); err == nil || !strings.Contains(err.Error(), "outside the text") {
			t.Errorf("%s bad range = %v", m, err)
		}
		if _, err := PickStatements(text, "", m, 0, 1, 1); err == nil || !strings.Contains(err.Error(), "nothing selected") {
			t.Errorf("%s empty selection = %v", m, err)
		}
	}
	if got, err := PickStatements(text, "", ExecSelectionOne, 0, 0, 20); err != nil || len(got) != 1 {
		t.Errorf("selection one = %v, %v", got, err)
	}
	if got, err := PickStatements(text, "", ExecSelectionSplit, 0, 0, 20); err != nil || len(got) < 2 {
		t.Errorf("selection split = %v, %v", got, err)
	}
	if _, err := PickStatements("SELECT 'open", "", ExecSelectionSplit, 0, 0, 12); err == nil {
		t.Error("a selection that does not split: want error")
	}
	for _, in := range []string{"a (b) c", "(((x)", "f(')') ", "-- (\n(a)", "/* ( */ (a) /* open", "'(", "(a))", "(x) -- (y"} {
		_ = parenPairs(in)
	}
	if got := parenPairs("f(a, (b))"); len(got) != 2 {
		t.Errorf("parenPairs = %v", got)
	}
}
