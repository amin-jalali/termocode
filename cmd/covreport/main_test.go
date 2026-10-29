package main

import (
	"strings"
	"testing"
)

func TestModulePath(t *testing.T) {
	got, err := modulePath([]byte("// c\nmodule example.com/m // x\n\ngo 1.26\n"))
	if err != nil || got != "example.com/m" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := modulePath([]byte("go 1.26\n")); err == nil {
		t.Fatal("no module line must fail")
	}
}

func TestCoverPkgsKeepsOnlyShippedPackages(t *testing.T) {
	s, _ := ParseSystems(strings.NewReader("One: internal/a cmd/b\nexclude: internal/a/fake\n"))
	list := []string{
		"example.com/m/internal/a",
		"example.com/m/internal/a/fake",
		"example.com/m/internal/other",
		"example.com/m/cmd/b",
		"example.com/m", // module root: not mapped
	}
	got := coverPkgs(list, "example.com/m", s)
	want := "example.com/m/internal/a,example.com/m/cmd/b"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestShortenCoverLine(t *testing.T) {
	in := "ok  \tm/internal/ai\t0.2s\tcoverage: 2.8% of statements in m/a, m/b, m/c\n"
	want := "ok  \tm/internal/ai\t0.2s\tcoverage: 2.8% of statements in -coverpkg\n"
	if got := shortenCoverLine(in); got != want {
		t.Errorf("got %q", got)
	}
	plain := "--- FAIL: TestX (0.00s)\n"
	if got := shortenCoverLine(plain); got != plain {
		t.Errorf("plain line changed: %q", got)
	}
}

func TestLineFilterWriter(t *testing.T) {
	var out strings.Builder
	w := &lineFilter{w: &out, f: strings.ToUpper}
	w.Write([]byte("ab"))
	w.Write([]byte("c\nde"))
	w.Close()
	if out.String() != "ABC\nDE" {
		t.Errorf("got %q", out.String())
	}
}
