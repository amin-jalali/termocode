//go:build unix

package app

import (
	"strconv"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/perfbudget"
	"github.com/amin-jalali/termocode/internal/raceview"
)

// Spec (AGX-5): the race compare page with 4 finished lanes of 200 files
// each, made and drawn at 150x42, median of 15, within
// race_compare_4_lanes.
func TestPerfRaceCompare4Lanes(t *testing.T) {
	perfbudget.Require(t)
	r := agentrace.Race{ID: "race-1", Name: "race/perf", Prompt: "make it fast", Verify: "go test ./..."}
	for k := 0; k < 4; k++ {
		l := agentrace.Lane{ID: "race-1-" + strconv.Itoa(k) + "-x", Title: "Agent " + strconv.Itoa(k), State: agentrace.Done, Stats: true,
			Verify: agentrace.VerifyResult(k % 2)}
		for i := 0; i < 200; i++ {
			l.Files = append(l.Files, agentrace.FileStat{Status: "M", Path: "internal/pkg" + strconv.Itoa(i) + "/file.go", Add: i, Del: i / 2})
		}
		r.Lanes = append(r.Lanes, l)
	}
	var ds []time.Duration
	for i := 0; i < 15; i++ {
		start := time.Now()
		c := raceview.NewCompare(r)
		c.SetSize(150, 42)
		_ = c.Box()
		ds = append(ds, time.Since(start))
	}
	perfbudget.Check(t, "race_compare_4_lanes", perfMs(perfMedian(ds)))
}
