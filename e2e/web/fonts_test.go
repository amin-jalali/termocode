//go:build browser

package web

import (
	"strings"
	"testing"
	"time"
)

// The symbol fonts (WEB-58): a terminal row with a prompt's icons shows
// them from the page's own fonts. Each glyph set is its own file and is
// fetched only when a row shows one of its glyphs.
func TestSymbolFontLoadsOnDemand(t *testing.T) {
	p, c := openShell(t, Phone)
	fonts := func() []string {
		var out []string
		for _, u := range p.Requests() {
			if strings.Contains(u, "/assets/fonts/") {
				out = append(out, u[strings.LastIndex(u, "/")+1:])
			}
		}
		return out
	}
	p.Holds(`true`, 300*time.Millisecond)
	if got := fonts(); len(got) > 0 {
		t.Fatalf("fonts fetched with no symbol on screen: %v", got)
	}
	c.Send(t, Delta("term:3", 2, map[string]any{"kind": "lines", "first": 2, "last": 2, "lines": []string{" main  ok"}, "tick": 4}))
	p.WaitJS(`[...document.querySelectorAll('[data-view="term"] .trow-text')].some((e) => e.textContent.includes("main"))`)
	p.WaitJS(`document.fonts.check('13px "termocode symbols"', "")`)
	got := strings.Join(fonts(), " ")
	if !strings.Contains(got, "nf-powerline.woff2") || !strings.Contains(got, "nf-octicons.woff2") || strings.Contains(got, "nf-devicons.woff2") {
		t.Errorf("fetched %q; want powerline and octicons only", got)
	}
	if fam := Eval[string](p, `getComputedStyle(document.querySelector('[data-view="term"] .trow')).fontFamily`); !strings.HasPrefix(fam, `"termocode symbols"`) {
		t.Errorf("terminal rows use %s", fam)
	}
	if bad := p.Problems(); len(bad) > 0 {
		t.Errorf("problems: %v", bad)
	}
}
