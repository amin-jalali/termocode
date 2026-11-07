package tests

import "testing"

// pgTAP results land under one "pgTAP" package, one file per connection,
// whatever the workspace's own framework is.
func TestTreeApplyPgTAPGroupsByConnection(t *testing.T) {
	tr := &Tree{Root: "/w", FW: FrameworkGo}
	tr.ResetPgTAP("pg-dev")
	c := tr.ApplyPgTAP("pg-dev", Event{Kind: EventResult, Package: PgTAPPackage, Test: "public.test_users", Status: StatusFailed, Message: "boom"})
	tr.ApplyPgTAP("pg-dev", Event{Kind: EventResult, Package: PgTAPPackage, Test: "public.test_orders", Status: StatusPassed})
	if c == nil || c.Status != StatusFailed || c.Message != "boom" {
		t.Fatalf("case %+v", c)
	}
	p := tr.PgTAP()
	if p == nil || p.Name != PgTAPPackage || len(p.Files) != 1 || p.Files[0].Rel != "pg-dev" || len(p.Files[0].Cases) != 2 {
		t.Fatalf("package %+v", p)
	}
	if conn, ok := PgTAPConn(p.Files[0].Path); !ok || conn != "pg-dev" {
		t.Fatalf("conn %q %v", conn, ok)
	}
	if _, ok := PgTAPConn("/w/main_test.go"); ok {
		t.Fatal("a real file is not a pgTAP file")
	}
	if got := tr.Counts(); got.Failed != 1 || got.Passed != 1 {
		t.Fatalf("counts %+v", got)
	}
	tr.ResetPgTAP("pg-dev") // a new run of the connection starts empty
	if len(tr.PgTAP().Files[0].Cases) != 0 {
		t.Fatal("reset kept old cases")
	}
}

func TestKeepPgTAPSurvivesRediscovery(t *testing.T) {
	old := &Tree{Root: "/w"}
	old.ApplyPgTAP("pg", Event{Kind: EventResult, Test: "t1", Status: StatusPassed})
	fresh := &Tree{Root: "/w", FW: FrameworkGo, Packages: []*Package{{Name: "x", Dir: "/w/x"}}}
	KeepPgTAP(old, fresh)
	if fresh.PgTAP() == nil || len(fresh.Packages) != 2 {
		t.Fatalf("packages %+v", fresh.Packages)
	}
}
