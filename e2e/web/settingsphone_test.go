//go:build browser

package web

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Settings on the phone (UI page 10): grouped rows with an icon, a plain
// name and the value, not a page of help text; switches change a yes/no
// setting at once; a tap opens a picker sheet; a risky key asks first;
// chips and search narrow the list; a long press gives Ask AI / Add to
// chat. Against the fake hub, 390×844 touch.

func phoneSettings() webproto.SettingsSnap {
	return webproto.SettingsSnap{Items: []webproto.SettingItem{
		{Key: "theme", Group: "Appearance", Type: "string", Value: "vscode-dark-plus", Default: "vscode-dark-plus", Description: "Color theme ID."},
		{Key: "icons", Group: "Appearance", Type: "enum", Value: "auto", Default: "auto", Description: "Icon set: nerd, unicode, ascii or auto.", Choices: []string{"auto", "nerd_font", "unicode", "ascii"}},
		{Key: "dashboard_logo", Group: "Appearance", Type: "enum", Value: "text", Default: "text", Choices: []string{"text", "image"}},
		{Key: "word_wrap", Group: "Editor", Type: "bool", Value: "false", Default: "false"},
		{Key: "tab_size", Group: "Editor", Type: "int", Value: "2", Default: "4"},
		{Key: "http_tls_insecure", Group: "HTTP", Type: "bool", Value: "false", Default: "false"},
	}}
}

func openPhoneSettings(t *testing.T) (*Page, *HubConn) {
	t.Helper()
	p, c := openRunView(t, Phone, "settings", "settings", "settings")
	c.Send(t, Snap("settings", 1, phoneSettings()))
	reply(t, c, expectOp(t, c, webproto.OpThemeList, ""), webproto.ThemeList{Active: "vscode-dark-plus", Themes: []webproto.ThemeInfo{{ID: "vscode-dark-plus", Name: "VS Code Dark+"}, {ID: "one-dark", Name: "One Dark"}}})
	p.WaitJS(`document.querySelector('.pg-row[data-key="theme"] .r')?.textContent === "VS Code Dark+"`)
	return p, c
}


func TestPhoneSettingsGroupedRows(t *testing.T) {
	p, c := openPhoneSettings(t)
	if Eval[bool](p, `document.querySelector('[data-view="settings"]').textContent.includes("Read only")`) {
		t.Error("the page still says it is read only")
	}
	if got := Eval[string](p, `[...document.querySelectorAll('.pg-sec')].map(s => s.dataset.sec + ":" + [...s.querySelectorAll('.pg-row')].map(r => r.querySelector('.t b').textContent).join(",")).join(" | ")`); got != "Appearance:Theme,Icons,Dashboard logo | Editor:Word wrap,Tab size | HTTP:HTTP TLS insecure" {
		t.Errorf("sections %q", got)
	}
	if !Eval[bool](p, `!!document.querySelector('.pg-row[data-key="icons"] .pg-tile svg') && document.querySelector('.pg-row[data-key="icons"] .r').textContent === "Auto" && !!document.querySelector('.pg-row[data-key="icons"] .pg-chev')`) {
		t.Error("a row is not icon · name · value · ›")
	}
	if Eval[string](p, `document.querySelector('.pg-row[data-key="tab_size"] .r').textContent`) != "2" {
		t.Error("tab size value")
	}
	if got := Eval[string](p, `[...document.querySelectorAll('.pg-chip')].map(c => c.textContent).join(",")`); got != "Changed 1,Appearance,Editor,HTTP" {
		t.Errorf("chips %q", got)
	}
	if Eval[bool](p, `document.documentElement.scrollWidth > innerWidth`) {
		t.Error("the page scrolls sideways")
	}

	// A switch changes the setting at once.
	if Eval[string](p, `document.querySelector('.pg-row[data-key="word_wrap"]').getAttribute("aria-checked")`) != "false" {
		t.Error("word wrap is not a switch that is off")
	}
	p.Tap(`.pg-row[data-key="word_wrap"]`)
	reply(t, c, expectOp(t, c, webproto.OpSettingsSet, `{"key":"word_wrap","value":"true"}`), webproto.SettingItem{Key: "word_wrap", Group: "Editor", Type: "bool", Value: "true", Default: "false"})

	// A tap on a choice row opens its picker: one line per choice.
	p.Tap(`.pg-row[data-key="icons"]`)
	p.WaitJS(`document.querySelectorAll('.pg-picker[data-picker="icons"] [role="radio"]').length === 4`)
	if Eval[string](p, `[...document.querySelectorAll('.pg-picker [role="radio"][aria-checked="true"]')].map(r => r.dataset.choice).join()`) != "auto" {
		t.Error("the current choice is not ticked")
	}
	if Eval[bool](p, `document.querySelector('.pg-picker').textContent.includes("Icon set: nerd")`) {
		t.Error("the long help shows before More…")
	}
	p.Tap(`.pg-picker [data-act="more"]`)
	p.WaitJS(`document.querySelector('.pg-picker').textContent.includes("Icon set: nerd")`)
	p.Tap(`.pg-picker [data-choice="ascii"]`)
	reply(t, c, expectOp(t, c, webproto.OpSettingsSet, `{"key":"icons","value":"ascii"}`), webproto.SettingItem{Key: "icons", Value: "ascii"})
	p.WaitJS(`!document.querySelector('.pg-picker')`)

	// A risky key asks first; Cancel sends nothing.
	p.Tap(`.pg-row[data-key="http_tls_insecure"]`)
	p.WaitJS(`!!document.querySelector('[data-act="confirm"]')`)
	p.Tap(`[data-act="cancel"]`)
	noOp(t, c, webproto.OpSettingsSet, 300*time.Millisecond)
	p.Tap(`.pg-row[data-key="http_tls_insecure"]`)
	p.Tap(`[data-act="confirm"]`)
	reply(t, c, expectOp(t, c, webproto.OpSettingsSet, `{"key":"http_tls_insecure","value":"true"}`), nil)

	// Chips and search narrow the list; empty groups are not drawn.
	p.Tap(`.pg-chip[data-chip="changed"]`)
	p.WaitJS(`document.querySelectorAll('.pg-row').length === 1 && !!document.querySelector('.pg-row[data-key="tab_size"]') && document.querySelectorAll('.pg-sec').length === 1`)
	p.Tap(`.pg-chip[data-chip="changed"]`)
	p.Tap(`[data-act="search"]`)
	p.Do(chromedp.SendKeys(`.pg-search input`, "wrap"))
	p.WaitJS(`document.querySelectorAll('.pg-row').length === 1 && !!document.querySelector('.pg-row[data-key="word_wrap"]')`)
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}

// A long press on a row: Ask AI about this puts the setting into the
// chat's next message (a view of that one setting) with the question in
// the box; Add to chat only adds it.
func TestPhoneSettingsToChat(t *testing.T) {
	p, c := openPhoneSettings(t)
	c.Send(t, Snap("chat", 1, chatWith(false)))
	Eval[bool](p, `(document.querySelector('.pg-row[data-key="icons"]').dispatchEvent(new MouseEvent("contextmenu", {bubbles: true, cancelable: true, clientX: 200, clientY: 300})), true)`)
	p.WaitJS(`[...document.querySelectorAll('.pg-menu button')].map(b => b.dataset.act).join() === "ask,chat,copy"`)
	p.Tap(`.pg-menu [data-act="ask"]`)
	p.WaitJS(`location.hash.startsWith("#/chat") && [...document.querySelectorAll('.composer-attach .chat-chip')].some(c => c.textContent.includes("Icons")) && document.querySelector('.composer textarea')?.value.includes("icons")`)
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}
