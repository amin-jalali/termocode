package containers

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// CP-11: the parsers handle a big host (2000 rows) inside a time budget
// and a per-row allocation budget. The budgets have a wide margin: they
// catch a quadratic parser, not a slow machine.

func bigPS(n int) []byte {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `{"ID":"%012d","Names":"proj%d-svc-%d","Image":"registry.example.com/team/app%d:1.%d","ImageID":"sha256:%064d","Command":"\"./run\"","CreatedAt":"2026-10-18 09:30:00 +0000 UTC","State":"running","Status":"Up 2 hours (healthy)","Ports":"0.0.0.0:8080->8080/tcp, :::8443->443/tcp","Labels":"com.docker.compose.project=proj%d,com.docker.compose.service=svc,com.docker.compose.project.working_dir=/work/p%d","Size":"1.2GB","Networks":"net%d"}`+"\n",
			i, i/5, i, i%40, i%9, i, i/5, i/5, i%7)
	}
	return []byte(b.String())
}

func bigImages(n int) []byte {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `{"ID":"sha256:%064d","Repository":"registry.example.com/team/app%d","Tag":"1.%d","CreatedAt":"2026-10-18 09:30:00 +0000 UTC","CreatedSince":"2 days ago","Size":"421MB","Containers":"N/A"}`+"\n", i, i%40, i%9)
	}
	return []byte(b.String())
}

func TestPerfGuardParsersBigHost(t *testing.T) {
	const n = 2000
	ps, imgs := bigPS(n), bigImages(n)
	if rows, err := ParseContainers(ps); err != nil || len(rows) != n {
		t.Fatalf("ps: %d rows, err %v", len(rows), err)
	}
	if rows, err := ParseImages(imgs); err != nil || len(rows) != n {
		t.Fatalf("images: %d rows, err %v", len(rows), err)
	}
	for name, f := range map[string]func(){
		"ParseContainers": func() { _, _ = ParseContainers(ps) },
		"ParseImages":     func() { _, _ = ParseImages(imgs) },
	} {
		start := time.Now()
		f()
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("%s of %d rows took %v (max 2s)", name, n, d)
		}
		if a := testing.AllocsPerRun(3, f); a > float64(n)*maxAllocsPerRow {
			t.Errorf("%s: %.0f allocs for %d rows (max %d per row)", name, a, n, maxAllocsPerRow)
		}
	}
	rows, _ := ParseContainers(ps)
	start := time.Now()
	groups := GroupByProject(rows, "proj3")
	if d := time.Since(start); d > time.Second || len(groups) < n/5 {
		t.Errorf("GroupByProject: %d groups in %v", len(groups), d)
	}
}

const maxAllocsPerRow = 200 // measured ~116 (ps) and ~55 (images)

func BenchmarkParseContainers2000(b *testing.B) {
	ps := bigPS(2000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = ParseContainers(ps)
	}
}

func BenchmarkParseImages2000(b *testing.B) {
	imgs := bigImages(2000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = ParseImages(imgs)
	}
}
