//go:build browser

package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The phone chat has the desktop's approved look, phone-sized (owner,
// 2026-11-09): a one-step answer has no run or step card, only a small
// line with its model and time that opens on a tap to its tokens, step
// and CLI; each tool call is one short line that opens on a tap; while
// it writes, the answer says Thinking… with its time and a Stop; a run
// of several steps is a strip above the answer that opens a full-screen
// sheet of its steps; the rules file shows once, at the top; the
// composer has the active profile's model picker.

const phoneChat = `document.querySelector('[data-view="chat"]')`

// phoneShot writes a screenshot to WEB_SHOTS_DIR (when set) as
// phone-chat-<name>.png.
func phoneShot(t *testing.T, p *Page, name, ready string) {
	t.Helper()
	if dir := os.Getenv(shotsEnv); dir != "" {
		snap(t, p, filepath.Join(dir, "phone-chat-"+name+".png"), ready)
	}
}

func TestPhoneChatOneStepReplyIsAChatAnswer(t *testing.T) {
	p, _ := openChat(t, Phone, chatWith(false, userMsg(0, "How many tables are in baaytona-db-1?"), gptReply(1),
		map[string]any{"i": 2, "role": "tool", "text": "⚙ Read a.go → 10 lines"}))
	const ai = phoneChat + `.querySelector('.msg-ai[data-i="1"]')`
	p.WaitJS(ai + `?.querySelector(".msg-meta")`)
	if Eval[bool](p, ai+`.querySelector(".run, details.step, .run-head") !== null`) {
		t.Error("a one-step answer still has a run or step card")
	}
	if got := Eval[string](p, ai+`.querySelector(".msg-meta").textContent.trim()`); got != "gpt-6-sol · 54s" {
		t.Errorf("the line under the answer reads %q, want gpt-6-sol · 54s", got)
	}
	if Eval[bool](p, ai+`.querySelector(".msg-meta-more") !== null`) {
		t.Error("the answer's details show before a tap")
	}
	p.TapJS(ai + `.querySelector(".msg-meta")`)
	p.WaitJS(ai + `.querySelector(".msg-meta-more")`)
	more := Eval[string](p, ai+`.querySelector(".msg-meta-more").textContent`)
	for _, want := range []string{"14,777 in", "32 out", "Step 1", "Codex"} {
		if !strings.Contains(more, want) {
			t.Errorf("the answer's details %q have no %q", more, want)
		}
	}
	rows := Eval[[]string](p, `[...`+ai+`.querySelectorAll(".tool-row .tool-sum")].map((e) => e.textContent.trim())`)
	if len(rows) != 2 || rows[0] != "✓ db_schema baaytona-db-1" || rows[1] != "✗ run_command psql -c 'select 1'" {
		t.Errorf("tool lines %q", rows)
	}
	if Eval[bool](p, ai+`.querySelector(".tool-row .tool-more") !== null`) {
		t.Error("a tool line is open before a tap")
	}
	if h := Eval[float64](p, ai+`.querySelector(".tool-row .tool-sum").getBoundingClientRect().height`); h > 36 {
		t.Errorf("a tool line is %.0f px tall, want one compact line", h)
	}
	p.TapJS(ai + `.querySelector(".tool-row .tool-sum")`)
	p.WaitJS(ai + `.querySelector(".tool-row.is-open .tool-more")?.textContent.includes("35 tables (36 lines)")`)
	if got := Eval[string](p, phoneChat+`.querySelector('.msg-tool[data-i="2"] .tool-sum')?.textContent.trim() ?? ""`); got != "✓ Read a.go" {
		t.Errorf("a plain turn's tool line reads %q", got)
	}
	phoneShot(t, p, "single", ai+`.querySelector(".tool-row.is-open")`)
}

func TestPhoneChatAnswerBeingWrittenSaysThinking(t *testing.T) {
	writing := map[string]any{"i": 1, "role": "assistant", "streaming": true, "text": "Let me look", "model": "gpt-6-sol",
		"meta": map[string]any{"started_ms": time.Now().Add(-12 * time.Second).UnixMilli(), "step": "Step 1", "cli": "Codex"}}
	p, c := openChat(t, Phone, chatWith(true, userMsg(0, "hi"), writing))
	const ai = phoneChat + `.querySelector('.msg-ai[data-i="1"]')`
	p.WaitJS(ai + `?.querySelector(".msg-think")`)
	got := Eval[string](p, ai+`.querySelector(".msg-think").textContent`)
	if !strings.Contains(got, "Thinking…") || !strings.Contains(got, "12s") && !strings.Contains(got, "13s") {
		t.Errorf("the answer being written says %q, want Thinking… 12s", got)
	}
	p.TapJS(ai + `.querySelector(".msg-think button")`)
	expectReq(t, c, "chat.stop")
}

// A run of several steps: a compact strip above the answer, which opens
// a full-screen sheet with each step's model, time, state, output, tool
// lines and error.
func TestPhoneChatRunStripAndSheet(t *testing.T) {
	run := map[string]any{"i": 1, "role": "assistant", "streaming": true, "text": "",
		"run": map[string]any{"id": "r-3", "profile": "plan-code-review", "state": "running", "started_ms": time.Now().Add(-40 * time.Second).UnixMilli()},
		"steps": []any{
			map[string]any{"id": "plan", "title": "Plan", "state": "completed", "model": "claude-opus-5-5", "elapsed_ms": 12000, "md": md("doc", mdPara("1. Add the column."))},
			map[string]any{"id": "code", "title": "Code", "state": "running", "model": "Codex · gpt-6-sol", "tools": []any{"⚙ Edit db.go → 3 lines"}, "text": "Adding the column"},
			map[string]any{"id": "review", "title": "Review", "state": "failed", "model": "claude-haiku-4-5", "error": "HTTP 400: no credit"},
		}}
	p, _ := openChat(t, Phone, chatWith(true, userMsg(0, "Add a created_at column"), run))
	const ai = phoneChat + `.querySelector('.msg-ai[data-i="1"]')`
	p.WaitJS(ai + `?.querySelector(".run-strip")`)
	if Eval[bool](p, ai+`.querySelector(".run-head, details.step") !== null`) {
		t.Error("the run still shows its old card in the chat")
	}
	if got := Eval[[]string](p, `[...`+ai+`.querySelectorAll(".run-strip .strip-step")].map((e) => e.textContent.trim())`); strings.Join(got, "|") != "Plan|Code|Review" {
		t.Errorf("strip steps %q", got)
	}
	if got := Eval[string](p, ai+`.querySelector(".run-strip .strip-where").textContent`); !strings.HasPrefix(got, "2/3") {
		t.Errorf("strip says %q, want 2/3", got)
	}
	if h := Eval[float64](p, ai+`.querySelector(".run-strip").getBoundingClientRect().height`); h > 48 {
		t.Errorf("the strip is %.0f px tall, want one compact line", h)
	}
	if !Eval[bool](p, `!!(`+ai+`.querySelector(".run-strip").compareDocumentPosition(`+ai+`.querySelector(".msg-body")) & Node.DOCUMENT_POSITION_FOLLOWING)`) {
		t.Error("the strip is not above the answer")
	}
	phoneShot(t, p, "strip", ai+`.querySelector(".run-strip")`)
	p.TapJS(ai + `.querySelector(".run-strip")`)
	const rv = `document.querySelector(".sheet .run-view")`
	p.WaitJS(rv + `?.querySelectorAll(".runv-step").length === 3`)
	text := Eval[string](p, rv+`.textContent`)
	for _, want := range []string{"Run view", "plan-code-review", "Plan", "claude-opus-5-5", "12s", "1. Add the column.", "Code", "gpt-6-sol", "Edit db.go", "Adding the column", "Review", "HTTP 400: no credit"} {
		if !strings.Contains(text, want) {
			t.Errorf("the run sheet has no %q:\n%s", want, text)
		}
	}
	if full := Eval[bool](p, `document.querySelector(".sheet:has(.run-view)").getBoundingClientRect().height >= innerHeight * 0.95`); !full {
		t.Error("the run sheet is not full-screen")
	}
	phoneShot(t, p, "sheet", rv)
	p.TapJS(rv + `.querySelector(".runv-close")`)
	p.WaitJS(`!document.querySelector(".run-view")`)
}

func TestPhoneChatRulesOnceAtTop(t *testing.T) {
	u0, u2 := userMsg(0, "hi"), userMsg(2, "and now?")
	u0["rules"], u2["rules"] = "AGENTS.md (679 B)", "AGENTS.md (679 B)"
	p, _ := openChat(t, Phone, chatWith(false, u0, aiMsg(1, md("doc", mdPara("Hello."))), u2))
	p.WaitJS(phoneChat + `?.querySelector(".chat-rules")`)
	if got := Eval[string](p, phoneChat+`.querySelector(".chat-rules").textContent.trim()`); got != "Rules: AGENTS.md (679 B)" {
		t.Errorf("the rules line reads %q", got)
	}
	if n := Eval[int](p, `[...`+phoneChat+`.querySelectorAll(".msg-extras .chip")].filter((c) => c.offsetParent && c.textContent.includes("AGENTS.md")).length`); n != 0 {
		t.Errorf("%d messages still show the rules chip", n)
	}
	if n := Eval[int](p, phoneChat+`.querySelectorAll(".chat-rules").length`); n != 1 {
		t.Errorf("%d rules lines, want 1", n)
	}
}

// The phone's composer has the active profile's model picker: the same
// ai topic as the desktop's; a tap opens a sheet of its provider's
// models with a check on the one in use, and picking one asks first.
func TestPhonePickerListsTheProfilesModels(t *testing.T) {
	p, c := openChat(t, Phone, chatWith(false, userMsg(0, "hi")))
	snap := aiFixture(true, true)
	snap.Profile, snap.Provider, snap.Model = "review", "Codex", "gpt-6-sol"
	snap.Models = []string{"gpt-6.1-sol", "gpt-6-astra", "gpt-6-sol"}
	c.Send(t, Snap("ai", 1, snap))
	// The picker waits behind ✦ at the composer's start (phonecompact_test.go).
	p.TapJS(phoneChat + `.querySelector('.composer .pick-toggle')`)
	const model = phoneChat + `.querySelector('.composer .ai-pick[data-pick="model"]')`
	p.WaitJS(model + `?.textContent.includes("gpt-6-sol")`)
	if !Eval[bool](p, phoneChat+`.querySelector('.composer .ai-pick[data-pick="profile"]')?.textContent.includes("review")`) {
		t.Error("the profile button does not name the active profile")
	}
	p.TapJS(model)
	const rows = `[...document.querySelectorAll(".sheet .ai-menu .actsheet-row")]`
	p.WaitJS(rows + `.length === 3`)
	if got := Eval[string](p, rows+`.map((b) => b.querySelector(".ai-menu-label").textContent).join("|")`); got != "gpt-6.1-sol|gpt-6-astra|gpt-6-sol" {
		t.Errorf("models %q", got)
	}
	if got := Eval[string](p, `document.querySelector(".sheet .ai-menu .actsheet-title").textContent`); got != "Codex" {
		t.Errorf("the list is named %q, want its provider Codex", got)
	}
	if got := Eval[string](p, rows+`.filter((b) => b.textContent.includes("✓")).map((b) => b.querySelector(".ai-menu-label").textContent).join("|")`); got != "gpt-6-sol" {
		t.Errorf("checked %q, want gpt-6-sol", got)
	}
	p.TapJS(rows + `[1]`)
	p.WaitJS(`[...document.querySelectorAll(".sheet")].some((s) => s.textContent.includes("Use model gpt-6-astra for the AI chat?"))`)
}
