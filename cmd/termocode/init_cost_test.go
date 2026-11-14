package main

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// PERF-1: every termocode process (the session client, the server, the
// UI, mcp …) runs the package init of every package it links before
// main. The heap that termocode's own packages allocate there is paid by
// each start and stays for the life of the process. The count is the
// same on every machine (no timing), so this guard runs in every test
// run.
//
// It runs this test binary again (it links the same packages as
// termocode) with GODEBUG=inittrace=1 and no test, and adds up the
// "init <pkg> @… ms, … ms clock, N bytes, M allocs" lines of
// github.com/amin-jalali/termocode packages.
func TestPerfInitOwnPackages(t *testing.T) {
	if os.Getenv("TERMOCODE_INIT_CHILD") == "1" {
		return
	}
	if testing.CoverMode() != "" {
		t.Skip("a coverage build counts its own init allocations")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "GODEBUG=inittrace=1", "TERMOCODE_INIT_CHILD=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("init trace run: %v\n%s", err, stderr.String())
	}
	type pkg struct {
		name          string
		bytes, allocs int
	}
	var own []pkg
	var bytesSum, allocsSum int
	sc := bufio.NewScanner(&stderr)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		// init <pkg> @0.1 ms, 0.2 ms clock, 123 bytes, 4 allocs (11 fields)
		if len(f) != 11 || f[0] != "init" || !strings.HasPrefix(f[1], "github.com/amin-jalali/termocode/") {
			continue
		}
		b, err1 := strconv.Atoi(f[7])
		a, err2 := strconv.Atoi(f[9])
		if err1 != nil || err2 != nil {
			t.Fatalf("bad inittrace line %q", sc.Text())
		}
		own = append(own, pkg{f[1], b, a})
		bytesSum += b
		allocsSum += a
	}
	if len(own) == 0 {
		t.Fatalf("no init lines of termocode packages:\n%s", stderr.String())
	}
	sort.Slice(own, func(i, j int) bool { return own[i].bytes > own[j].bytes })
	for _, p := range own[:min(8, len(own))] {
		t.Logf("init %s: %d KiB, %d allocs", strings.TrimPrefix(p.name, "github.com/amin-jalali/termocode/"), p.bytes/1024, p.allocs)
	}
	perfbudget.Check(t, "init_own_heap_kib", float64(bytesSum/1024))
	perfbudget.Check(t, "init_own_allocs", float64(allocsSum))
}
