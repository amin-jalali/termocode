package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTopUncoveredRanksByMissedStatements(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package a

func Small() {
	_ = 1
}

type T struct{}

func (t *T) Big() {
	_ = 1
	_ = 2
	_ = 3
}

func Done() {
	_ = 1
}
`
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := ParseProfile(strings.NewReader(`mode: set
example.com/m/internal/a/a.go:3.14,5.2 1 0
example.com/m/internal/a/a.go:9.17,12.2 3 0
example.com/m/internal/a/a.go:15.14,17.2 1 1
example.com/m/internal/other/o.go:1.1,2.2 9 0
`))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := ParseSystems(strings.NewReader("One: internal/a\n"))
	got := TopUncovered(p, mod, root, s, 20)["One"]
	if len(got) != 2 {
		t.Fatalf("got %+v, want Big and Small only (Done is covered)", got)
	}
	if got[0].Name != "(*T).Big" || got[0].Missed != 3 || got[0].Total != 3 {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Name != "Small" || got[1].File != "internal/a/a.go" || got[1].Line != 3 {
		t.Errorf("second = %+v", got[1])
	}
	if lim := TopUncovered(p, mod, root, s, 1)["One"]; len(lim) != 1 {
		t.Errorf("limit 1 gave %d", len(lim))
	}
}
