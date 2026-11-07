//go:build browser && webdev

package web

import (
	"strings"
	"testing"
)

// The phone app shell (MOBUX-2), on the component kit at 390 px with
// touch: the top bar (project switcher ▾, live dot, ☰), the stream, and
// the dock at the bottom (chips above the composer: attach chips, 🎤,
// Send) inside the one-hand thumb zone.

const shell = `document.querySelector("#shell .ph-shell")`

func openShell(t *testing.T) *Page {
	t.Helper()
	p := openKit(t, Phone)
	p.WaitJS(shell)
	return p
}

// rect returns top, bottom and height of the first match of sel in the shell.
func shellRect(p *Page, sel string) [3]float64 {
	p.T.Helper()
	return Eval[[3]float64](p, `(() => { const r = `+shell+`.querySelector(`+jsq(sel)+`).getBoundingClientRect(); return [r.top, r.bottom, r.height]; })()`)
}

func jsq(s string) string { return "`" + s + "`" }

// The top bar holds the switcher with ▾, the live mark (shape + word)
// and ☰, every one a 44 px target.
func TestPhoneShellTopBar(t *testing.T) {
	p := openShell(t)
	if got := Eval[string](p, shell+`.querySelector(".ph-switch").textContent`); !strings.Contains(got, "shop") || !strings.Contains(got, "▾") {
		t.Errorf("switcher reads %q, want shop ▾", got)
	}
	if got := Eval[string](p, shell+`.querySelector(".ph-live").textContent`); !strings.Contains(got, "●") || !strings.Contains(got, "Live") {
		t.Errorf("live mark reads %q, want ● Live", got)
	}
	if got := Eval[string](p, shell+`.querySelector(".ph-top-btn.is-menu").getAttribute("aria-label")`); got != "All tools" {
		t.Errorf("☰ is labelled %q, want All tools", got)
	}
	small := Eval[[]string](p, `[...`+shell+`.querySelectorAll("button, textarea")].filter((b) => { const r = b.getBoundingClientRect(); return r.height < 44 || r.width < 44; }).map((b) => b.className + " " + b.textContent.trim())`)
	if len(small) > 0 {
		t.Errorf("targets under 44 px: %q", small)
	}
}

// The dock sits at the bottom: chips above the composer, Send in the
// lowest quarter of the shell (the thumb zone), the stream between.
func TestPhoneShellThumbZone(t *testing.T) {
	p := openShell(t)
	sh := shellRect(p, ".ph-dock")
	all := Eval[[3]float64](p, `(() => { const r = `+shell+`.getBoundingClientRect(); return [r.top, r.bottom, r.height]; })()`)
	if d := all[1] - sh[1]; d > 1 || d < -1 {
		t.Errorf("dock ends %.0f px above the shell's bottom", d)
	}
	chips, comp := shellRect(p, ".ph-chips"), shellRect(p, ".ph-composer")
	if chips[1] > comp[0]+1 {
		t.Errorf("chip row (bottom %.0f) is not above the composer (top %.0f)", chips[1], comp[0])
	}
	send := shellRect(p, ".ph-composer-send")
	if mid := (send[0] + send[1]) / 2; mid < all[0]+all[2]*0.75 {
		t.Errorf("Send's middle %.0f is above the thumb zone (lowest quarter from %.0f)", mid, all[0]+all[2]*0.75)
	}
	st, top := shellRect(p, ".ph-stream"), shellRect(p, ".ph-top")
	if st[0] < top[1]-1 || st[1] > sh[0]+1 {
		t.Errorf("stream %v is not between the top bar %v and the dock %v", st, top, sh)
	}
	if got := Eval[string](p, shell+`.querySelector(".ph-stream").getAttribute("role")`); got != "log" {
		t.Errorf("stream role %q, want log", got)
	}
}

// The switcher opens a sheet of projects, the current one marked ✓ and
// aria-current; a pick changes the bar.
func TestPhoneShellProjectSwitcher(t *testing.T) {
	p := openShell(t)
	p.TapJS(shell + `.querySelector(".ph-switch")`)
	p.WaitJS(`document.querySelector(".ph-switch-list")`)
	cur := Eval[string](p, `document.querySelector(".ph-switch-row[aria-current='true']").textContent`)
	if !strings.Contains(cur, "shop") || !strings.Contains(cur, "✓") {
		t.Errorf("current row reads %q, want shop ✓", cur)
	}
	p.TapJS(`[...document.querySelectorAll(".ph-switch-row")].find((b) => b.textContent.includes("api"))`)
	p.WaitJS(`!document.querySelector(".ph-switch-list")`)
	if got := Eval[string](p, shell+`.querySelector(".ph-switch").textContent`); !strings.Contains(got, "api") {
		t.Errorf("after the pick the switcher reads %q, want api", got)
	}
}

// Offline: the mark says it with its own shape, Send is greyed and does
// nothing, the draft stays.
func TestPhoneShellOffline(t *testing.T) {
	p := openShell(t)
	p.TapJS(ButtonByText("Shell offline"))
	p.WaitJS(shell + `.querySelector(".ph-live").textContent.includes("Offline")`)
	if got := Eval[string](p, shell+`.querySelector(".ph-live").textContent`); strings.Contains(got, "●") {
		t.Errorf("offline mark %q still has the live shape", got)
	}
	if got := Eval[string](p, shell+`.querySelector(".ph-composer-send").getAttribute("aria-disabled")`); got != "true" {
		t.Errorf("offline Send aria-disabled = %q", got)
	}
}

// 🎤 puts the cursor in the box and says to use the keyboard's own
// dictation (no audio leaves the phone through the page).
func TestPhoneShellMic(t *testing.T) {
	p := openShell(t)
	p.TapJS(shell + `.querySelector(".ph-composer-mic")`)
	p.WaitJS(`document.activeElement === ` + shell + `.querySelector(".ph-composer-box")`)
	p.WaitJS(shell + `.querySelector(".ph-composer-hint")`)
	if got := Eval[string](p, shell+`.querySelector(".ph-composer-hint").textContent`); !strings.Contains(got, "keyboard") {
		t.Errorf("mic hint reads %q", got)
	}
}

// A new message keeps the stream at its end while the reader is there.
func TestPhoneShellStreamSticks(t *testing.T) {
	p := openShell(t)
	for i := 0; i < 6; i++ {
		p.TapJS(ButtonByText("Shell: add a message"))
	}
	p.WaitJS(`(() => { const s = ` + shell + `.querySelector(".ph-stream"); return s.scrollHeight - s.scrollTop - s.clientHeight < 4; })()`)
}
