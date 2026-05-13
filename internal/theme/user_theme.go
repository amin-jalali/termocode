package theme

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// UserThemeID is the stable id used to refer to the user's custom palette.
// Selecting this id from the picker applies the on-disk overrides on top
// of the built-in VSCode Dark+ palette (so any field the user hasn't
// overridden inherits a sensible default).
const UserThemeID = "custom"

// UserThemeName is the human-readable label the picker shows.
const UserThemeName = "Custom"

// UserThemePath returns the absolute path to user_theme.json. Mirrors
// ConfigPath: $XDG_CONFIG_HOME wins, otherwise ~/.config.
func UserThemePath() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "termocode", "user_theme.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "termocode", "user_theme.json"), nil
}

// userThemeFile is the persisted shape — a flat string->string map of
// palette key (e.g. "BgEditor") to hex color (e.g. "#1c1c1c"). Unknown
// keys are silently dropped on load.
type userThemeFile map[string]string

// LoadUserOverrides reads the user theme file and returns its raw entries.
// Missing/corrupt = empty map (never an error).
func LoadUserOverrides() map[string]string {
	path, err := UserThemePath()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return nil
	}
	var raw userThemeFile
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

// SaveUserOverrides persists the given key->hex map. Existing keys not in
// `entries` are preserved (read-modify-write); entries with empty values
// are deleted.
func SaveUserOverrides(entries map[string]string) error {
	path, err := UserThemePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out := userThemeFile{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	for k, v := range entries {
		v = strings.TrimSpace(v)
		if v == "" {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	enc, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, enc, 0o644)
}

// HasUserTheme reports whether a non-empty user theme file exists. The
// theme picker consults this to decide whether to surface the "Custom"
// row.
func HasUserTheme() bool {
	return len(LoadUserOverrides()) > 0
}

// PaletteKeys is the canonical ordered list of palette fields users can
// override in user_theme.json. The keybinding-customization UI mirrors
// this slice's order so the picker reads consistently.
var PaletteKeys = []string{
	"BgEditor",
	"BgSidebar",
	"BgActivityBar",
	"BgTitleBar",
	"BgPanel",
	"BgStatusBar",
	"BgSelection",
	"BgHover",
	"BgActiveLine",
	"BgInactiveSel",
	"BgSectionHdr",

	"TextPrimary",
	"TextSecondary",
	"TextMuted",
	"TextDim",
	"TextDimmer",
	"TextWhite",
	"Cursor",
	"ActiveLineNum",

	"BorderDefault",
	"BorderFocus",
	"BorderSubtle",

	"SyntaxKeyword",
	"SyntaxControl",
	"SyntaxFunction",
	"SyntaxVariable",
	"SyntaxType",
	"SyntaxString",
	"SyntaxNumber",
	"SyntaxComment",
	"SyntaxRegex",
	"SyntaxConstant",
	"SyntaxOperator",

	"DiagError",
	"DiagWarning",
	"DiagInfo",
	"DiagHint",

	"GitModified",
	"GitAdded",
	"GitUntracked",
	"GitDeleted",
	"GitConflict",
	"GitIgnored",
	"GitSubmodule",
}

// CurrentPaletteHex returns the active hex value for a palette field by
// name. Used by the editor UI to show the current color before the user
// types a new one. Returns "" for unknown keys.
func CurrentPaletteHex(field string) string {
	c, ok := paletteFieldValue(field)
	if !ok {
		return ""
	}
	return Hex(c)
}

// paletteFieldValue returns the live Color256 for a palette field by name.
// Centralised so SaveUserOverrides + the picker can both consult the
// authoritative source.
func paletteFieldValue(field string) (Color256, bool) {
	switch field {
	case "BgEditor":
		return BgEditor, true
	case "BgSidebar":
		return BgSidebar, true
	case "BgActivityBar":
		return BgActivityBar, true
	case "BgTitleBar":
		return BgTitleBar, true
	case "BgPanel":
		return BgPanel, true
	case "BgStatusBar":
		return BgStatusBar, true
	case "BgSelection":
		return BgSelection, true
	case "BgHover":
		return BgHover, true
	case "BgActiveLine":
		return BgActiveLine, true
	case "BgInactiveSel":
		return BgInactiveSel, true
	case "BgSectionHdr":
		return BgSectionHdr, true
	case "TextPrimary":
		return TextPrimary, true
	case "TextSecondary":
		return TextSecondary, true
	case "TextMuted":
		return TextMuted, true
	case "TextDim":
		return TextDim, true
	case "TextDimmer":
		return TextDimmer, true
	case "TextWhite":
		return TextWhite, true
	case "Cursor":
		return Cursor, true
	case "ActiveLineNum":
		return ActiveLineNum, true
	case "BorderDefault":
		return BorderDefault, true
	case "BorderFocus":
		return BorderFocus, true
	case "BorderSubtle":
		return BorderSubtle, true
	case "SyntaxKeyword":
		return SyntaxKeyword, true
	case "SyntaxControl":
		return SyntaxControl, true
	case "SyntaxFunction":
		return SyntaxFunction, true
	case "SyntaxVariable":
		return SyntaxVariable, true
	case "SyntaxType":
		return SyntaxType, true
	case "SyntaxString":
		return SyntaxString, true
	case "SyntaxNumber":
		return SyntaxNumber, true
	case "SyntaxComment":
		return SyntaxComment, true
	case "SyntaxRegex":
		return SyntaxRegex, true
	case "SyntaxConstant":
		return SyntaxConstant, true
	case "SyntaxOperator":
		return SyntaxOperator, true
	case "DiagError":
		return DiagError, true
	case "DiagWarning":
		return DiagWarning, true
	case "DiagInfo":
		return DiagInfo, true
	case "DiagHint":
		return DiagHint, true
	case "GitModified":
		return GitModified, true
	case "GitAdded":
		return GitAdded, true
	case "GitUntracked":
		return GitUntracked, true
	case "GitDeleted":
		return GitDeleted, true
	case "GitConflict":
		return GitConflict, true
	case "GitIgnored":
		return GitIgnored, true
	case "GitSubmodule":
		return GitSubmodule, true
	}
	return 0, false
}

// IsHexColor reports whether `s` looks like a usable hex color spec
// (`#rgb` or `#rrggbb`). This is the validation gate for the user
// theme editor — anything else is rejected before persist.
func IsHexColor(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#") {
		return false
	}
	rest := s[1:]
	if len(rest) != 3 && len(rest) != 6 {
		return false
	}
	for _, r := range rest {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
