package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFloors(t *testing.T) {
	in := "# comment\n\nGit: 88.2\nSession/transfer/update: 81\nTotal: 80.5\n"
	got, err := ParseFloors(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{"Git": 88.2, "Session/transfer/update": 81, "Total": 80.5}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	for _, bad := range []string{"Git 88\n", "Git: x\n", "Git: 88\nGit: 90\n", "Git: 101\n", ": 5\n"} {
		if _, err := ParseFloors(strings.NewReader(bad)); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestBelowFloors(t *testing.T) {
	r := sampleReport() // Git 95%, App shell 50%, Total 80%
	if got := BelowFloors(r, map[string]float64{"Git": 95, "App shell": 50, "Total": 80}); len(got) != 0 {
		t.Errorf("exactly at the floors passes: %v", got)
	}
	got := BelowFloors(r, map[string]float64{"Git": 95.5, "App shell": 40, "Total": 81})
	if len(got) != 2 || !strings.Contains(got[0], "Git") || !strings.Contains(got[1], "Total") {
		t.Errorf("below: %v", got)
	}
	// Every system needs a floor, and a floor needs a system: a renamed or
	// new system never slips through unchecked.
	got = BelowFloors(r, map[string]float64{"Git": 90, "Old name": 10})
	joined := strings.Join(got, "\n")
	if len(got) != 2 || !strings.Contains(joined, "App shell has no floor") || !strings.Contains(joined, "Old name") {
		t.Errorf("missing / unknown: %v", got)
	}
}

// -floors fails the run when a system is under its floor.
func TestRunChecksFloorsFile(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/m\n")
	write("systems.txt", "One: internal/a\n")
	write("p.out", "mode: set\nexample.com/m/internal/a/a.go:1.1,2.2 3 1\nexample.com/m/internal/a/a.go:3.1,4.2 1 0\n") // 75%
	write("ok.txt", "One: 75\n")
	write("high.txt", "One: 80\n")
	for floors, wantFail := range map[string]bool{"ok.txt": false, "high.txt": true} {
		fail, err := run(root, "systems.txt", "", filepath.Join(root, "p.out"), floors, false, 0, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if fail != wantFail {
			t.Errorf("floors %s: fail = %v, want %v", floors, fail, wantFail)
		}
	}
}
