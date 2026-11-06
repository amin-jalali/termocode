package webproto

// The laptop's color theme from the web (WEBP-7; the laptop's
// Preferences: Color Theme… and Edit Custom Theme). The page lists the
// themes and the custom theme's colors; picking a theme or changing one
// custom color is the user's own preference, saved at once in the
// laptop's config, as the laptop's pickers save it. The page follows the
// laptop's colors (status.theme).

// Op names.
const (
	OpThemeList     = "theme.list"
	OpThemeSet      = "theme.set"
	OpThemeColors   = "theme.colors"
	OpThemeColorSet = "theme.color_set"
)

// ThemeArgs names a theme by its id ("one-dark").
type ThemeArgs struct {
	ID string `json:"id"`
}

func (a *ThemeArgs) validate(int) error {
	if !validThemeName(a.ID, false) {
		return invalid("bad theme id")
	}
	return nil
}

// ThemeColorArgs sets one color of the custom theme; an empty Color gives
// the key back to the theme.
type ThemeColorArgs struct {
	Key   string `json:"key"`   // a theme palette field ("BgEditor")
	Color string `json:"color"` // "#rgb" or "#rrggbb", or ""
}

func (a *ThemeColorArgs) validate(int) error {
	if !validThemeName(a.Key, true) {
		return invalid("bad color key")
	}
	if a.Color == "" {
		return nil
	}
	if (len(a.Color) != 4 && len(a.Color) != 7) || a.Color[0] != '#' {
		return invalid("bad color")
	}
	for _, r := range a.Color[1:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return invalid("bad color")
		}
	}
	return nil
}

// validThemeName: a theme id (lower case, digits, - and _) or a palette
// key (letters and digits).
func validThemeName(s string, key bool) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if key {
			ok = ok || r >= 'A' && r <= 'Z'
		} else {
			ok = ok || r == '-' || r == '_'
		}
		if !ok {
			return false
		}
	}
	return true
}

// ThemeInfo is one of the laptop's color themes.
type ThemeInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Light bool   `json:"light,omitempty"`
}

// ThemeList answers theme.list and theme.set: the themes in the laptop
// picker's order and the one the laptop shows.
type ThemeList struct {
	Themes []ThemeInfo `json:"themes"`
	Active string      `json:"active"`
}

// ThemeColor is one color of the custom theme: its value now, and
// whether it is the user's own.
type ThemeColor struct {
	Key    string `json:"key"`
	Color  string `json:"color"`
	Custom bool   `json:"custom,omitempty"`
}

// ThemeColors answers theme.colors and theme.color_set, in the laptop
// editor's order.
type ThemeColors struct {
	Colors []ThemeColor `json:"colors"`
}

var opThemeRows = []Policy{
	{Op: OpThemeList, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpThemeSet, New: newOf[ThemeArgs](), Role: RoleController, Rate: perSec(4), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpThemeColors, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpThemeColorSet, New: newOf[ThemeColorArgs](), Role: RoleController, Rate: perSec(4), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opThemeRows...); return true }()

func init() {
	registerResult(OpThemeList, ThemeList{})
	registerResult(OpThemeSet, ThemeList{})
	registerResult(OpThemeColors, ThemeColors{})
	registerResult(OpThemeColorSet, ThemeColors{})
}
