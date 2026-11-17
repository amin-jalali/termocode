//go:build browser && record

package web

// The phone GIF of assets/gifs: the real web page in headless Chrome at
// 390x844, talking to the fake hub of these tests (no laptop, no GitHub).
//
//	go run ./cmd/recordgifs -only phone-now
//
// runs this test (GIF_FRAMES_DIR is where it writes frames.json and the
// PNG screenshots; cmd/recordgifs encodes them).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// recordPhone is the phone at scale 2: sharp text, a GIF of a sane size.
var recordPhone = Viewport{Name: "phone", W: 390, H: 844, Scale: 2, Mobile: true}

// webRec takes the frames of one clip: a screenshot per step and how long
// the GIF shows it.
type webRec struct {
	t       *testing.T
	p       *Page
	dir     string
	clip    recClip
	at      time.Duration
	pending string
}

func newWebRec(t *testing.T, p *Page, name, note string) *webRec {
	dir := os.Getenv("GIF_FRAMES_DIR")
	if dir == "" {
		t.Skip("GIF_FRAMES_DIR is not set (run go run ./cmd/recordgifs)")
	}
	return &webRec{t: t, p: p, dir: dir, clip: recClip{Name: name, Note: note}}
}

// caption starts a new scene caption at the next frame.
func (r *webRec) caption(text string) { r.pending = text }

// shot takes a screenshot and shows it for hold.
func (r *webRec) shot(hold time.Duration) {
	r.t.Helper()
	// A moment for the last change to be painted.
	time.Sleep(150 * time.Millisecond)
	ctx, cancel := context.WithTimeout(r.p.Ctx, 10*time.Second)
	defer cancel()
	png, err := chromedp.Run(ctx, chromedp.CaptureScreenshot())
	if err != nil {
		r.t.Fatal(err)
	}
	name := fmt.Sprintf("%03d.png", len(r.clip.Frames))
	if err := os.WriteFile(filepath.Join(r.dir, name), png, 0o644); err != nil {
		r.t.Fatal(err)
	}
	if r.pending != "" {
		r.clip.Captions = append(r.clip.Captions, recCaption{At: r.at, Text: r.pending})
		r.pending = ""
	}
	r.clip.Frames = append(r.clip.Frames, recFrame{PNG: name, Hold: hold})
	r.at += hold
}

// tap shows a touch mark on the element for a moment, then taps it.
func (r *webRec) tap(el string, hold time.Duration) {
	r.t.Helper()
	r.p.WaitJS(el)
	Eval[bool](r.p, `(() => {
		const el = `+el+`;
		el.scrollIntoView({block: "nearest"});
		const b = el.getBoundingClientRect(), d = document.createElement("div");
		d.id = "tc-rec-tap";
		d.style.cssText = "position:fixed;z-index:2147483647;pointer-events:none;width:44px;height:44px;border-radius:50%;" +
			"background:rgba(255,255,255,.28);border:2px solid rgba(255,255,255,.75);left:" + (b.left + b.width / 2 - 22) + "px;top:" + (b.top + b.height / 2 - 22) + "px";
		document.body.appendChild(d);
		return true;
	})()`)
	r.shot(hold)
	Eval[bool](r.p, `(document.getElementById("tc-rec-tap")?.remove(), true)`)
	r.p.TapJS(el)
}

// save writes frames.json.
func (r *webRec) save() {
	r.t.Helper()
	b, err := json.MarshalIndent(r.clip, "", " ")
	if err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, "frames.json"), b, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// recClip, recFrame and recCaption are frames.json as gifkit.Load reads
// it (internal/devtools/gifkit: Clip, Frame, Caption).
type recClip struct {
	Name     string       `json:"name"`
	Note     string       `json:"note,omitempty"`
	Frames   []recFrame   `json:"frames"`
	Captions []recCaption `json:"captions"`
}

type recFrame struct {
	PNG  string        `json:"png"`
	Hold time.Duration `json:"hold_ns"`
}

type recCaption struct {
	At   time.Duration `json:"at_ns"`
	Text string        `json:"text"`
}

// TestRecordPhoneNow: Now on the phone (what waits on you, why, Done with
// Undo), then a pull request in Review and Approve & next.
func TestRecordPhoneNow(t *testing.T) {
	now := time.Now().UnixMilli()
	s := StartServer(t)
	p := NewPage(t, s, recordPhone)
	// Dark, like the other GIFs.
	p.Media(map[string]string{"prefers-reduced-motion": "reduce", "prefers-color-scheme": "dark"})
	rec := newWebRec(t, p, "phone-now", "a fake hub (sample data)")
	c := p.Login("c-1")
	c.Send(t, Snap("chat", 1, chatWith(false)))
	c.Send(t, Snap("status", 2, statusData("main", 0)))
	Eval[bool](p, `(location.hash = "#/home", true)`)
	c.WaitSub(t, "inbox")
	card := nowItem("card:c-1", "card", "needs_you", "checkout-fix asks to run tests", "me", "Waits for your OK", "agents", 1, now-12*60_000, "answer")
	rest := []any{
		nowItem("ci:feat/retry-webhook", "ci", "failed", "CI · feat/retry-webhook", "me", "Your branch is red", "redci", 1, now-40*60_000, "fix"),
		nowItem("pr:412", "pr", "needs_you", "#412 Add retry to webhook", "me", "Sara asks for your review", "reviews", 2, now-3*3600_000),
		nowItem("run:r-2", "run", "working", "docs-sync agent", "none", "Running", "", 4, now-4*60_000, "stop"),
		nowItem("lane:aaaaaaaaaaaa", "lane", "waiting", "migrate-db", "others", "Waits on Sara", "", 4, now-3*3600_000),
	}
	inbox := func(items []any, needs int) map[string]any {
		return map[string]any{"items": items, "counts": map[string]any{"needs_you": needs, "working": 1, "failed": 1}, "now": nowInfo(true, 3)}
	}
	c.Send(t, Snap("inbox", 1, inbox(append([]any{card}, rest...), 2)))
	if r := nextReq(t, c); r.Op != "view.list" {
		t.Fatalf("page sent %s; want view.list", show(r))
	} else {
		c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
	}
	p.WaitJS(nowHome + `?.querySelector('.now-row[data-id="pr:412"]')`)

	// Now: one line, your turn first, what runs.
	rec.caption("Now: what waits on you, on your phone.")
	rec.shot(2200 * time.Millisecond)

	// Why this? and Done, with Undo.
	rec.tap(nowHome+`.querySelector('.now-row[data-id="card:c-1"] .now-open')`, 500*time.Millisecond)
	p.WaitJS(nowHome + `.querySelector(".now-whyp")?.textContent.includes("Why this?")`)
	rec.caption("Each item says why it is there.")
	rec.shot(1600 * time.Millisecond)
	rec.tap(`[...`+nowHome+`.querySelectorAll(".now-whyp button")].find((b) => b.textContent.trim() === "Done")`, 400*time.Millisecond)
	nowReq(t, c, "now.act", map[string]any{"id": "card:c-1"})
	c.Send(t, Snap("inbox", 2, inbox(rest, 1))) // the hub drops the done item
	p.WaitJS(`[...document.querySelectorAll(".toast")].some((x) => x.textContent.includes("Done: checkout-fix"))`)
	rec.shot(1300 * time.Millisecond)
	// The toast leaves by itself (not recorded): it would cover Review.
	p.WaitJS(`!document.querySelector(".toast")`)

	// The pull request: Why this? → Open → Review.
	rec.tap(nowHome+`.querySelector('.now-row[data-id="pr:412"] .now-open')`, 400*time.Millisecond)
	rec.tap(`[...`+nowHome+`.querySelectorAll(".now-whyp button")].find((b) => b.textContent.trim() === "Open")`, 600*time.Millisecond)
	nowReq(t, c, "review.list", reviewQueue())
	nowReq(t, c, "review.get", reviewSnap(412, "Add retry to webhook", "3f2a1c9aa11", "Webhooks fail on short network drops. ENG-231", "ENG-231"))
	p.WaitJS(`document.querySelector(".rv-page .rv-band")`)
	rec.caption("Review: one pull request, in one fixed order.")
	rec.shot(2300 * time.Millisecond)
	Eval[bool](p, `(document.querySelector(".rv-page .rv-body").scrollTop = 9999, true)`)
	rec.shot(1800 * time.Millisecond)

	// Approve & next: the next one at once, the approval after its Undo.
	rec.tap(`[...document.querySelectorAll(".rv-page .rv-acts button")].find((b) => b.textContent.includes("Approve"))`, 400*time.Millisecond)
	nowReq(t, c, "review.get", reviewSnap(418, "Cache user list", "a71c0e2bb22", "User list page is slow (2 s). ENG-236", "ENG-236"))
	p.WaitJS(`document.querySelector(".rv-undo")?.textContent.includes("Approved #412")`)
	rec.caption("Approve & next, with Undo.")
	rec.shot(2400 * time.Millisecond)
	rec.save()
}
