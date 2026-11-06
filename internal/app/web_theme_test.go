package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/editor"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webThemeRig is a web rig with its own config folder; the laptop's
// colors come back after the test.
func webThemeRig(t *testing.T) *webFilesRig {
	t.Helper()
	r := newWebFilesRig(t)
	r.m.editor = editor.New(nil) // the laptop's terminals follow the theme
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	was := r.m.themeID
	if was == "" {
		was = "vscode-dark-plus"
	}
	t.Cleanup(func() { theme.ApplyTheme(was) })
	return r
}

// Spec (WEBP-7): theme.list names the laptop's color themes and the one
// it shows; theme.set switches the laptop to another one and saves the
// choice, as Preferences: Color Theme… does (the page follows the
// laptop's colors). A theme the laptop does not have is refused.
func TestWebThemePicker(t *testing.T) {
	r := webThemeRig(t)
	var list webproto.ThemeList
	webResData(t, r.call(webproto.OpThemeList, &webproto.EmptyArgs{}), &list)
	all := theme.AvailableThemes()
	if len(list.Themes) != len(all) || list.Themes[0].ID != all[0].ID || list.Themes[0].Name != all[0].Name {
		t.Fatalf("themes %+v", list.Themes)
	}
	var light string
	for _, th := range all {
		if th.Light {
			light = th.ID
		}
	}
	if light == "" {
		t.Fatal("no light theme to pick")
	}
	webResData(t, r.call(webproto.OpThemeSet, &webproto.ThemeArgs{ID: light}), &list)
	if r.m.themeID != light || theme.LoadThemeID() != light || list.Active != light {
		t.Errorf("after set: model %q, saved %q, reply %q; want %q", r.m.themeID, theme.LoadThemeID(), list.Active, light)
	}
	for _, th := range list.Themes {
		if th.ID == light && !th.Light {
			t.Errorf("%s is not marked light", light)
		}
	}
	if f := r.call(webproto.OpThemeSet, &webproto.ThemeArgs{ID: "no-such-theme"}); webErrCode(f) != "not_found" {
		t.Errorf("an unknown theme: %v", f)
	}
}

// Spec (WEBP-7): theme.colors lists every color of the custom theme
// with its value now (the user's own marked custom); theme.color_set
// saves one in user_theme.json and shows the custom theme, as
// Preferences: Edit Custom Theme does; an empty color gives the key back
// to the theme. An unknown key is refused.
func TestWebThemeEditor(t *testing.T) {
	r := webThemeRig(t)
	var colors webproto.ThemeColors
	webResData(t, r.call(webproto.OpThemeColors, &webproto.EmptyArgs{}), &colors)
	if len(colors.Colors) != len(theme.PaletteKeys) || colors.Colors[0].Key != theme.PaletteKeys[0] || colors.Colors[0].Color != theme.CurrentPaletteHex(theme.PaletteKeys[0]) || colors.Colors[0].Custom {
		t.Fatalf("colors %+v", colors.Colors[:1])
	}
	webResData(t, r.call(webproto.OpThemeColorSet, &webproto.ThemeColorArgs{Key: "BgEditor", Color: "#112233"}), &colors)
	if got := theme.LoadUserOverrides()["BgEditor"]; got != "#112233" {
		t.Errorf("saved %q", got)
	}
	if r.m.themeID != theme.UserThemeID {
		t.Errorf("theme %q after a custom color; want the custom theme", r.m.themeID)
	}
	var bg webproto.ThemeColor
	for _, c := range colors.Colors {
		if c.Key == "BgEditor" {
			bg = c
		}
	}
	if bg.Color != "#112233" || !bg.Custom {
		t.Errorf("BgEditor %+v", bg)
	}
	webResData(t, r.call(webproto.OpThemeColorSet, &webproto.ThemeColorArgs{Key: "BgEditor", Color: ""}), &colors)
	if _, ok := theme.LoadUserOverrides()["BgEditor"]; ok {
		t.Error("an empty color did not give the key back")
	}
	if f := r.call(webproto.OpThemeColorSet, &webproto.ThemeColorArgs{Key: "NoSuchKey", Color: "#fff"}); webErrCode(f) != "not_found" {
		t.Errorf("an unknown key: %v", f)
	}
}
