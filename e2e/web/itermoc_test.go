//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The Itermoc view (WEBP-ITERMOC): the laptop's AI setup, read only. A
// model slot shows only whether its key is there; every change opens
// the laptop's own form, after the question here (Cancel first) and the
// laptop's card.

func itermocFixture() webproto.Itermoc {
	return webproto.Itermoc{
		Profiles: []webproto.ItermocProfile{
			{Name: "default", Source: "builtin", Trust: "use", Active: true, Steps: 1, Model: "main"},
			{Name: "review", Title: "Review", Source: "user", Trust: "use", Steps: 2},
			{Name: "house", Title: "House review", Source: "project", Trust: "ask", Steps: 1},
		},
		Slots: []webproto.ItermocSlot{
			{Name: "main", Provider: "ollama", Model: "qwen", HasKey: "none"},
			{Name: "quick", Provider: "openai", Model: "gpt-x", HasKey: "ok"},
			{Name: "cheap", Provider: "openai", Model: "gpt-y", HasKey: "missing", User: true},
		},
		Providers: []webproto.ItermocProvider{{Kind: "openai", Name: "OpenAI", Label: "API key", Cost: "API · pay per use", Models: 12}},
		Services:  []webproto.ItermocService{{Name: "linear", State: "running", Tools: 9}},
		Listening: true,
		Agents:    []webproto.ItermocAgent{{Name: "helper", Since: "09:12", Calls: 3}},
	}
}

const imView = `document.querySelector('[data-view="itermoc"]')`

func TestItermocDesk(t *testing.T) {
	p, c := openFiles(t, Desktop)
	p.TapJS(`document.querySelector('nav.act-bar .act-item[data-act="itermoc"]')`)
	reply(t, c, expectOp(t, c, "itermoc.state", `{}`), itermocFixture())
	key := func(slot string) string {
		return Eval[string](p, imView+`.querySelector('.im-slot[data-name="`+slot+`"] .im-key')?.getAttribute("aria-label") ?? ""`)
	}
	p.WaitJS(imView + `?.querySelectorAll(".im-slot").length === 3`)
	if got := key("quick") + "|" + key("cheap") + "|" + key("main"); got != "Key set|No key|No key needed" {
		t.Errorf("key marks %q", got)
	}
	if got := p.Text(`.im-profile[data-name="default"]`); !strings.Contains(got, "in use") {
		t.Errorf("default row %q", got)
	}
	if Eval[bool](p, `!!document.querySelector('.im-profile[data-name="house"] [data-act="use"]')`) {
		t.Error("an untrusted project profile offers Use")
	}
	for _, want := range []string{"OpenAI", "12 models", "linear", "9 tools", "helper"} {
		if got := p.Text(`[data-view="itermoc"] .view-body`); !strings.Contains(got, want) {
			t.Errorf("view has no %q:\n%s", want, got)
		}
	}

	// Add AI… asks here first (Cancel first and focused), then the laptop.
	p.TapJS(imView + `.querySelector('[data-act="ai-add"]')`)
	p.WaitJS(`document.querySelector(".ask-text")?.textContent.includes("on the laptop") && document.activeElement?.textContent === "Cancel"`)
	noReq(t, c, 300*time.Millisecond)
	p.TapJS(`[...document.querySelectorAll(".ask .btn")].find((b) => b.textContent === "Cancel")`)
	p.WaitJS(`!document.querySelector(".ask")`)
	noReq(t, c, 300*time.Millisecond)
	p.TapJS(imView + `.querySelector('[data-act="ai-add"]')`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, "itermoc.ask", `{"action":"ai-add"}`), webproto.AskRef{Card: "k1"})
	p.WaitJS(`document.querySelector(".waiting")`)
}

// Use on a profile asks the laptop, as the chat's picker does.
func TestItermocUseProfile(t *testing.T) {
	p, c := openFiles(t, Desktop)
	p.TapJS(`document.querySelector('nav.act-bar .act-item[data-act="itermoc"]')`)
	reply(t, c, expectOp(t, c, "itermoc.state", `{}`), itermocFixture())
	p.WaitJS(imView + `?.querySelector('.im-profile[data-name="review"] [data-act="use"]')`)
	p.TapJS(imView + `.querySelector('.im-profile[data-name="review"] [data-act="use"]')`)
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === "Use profile review for the next chat messages?"`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, "ai.use_profile", `{"name":"review"}`), webproto.AskRef{Card: "k2"})
}

// The commands open the view at their section; AI: Next Profile asks to
// use the next profile the laptop trusts.
func TestItermocCommands(t *testing.T) {
	p, c := openFiles(t, Desktop)
	wbKey(p, "P", true, true)
	reply(t, c, expectOp(t, c, "palette.list", `{}`), webproto.PaletteList{Items: []webproto.PaletteItem{}})
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("AI: Next Profile")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "AI: Next Profile"`)
	p.Key("\r")
	reply(t, c, expectOp(t, c, "itermoc.state", `{}`), itermocFixture())
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === "Use profile review for the next chat messages?"`)
	p.TapJS(`[...document.querySelectorAll(".ask .btn")].find((b) => b.textContent === "Cancel")`)
	p.WaitJS(`!document.querySelector(".ask")`)

	wbKey(p, "P", true, true)
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("Itermoc: AI Providers")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "Itermoc: AI Providers"`)
	p.Key("\r")
	p.WaitJS(`document.querySelector('.im-sec[data-sec="models"].is-target')`)
}

// The phone has the view on More.
func TestItermocPhone(t *testing.T) {
	p, c := openFiles(t, Phone)
	p.GoTab("more")
	p.TapJS(`document.querySelector('.more-link[data-sub="itermoc"]')`)
	reply(t, c, expectOp(t, c, "itermoc.state", `{}`), itermocFixture())
	p.WaitJS(imView + `?.querySelectorAll(".im-profile").length === 3`)
	if got := Eval[string](p, imView+`.querySelector('.im-slot[data-name="quick"] .im-key')?.getAttribute("aria-label") ?? ""`); got != "Key set" {
		t.Errorf("quick key %q", got)
	}
	if got := p.Problems(); len(got) > 0 {
		t.Errorf("problems: %v", got)
	}
}
