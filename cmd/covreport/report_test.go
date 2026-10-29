package main

import (
	"bytes"
	"strings"
	"testing"
)

func sampleReport() Report {
	return Report{
		Systems: []Stat{{Name: "Git", Total: 200, Covered: 190}, {Name: "App shell", Total: 100, Covered: 50}},
		Total:   Stat{Name: "Total", Total: 300, Covered: 240},
	}
}

func TestPrintTable(t *testing.T) {
	var buf bytes.Buffer
	PrintTable(&buf, sampleReport())
	out := buf.String()
	for _, want := range []string{"System", "Git", "95.0%", "App shell", "50.0%", "Total", "80.0%"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

func TestPrintTopFuncs(t *testing.T) {
	var buf bytes.Buffer
	PrintTopFuncs(&buf, sampleReport(), map[string][]FuncStat{
		"Git": {{Name: "(*R).Run", File: "internal/git/r.go", Line: 12, Missed: 7, Total: 9}},
	})
	out := buf.String()
	if !strings.Contains(out, "internal/git/r.go:12") || !strings.Contains(out, "(*R).Run") || !strings.Contains(out, "7 of 9") {
		t.Errorf("bad list:\n%s", out)
	}
	if !strings.Contains(out, "App shell") || !strings.Contains(out, "nothing uncovered") {
		t.Errorf("a system without gaps should say so:\n%s", out)
	}
}

func TestBelowMin(t *testing.T) {
	r := sampleReport()
	if got := Below(r, 0, 0); len(got) != 0 {
		t.Errorf("no limits: %v", got)
	}
	got := Below(r, 90, 0)
	if len(got) != 1 || !strings.Contains(got[0], "App shell") {
		t.Errorf("per-system 90: %v", got)
	}
	got = Below(r, 0, 85)
	if len(got) != 1 || !strings.Contains(got[0], "Total") {
		t.Errorf("total 85: %v", got)
	}
	if got := Below(r, 50, 80); len(got) != 0 {
		t.Errorf("exactly at the limit passes: %v", got)
	}
}
