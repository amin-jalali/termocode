package agentrun

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// teamHandle is a run with steps a, b (title Beta) and c running and
// team mode on.
func teamHandle(o Options) *Handle {
	o.Team, o.Parallel = true, 3
	h := &Handle{o: o, id: "r1", run: agentdag.Run{ID: "r1", Nodes: []agentdag.Node{
		{ID: "a", Title: "Alpha"}, {ID: "b", Title: "Beta"}, {ID: "c", Title: "Gamma"}, {ID: "d", Title: "Later"},
	}}}
	for _, id := range []string{"a", "b", "c"} {
		h.mail.openNode(id)
	}
	return h
}

// OMX-40: a step sends a note to one teammate by id or title; it lands
// in that step's mailbox with the sender named.
func TestTeamSendToOneTeammate(t *testing.T) {
	h := teamHandle(Options{})
	to, err := h.TeamSend("a", "beta", "I took parser.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(to) != 1 || to[0] != "b" {
		t.Fatalf("sent to %v", to)
	}
	got := h.mail.take("b")
	if len(got) != 1 || !strings.HasPrefix(got[0], "From step a (Alpha): ") || !strings.HasSuffix(got[0], "I took parser.go") {
		t.Fatalf("b's box %q", got)
	}
	if left := h.mail.take("c"); len(left) != 0 {
		t.Fatalf("c got %q", left)
	}
}

// "all" (or no to) reaches every other running step, never the sender.
func TestTeamSendBroadcast(t *testing.T) {
	h := teamHandle(Options{})
	to, err := h.TeamSend("a", "", "schema changed")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(to, ",") != "b,c" {
		t.Fatalf("sent to %v", to)
	}
	if len(h.mail.take("a")) != 0 {
		t.Fatal("the sender got its own note")
	}
	if _, err := h.TeamSend("a", "all", "again"); err != nil {
		t.Fatal(err)
	}
}

// A note to itself, to a step that does not run, or with nobody else
// running is refused with a reason.
func TestTeamSendRefusals(t *testing.T) {
	h := teamHandle(Options{})
	for _, tc := range []struct{ to, want string }{
		{"a", "yourself"},
		{"d", "does not run now"},
		{"zzz", "no step"},
	} {
		if _, err := h.TeamSend("a", tc.to, "x"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("to %q: err %v, want %q", tc.to, err, tc.want)
		}
	}
	h.mail.closeNode("b", func(string) {})
	h.mail.closeNode("c", func(string) {})
	if _, err := h.TeamSend("a", "", "x"); err == nil || !strings.Contains(err.Error(), "no other step") {
		t.Errorf("alone: err %v", err)
	}
}

// The app's check (secrets) refuses a note before it is queued.
func TestTeamSendCheck(t *testing.T) {
	h := teamHandle(Options{TeamCheck: func(string) error { return errors.New("looks like a secret") }})
	if _, err := h.TeamSend("a", "b", "sk-123"); err == nil {
		t.Fatal("check did not refuse")
	}
	if len(h.mail.take("b")) != 0 {
		t.Fatal("refused note was queued")
	}
}

// Claims: the first step gets a key, a second one is told who holds it;
// the owner may claim again and release; others may not release.
func TestTeamClaims(t *testing.T) {
	h := teamHandle(Options{})
	if owner, ok, err := h.Claim("a", " internal/x.go "); err != nil || !ok || owner != "a" {
		t.Fatalf("a claim: %q %v %v", owner, ok, err)
	}
	if owner, ok, _ := h.Claim("b", "internal/x.go"); ok || owner != "a" {
		t.Fatalf("b claim: %q %v", owner, ok)
	}
	if _, ok, _ := h.Claim("a", "internal/x.go"); !ok {
		t.Fatal("owner claim again refused")
	}
	if err := h.Release("b", "internal/x.go"); err == nil {
		t.Fatal("b released a's claim")
	}
	if got := h.Claims(); len(got) != 1 || got["internal/x.go"] != "a" {
		t.Fatalf("claims %v", got)
	}
	if err := h.Release("a", "internal/x.go"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := h.Claim("b", "internal/x.go"); !ok {
		t.Fatal("free key refused")
	}
	if _, _, err := h.Claim("a", "  "); err == nil {
		t.Fatal("empty key accepted")
	}
}

// A step that ends drops its claims, so they never block the others.
func TestTeamClaimsDroppedWhenStepEnds(t *testing.T) {
	h := teamHandle(Options{})
	h.Claim("a", "k1")
	h.Claim("a", "k2")
	h.Claim("b", "k3")
	if got := h.releaseAll("a"); strings.Join(got, ",") != "k1,k2" {
		t.Fatalf("released %v", got)
	}
	if got := h.Claims(); len(got) != 1 || got["k3"] != "b" {
		t.Fatalf("claims %v", got)
	}
}

// The claim list is capped.
func TestTeamClaimsCap(t *testing.T) {
	h := teamHandle(Options{})
	for i := range TeamMaxClaims {
		if _, ok, err := h.Claim("a", "k"+strings.Repeat("x", i)); !ok || err != nil {
			t.Fatalf("claim %d: %v %v", i, ok, err)
		}
	}
	if _, _, err := h.Claim("b", "one-more"); err == nil {
		t.Fatal("cap not enforced")
	}
}

// withTeam adds the four team tools and the team note to an agent step
// only with team mode on and more than one step at a time.
func TestWithTeamAddsToolsAndNote(t *testing.T) {
	h := teamHandle(Options{})
	ag := &ai.Agent{Tools: []ai.Tool{{Name: "read_file"}}}
	hist := []ai.Message{{Role: ai.RoleSystem, Content: "sys"}, {Role: ai.RoleUser, Content: "go"}}
	hist = h.withTeam(agentdag.Node{ID: "a"}, ag, hist)
	var names []string
	for _, tl := range ag.Tools {
		names = append(names, tl.Name)
	}
	if strings.Join(names, ",") != "read_file,team_send,team_claim,team_release,team_claims" {
		t.Fatalf("tools %v", names)
	}
	if !strings.Contains(hist[0].Content, TeamNote) || hist[1].Content != "go" {
		t.Fatalf("history %+v", hist)
	}
	off := teamHandle(Options{})
	off.o.Team = false
	ag2 := &ai.Agent{}
	off.withTeam(agentdag.Node{ID: "a"}, ag2, nil)
	if len(ag2.Tools) != 0 {
		t.Fatal("team tools with team mode off")
	}
	one := teamHandle(Options{})
	one.o.Parallel = 1
	ag3 := &ai.Agent{}
	one.withTeam(agentdag.Node{ID: "a"}, ag3, nil)
	if len(ag3.Tools) != 0 {
		t.Fatal("team tools with one step at a time")
	}
}

// The tools work through Run with the step from the context.
func TestTeamToolsRun(t *testing.T) {
	h := teamHandle(Options{})
	ctx := context.Background()
	tools := h.teamTools("a")
	find := func(name string) ai.Tool {
		for _, tl := range tools {
			if tl.Name == name {
				return tl
			}
		}
		t.Fatalf("no %s", name)
		return ai.Tool{}
	}
	if out, err := find("team_claim").Run(ctx, map[string]any{"key": "db.go"}); err != nil || !strings.Contains(out, "claimed") {
		t.Fatalf("claim: %q %v", out, err)
	}
	h.Claim("b", "api.go")
	out, err := find("team_claim").Run(ctx, map[string]any{"key": "api.go"})
	if err != nil || !strings.Contains(out, "held by step b") {
		t.Fatalf("held claim: %q %v", out, err)
	}
	if out, _ := find("team_claims").Run(ctx, nil); !strings.Contains(out, "db.go: a") || !strings.Contains(out, "api.go: b") {
		t.Fatalf("claims: %q", out)
	}
	if out, err := find("team_send").Run(ctx, map[string]any{"to": "b", "message": "hi"}); err != nil || !strings.Contains(out, "b") {
		t.Fatalf("send: %q %v", out, err)
	}
	if _, err := find("team_release").Run(ctx, map[string]any{"key": "db.go"}); err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools {
		if tl.Name != "team_send" && !tl.ReadOnly {
			t.Errorf("%s should not ask a card (it touches no files)", tl.Name)
		}
	}
}
