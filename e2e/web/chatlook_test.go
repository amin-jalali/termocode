//go:build browser

package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop chat reads as a chat (owner, 2026-11-09): an answer of a
// one-step profile has no run or step card under it, only a small line
// with its model and time (the rest in its tooltip); a tool call is one
// short line in the answer that opens to show the call and its result;
// while it writes, the answer says Thinking… with its time and a Stop.
// The phone has the same look, phone-sized (phonechatlook_test.go).

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
	t0 := time.Now()
	writing := map[string]any{"i": 1, "role": "assistant", "streaming": true, "text": "Let me look", "model": "gpt-6-sol",
		"meta": map[string]any{"started_ms": t0.Add(-12 * time.Second).UnixMilli(), "step": "Step 1", "cli": "Codex"}}
	p, c := openChat(t, Desktop, chatWith(true, userMsg(0, "hi"), writing))
	const ai = deskChat + `.querySelector('.msg-ai[data-i="1"]')`
	p.WaitJS(ai + `?.querySelector(".msg-think")`)
	got := Eval[string](p, ai+`.querySelector(".msg-think").textContent`)
	// 12 s when the answer was sent, plus the time the page took to open
	// it (seconds on a slow runner).
	most := 13 + int(time.Since(t0)/time.Second)
	secs := regexp.MustCompile(`(\d+)s`).FindStringSubmatch(got)
	n := -1
	if secs != nil {
		n, _ = strconv.Atoi(secs[1])
	}
	if !strings.Contains(got, "Thinking…") || n < 12 || n > most {
		t.Errorf("the answer being written says %q, want Thinking… 12s to %ds", got, most)
	}
	p.TapJS(ai + `.querySelector(".msg-think button")`)
	expectReq(t, c, "chat.stop")
}

// A run of several steps shows a small strip of its steps above the
// answer; Show opens the Run view beside the chat with each step's
// model, time, state, output, tool calls and error.
func TestDeskChatRunStripAndRunView(t *testing.T) {
	run := map[string]any{"i": 1, "role": "assistant", "streaming": true, "text": "",
		"run": map[string]any{"id": "r-3", "profile": "plan-code-review", "state": "running", "started_ms": time.Now().Add(-40 * time.Second).UnixMilli()},
		"steps": []any{
			map[string]any{"id": "plan", "title": "Plan", "state": "completed", "model": "claude-opus-5-5", "elapsed_ms": 12000, "md": md("doc", mdPara("1. Add the column."))},
			map[string]any{"id": "code", "title": "Code", "state": "running", "model": "Codex · gpt-6-sol", "tools": []any{"⚙ Edit db.go → 3 lines"}, "text": "Adding the column"},
			map[string]any{"id": "review", "title": "Review", "state": "failed", "model": "claude-haiku-4-5", "error": "HTTP 400: no credit"},
		}}
	p, _ := openChat(t, Desktop, chatWith(true, userMsg(0, "Add a created_at column"), run))
	const ai = deskChat + `.querySelector('.msg-ai[data-i="1"]')`
	p.WaitJS(ai + `?.querySelector(".run-strip")`)
	if Eval[bool](p, ai+`.querySelector(".run-head, details.step") !== null`) {
		t.Error("the run still shows its old card in the chat")
	}
	if got := Eval[[]string](p, `[...`+ai+`.querySelectorAll(".run-strip .strip-step")].map((e) => e.textContent.trim())`); strings.Join(got, "|") != "Plan|Code|Review" {
		t.Errorf("strip steps %q", got)
	}
	if got := Eval[string](p, ai+`.querySelector(".run-strip .strip-where").textContent`); !strings.HasPrefix(got, "step 2 of 3 · 4") {
		t.Errorf("strip says %q, want step 2 of 3 · 40s", got)
	}
	if !Eval[bool](p, `!!(`+ai+`.querySelector(".run-strip").compareDocumentPosition(`+ai+`.querySelector(".msg-body")) & Node.DOCUMENT_POSITION_FOLLOWING)`) {
		t.Error("the strip is not above the answer")
	}
	if Eval[bool](p, `document.querySelector(".run-view") !== null`) {
		t.Fatal("the Run view is open before Show")
	}
	p.TapJS(ai + `.querySelector(".run-strip .strip-show")`)
	const rv = `document.querySelector(".run-view")`
	p.WaitJS(rv + `?.querySelectorAll(".runv-step").length === 3`)
	text := Eval[string](p, rv+`.textContent`)
	for _, want := range []string{"Run view", "plan-code-review", "Plan", "claude-opus-5-5", "12s", "1. Add the column.", "Code", "gpt-6-sol", "Edit db.go", "Adding the column", "Review", "HTTP 400: no credit"} {
		if !strings.Contains(text, want) {
			t.Errorf("the Run view has no %q:\n%s", want, text)
		}
	}
	chatShot(t, p, "run", rv)
	p.TapJS(rv + `.querySelector(".runv-close")`)
	p.WaitJS(`!document.querySelector(".run-view")`)
}

// The rules file a turn used shows once, at the top of the chat, not
// on every message.
func TestDeskChatRulesOnceAtTop(t *testing.T) {
	u0, u2 := userMsg(0, "hi"), userMsg(2, "and now?")
	u0["rules"], u2["rules"] = "AGENTS.md (679 B)", "AGENTS.md (679 B)"
	p, _ := openChat(t, Desktop, chatWith(false, u0, aiMsg(1, md("doc", mdPara("Hello."))), u2))
	p.WaitJS(deskChat + `?.querySelector(".chat-rules")`)
	if got := Eval[string](p, deskChat+`.querySelector(".chat-rules").textContent.trim()`); got != "Rules: AGENTS.md (679 B)" {
		t.Errorf("the rules line reads %q", got)
	}
	if n := Eval[int](p, `[...`+deskChat+`.querySelectorAll(".msg-extras .chip")].filter((c) => c.offsetParent && c.textContent.includes("AGENTS.md")).length`); n != 0 {
		t.Errorf("%d messages still show the rules chip", n)
	}
	if n := Eval[int](p, deskChat+`.querySelectorAll(".chat-rules").length`); n != 1 {
		t.Errorf("%d rules lines, want 1", n)
	}
}

// The model picker lists the active profile's provider's models (the
// owner's GPT profile: Codex's), with a check on the one in use; a
// profile of several steps shows its steps and their models instead.
func TestDeskAIPickerListsTheProfilesModels(t *testing.T) {
	p, c := openFiles(t, Desktop)
	c.Send(t, Snap("chat", 1, chatWith(false, userMsg(0, "hi"))))
	snap := aiFixture(true, true)
	snap.Profile, snap.Provider, snap.Model = "review", "Codex", "gpt-6-sol"
	snap.Models = []string{"gpt-6.1-sol", "gpt-6-astra", "gpt-6-sol"}
	c.Send(t, Snap("ai", 1, snap))
	const model = `document.querySelector('.ai-pick[data-pick="model"]')`
	p.WaitJS(model + `?.textContent.includes("gpt-6-sol")`)
	p.TapJS(model)
	p.WaitJS(`document.querySelector(".ctx-menu .ctx-item")`)
	if got := menuLabels(p); got != "gpt-6.1-sol|gpt-6-astra|gpt-6-sol" {
		t.Errorf("models %q", got)
	}
	if got := Eval[string](p, `document.querySelector(".ctx-menu").getAttribute("aria-label")`); got != "Codex" {
		t.Errorf("the list is named %q, want its provider Codex", got)
	}
	if got := Eval[string](p, `[...document.querySelectorAll(".ctx-menu .ctx-item")].filter((b) => b.querySelector(".ctx-keys")?.textContent === "✓").map((b) => b.querySelector(".ctx-label").textContent).join("|")`); got != "gpt-6-sol" {
		t.Errorf("checked %q, want gpt-6-sol", got)
	}
	p.Key(kb.Escape)
	p.WaitJS(`!document.querySelector(".ctx-menu")`)

	snap.Model, snap.Provider, snap.Models = "", "", []string{}
	snap.Steps = []webproto.AIStep{{Title: "Reviewer", Model: "claude-opus-5-5"}, {Title: "Fixer", Model: "claude-haiku-4-5"}}
	c.Send(t, Snap("ai", 2, snap))
	p.WaitJS(model + `?.textContent.includes("2 steps")`)
	p.TapJS(model)
	p.WaitJS(`document.querySelector(".ctx-menu .ctx-item")`)
	if got := menuLabels(p); got != "Reviewer · claude-opus-5-5|Fixer · claude-haiku-4-5" {
		t.Errorf("steps %q", got)
	}
	if got := Eval[string](p, `document.querySelector(".ctx-menu").getAttribute("aria-label")`); got != "review" {
		t.Errorf("the steps list is named %q, want the profile review", got)
	}
}
