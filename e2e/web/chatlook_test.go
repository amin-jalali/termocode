//go:build browser

package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The desktop chat reads as a chat (owner, 2026-11-09): an answer of a
// one-step profile has no run or step card under it, only a small line
// with its model and time (the rest in its tooltip); a tool call is one
// short line in the answer that opens to show the call and its result;
// while it writes, the answer says Thinking… with its time and a Stop.
// The phone keeps its own look (TestChatRunSteps).

const deskChat = `document.querySelector('.chatcol [data-view="chat"]')`

// chatShot writes a screenshot to WEB_SHOTS_DIR (when set) as
// chat-look-<name>.png.
func chatShot(t *testing.T, p *Page, name, ready string) {
	t.Helper()
	if dir := os.Getenv(shotsEnv); dir != "" {
		snap(t, p, filepath.Join(dir, "chat-look-"+name+".png"), ready)
	}
}

// gptReply is the owner's GPT answer: one Codex step on gpt-6-sol that
// called two tools, the second failed.
func gptReply(i int) map[string]any {
	return map[string]any{"i": i, "role": "assistant", "model": "gpt-6-sol",
		"md":   md("doc", mdPara("The project schema defines 35 tables.")),
		"meta": map[string]any{"elapsed_ms": 54104, "in": 14777, "out": 32, "step": "Step 1", "cli": "Codex"},
		"steps": []any{map[string]any{"id": "step-1", "title": "Step 1", "model": "Codex · gpt-6-sol", "state": "completed",
			"tools": []any{"⚙ db_schema baaytona-db-1 → 35 tables (36 lines)", "⚙ run_command psql -c 'select 1' → error: exit status 2"}}}}
}

func TestDeskChatOneStepReplyIsAChatAnswer(t *testing.T) {
	p, _ := openChat(t, Desktop, chatWith(false, userMsg(0, "How many tables are in baaytona-db-1?"), gptReply(1),
		map[string]any{"i": 2, "role": "tool", "text": "⚙ Read a.go → 10 lines"}))
	const ai = deskChat + `.querySelector('.msg-ai[data-i="1"]')`
	p.WaitJS(ai + `?.querySelector(".msg-meta")`)
	if Eval[bool](p, ai+`.querySelector(".run, details.step, .run-head") !== null`) {
		t.Error("a one-step answer still has a run or step card")
	}
	if got := Eval[string](p, ai+`.querySelector(".msg-meta").textContent.trim()`); got != "gpt-6-sol · 54s" {
		t.Errorf("the line under the answer reads %q, want gpt-6-sol · 54s", got)
	}
	tip := Eval[string](p, ai+`.querySelector(".msg-meta").title`)
	for _, want := range []string{"14,777 in", "32 out", "Step 1", "Codex"} {
		if !strings.Contains(tip, want) {
			t.Errorf("the line's tooltip %q has no %q", tip, want)
		}
	}
	rows := Eval[[]string](p, `[...`+ai+`.querySelectorAll(".tool-row .tool-sum")].map((e) => e.textContent.trim())`)
	if len(rows) != 2 || rows[0] != "✓ db_schema baaytona-db-1" || rows[1] != "✗ run_command psql -c 'select 1'" {
		t.Errorf("tool lines %q", rows)
	}
	if Eval[bool](p, ai+`.querySelector(".tool-row .tool-more") !== null`) {
		t.Error("a tool line is open before a click")
	}
	p.TapJS(ai + `.querySelector(".tool-row .tool-sum")`)
	p.WaitJS(ai + `.querySelector(".tool-row.is-open .tool-more")?.textContent.includes("35 tables (36 lines)")`)
	// A tool line of a plain chat turn looks the same.
	if got := Eval[string](p, deskChat+`.querySelector('.msg-tool[data-i="2"] .tool-sum')?.textContent.trim() ?? ""`); got != "✓ Read a.go" {
		t.Errorf("a plain turn's tool line reads %q", got)
	}
	chatShot(t, p, "single", ai+`.querySelector(".tool-row.is-open")`)
}

func TestDeskChatAnswerBeingWrittenSaysThinking(t *testing.T) {
	writing := map[string]any{"i": 1, "role": "assistant", "streaming": true, "text": "Let me look", "model": "gpt-6-sol",
		"meta": map[string]any{"started_ms": time.Now().Add(-12 * time.Second).UnixMilli(), "step": "Step 1", "cli": "Codex"}}
	p, c := openChat(t, Desktop, chatWith(true, userMsg(0, "hi"), writing))
	const ai = deskChat + `.querySelector('.msg-ai[data-i="1"]')`
	p.WaitJS(ai + `?.querySelector(".msg-think")`)
	got := Eval[string](p, ai+`.querySelector(".msg-think").textContent`)
	if !strings.Contains(got, "Thinking…") || !strings.Contains(got, "12s") && !strings.Contains(got, "13s") {
		t.Errorf("the answer being written says %q, want Thinking… 12s", got)
	}
	p.TapJS(ai + `.querySelector(".msg-think button")`)
	expectReq(t, c, "chat.stop")
}
