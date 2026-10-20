package theme

import (
	"github.com/mattn/go-runewidth"
	"testing"
)

func TestDatabaseNerdIconUsesSingleCellBMPGlyph(t *testing.T) {
	runes := []rune(IconDatabases.NerdFont)
	if len(runes) != 1 || runes[0] < 0xe000 || runes[0] > 0xf8ff || runewidth.StringWidth(IconDatabases.NerdFont) != 1 {
		t.Fatalf("database icon %q must be a single-cell BMP private-use glyph", IconDatabases.NerdFont)
	}
}
