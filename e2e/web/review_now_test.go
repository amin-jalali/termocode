//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Review on the phone (390x844): #/home/review/<n> shows one pull
// request in one fixed order: the risk band with "1 of N", the question
// with its task, the solution, the changes with the main change first,
// one proof row per check with its commit (a stale row says "old code"
// and offers Run again, a red one ✦ Fix and Log), the risks and one
// suggestion; Open, Skip, Comment and Approve & next at the bottom.
// Approve & next opens the next one at once and sends the approval only
// after a 4 s Undo; it names the commit it approves, and a refusal (new
// code came in) is said plainly.

func reviewQueue() map[string]any {
	return map[string]any{"items": []any{
		map[string]any{"number": 412, "title": "Add retry to webhook", "author": "sara"},
		map[string]any{"number": 418, "title": "Cache user list", "author": "sara"},
		map[string]any{"number": 420, "title": "Bump deps", "author": "dependabot[bot]", "bot": true},
	}}
}

func reviewSnap(n int, title, head, question, task string) map[string]any {
	return map[string]any{
		"number": n, "title": title, "author": "sara", "url": "https://github.com/o/r/pull/412", "head": head, "branch": "feat/x",
		"add": 26, "del": 2, "files": 2, "question": question, "task": task, "solution": "Retry 3 times with backoff. Stop on 4xx.",
		"groups": []any{
			map[string]any{"title": "send.go", "files": []any{"webhook/send.go"}, "add": 18, "del": 2, "main": true},
			map[string]any{"title": "Rest: test, config", "files": []any{"webhook/send_test.go", "config/app.yaml"}, "add": 8, "del": 0},
		},
		"proof": []any{
			map[string]any{"name": "e2e-linux", "state": "fail", "sha": head[:7], "url": "https://x/2", "fix": true},
			map[string]any{"name": "Race test", "state": "stale", "sha": "9b1e0d2", "again": true},
			map[string]any{"name": "Unit tests", "state": "ok", "sha": head[:7]},
			map[string]any{"name": "e2e-macos", "state": "notrun", "sha": head[:7]},
		},
		"risk":       map[string]any{"level": "medium", "reasons": []any{"touches network code (path rule)"}},
		"suggestion": map[string]any{"text": "Approve after e2e-linux is green.", "notes": 1},
		"state":      "OPEN",
	}
}

// openReview opens the Home, then #/home/review/412, and answers the
// queue and the first review.
func openReview(t *testing.T) (*Page, *HubConn) {
	t.Helper()
	p, c := openNow(t, nowInbox(time.Now().UnixMilli(), true))
	Eval[bool](p, `(location.hash = "#/home/review/412", true)`)
	nowReq(t, c, "review.list", reviewQueue())
	if a := nowReq(t, c, "review.get", reviewSnap(412, "Add retry to webhook", "3f2a1c9aa11", "Webhooks fail on short network drops. ENG-231", "ENG-231")); a["number"] != float64(412) {
		t.Fatalf("review.get %v", a)
	}
	p.WaitJS(`document.querySelector(".rv-page .rv-band")`)
	return p, c
}

func TestReviewPhoneOrder(t *testing.T) {
	p, _ := openReview(t)
	band := Eval[string](p, `document.querySelector(".rv-page .rv-band").textContent`)
	for _, w := range []string{"Medium risk · 1 of 2", "#412 Add retry to webhook", "sara · 2 files +26 -2 · commit 3f2a1c9"} {
		if !strings.Contains(band, w) {
			t.Errorf("risk band lacks %q: %q", w, band)
		}
	}
	order := Eval[[]string](p, `[...document.querySelectorAll(".rv-page .rv-h")].map((h) => h.textContent.trim())`)
	if strings.Join(order, "|") != "Question|Solution|Changes · main first|Proof · 3f2a1c9|Risks|Suggestion" {
		t.Errorf("sections %q", order)
	}
	if task := Eval[string](p, `document.querySelector(".rv-page .rv-task")?.textContent ?? ""`); task != "ENG-231" {
		t.Errorf("task link %q", task)
	}
	if main := Eval[string](p, `document.querySelector(".rv-page .rv-group.is-main")?.textContent ?? ""`); !strings.Contains(main, "send.go") || !strings.Contains(main, "+18") {
		t.Errorf("main change %q", main)
	}
	type row struct{ Name, State, Tail string }
	rows := Eval[[]row](p, `[...document.querySelectorAll(".rv-page .rv-proof")].map((r) => ({Name: r.querySelector(".rv-pname").textContent, State: r.dataset.state, Tail: r.querySelector(".rv-ptail").textContent}))`)
	if len(rows) != 4 || rows[0].State != "fail" || !strings.Contains(rows[0].Tail, "Fix") || !strings.Contains(rows[0].Tail, "Log") ||
		rows[1].State != "stale" || !strings.Contains(rows[1].Tail, "old code 9b1e0d2") || !strings.Contains(rows[1].Tail, "Run again") ||
		rows[3].State != "notrun" || !strings.Contains(rows[3].Tail, "not run") {
		t.Errorf("proof rows %+v", rows)
	}
	acts := Eval[[]string](p, `[...document.querySelectorAll(".rv-page .rv-acts .btn")].map((b) => b.textContent.trim())`)
	if strings.Join(acts, "|") != "Open O|Skip S|Comment C|Approve & next A" {
		t.Errorf("actions %q", acts)
	}
	if foot := Eval[string](p, `document.querySelector(".rv-page .rv-foot").textContent`); !strings.Contains(foot, "Approve only. Merge waits for green.") {
		t.Errorf("foot %q", foot)
	}
	shotTo(t, p, "review-phone")
	Eval[bool](p, `(document.querySelector(".rv-page .rv-body").scrollTop = 9999, true)`)
	shotTo(t, p, "review-phone-scrolled")
	p.noOverflow("review")
}

func TestReviewPhoneApproveAndNext(t *testing.T) {
	p, c := openReview(t)
	p.TapJS(`[...document.querySelectorAll(".rv-page .rv-acts button")].find((b) => b.textContent.includes("Approve"))`)
	// The next one opens at once; the approval waits for its Undo.
	if a := nowReq(t, c, "review.get", reviewSnap(418, "Cache user list", "a71c0e2bb22", "User list page is slow (2 s). ENG-236", "ENG-236")); a["number"] != float64(418) {
		t.Fatalf("next %v", a)
	}
	p.WaitJS(`document.querySelector(".rv-page .rv-band")?.textContent.includes("2 of 2")`)
	p.WaitJS(`document.querySelector(".rv-undo")?.textContent.includes("Approved #412 · 3f2a1c9")`)
	shotTo(t, p, "review-approve-next")
	a := nowReq(t, c, "review.act", map[string]any{"number": 412, "head": "3f2a1c9"})
	if a["do"] != "approve" || a["number"] != float64(412) || a["head"] != "3f2a1c9aa11" {
		t.Fatalf("approve sent %v", a)
	}
	// Undo within 4 s: nothing is sent, and that review is back.
	p.TapJS(`[...document.querySelectorAll(".rv-page .rv-acts button")].find((b) => b.textContent.includes("Approve"))`)
	p.WaitJS(`document.querySelector(".rv-undo button")`)
	p.TapJS(`document.querySelector(".rv-undo button")`)
	c.Quiet(t, 5*time.Second)
	p.WaitJS(`document.querySelector(".rv-page .rv-band")?.textContent.includes("#418")`)
	// Skip the last one: the queue is done, a calm end with Back to Now.
	p.TapJS(`[...document.querySelectorAll(".rv-page .rv-acts button")].find((b) => b.textContent.includes("Skip"))`)
	p.WaitJS(`document.querySelector(".rv-page .rv-done")?.textContent.includes("Review queue clear.")`)
	shotTo(t, p, "review-queue-clear")
}

func TestReviewPhoneHeadMoved(t *testing.T) {
	p, c := openReview(t)
	p.TapJS(`[...document.querySelectorAll(".rv-page .rv-acts button")].find((b) => b.textContent.includes("Approve"))`)
	nowReq(t, c, "review.get", reviewSnap(418, "Cache user list", "a71c0e2bb22", "", ""))
	for {
		r := nextReq(t, c)
		if r.Op != "review.act" {
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
			continue
		}
		c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeConflict, Msg: "New code came in. You saw 3f2a1c9. Now it is 7d40b21. Approve was not sent."})
		break
	}
	p.WaitJS(`[...document.querySelectorAll(".rv-moved")].some((x) => x.textContent.includes("New code came in.") && x.textContent.includes("Review new code"))`)
	shotTo(t, p, "review-head-moved")
}
