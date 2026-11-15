//go:build unix

package app

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/now"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// webTestNowModel is a model with a red CI on main (head abc1234def), a
// card that waits, a running build and a failed test, and a Now store in
// a temp folder.
func webTestNowModel(t *testing.T) (*Model, *webConn, *time.Time) {
	t.Helper()
	m := aiTestModel(t)
	m.output = newOutputStore()
	clk := webTestClock(t)
	webTestHub(t, &m)
	m.ws = workspace.Workspace{Root: t.TempDir()}
	m.nowS = now.Open(filepath.Join(t.TempDir(), "now.json"))
	webTestTaskRun(&m, "build", clk.Add(-time.Minute), true, 0)
	webTestTaskRun(&m, "test", clk.Add(-2*time.Minute), false, 1)
	m.ci = &ciWatch{branch: "main", loaded: true, snap: ghci.Snapshot{Branch: "main", HeadSHA: "abc1234def",
		Runs: []ghci.Run{{Workflow: "e2e-linux", Status: "completed", Conclusion: "failure"}}}}
	webTestToolCard(&m, ai.Tool{Name: "write_file"})
	c := webTestSub(t, &m, "inbox")
	return &m, c, clk
}

func webTestLastInbox(t *testing.T, m *Model, c *webConn) webproto.InboxSnap {
	t.Helper()
	m.webAfterUpdate(nil)
	frames := webTestNow(t, c)
	s, n := webTestInbox(t, frames)
	if n == 0 {
		snap, err := webInboxSnapshot(m, "")
		if err != nil {
			t.Fatal(err)
		}
		return snap.(webproto.InboxSnap)
	}
	return s
}

func webTestHasItem(s webproto.InboxSnap, id string) bool {
	return slices.ContainsFunc(s.Items, func(it webproto.InboxItem) bool { return it.ID == id })
}

// Spec (Now, part 1): every inbox item says whose turn it is, why (one
// plain reason from a real signal), the rule that placed it and its
// level; the red CI names the commit it is about and offers the fix; the
// snap carries the user's rules with a live count and whether the
// first-run questions were answered.
func TestInboxNowFields(t *testing.T) {
	m, c, clk := webTestNowModel(t)
	s, _ := webTestInbox(t, webTestNow(t, c))

	ci := webTestInboxItem(t, s, "ci:main")
	if ci.Turn != now.TurnMe || ci.Rule != "redci" || ci.Level != 1 || ci.Why != "Your branch is red" || ci.Head != "abc1234def" ||
		ci.Source != "ci" || ci.Signal != now.SigCIRed || !slices.Contains(ci.Can, "fix") {
		t.Errorf("ci %+v", ci)
	}
	card := webTestInboxItem(t, s, "card:card-1")
	if card.Turn != now.TurnMe || card.Rule != "agents" || card.Level != 1 || card.Why != "Waits for your OK" || card.Source != "agents" ||
		card.WhyAt != clk.UnixMilli() {
		t.Errorf("card %+v", card)
	}
	if b := webTestInboxItem(t, s, "task:build"); b.Turn != now.TurnNone || b.Why != "Running" || b.Rule != "" {
		t.Errorf("running build %+v", b)
	}
	if x := webTestInboxItem(t, s, "task:test"); x.Turn != now.TurnMe || x.Level != 4 || x.Rule != "" || x.Why != "Task failed" {
		t.Errorf("failed test %+v", x)
	}
	if s.Now == nil {
		t.Fatal("the inbox has no Now info")
	}
	if s.Now.Setup || s.Now.Show != 5 || len(s.Now.Rules) != 6 || s.Now.Hidden != 0 {
		t.Fatalf("now info %+v", *s.Now)
	}
	count := map[string]int{}
	for _, r := range s.Now.Rules {
		count[r.ID] = r.Count
	}
	if count["agents"] != 1 || count["redci"] != 1 || count["reviews"] != 0 {
		t.Errorf("rule counts %v", count)
	}
	_ = m
}

// Spec (Now, part 1): now.act puts an item away and undo brings it back:
// Done hides it (counted in Hidden), Later (free text) hides it until its
// time and then puts it back on your turn with "Back from Later", Mute
// hides it until it asks me; a bad time is refused with a hint. Nothing
// acts on the item's source.
func TestNowActDoneLaterMuteUndo(t *testing.T) {
	m, c, clk := webTestNowModel(t)
	webTestNow(t, c)

	if f, _ := webTestCall(t, m, c, webproto.OpNowAct, `{"do":"done","id":"ci:main"}`); f["t"] != "res" {
		t.Fatalf("done: %v", f)
	}
	s := webTestLastInbox(t, m, c)
	if webTestHasItem(s, "ci:main") || s.Now == nil || s.Now.Hidden != 1 {
		t.Fatalf("after done: %+v", s)
	}
	if m.ci == nil || m.ci.snap.State() != ghci.StateFail {
		t.Fatal("done changed the CI source")
	}
	if f, _ := webTestCall(t, m, c, webproto.OpNowAct, `{"do":"undo"}`); f["t"] != "res" {
		t.Fatalf("undo: %v", f)
	}
	*clk = clk.Add(time.Second)
	if s := webTestLastInbox(t, m, c); !webTestHasItem(s, "ci:main") {
		t.Fatal("undo did not bring ci:main back")
	}

	if f, _ := webTestCall(t, m, c, webproto.OpNowAct, `{"do":"later","id":"card:card-1","when":"soonish"}`); f["code"] != "bad_request" {
		t.Fatalf("bad later: %v", f)
	}
	f, _ := webTestCall(t, m, c, webproto.OpNowAct, `{"do":"later","id":"card:card-1","when":"in 2 hours"}`)
	if f["t"] != "res" {
		t.Fatalf("later: %v", f)
	}
	*clk = clk.Add(time.Second)
	if s := webTestLastInbox(t, m, c); webTestHasItem(s, "card:card-1") {
		t.Fatal("a later item shows before its time")
	}
	*clk = clk.Add(2 * time.Hour)
	s = webTestLastInbox(t, m, c)
	if back := webTestInboxItem(t, s, "card:card-1"); back.Turn != now.TurnMe || back.Why != now.WhyBack {
		t.Fatalf("back from later: %+v", back)
	}

	if f, _ := webTestCall(t, m, c, webproto.OpNowAct, `{"do":"mute","id":"task:test"}`); f["t"] != "res" {
		t.Fatalf("mute: %v", f)
	}
	*clk = clk.Add(time.Second)
	if s := webTestLastInbox(t, m, c); webTestHasItem(s, "task:test") {
		t.Fatal("a muted item shows")
	}
	if f, _ := webTestCall(t, m, c, webproto.OpNowAct, `{"do":"done","id":"nope:1"}`); f["code"] != "not_found" {
		t.Fatalf("done of an unknown item: %v", f)
	}
}

// Spec (Now, part 1): now.rules answers the first-run questions (or
// skips them: the defaults stay) and replaces the rule rows (at most 7);
// the items are placed again at once.
func TestNowRulesOp(t *testing.T) {
	m, c, clk := webTestNowModel(t)
	webTestNow(t, c)
	if f, _ := webTestCall(t, m, c, webproto.OpNowRules, `{"setup":{"top":["review"],"from":["github","agents","ci"],"show":3,"hide_bots":true}}`); f["t"] != "res" {
		t.Fatalf("setup: %v", f)
	}
	*clk = clk.Add(time.Second)
	s := webTestLastInbox(t, m, c)
	if !s.Now.Setup || s.Now.Show != 3 {
		t.Fatalf("after setup: %+v", *s.Now)
	}
	if card := webTestInboxItem(t, s, "card:card-1"); card.Rule != "" || card.Level != 4 {
		t.Errorf("agents off, card still placed: %+v", card)
	}
	if f, _ := webTestCall(t, m, c, webproto.OpNowRules, `{"skip":true}`); f["t"] != "res" {
		t.Fatalf("skip: %v", f)
	}
	*clk = clk.Add(time.Second)
	s = webTestLastInbox(t, m, c)
	if card := webTestInboxItem(t, s, "card:card-1"); card.Rule != "agents" || !s.Now.Setup || s.Now.Show != 5 {
		t.Errorf("skip did not keep the defaults: %+v %+v", card, *s.Now)
	}
	rules := `{"rules":[` + `{"id":"a","name":"A","sentence":"s","signal":"agent","level":1,"on":true}`
	for i := 0; i < 7; i++ {
		rules += `,{"id":"r` + string(rune('a'+i)) + `","name":"R","sentence":"s","signal":"mention","level":3,"on":true}`
	}
	rules += `]}`
	if _, err := webproto.DecodeArgs(webproto.OpNowRules, json.RawMessage(rules)); err == nil {
		t.Fatal("8 rules were taken")
	}
}
