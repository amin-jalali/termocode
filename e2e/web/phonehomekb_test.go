//go:build browser

package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The phone's Home chat with the iOS keyboard up (owner, iPhone Safari,
// 2026-11-15: "the page looks broken, the text box is missing"): the
// composer sits right above the keyboard, inside the visual viewport;
// the user's message and its rules chip stack; a block in a long stream
// keeps its height (it shrank to an empty rule line).

const homeView = `document.querySelector('[data-view="home"]')`

// homeKBChat is a long chat whose last turn used a rules file.
func homeKBChat() map[string]any {
	msgs := []map[string]any{}
	for i := 0; i < 8; i += 2 {
		msgs = append(msgs, userMsg(i, "Which port does the database use on the server?"),
			aiMsg(i+1, md("doc", mdPara("The database's own port on the server side; on your side a free local port that termocode picks for the tunnel."))))
	}
	last := userMsg(8, "I want to add an MCP server. What do I do?")
	last["rules"] = "AGENTS.md (679 B)"
	msgs = append(msgs, last)
	return chatWith(false, msgs...)
}

func TestPhoneHomeKeyboardKeepsTheComposerInView(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	c.Send(t, Snap("chat", 1, homeKBChat()))
	Eval[bool](p, `(location.hash = "#/home", true)`)
	p.WaitJS(homeView + `?.querySelector('.msg[data-i="8"]') && ` + homeView + `.querySelector(".home-foot textarea")`)
	// Three chips the user tapped earlier: their blocks wait in the stream.
	for _, id := range []string{"board", "projects", "tasks"} {
		p.TapJS(`[...` + homeView + `.querySelectorAll(".home-chip")].find((b) => b.textContent.trim() === ` + jsString(homeChipLabel(id)) + `)`)
	}
	p.WaitJS(homeView + `.querySelectorAll(".home-block").length === 3`)
	// The iOS keyboard: the box has the focus, the visible part is 450 px.
	Eval[bool](p, `(`+homeView+`.querySelector(".home-foot textarea").focus(), true)`)
	fakeViewport(p, 450, 0)
	p.WaitJS(`document.documentElement.dataset.kb === "up"`)

	r := Eval[struct{ Top, Bottom float64 }](p, `(() => {
		const b = `+homeView+`.querySelector(".home-foot .composer").getBoundingClientRect();
		return { Top: b.top, Bottom: b.bottom };
	})()`)
	if r.Top < 0 || r.Bottom > 450.5 {
		t.Errorf("the composer is at %.0f–%.0f px, want it inside the visible 0–450 px above the keyboard", r.Top, r.Bottom)
	}

	// The stream stays at its end: the newest message shows above the box.
	p.WaitJS(`(() => { const s = ` + homeView + `.querySelector(".home-stream"); return s.scrollHeight - s.scrollTop - s.clientHeight < 2; })()`)
	shotTo(t, p, "home-kb")

	// The user's message, then its chip under it (not side by side).
	type box struct{ Top, Bottom, Left, Right float64 }
	st := Eval[struct{ Bubble, Chip box }](p, `(() => {
		const m = `+homeView+`.querySelector('.msg[data-i="8"]');
		const box = (el) => { const r = el.getBoundingClientRect(); return { Top: r.top, Bottom: r.bottom, Left: r.left, Right: r.right }; };
		return { Bubble: box(m.querySelector(".bubble")), Chip: box(m.querySelector(".msg-extras")) };
	})()`)
	if st.Chip.Top < st.Bubble.Bottom-0.5 {
		t.Errorf("the rules chip (%+v) sits beside the bubble (%+v), want it under it", st.Chip, st.Bubble)
	}

	// No block in the stream is squeezed to an empty line.
	if thin := Eval[[]float64](p, `[...`+homeView+`.querySelectorAll(".home-stream > *")].map((e) => e.getBoundingClientRect().height).filter((h) => h < 12)`); len(thin) > 0 {
		t.Errorf("stream items squeezed to %v px: they show as empty rule lines", thin)
	}
}

// homeChipLabel is the English label of a Home chip.
func homeChipLabel(id string) string {
	return map[string]string{"board": "Board", "projects": "Projects", "tasks": "Tasks"}[id]
}

// shotTo saves a screenshot to $WEB_SHOTS_DIR/<name>.png when it is set.
func shotTo(t *testing.T, p *Page, name string) {
	t.Helper()
	dir := os.Getenv(shotsEnv)
	if dir == "" {
		return
	}
	ctx, cancel := context.WithTimeout(p.Ctx, 10*time.Second)
	defer cancel()
	png, err := chromedp.Run(ctx, chromedp.CaptureScreenshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
}
