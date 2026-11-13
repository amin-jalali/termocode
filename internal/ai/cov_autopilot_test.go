package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseGuardianReasonStaysValidText(t *testing.T) {
	ok, r := ParseGuardian("ASK: x" + strings.Repeat("é", 200))
	if ok || !utf8.ValidString(r) {
		t.Errorf("a long reason must stay valid text: ok=%v valid=%v", ok, utf8.ValidString(r))
	}
	if n := utf8.RuneCountInString(r); n > 161 {
		t.Errorf("reason is %d runes", n)
	}
}

type guardianProvider struct {
	answer string
	err    error
	seen   *Request
}

func (g guardianProvider) Name() string        { return "g" }
func (g guardianProvider) Configured() bool    { return true }
func (g guardianProvider) SupportsTools() bool { return false }
func (g guardianProvider) Stream(_ context.Context, req Request) <-chan Chunk {
	if g.seen != nil {
		*g.seen = req
	}
	ch := make(chan Chunk, 2)
	if g.err != nil {
		ch <- Chunk{Done: true, Err: g.err}
	} else {
		ch <- Chunk{Delta: g.answer}
		ch <- Chunk{Done: true}
	}
	close(ch)
	return ch
}

func TestGuardianModelReview(t *testing.T) {
	if NewGuardian(nil, "m") != nil {
		t.Error("no provider, no guardian")
	}
	var req Request
	g := NewGuardian(guardianProvider{answer: "ALLOW", seen: &req}, "quick-model")
	ok, _ := g(context.Background(), Tool{Name: "run_command"}, "Run: ls ghp_abcdefghijklmnopqrstuvwxyz0123456789", RiskMedium)
	if !ok {
		t.Error("ALLOW lets the call run")
	}
	if req.Model != "quick-model" || len(req.Messages) != 2 {
		t.Fatalf("request %+v", req)
	}
	if strings.Contains(req.Messages[1].Content, "ghp_abcdef") || !strings.Contains(req.Messages[1].Content, "Tool: run_command") {
		t.Errorf("the guardian sees the action with secrets taken out: %q", req.Messages[1].Content)
	}
	g = NewGuardian(guardianProvider{err: errors.New("down")}, "m")
	if ok, why := g(context.Background(), Tool{Name: "t"}, "x", RiskMedium); ok || !strings.Contains(why, "did not answer") {
		t.Errorf("a failed review asks the user: %v %q", ok, why)
	}
	g = NewGuardian(guardianProvider{answer: "ASK: it deletes files"}, "m")
	if ok, why := g(context.Background(), Tool{Name: "t"}, "x", RiskMedium); ok || why != "it deletes files" {
		t.Errorf("ASK: %v %q", ok, why)
	}
}

func TestAutopilotGuardianWithoutReasonStillAsks(t *testing.T) {
	p := &ApprovalPolicy{Autopilot: AutopilotMedium, Guardian: func(context.Context, Tool, string, Risk) (bool, string) { return false, "" }}
	tool := Tool{Name: "edit_file", Mutating: true, Class: ClassFileWrite}
	v, why, _ := p.Decide(context.Background(), tool, map[string]any{"path": "a.go"}, "Edit a.go", false)
	if v != VerdictAsk || !strings.Contains(why, "guardian asks you to check") {
		t.Errorf("verdict %v %q", v, why)
	}
}

func TestApprovalGlobsAndLevels(t *testing.T) {
	for _, c := range []struct {
		p, s string
		want bool
	}{
		{"run: npm *", "run: npm test", true}, {"a?c", "abc", true}, {"a?c", "ac", false}, {"a?", "a", false},
		{"*", "", true}, {"a*", "b", false}, {"a*c*", "abxcx", true}, {"abc", "ab", false}, {"ab", "abc", false},
	} {
		if globText(c.p, c.s) != c.want {
			t.Errorf("globText(%q,%q) != %v", c.p, c.s, c.want)
		}
	}
	if (ApprovalRule{Match: "x"}).matches("t", "x") {
		t.Error("a rule without a tool matches nothing")
	}
	if CheckAutopilot("bogus") != AutopilotOff || autopilotMax("bogus") != -1 || autopilotMax(AutopilotLow) != RiskLow {
		t.Error("autopilot levels")
	}
}
