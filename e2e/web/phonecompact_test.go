//go:build browser

package web

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The phone gives its screen to the conversation (owner, 2026-11-09):
// the view's header is one slim row (no big "Chat" heading: the tab bar
// already says it; the chat's name is the button that opens the Chats
// list, New chat an icon); the composer is one row (the profile and
// model picker in the sheet its + opens, Send an icon); the tab
// bar is compact. Every control stays at least 44x44 px and reachable.

// phoneRegionsJS measures the phone shell's regions, in CSS px.
const phoneRegionsJS = `(() => {
	const hgt = (sel) => { const e = document.querySelector(sel); return e ? e.getBoundingClientRect().height : 0; };
	return {
		strip: hgt(".strip"), bars: hgt(".bars"), head: hgt('[data-view="chat"] .view-head'),
		body: hgt('[data-view="chat"] .view-body'), foot: hgt('[data-view="chat"] .composer'),
		tabs: hgt(".tabbar"), vh: innerHeight,
	};
})()`

type phoneRegions struct{ Strip, Bars, Head, Body, Foot, Tabs, Vh float64 }

// compactChat is the owner's chat: a long name and a few turns.
func compactChat(title string, turns ...string) map[string]any {
	c := chatWith(false)
	msgs := []any{}
	for i, s := range turns {
		if i%2 == 0 {
			msgs = append(msgs, userMsg(i, s))
		} else {
			msgs = append(msgs, aiMsg(i, md("doc", mdPara(s))))
		}
	}
	c["messages"], c["title"] = msgs, title
	return c
}

var compactTurns = []string{
	"I want to connect my db over ssh",
	"Open the connection's settings and set SSH host, user and key. termocode opens the tunnel itself when a db_* tool needs it.",
	"Which port does it use?",
	"The database's own port on the server side; on your side a free local port that termocode picks.",
}

// openCompact opens the owner's chat on a 390x844 phone in lang with
// the ai topic, so the composer has its picker.
func openCompact(t *testing.T, lang, scheme, title string) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPageLang(t, s, Phone, lang)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce", "prefers-color-scheme": scheme})
	c := p.Login("c-1")
	c.Send(t, Snap("chat", 1, compactChat(title, compactTurns...)))
	p.WaitJS(chatView + `?.querySelector(".msg")`)
	snap := aiFixture(true, true)
	snap.Profile, snap.Provider, snap.Model = "GPT", "Codex", "gpt-6-sol"
	snap.Models = []string{"gpt-6.1-sol", "gpt-6-sol"}
	c.Send(t, Snap("ai", 1, snap))
	p.WaitJS(chatView + `?.querySelector(".composer .composer-plus")`)
	return p, c
}

func TestPhoneCompactChatGivesTheScreenToTheConversation(t *testing.T) {
	for _, lang := range []string{"en-US", "fa-IR"} {
		t.Run(lang, func(t *testing.T) {
			title := "I want to connect my db over ssh"
			if lang == "fa-IR" {
				title = "می‌خواهم پایگاه داده را با ssh وصل کنم"
			}
			p, c := openCompact(t, lang, "light", title)
			// The first-run hint is slim and shows on the first visit only.
			if h := Eval[float64](p, `document.querySelector(".hint")?.getBoundingClientRect().height ?? 0`); h == 0 || h > 45 {
				t.Errorf("the first-run hint is %.0f px tall, want it shown and slim (≤ 45)", h)
			}
			if !Eval[bool](p, `JSON.parse(localStorage.getItem("termocode.prefs.v1") ?? "{}").firstRunDone === true`) {
				t.Error("the first-run hint will show again on the next visit")
			}
			Eval[bool](p, `(document.querySelector(".hint-x")?.click(), true)`)
			p.WaitJS(`!document.querySelector(".hint")`)
			r := Eval[phoneRegions](p, phoneRegionsJS)
			t.Logf("regions at 390x844: %+v", r)
			if r.Head > 48 {
				t.Errorf("the chat header is %.0f px tall, want one row (≤ 48)", r.Head)
			}
			if r.Foot > 68 {
				t.Errorf("the composer is %.0f px tall, want one row (≤ 68)", r.Foot)
			}
			if r.Tabs > 52 {
				t.Errorf("the tab bar is %.0f px tall, want ≤ 52", r.Tabs)
			}
			if pct := r.Body / r.Vh * 100; pct < 75 {
				t.Errorf("the conversation gets %.0f%% of the screen (%.0f of %.0f px), want ≥ 75%%", pct, r.Body, r.Vh)
			}
			// No big view heading on screen; the name is the title, in one row.
			if h := Eval[float64](p, chatView+`.querySelector(".view-title").getBoundingClientRect().height`); h > 1 {
				t.Errorf("the big Chat heading still shows (%.0f px)", h)
			}
			if got := Eval[string](p, chatView+`.querySelector(".view-title").textContent`); got == "" {
				t.Error("the view lost its heading for a screen reader")
			}
			name := chatView + `.querySelector(".view-head .chat-name")`
			if got := Eval[string](p, name+`?.textContent ?? ""`); !strings.Contains(got, title) {
				t.Errorf("the header's chat name reads %q, want %q", got, title)
			}
			// Every control in the header, the composer and the tab bar is ≥ 44 px.
			if bad := Eval[[]string](p, smallTargetsJS(`[data-view="chat"] .view-head button, [data-view="chat"] .composer button, .tabbar .nav-item`)); len(bad) > 0 {
				t.Errorf("targets under 44 px: %v", bad)
			}
			p.noOverflow("compact chat")
			// Send is an icon with its name (for a screen reader).
			send := chatView + `.querySelector(".composer .send button")`
			if !Eval[bool](p, send+`.querySelector("svg") !== null`) || Eval[string](p, send+`.textContent.trim()`) == "" {
				t.Error("Send is not an icon with its name")
			}
			if w := Eval[float64](p, send+`.getBoundingClientRect().width`); w > 48 {
				t.Errorf("Send is %.0f px wide, want an icon button (≤ 48)", w)
			}
			// The picker is one tap away (+); picking a model opens its list.
			p.TapJS(chatView + `.querySelector(".composer .composer-plus")`)
			model := `document.querySelector('.sheet .plus-sheet .ai-pick[data-pick="model"]')`
			p.WaitJS(model + `?.getBoundingClientRect().height >= 43.5`)
			if !Eval[bool](p, `document.querySelector('.sheet .plus-sheet .ai-pick[data-pick="profile"]')?.textContent.includes("GPT")`) {
				t.Error("the picker does not name the profile")
			}
			p.TapJS(model)
			p.WaitJS(`document.querySelectorAll(".sheet .ai-menu .actsheet-row").length === 2`)
			Eval[bool](p, `([...document.querySelectorAll(".scrim")].forEach((s) => s.click()), true)`)
			p.WaitJS(`!document.querySelector(".sheet")`)
			// The name opens the Chats list; New chat is one tap.
			c.Send(t, Snap("chats", 1, map[string]any{"chats": []any{
				map[string]any{"id": "c-1", "title": title, "updated": 3000, "msgs": 4, "open": true}}}))
			p.TapJS(name)
			p.WaitJS(chatView + `.querySelectorAll(".chat-row").length === 1 && ` + name + `.getAttribute("aria-pressed") === "true"`)
			p.TapJS(name)
			p.WaitJS(chatView + `.querySelector(".msgs")`)
			p.TapJS(chatView + `.querySelector(".view-head .chat-new")`)
			expectReq(t, c, "chat.new")
		})
	}
}

// The other tabs share the slim header.
func TestPhoneCompactHeaderOnEveryTab(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Login("c-1")
	for _, tab := range []string{"files", "term", "git", "more"} {
		p.GoTab(tab)
		h := Eval[float64](p, fmt.Sprintf(`document.querySelector('[data-view="%s"] .view-head')?.getBoundingClientRect().height ?? 0`, tab))
		if h > 48 {
			t.Errorf("%s: the header is %.0f px tall, want one row (≤ 48)", tab, h)
		}
	}
}

// TestPhoneDesignShots writes the phone chat at 390x844 in light, dark
// and Persian to WEB_SHOTS_DIR as phone-design-<name>.png.
func TestPhoneDesignShots(t *testing.T) {
	dir := os.Getenv(shotsEnv)
	if dir == "" {
		t.Skip("set " + shotsEnv + " to write screenshots")
	}
	for _, tc := range []struct{ name, lang, scheme, title string }{
		{"light", "en-US", "light", "I want to connect my db over ssh"},
		{"dark", "en-US", "dark", "I want to connect my db over ssh"},
		{"fa", "fa-IR", "dark", "می‌خواهم پایگاه داده را با ssh وصل کنم"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPageLang(t, s, Phone, tc.lang)
			p.Media(map[string]string{"prefers-reduced-motion": "reduce", "prefers-color-scheme": tc.scheme})
			c := p.Login("c-1")
			c.Send(t, Snap("chat", 1, compactChat(tc.title, compactTurns...)))
			ai := aiFixture(true, true)
			ai.Profile, ai.Provider, ai.Model = "GPT", "Codex", "gpt-6-sol"
			c.Send(t, Snap("ai", 1, ai))
			ready := chatView + `?.querySelector(".msg") && document.querySelector(".composer .ai-pick, .composer .pick-toggle, .composer .composer-plus")`
			snap(t, p, filepath.Join(dir, "phone-design-"+tc.name+".png"), ready)
		})
	}
}
