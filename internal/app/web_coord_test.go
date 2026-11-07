package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webCoordRig is a web rig with one controller on model m.
func webCoordRig(t *testing.T, m *Model) *webFilesRig {
	t.Helper()
	h := newWebHub()
	t.Cleanup(h.stop)
	m.web = h
	r := &webFilesRig{t: t, m: m, h: h, kept: map[*webConn][]map[string]any{}}
	r.c = r.conn("c-1", webproto.RoleController)
	return r
}

// Spec (MOB-64, mobile plan §0, §3.9): the phone asks for a coordinator
// run; nothing starts until the laptop's card (Cancel first) says yes.
// The card names the profile ("plan" when the phone names none), its
// planner step and the task. Cancel sends nothing; yes sends the task in
// the open chat with that profile once, and the laptop's own profile
// stays.
func TestWebCoordinatorRunThroughTheLaptop(t *testing.T) {
	m, _ := aiRunModel(t, fakeAIProvider{reply: "x"}, fakeAIProvider{reply: "y"})
	r := webCoordRig(t, m)
	before := len(m.ai.conv.Messages)

	f := r.call(webproto.OpAIRunStart, &webproto.AIRunStartArgs{Task: "Add rate limits to the login API", Ref: "TASKS.md#API-7"})
	if f["t"] != "res" {
		t.Fatalf("answer %v", f)
	}
	if !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.Buttons()[0].ID != webproto.ChoiceCancel {
		t.Fatal("no laptop card, or Cancel is not first")
	}
	body := m.web.asks.shown.body
	wantAll(t, body, "Profile plan", "planner", "TASKS.md#API-7", "Add rate limits to the login API", "Reject first", "Requested from")
	if len(m.ai.conv.Messages) != before {
		t.Fatal("something was sent before the laptop said yes")
	}
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceCancel)
	if len(m.ai.conv.Messages) != before {
		t.Fatal("Cancel sent the task")
	}

	r.call(webproto.OpAIRunStart, &webproto.AIRunStartArgs{Task: "Add rate limits to the login API"})
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceConfirm)
	rm := aiRunMsg(t, m)
	if rm.Run.Profile != ai.ProfilePlan {
		t.Fatalf("run profile %q", rm.Run.Profile)
	}
	sent := false
	for _, msg := range m.ai.conv.Messages[before:] {
		if msg.Role == ai.StoredUser && strings.Contains(msg.Text, "Add rate limits to the login API") {
			sent = true
		}
	}
	if !sent {
		t.Fatalf("the task is not the run's message: %+v", m.ai.conv.Messages[before:])
	}
	if m.ai.profile != ai.ProfileReview || m.ai.kwProfile != "" {
		t.Fatalf("the laptop's profile changed: %q kw %q", m.ai.profile, m.ai.kwProfile)
	}
}

// Spec (MOB-64): ai.run_start is refused, with no card, for a profile
// that does not exist, one without a planner step, an untrusted project
// profile, and while the AI answers.
func TestWebCoordinatorRunRefusals(t *testing.T) {
	m, _ := aiRunModel(t, fakeAIProvider{reply: "x"}, fakeAIProvider{reply: "y"})
	r := webCoordRig(t, m)
	for _, c := range []struct{ profile, code string }{
		{"nope", "not_found"},
		{ai.ProfileReview, "bad_request"},
	} {
		if code := webErrCode(r.call(webproto.OpAIRunStart, &webproto.AIRunStartArgs{Task: "x", Profile: c.profile})); code != c.code || m.confirmOpen {
			t.Errorf("%s: %s (card %v)", c.profile, code, m.confirmOpen)
		}
	}
	m.ai.profFake = []ai.Profile{{Name: "repo-plan", Source: ai.ProfileSourceProject,
		Steps: []ai.ProfileStep{{ID: "planner", Slot: ai.SlotMain, Plan: true}}}}
	m.ai.reloadProfiles()
	if code := webErrCode(r.call(webproto.OpAIRunStart, &webproto.AIRunStartArgs{Task: "x", Profile: "repo-plan"})); code != "forbidden" || m.confirmOpen {
		t.Errorf("untrusted project profile: %s", code)
	}
	m.ai.streaming = true
	if code := webErrCode(r.call(webproto.OpAIRunStart, &webproto.AIRunStartArgs{Task: "x"})); code != "busy" || m.confirmOpen {
		t.Errorf("while answering: %s", code)
	}
}

// Spec (MOB-64): the runs topic lists the steps that work now and offers
// "send"; runs.send hands the step the phone's message, which it reads
// after its current tool call, as m on the laptop. A step that does not
// work now is refused, and an ended run offers no send.
func TestWebRunsSendToARunningStep(t *testing.T) {
	n := newNodeCtlRig(t)
	r := webCoordRig(t, n.m)
	snap := n.m.webRunsBuild(clock.Now())
	if len(snap.Runs) == 0 {
		t.Fatal("no run listed")
	}
	run := snap.Runs[0]
	if !slices.Contains(run.Can, "send") || len(run.Running) != 1 || run.Running[0].ID != "reviewer" {
		t.Fatalf("run %+v", run)
	}
	if code := webErrCode(r.call(webproto.OpRunsSend, &webproto.RunsSendArgs{Key: run.Key, Step: "fixer", Text: "x"})); code != "conflict" {
		t.Errorf("a step that waits: %s", code)
	}
	if code := webErrCode(r.call(webproto.OpRunsSend, &webproto.RunsSendArgs{Key: "nope/nope", Text: "x"})); code != "not_found" {
		t.Errorf("unknown run: %s", code)
	}
	if f := r.call(webproto.OpRunsSend, &webproto.RunsSendArgs{Key: run.Key, Text: "check the tests too"}); f["t"] != "res" {
		t.Fatalf("send: %v", f)
	}
	n.reply("first look")
	n.until(func() bool {
		return strings.Contains(strings.Join(n.step("reviewer").Notes, "\n"), "✉ you: check the tests too")
	})
	n.reply("reviewed")
	n.reply("fixed")
	n.until(func() bool { return n.cmd == nil })
	if snap := n.m.webRunsBuild(clock.Now()); len(snap.Runs[0].Running) != 0 || slices.Contains(snap.Runs[0].Can, "send") {
		t.Fatalf("an ended run still offers send: %+v", snap.Runs[0])
	}
}
