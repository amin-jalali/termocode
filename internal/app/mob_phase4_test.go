package app

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/perfbudget"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// MOB-66 (mobile plan §3.9, §3.10, §6): the Phase 4 phone ops —
// workflows, races, coordinator runs and new lanes — are all on the
// protocol and wired in the hub. The names are strings on purpose: this
// gate checks the plan's list, whichever file a phase put them in.

// mobPhase4CardOps start work on the laptop: each waits for a laptop card
// (Cancel first) and is answered through webAskOps.
var mobPhase4CardOps = []string{
	"workflows.run", "workflows.save",
	"ai.race_start", "ai.run_start", "ai.lane_start",
}

// mobPhase4Ops act at once (a dry run never writes; summary, verify and
// stop act on a race the laptop already started; runs.send hands a
// running step a message).
var mobPhase4Ops = []string{
	"workflows.dry_run",
	"ai.race_summary", "ai.race_verify", "ai.race_stop",
	"runs.send",
}

// Spec: every Phase 4 op has a policy row for a controller (never a
// viewer), an audit line, a rate and a size cap; card ops wait for a
// laptop card and are asks; the others have a hub handler; the races
// topic is on the protocol and in the hub.
func TestMobPhase4OpsWired(t *testing.T) {
	check := func(op string, card bool) {
		p, ok := webproto.Lookup(op)
		if !ok {
			t.Errorf("%s: no policy row in webproto", op)
			return
		}
		if p.Role != webproto.RoleController {
			t.Errorf("%s: role %v, want controller (a viewer never starts or steers work)", op, p.Role)
		}
		if p.Audit != webproto.AuditYes {
			t.Errorf("%s: not audited", op)
		}
		if p.Rate.N <= 0 || p.Rate.Per <= 0 {
			t.Errorf("%s: no rate limit", op)
		}
		if p.MaxBytes <= 0 || p.MaxBytes > 256<<10 {
			t.Errorf("%s: size cap %d", op, p.MaxBytes)
		}
		_, ask := webAskOps[op]
		if _, later := webAskLaterOps[op]; later { // the card after a read off the UI thread
			ask = true
		}
		_, now := webOps[op]
		switch {
		case card && !p.LaptopCard:
			t.Errorf("%s: starts work but does not wait for a laptop card", op)
		case card && !ask:
			t.Errorf("%s: not in webAskOps (the laptop card)", op)
		case !card && !ask && !now:
			t.Errorf("%s: no hub handler", op)
		}
	}
	for _, op := range mobPhase4CardOps {
		check(op, true)
	}
	for _, op := range mobPhase4Ops {
		check(op, false)
	}
	if _, ok := webTopics["races"]; !ok {
		t.Error("the races topic is not in the hub")
	}
	found := false
	for _, td := range webproto.Topics() {
		if td.Name == "races" {
			found = true
		}
	}
	if !found {
		t.Error("the races topic is not on the protocol")
	}
}

// web_races_topic_20: the races topic for 20 races of 4 finished lanes
// with 200 changed files each (the most agentrace keeps), built and
// encoded as on the wire, median of 9.
func TestPerfWebRacesTopic20(t *testing.T) {
	perfbudget.Require(t)
	def, ok := webTopics["races"]
	if !ok {
		t.Fatal("the races topic is not in the hub")
	}
	m := &Model{}
	s := m.raceS()
	s.dir = t.TempDir()
	now := time.Now().UnixMilli()
	for n := 1; n <= 20; n++ {
		id := "race-" + strconv.Itoa(n)
		r := agentrace.Race{ID: id, Number: n, Name: "race/perf-" + strconv.Itoa(n), Prompt: "make the queue fast and safe",
			Verify: "go test ./...", StartedMs: now - int64(n)*60_000,
			Summary: []string{"Lane 1 adds a mutex.", "Lane 2 rewrites it around a channel.", "Lane 1 looks the most complete."}}
		for k := 1; k <= 4; k++ {
			l := agentrace.Lane{ID: agentrace.LaneID(n, k, "claude"), Agent: "claude", Title: "Agent " + strconv.Itoa(k),
				State: agentrace.Done, Stats: true, Verify: agentrace.VerifyResult(k % 2), Note: "lane note"}
			for i := 0; i < 200; i++ {
				l.Files = append(l.Files, agentrace.FileStat{Status: "M", Path: "internal/pkg" + strconv.Itoa(i) + "/file.go", Add: i, Del: i / 2})
			}
			r.Lanes = append(r.Lanes, l)
		}
		s.races = append(s.races, r)
	}
	var took []time.Duration
	for range 9 {
		t0 := time.Now()
		snap, err := def.snapshot(m, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := json.Marshal(snap); err != nil {
			t.Fatal(err)
		}
		took = append(took, time.Since(t0))
	}
	perfbudget.Check(t, "web_races_topic_20", medianMs(took))
}
