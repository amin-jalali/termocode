package main

import (
	"bytes"
	"strings"
	"testing"
)

const mod = "example.com/m"

const unitProfile = `mode: set
example.com/m/internal/a/a.go:1.1,3.2 4 1
example.com/m/internal/a/a.go:5.1,6.2 6 0
example.com/m/internal/b/b.go:1.1,2.2 10 0
example.com/m/internal/fake/f.go:1.1,2.2 50 0
example.com/m/internal/new/n.go:1.1,2.2 7 1
`

const e2eProfile = `mode: set
example.com/m/internal/a/a.go:5.1,6.2 6 1
example.com/m/internal/b/b.go:1.1,2.2 10 0
`

func testSystems(t *testing.T) *Systems {
	t.Helper()
	s, err := ParseSystems(strings.NewReader("One: internal/a\nTwo: internal/b\nexclude: internal/fake\n"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMergeUnionsCoverage(t *testing.T) {
	u, err := ParseProfile(strings.NewReader(unitProfile))
	if err != nil {
		t.Fatal(err)
	}
	e, err := ParseProfile(strings.NewReader(e2eProfile))
	if err != nil {
		t.Fatal(err)
	}
	m := Merge(u, e)
	// a.go's second block was only hit by e2e: now covered, still 2 blocks.
	var a []Block
	for _, b := range m {
		if strings.HasSuffix(b.File, "/a.go") {
			a = append(a, b)
		}
	}
	if len(a) != 2 || a[0].Count == 0 || a[1].Count == 0 {
		t.Fatalf("merged a.go blocks = %+v", a)
	}
	if len(m) != 5 {
		t.Fatalf("merge made %d blocks, want 5 (same block counted once)", len(m))
	}
}

func TestWriteProfileRoundTrips(t *testing.T) {
	u, _ := ParseProfile(strings.NewReader(unitProfile))
	var buf bytes.Buffer
	if err := WriteProfile(&buf, u); err != nil {
		t.Fatal(err)
	}
	back, err := ParseProfile(&buf)
	if err != nil || len(back) != len(u) {
		t.Fatalf("round trip: %v, %d vs %d blocks", err, len(back), len(u))
	}
}

func TestParseProfileRejectsGarbage(t *testing.T) {
	for _, in := range []string{"mode: set\nnot a block line\n", "mode: set\nf.go:1.1,2.2 x 1\n"} {
		if _, err := ParseProfile(strings.NewReader(in)); err == nil {
			t.Errorf("ParseProfile(%q) should fail", in)
		}
	}
}

// Coverage is statement-weighted, not an average of package percents.
func TestSummarizeWeightsByStatements(t *testing.T) {
	s, err := ParseSystems(strings.NewReader("Big: internal/big internal/tiny\nexclude: internal/fake\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseProfile(strings.NewReader(`mode: set
example.com/m/internal/big/x.go:1.1,2.2 90 1
example.com/m/internal/tiny/y.go:1.1,2.2 10 0
example.com/m/internal/fake/f.go:1.1,2.2 99 0
example.com/m/internal/stray/z.go:1.1,2.2 5 0
`))
	if err != nil {
		t.Fatal(err)
	}
	r := Summarize(p, mod, s)
	big := r.System("Big")
	if big.Total != 100 || big.Covered != 90 {
		t.Fatalf("Big = %+v, want 90/100 (mean of package percents would say 50%%)", big)
	}
	if pct := big.Percent(); pct != 90 {
		t.Fatalf("percent = %v", pct)
	}
	if r.Total.Total != 100 {
		t.Fatalf("excluded and unmapped statements must not count: %+v", r.Total)
	}
	if len(r.Unmapped) != 1 || r.Unmapped[0] != "internal/stray" {
		t.Fatalf("unmapped = %v", r.Unmapped)
	}
}

func TestSummarizeSystemWithoutProfileLinesIsZero(t *testing.T) {
	r := Summarize(nil, mod, testSystems(t))
	if got := r.System("One"); got.Total != 0 || got.Percent() != 0 {
		t.Fatalf("%+v", got)
	}
}
