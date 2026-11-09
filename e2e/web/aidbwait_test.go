//go:build browser

package web

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The chat says what its tool waits for (owner's bug, phone 2026-11-09:
// an AI chat sat 4+ minutes on "Read the schema of 001" while its db_*
// tool waited on a password nobody saw). While the hub's newest message
// carries a wait, its tool line shows "Waiting for you: <text>" with an
// Answer button, on the phone and the desktop. Putting the question's
// sheet off (Escape, or a tap beside it) keeps the question open, it does not
// cancel the tool; Answer opens it again. The message without the wait
// drops the line.

// aiWaitReply is a one-step reply whose db_schema call waits (wait set).
func aiWaitReply(wait bool) map[string]any {
	m := map[string]any{"i": 1, "role": "assistant", "streaming": true, "text": "", "model": "gpt-6-sol",
		"meta":  map[string]any{"started_ms": time.Now().Add(-20 * time.Second).UnixMilli(), "step": "Step 1"},
		"steps": []any{map[string]any{"id": "step-1", "title": "Step 1", "state": "running", "tools": []any{"⚙ Read the schema of 001"}}}}
	if wait {
		m["wait"] = map[string]any{"text": "Database password for BaaytonaDB", "conn": "baaytonadb", "need": "password"}
	}
	return m
}

func TestAIChatSaysWhatTheToolWaitsFor(t *testing.T) {
	for _, vp := range []Viewport{Phone, Desktop} {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
			c := p.LoginMeta(hereMeta("c-aidbw"))
			chat := phoneChat
			if vp.Name == Desktop.Name {
				chat = deskChat
			}
			line := chat + `?.querySelector('.msg-ai[data-i="1"] .tool-wait')`
			c.Send(t, Snap("chat", 1, chatWith(true, userMsg(0, "Read the schema of 001"), aiWaitReply(true))))
			p.WaitJS(chat + `?.querySelector('.msg-ai[data-i="1"] .tool-row')`)

			// No question in this page yet: the line, no button.
			p.WaitJS(`(` + line + `)?.textContent.includes("Waiting for you: Database password for BaaytonaDB")`)
			if Eval[bool](p, `!!(`+line+`)?.querySelector("button")`) {
				t.Error("an Answer button with no question to open")
			}

			// The question comes: its sheet opens; Escape puts it off
			// without cancelling the tool.
			c.Send(t, aiDBAsk("ask-w1", webproto.DBNeed{Need: "password"}, "The database needs a password."))
			p.WaitJS(`document.activeElement?.matches('.sheet .db-need input[type="password"]')`)
			p.Key(kb.Escape)
			p.WaitJS(`!document.querySelector(".sheet .db-need")`)
			noAnswer(t, c, 300*time.Millisecond)

			// Answer on the tool line opens it again.
			p.WaitJS(`(` + line + `)?.querySelector("button")?.textContent === "Answer"`)
			p.TapJS(line + `.querySelector("button")`)
			p.WaitJS(`!!document.querySelector('.sheet .db-need input[type="password"]')`)
			if got := p.Text(".sheet .row-ask-text"); got != "The database needs a password." {
				t.Fatalf("the sheet asks %q", got)
			}

			// Answered (on the laptop): the sheet closes; the message
			// without its wait drops the line.
			aiDBAnswered(t, c, "ask-w1", "laptop")
			p.WaitJS(`!document.querySelector(".sheet .db-need")`)
			c.Send(t, Delta("chat", 2, map[string]any{"op": "message", "msg": aiWaitReply(false)}))
			p.WaitJS(`!(` + line + `) && !!` + chat + `?.querySelector('.msg-ai[data-i="1"] .tool-row')`)
			if pr := p.Problems(); len(pr) > 0 {
				t.Errorf("problems: %q", pr)
			}
		})
	}
}
