package activity

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Extension items (Group I) ────────────────────────────────────────────
//
// Extensions can add their own panels to the activity bar. They sit after
// the built-in top items. Their View values start at ViewExtBase, in the
// order given to SetExtraItems, so View(ViewExtBase+i) is extra item i.

// ViewExtBase is the first View value used by extension items.
const ViewExtBase View = 1000

// ExtraItem describes one extension activity-bar item.
type ExtraItem struct {
	// Label is the panel title (used for the ASCII fallback letter).
	Label string
	// Glyph is an optional 1-cell icon. Empty → first letter of Label.
	Glyph string
}

// extraItems is package-level so every copy of the value-type Model sees
// the same list (the list changes only on extension load / reload).
var extraItems []item

// SetExtraItems replaces the extension items. Pass nil to remove them.
func SetExtraItems(items []ExtraItem) {
	extraItems = extraItems[:0:0]
	for i, it := range items {
		extraItems = append(extraItems, item{
			view:  ViewExtBase + View(i),
			icon:  extraIcon(it),
			label: it.Label,
		})
	}
}

// ExtIndex maps a View to its extension item index.
func ExtIndex(v View) (int, bool) {
	i := int(v - ViewExtBase)
	if v < ViewExtBase || i >= len(extraItems) {
		return 0, false
	}
	return i, true
}

// topAll is the built-in top group plus the extension items.
func topAll() []item {
	if len(extraItems) == 0 {
		return topItems
	}
	out := make([]item, 0, len(topItems)+len(extraItems))
	out = append(out, topItems...)
	return append(out, extraItems...)
}

// extraIcon builds a theme.Icon from an extension glyph. The glyph must be
// exactly one cell wide; anything else falls back to the label's first
// letter so the bar layout can never break.
func extraIcon(it ExtraItem) theme.Icon {
	letter := "?"
	if r, _ := utf8.DecodeRuneInString(strings.TrimSpace(it.Label)); r != utf8.RuneError && unicode.IsPrint(r) {
		letter = string(unicode.ToUpper(r))
		if runewidth.StringWidth(letter) != 1 {
			letter = "?"
		}
	}
	g := strings.TrimSpace(it.Glyph)
	if utf8.RuneCountInString(g) != 1 || runewidth.StringWidth(g) != 1 {
		g = letter
	}
	ascii := letter
	if g != "" && g[0] < 0x80 {
		ascii = g
	}
	return theme.Icon{NerdFont: g, Unicode: g, ASCII: ascii}
}
