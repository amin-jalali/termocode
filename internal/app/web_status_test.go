package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/statusbar"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestHub gives m a running hub with no socket (publish and topics
// only); it stops with the test.
func webTestHub(t *testing.T, m *Model) *webHub {
	t.Helper()
	h := newWebHub()
	t.Cleanup(h.stop)
	m.web = h
	return h
}

// webTestSub adds a socketless browser to the hub and subscribes it to
// topics (the snaps are queued on it).
func webTestSub(t *testing.T, m *Model, topics ...string) *webConn {
	t.Helper()
	h := m.web
	c := &webConn{hub: h, id: "c-t" + string(rune('a'+len(h.live))), device: "phone", role: webproto.RoleController, out: newWebQueue(), subs: map[string]*webSub{}}
	c.ctx, c.cancel = context.WithCancel(h.ctx)
	h.live[c] = true
	m.onWebSub(webSubMsg{hub: h, conn: c, topics: topics})
	return c
}

// webTestNow is webTestTake that does not wait: nothing queued, no frames.
func webTestNow(t *testing.T, c *webConn) []map[string]any {
	t.Helper()
	c.out.mu.Lock()
	empty := len(c.out.items) == 0 && len(c.out.owed) == 0
	c.out.mu.Unlock()
	if empty {
		return nil
	}
	return webTestTake(t, c)
}

// webTestClock swaps webNow for a clock the test moves.
func webTestClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Unix(1_700_000_000, 0)
	old := webNow
	webNow = func() time.Time { return now }
	t.Cleanup(func() { webNow = old })
	return &now
}

// webTestData decodes a frame's data into v.
func webTestData(t *testing.T, f map[string]any, v any) {
	t.Helper()
	b, _ := json.Marshal(f["data"])
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
}

// Spec (WEB-12): the status snapshot says what the laptop's status bar
// says: workspace, branch, ahead / behind, the AI chip's text and state,
// plus the counts the strip needs.
func TestStatusSnapshotMatchesChip(t *testing.T) {
	m := aiTestModel(t)
	m.ai.provider = fakeAIProvider{}
	m.gitBranch = git.Branch{Name: "main", Ahead: 2, Behind: 1}
	m.gitFiles = []git.FileStatus{{Path: "a.go"}, {Path: "b.go"}, {Path: "c.go"}}
	m.terminalTabs = []terminalTab{{BufID: 3, Busy: true}, {BufID: 4}, {BufID: 5, Task: "build", Running: true}}
	h := webTestHub(t, &m)
	c := webTestSub(t, &m, "status")
	frames := webTestNow(t, c)
	if len(frames) != 1 || frames[0]["t"] != "snap" {
		t.Fatalf("frames %v", frames)
	}
	var s webproto.StatusSnap
	webTestData(t, frames[0], &s)
	chip := m.statusState()
	if s.Workspace != chip.Project || s.Branch != chip.Branch || s.Ahead != chip.Ahead || s.Behind != chip.Behind {
		t.Errorf("strip %+v, chip project=%q branch=%q %d/%d", s, chip.Project, chip.Branch, chip.Ahead, chip.Behind)
	}
	if s.AI.Label != chip.AI || s.AI.State != "idle" || s.AI.Busy || chip.AIState != statusbar.AIIdle {
		t.Errorf("ai %+v, chip %q %v", s.AI, chip.AI, chip.AIState)
	}
	if s.DirtyFiles != 3 || s.Terminals != (webproto.StatusTerms{Count: 3, Busy: 2}) || s.Web.Clients != 1 {
		t.Errorf("counts %+v", s)
	}
	if s.Root != aiWorkspaceRoot() {
		t.Errorf("root %q", s.Root)
	}
	m.ai.streaming = true
	if s := m.webStatus(&h.shell.status); s.AI.State != "busy" || !s.AI.Busy {
		t.Errorf("streaming: %+v", s.AI)
	}
}

// Spec (WEB-12): status changes go out after the Update that made them,
// at most one per 100 ms: changes inside that time wait for one tick and
// go out as the newest state; no change, no frame; nobody listening, no
// work.
func TestStatusCoalesced10Hz(t *testing.T) {
	m := aiTestModel(t)
	now := webTestClock(t)
	webTestHub(t, &m)
	if cmd := m.webAfterUpdate(nil); cmd != nil {
		t.Fatal("nobody listens: no tick")
	}
	c := webTestSub(t, &m, "status")
	webTestNow(t, c) // the snap
	if m.webAfterUpdate(nil); len(webTestNow(t, c)) != 0 {
		t.Fatal("no change must send nothing")
	}
	m.gitBranch.Ahead = 1
	m.webAfterUpdate(nil)
	got := webTestNow(t, c)
	if len(got) != 1 || got[0]["t"] != "delta" {
		t.Fatalf("first change: %v", got)
	}
	ticks := 0
	for i := 2; i <= 9; i++ {
		*now = now.Add(10 * time.Millisecond)
		m.gitBranch.Ahead = i
		if m.webAfterUpdate(nil) != nil {
			ticks++
		}
	}
	if got := webTestNow(t, c); len(got) != 0 || ticks != 1 {
		t.Fatalf("inside 100 ms: frames %v, ticks %d (want none, one tick)", got, ticks)
	}
	*now = now.Add(20 * time.Millisecond)
	m.webAfterUpdate(webStatusTickMsg{})
	got = webTestNow(t, c)
	var s webproto.StatusSnap
	if len(got) != 1 {
		t.Fatalf("after the tick: %v", got)
	}
	webTestData(t, got[0], &s)
	if s.Ahead != 9 || got[0]["seq"] != float64(3) {
		t.Fatalf("after the tick: %v", got[0])
	}
}

// Spec (WEB-12, WEB-UX-8): status.theme carries the laptop palette as hex
// per field; a palette change is a status delta.
func TestStatusThemeFollowsLaptop(t *testing.T) {
	m := aiTestModel(t)
	m.themeID = "dark"
	webTestClock(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m, "status")
	var s webproto.StatusSnap
	webTestData(t, webTestNow(t, c)[0], &s)
	if s.Theme.Name != "dark" || s.Theme.Light || len(s.Theme.Colors) != len(webThemeFields) || s.Theme.Colors["AIAccent"] != theme.Hex(theme.AIAccent) {
		t.Fatalf("theme %+v", s.Theme)
	}
	// The code colours of the file view follow the laptop too.
	if s.Theme.Colors["SyntaxKeyword"] != theme.Hex(theme.SyntaxKeyword) || s.Theme.Colors["SyntaxComment"] != theme.Hex(theme.SyntaxComment) {
		t.Fatalf("theme has no syntax colours: %+v", s.Theme.Colors)
	}
	old := theme.AIAccent
	t.Cleanup(func() { theme.AIAccent = old })
	theme.AIAccent = 33
	m.webAfterUpdate(nil)
	got := webTestNow(t, c)
	if len(got) != 1 {
		t.Fatalf("palette change: %v", got)
	}
	webTestData(t, got[0], &s)
	if s.Theme.Colors["AIAccent"] != theme.Hex(33) {
		t.Fatalf("theme after change %+v", s.Theme)
	}
}
