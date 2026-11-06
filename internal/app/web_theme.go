package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The laptop's color theme from the web (WEBP-7): Preferences: Color
// Theme… and Edit Custom Theme on the page. Both save as the laptop's
// own pickers save (config.json, user_theme.json) and repaint the
// laptop; the page follows the laptop's colors through status.theme.

func init() {
	webOps[webproto.OpThemeList] = (*Model).webThemeList
	webOps[webproto.OpThemeSet] = (*Model).webThemeSet
	webOps[webproto.OpThemeColors] = (*Model).webThemeColors
	webOps[webproto.OpThemeColorSet] = (*Model).webThemeColorSet
}

// webThemes lists the themes and the one the laptop shows.
func (m *Model) webThemes() webproto.ThemeList {
	active := m.themeID
	if active == "" {
		active = theme.LoadThemeID()
	}
	if active == "" {
		active = "vscode-dark-plus"
	}
	out := webproto.ThemeList{Themes: []webproto.ThemeInfo{}, Active: active}
	for _, t := range theme.AvailableThemes() {
		out.Themes = append(out.Themes, webproto.ThemeInfo{ID: t.ID, Name: t.Name, Light: t.Light})
	}
	return out
}

func (m *Model) webThemeList(call *webCall) tea.Cmd {
	call.reply(m.webThemes())
	return nil
}

func (m *Model) webThemeSet(call *webCall) tea.Cmd {
	id := call.args.(*webproto.ThemeArgs).ID
	known := false
	for _, t := range theme.AvailableThemes() {
		known = known || t.ID == id
	}
	if !known {
		call.fail(webproto.CodeNotFound, "The laptop has no theme of that name.")
		return nil
	}
	if m.themePreviewing { // the laptop's own picker is open: the web's choice wins
		m.endThemePreview(true)
	}
	m.applyTheme(id)
	call.reply(m.webThemes())
	return nil
}

// webThemeColorList is the custom theme's colors now.
func webThemeColorList() webproto.ThemeColors {
	own := theme.LoadUserOverrides()
	out := webproto.ThemeColors{Colors: make([]webproto.ThemeColor, 0, len(theme.PaletteKeys))}
	for _, k := range theme.PaletteKeys {
		c := webproto.ThemeColor{Key: k, Color: theme.CurrentPaletteHex(k)}
		if v, ok := own[k]; ok {
			c.Color, c.Custom = v, true
		}
		out.Colors = append(out.Colors, c)
	}
	return out
}

func (m *Model) webThemeColors(call *webCall) tea.Cmd {
	call.reply(webThemeColorList())
	return nil
}

func (m *Model) webThemeColorSet(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ThemeColorArgs)
	known := false
	for _, k := range theme.PaletteKeys {
		known = known || k == a.Key
	}
	if !known {
		call.fail(webproto.CodeNotFound, "The custom theme has no color of that name.")
		return nil
	}
	if a.Color != "" && !theme.IsHexColor(a.Color) {
		call.fail(webproto.CodeBadRequest, "A color is # and 3 or 6 hex digits.")
		return nil
	}
	// SaveUserOverrides keeps the other keys; an empty value deletes one.
	if err := theme.SaveUserOverrides(map[string]string{a.Key: a.Color}); err != nil {
		call.fail(webproto.CodeInternal, "The custom theme was not saved.")
		return nil
	}
	switch {
	case theme.HasUserTheme():
		m.applyTheme(theme.UserThemeID)
	case m.themeID == theme.UserThemeID: // the last custom color went: the default theme
		m.applyTheme("vscode-dark-plus")
	}
	call.reply(webThemeColorList())
	return nil
}
