package importview

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/amin-jalali/termocode/internal/theme"
)

// The panel shows what a user can read: command titles, setting names and
// theme names, not the ids the files use. The ids stay in the plan (they
// are what gets written); only the text on screen changes.

// Option changes how New shows the rows.
type Option func(*Model)

// WithActionTitle sets how a keymap action name ("QuickOpen") becomes the
// title users know from the command palette ("Go to File"). It returns ""
// for an action that has no title; the panel then spells the name out.
func WithActionTitle(f func(name string) string) Option {
	return func(m *Model) { m.actionTitleOf = f }
}

// actionTitle is the text for a keymap action name.
func (m Model) actionTitle(name string) string {
	if m.actionTitleOf != nil {
		if t := clean(m.actionTitleOf(name)); t != "" {
			return t
		}
	}
	return spellOut(name)
}

// spellOut turns "CommandPalette" into "Command palette".
func spellOut(name string) string {
	var b strings.Builder
	r := []rune(name)
	for i, c := range r {
		if i > 0 && unicode.IsUpper(c) && (unicode.IsLower(r[i-1]) || unicode.IsDigit(r[i-1])) {
			b.WriteByte(' ')
			b.WriteRune(unicode.ToLower(c))
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// settingLabel turns the config key "tab_size" into "Tab size".
func settingLabel(key string) string {
	s := strings.ReplaceAll(key, "_", " ")
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

// settingValue shows a value the way the Settings page does: on / off for
// a switch, the plain value otherwise.
func settingValue(v any) string {
	if b, ok := v.(bool); ok {
		if b {
			return "on"
		}
		return "off"
	}
	return fmt.Sprint(v)
}

// themeName is the theme's display name; an unknown id stays as it is.
func themeName(id string) string {
	if t, ok := theme.LookupTheme(id); ok && t.Name != "" {
		return t.Name
	}
	return id
}
