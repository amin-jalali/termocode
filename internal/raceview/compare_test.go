package raceview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentrace"
)

func demoRace() agentrace.Race {
	cost := 0.42
	return agentrace.Race{ID: "race-3", Number: 3, Name: "race/make-the-toast-queue-thread-safe", Prompt: "make the toast queue thread-safe",
		Verify: "go test ./...", StartedMs: 0, Lanes: []agentrace.Lane{
			{ID: "race-3-1-claude", Agent: "claude", Title: "Claude Code", State: agentrace.Done, Stats: true, Cost: &cost,
				Files:  []agentrace.FileStat{{Status: "M", Path: "internal/toast/queue.go", Add: 31, Del: 7}, {Status: "M", Path: "internal/toast/queue_test.go", Add: 10, Del: 2}},
				Verify: agentrace.Verify{State: agentrace.VerifyPass}, Note: "used a mutex"},
			{ID: "race-3-2-codex", Agent: "codex", Title: "Codex", State: agentrace.Done, Stats: true,
				Files:  []agentrace.FileStat{{Status: "M", Path: "internal/toast/queue.go", Add: 80, Del: 30}, {Status: "A", Path: "internal/toast/chan.go", Add: 8}},
				Verify: agentrace.Verify{State: agentrace.VerifyFail, Exit: 1}},
			{ID: "race-3-3-gemini", Agent: "gemini", Title: "Gemini CLI", State: agentrace.Working},
		}}
}

// Spec (AGX-5, plan 2.5): the compare page has one row per lane with
// state, files, +/-, verify and cost, and the files of the selected lane
// under it; keys act on the selected lane (Enter review, m merge, d
// discard) or on the race (v verify all, s summary, x stop, Esc close).
func TestCompareTableAndKeys(t *testing.T) {
	c := NewCompare(demoRace())
	c.SetSize(150, 42)
	c.SetNow(4 * 60 * 1000)
	out := ansi.Strip(c.Box())
	for _, want := range []string{
		"race/make-the-toast-queue-thread-safe", `"make the toast queue thread-safe"`, "3 lanes · 4 m",
		"LANE", "STATE", "FILES", "+/-", "VERIFY", "COST",
		"Claude Code", "done", "+41 -9", "passed", "$0.42", "used a mutex",
		"Codex", "+88 -30", "exit 1",
		"Gemini CLI", "working", "not yet",
		"Claude Code · 2 files", "internal/toast/queue.go", "+31 -7",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	act := func(k string) ActionMsg {
		t.Helper()
		var cmd = c.Key(key(k))
		msg, _ := run(cmd).(ActionMsg)
		return msg
	}
	if a := act("enter"); a != (ActionMsg{Action: ActReview, Lane: "race-3-1-claude"}) {
		t.Errorf("enter %+v", a)
	}
	c.Key(key("down"))
	if a := act("m"); a != (ActionMsg{Action: ActMerge, Lane: "race-3-2-codex"}) {
		t.Errorf("m %+v", a)
	}
	if a := act("d"); a != (ActionMsg{Action: ActDiscard, Lane: "race-3-2-codex"}) {
		t.Errorf("d %+v", a)
	}
	for k, want := range map[string]Action{"v": ActVerify, "s": ActSummary, "x": ActStop, "r": ActRefresh, "esc": ActClose} {
		if a := act(k); a.Action != want {
			t.Errorf("%s %+v", k, a)
		}
	}
	if out := ansi.Strip(c.Box()); !strings.Contains(out, "Codex · 2 files") || !strings.Contains(out, "chan.go") {
		t.Errorf("selected lane files:\n%s", out)
	}
	// A working lane cannot be merged yet: m says so and does nothing.
	c.Key(key("down"))
	if a := act("m"); a.Action != ActNone || !strings.Contains(ansi.Strip(c.Box()), "still working") {
		t.Errorf("m on a working lane %+v", a)
	}
}

// Spec (AGX-5): the page fits 80x24 with lane rows only (state, files,
// verify) and the selected lane's files under them; the summary shows
// when there is one; merged and discarded lanes stay as rows.
func TestCompareNarrowAndSummary(t *testing.T) {
	r := demoRace()
	r.Summary = []string{"Claude Code adds a mutex.", "Codex rewrites it with a channel.", "Gemini CLI is not done."}
	r.Lanes[1].State = agentrace.Discarded
	c := NewCompare(r)
	c.SetSize(80, 24)
	box := c.Box()
	lines := strings.Split(box, "\n")
	x, y := c.Origin()
	if len(lines)+y > 24 {
		t.Errorf("%d rows at %d", len(lines), y)
	}
	for _, l := range lines {
		if ansi.StringWidth(l)+x > 80 {
			t.Errorf("row of %d at %d", ansi.StringWidth(l), x)
		}
	}
	out := ansi.Strip(box)
	for _, want := range []string{"Claude Code", "discarded", "Gemini CLI", "Claude Code adds a mutex."} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "COST") {
		t.Errorf("narrow page has the cost column:\n%s", out)
	}
	// A closed lane: m and d do nothing.
	c.Key(key("down"))
	if msg, _ := run(c.Key(key("m"))).(ActionMsg); msg.Action != ActNone {
		t.Errorf("m on a discarded lane %+v", msg)
	}
}

// Spec (AGX-5): the selected lane is kept by id when the race changes.
func TestCompareKeepsSelection(t *testing.T) {
	c := NewCompare(demoRace())
	c.Key(key("down"))
	r := demoRace()
	r.Lanes = append([]agentrace.Lane{{ID: "race-3-0-x", Title: "X", State: agentrace.Done}}, r.Lanes...)
	c.SetRace(r)
	if c.Selected() != "race-3-2-codex" {
		t.Errorf("selected %s", c.Selected())
	}
}
