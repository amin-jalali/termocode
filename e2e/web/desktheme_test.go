//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Preferences: Color Theme… and Edit Custom Theme (WEBP-7): the
// laptop's themes in a picker (the one shown now marked); one picked
// switches the laptop, and this page follows its colors. The custom
// theme's colors in a picker; one picked takes a color typed here, and
// a custom one can go back to the theme's own.
func TestDeskColorTheme(t *testing.T) {
	p, c := openFiles(t, Desktop)
	themes := []webproto.ThemeInfo{{ID: "vscode-dark-plus", Name: "VSCode Dark+"}, {ID: "catppuccin-latte", Name: "Catppuccin Latte", Light: true}}
	runCmd(t, p, c, true, "color theme", "Preferences: Color Theme…")
	reply(t, c, waitReq(t, c, webproto.OpThemeList), webproto.ThemeList{Themes: themes, Active: "vscode-dark-plus"})
	p.WaitJS(`[...document.querySelectorAll(".quick-row")].map((e) => e.querySelector(".quick-label").textContent + "=" + (e.querySelector(".quick-detail")?.textContent ?? "")).join() === "VSCode Dark+=shown now,Catppuccin Latte=light"`)
	p.Key("latte")
	pickRow(p, "Catppuccin Latte")
	r := waitArgs(t, c, webproto.OpThemeSet, `{"id":"catppuccin-latte"}`)
	reply(t, c, r, webproto.ThemeList{Themes: themes, Active: "catppuccin-latte"})
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Color theme: Catppuccin Latte.")`)
}

func TestDeskEditCustomTheme(t *testing.T) {
	p, c := openFiles(t, Desktop)
	colors := webproto.ThemeColors{Colors: []webproto.ThemeColor{{Key: "BgEditor", Color: "#1e1e1e"}, {Key: "AIAccent", Color: "#c586c0", Custom: true}}}
	runCmd(t, p, c, true, "edit custom theme", "Preferences: Edit Custom Theme")
	reply(t, c, waitReq(t, c, webproto.OpThemeColors), colors)
	p.WaitJS(`[...document.querySelectorAll(".quick-row")].map((e) => e.querySelector(".quick-label").textContent + "=" + (e.querySelector(".quick-detail")?.textContent ?? "")).join() === "BgEditor=#1e1e1e,AIAccent=#c586c0 · custom"`)
	p.Key("bgeditor")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "BgEditor"`)
	p.Key("\r")
	p.WaitJS(`document.querySelector(".quick-box")?.placeholder === "BgEditor: a color like #1e1e2e" && document.activeElement === document.querySelector(".quick-box")`)
	p.Key("#112233")
	p.Key("\r")
	r := waitArgs(t, c, webproto.OpThemeColorSet, `{"key":"BgEditor","color":"#112233"}`)
	reply(t, c, r, colors)
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("BgEditor is #112233 now.")`)

	// A custom color can also go back to the theme's own.
	runCmd(t, p, c, false, "edit custom theme", "Preferences: Edit Custom Theme")
	reply(t, c, waitReq(t, c, webproto.OpThemeColors), colors)
	p.WaitJS(`document.querySelector(".quick-row .quick-label")`)
	p.Key("aiaccent")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "AIAccent"`)
	p.Key("\r")
	p.WaitJS(`[...document.querySelectorAll(".quick-row .quick-label")].map((e) => e.textContent).join() === "Change Color…,Back to the Theme's Color"`)
	p.Key("back")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "Back to the Theme's Color"`)
	p.Key("\r")
	r = waitArgs(t, c, webproto.OpThemeColorSet, `{"key":"AIAccent","color":""}`)
	reply(t, c, r, colors)
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("AIAccent is back to the theme's color.")`)
}
