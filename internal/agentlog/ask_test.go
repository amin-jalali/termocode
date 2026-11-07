package agentlog

import (
	"strings"
	"testing"
)

const askLines = `{"type":"user","sessionId":"s2","cwd":"/w","timestamp":"2026-11-05T10:00:00Z","message":{"role":"user","content":"Plan the cache."}}
{"type":"assistant","sessionId":"s2","timestamp":"2026-11-05T10:00:05Z","message":{"id":"m1","role":"assistant","content":[{"type":"tool_use","id":"q1","name":"AskUserQuestion","input":{"questions":[{"header":"Database","question":"Which database do you want for the integration tests?","multiSelect":false,"options":[{"label":"SQLite","description":"Fast, no server needed"},{"label":"Postgres","description":"Same as production"}]}]}}]}}
{"type":"user","sessionId":"s2","timestamp":"2026-11-05T10:00:09Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"q1","content":"User answered: SQLite"}]}}
{"type":"assistant","sessionId":"s2","timestamp":"2026-11-05T10:00:20Z","message":{"id":"m2","role":"assistant","content":[{"type":"tool_use","id":"p1","name":"ExitPlanMode","input":{"plan":"# Cache plan\n\n1. Add an LRU in cache.go\n2. Test it with SQLite\n\nToken: sk-ant-api03-QQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQ"}}]}}
`

// Spec (CMP-14): a question or plan-approval call keeps its full text
// (Detail, redacted) for the native panel of the chat view: the
// question(s) with their numbered options and descriptions, or the plan
// as written. Other calls have no Detail. Pending is the newest call
// still without its result.
func TestAskDetailAndPending(t *testing.T) {
	s, err := Parse(Claude, strings.NewReader(askLines))
	if err != nil {
		t.Fatal(err)
	}
	var q, p Turn
	for _, tr := range s.Turns {
		switch tr.Tool {
		case "AskUserQuestion":
			if tr.Kind == ToolCall {
				q = tr
			}
		case "ExitPlanMode":
			p = tr
		}
	}
	wantQ := "Database: Which database do you want for the integration tests?\n  1. SQLite — Fast, no server needed\n  2. Postgres — Same as production"
	if q.Detail != wantQ {
		t.Errorf("question detail:\n%q\nwant\n%q", q.Detail, wantQ)
	}
	if !strings.HasPrefix(p.Detail, "# Cache plan\n\n1. Add an LRU in cache.go\n2. Test it with SQLite") {
		t.Errorf("plan detail: %q", p.Detail)
	}
	if strings.Contains(p.Detail, "QQQQ") {
		t.Errorf("secret left in the plan: %q", p.Detail)
	}
	pend, ok := s.Pending()
	if !ok || pend.Tool != "ExitPlanMode" {
		t.Errorf("pending %+v %v, want the plan call", pend, ok)
	}
	// Once answered, nothing is pending.
	done := askLines + `{"type":"user","sessionId":"s2","timestamp":"2026-11-05T10:00:30Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"p1","content":"User approved the plan"}]}}` + "\n"
	s, _ = Parse(Claude, strings.NewReader(done))
	if pend, ok := s.Pending(); ok {
		t.Errorf("pending after the result: %+v", pend)
	}
	// A plain call has no Detail.
	s = parseFixture(t, Claude, claudeFixture)
	for _, tr := range s.Turns {
		if tr.Detail != "" {
			t.Errorf("%s call has detail %q", tr.Tool, tr.Detail)
		}
	}
}
