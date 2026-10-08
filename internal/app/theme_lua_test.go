package app

import (
	"regexp"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Every colour the colourscheme Lua reads (c.x, C.x, c['x']) must exist in
// theme.NvimColors for every theme, or nvim silently drops the highlight.
func TestThemeLuaReferencesExist(t *testing.T) {
	defer theme.ApplyTheme("vscode-dark-plus")
	re := regexp.MustCompile(`\b[cC]\.([a-z_0-9]+)|\b[cC]\['([a-z_0-9]+)'\]`)
	srcs := []string{themeLuaBody, gitSignsLua, buildCommitDiffLua("/x", "x")}
	for _, nt := range theme.AvailableThemes() {
		theme.ApplyTheme(nt.ID)
		colors := theme.NvimColors(nt.ID)
		for _, src := range srcs {
			for _, m := range re.FindAllStringSubmatch(src, -1) {
				k := m[1] + m[2]
				if _, ok := colors[k]; !ok {
					t.Errorf("%s: Lua reads colour %q which NvimColors does not define", nt.ID, k)
				}
			}
		}
		lua := themeLua(nt.ID, nt.Light)
		if strings.Contains(lua, "`") {
			t.Fatal("backtick in Lua")
		}
		if nt.Light && !strings.Contains(lua, "background = 'light'") {
			t.Errorf("%s: light theme must set background=light", nt.ID)
		}
	}
}
