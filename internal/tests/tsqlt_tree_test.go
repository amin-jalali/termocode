package tests

import "testing"

// tSQLt results land under one "tSQLt" package, one file per connection,
// each case named Class.Test.
func TestTreeApplyTSQLtGroupsByConnection(t *testing.T) {
	tr := &Tree{Root: "/w", FW: FrameworkGo}
	tr.ResetTSQLt("sql-dev")
	c := tr.ApplyTSQLt("sql-dev", Event{Kind: EventResult, Package: TSQLtPackage, Class: "UserTests", Test: "test login", Status: StatusFailed, Message: "boom"})
	tr.ApplyTSQLt("sql-dev", Event{Kind: EventResult, Package: TSQLtPackage, Class: "OrderTests", Test: "test total", Status: StatusPassed})
	if c == nil || c.Name != "UserTests.test login" || c.Status != StatusFailed || c.Message != "boom" {
		t.Fatalf("case %+v", c)
	}
	p := tr.TSQLt()
	if p == nil || p.Name != TSQLtPackage || len(p.Files) != 1 || p.Files[0].Rel != "sql-dev" || len(p.Files[0].Cases) != 2 {
		t.Fatalf("package %+v", p)
	}
	if conn, ok := TSQLtConn(p.Files[0].Path); !ok || conn != "sql-dev" {
		t.Fatalf("conn %q %v", conn, ok)
	}
	if _, ok := TSQLtConn("pgtap://x"); ok {
		t.Fatal("a pgTAP file is not a tSQLt file")
	}
	if _, ok := PgTAPConn(p.Files[0].Path); ok {
		t.Fatal("a tSQLt file is not a pgTAP file")
	}
	if got := tr.Counts(); got.Failed != 1 || got.Passed != 1 {
		t.Fatalf("counts %+v", got)
	}
	tr.ApplyTSQLt("sql-dev", Event{Kind: EventResult, Class: "UserTests", Test: "test login", Status: StatusPassed})
	if len(tr.TSQLt().Files[0].Cases) != 2 || tr.TSQLt().Files[0].Cases[0].Status != StatusPassed {
		t.Fatal("a rerun must update the same case")
	}
	tr.ResetTSQLt("sql-dev")
	if len(tr.TSQLt().Files[0].Cases) != 0 {
		t.Fatal("reset kept old cases")
	}
}

func TestKeepTSQLtSurvivesRediscovery(t *testing.T) {
	old := &Tree{Root: "/w"}
	old.ApplyTSQLt("sql", Event{Kind: EventResult, Class: "C", Test: "t1", Status: StatusPassed})
	fresh := &Tree{Root: "/w", FW: FrameworkGo, Packages: []*Package{{Name: "x", Dir: "/w/x"}}}
	KeepTSQLt(old, fresh)
	if fresh.TSQLt() == nil || len(fresh.Packages) != 2 {
		t.Fatalf("packages %+v", fresh.Packages)
	}
}
