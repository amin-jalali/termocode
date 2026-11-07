package statusbar

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/theme"
)

// DBX-21: the database chip carries the connection's colour as a bar
// before its icon; no colour, no bar. ASCII mode uses "|".
func TestDatabaseChipTint(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	plain := stripCSI(renderDatabaseChip(State{ShowDatabase: true, DatabaseName: "pg"}))
	if strings.Contains(plain, "▌") {
		t.Errorf("chip without a colour has a bar: %q", plain)
	}
	tinted := stripCSI(renderDatabaseChip(State{ShowDatabase: true, DatabaseName: "pg", DatabaseTinted: true, DatabaseTint: theme.AccentBlue}))
	if !strings.HasPrefix(tinted, "▌") || !strings.HasSuffix(tinted, "pg") {
		t.Errorf("tinted chip = %q", tinted)
	}
	theme.CurrentIconMode = theme.IconModeASCII
	if got := stripCSI(renderDatabaseChip(State{ShowDatabase: true, DatabaseName: "pg", DatabaseTinted: true, DatabaseTint: theme.AccentBlue})); !strings.HasPrefix(got, "|") || strings.Contains(got, "▌") {
		t.Errorf("ASCII tinted chip = %q", got)
	}
	// The cache sees a colour change.
	a := State{ShowDatabase: true, DatabaseName: "pg", DatabaseTinted: true, DatabaseTint: theme.AccentBlue}
	b := a
	b.DatabaseTint = theme.AccentGreen
	if sameState(&a, &b) {
		t.Error("cache ignores the database tint")
	}
}
