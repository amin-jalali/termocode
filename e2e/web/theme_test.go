//go:build browser

package web

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Following the laptop's theme (WEB-UX-8): status.theme carries the
// laptop palette; the page shows it in its own scheme, always readable.

var (
	oneDark = map[string]any{"name": "one-dark", "colors": map[string]string{
		"BgEditor": "#282c34", "BgSidebar": "#21252b", "BgPanel": "#1e2127", "BgFloat": "#2c313a",
		"BgHover": "#2c313c", "BgSelection": "#3e4451", "BgStatusBar": "#21252b", "OnAccent": "#ffffff",
		"TextPrimary": "#abb2bf", "TextSecondary": "#9da5b4", "TextMuted": "#7f848e", "TextDim": "#5c6370",
		"BorderDefault": "#3e4451", "BorderFocus": "#528bff", "AIAccent": "#c678dd", "AccentBlue": "#61afef",
		"AccentGreen": "#98c379", "AccentAmber": "#e5c07b", "AccentRedCoral": "#e06c75",
		"GitModified": "#d19a66", "GitAdded": "#98c379", "GitDeleted": "#e06c75", "GitConflict": "#c678dd",
	}}
	// unreadable is a broken theme: grey on grey, a white strip under white text.
	unreadable = map[string]any{"name": "bad", "colors": map[string]string{
		"BgEditor": "#3a3a3a", "TextPrimary": "#444444", "TextMuted": "#404040", "AIAccent": "#3b3b3b",
		"BgStatusBar": "#eeeeee", "OnAccent": "#ffffff", "BgSelection": "#454545",
	}}
	solarLight = map[string]any{"name": "solarized-light", "light": true, "colors": map[string]string{
		"BgEditor": "#fdf6e3", "BgSidebar": "#eee8d5", "BgPanel": "#fdf6e3", "BgFloat": "#eee8d5",
		"TextPrimary": "#657b83", "TextMuted": "#93a1a1", "AIAccent": "#6c71c4", "AccentBlue": "#268bd2",
		"AccentGreen": "#859900", "AccentAmber": "#b58900", "AccentRedCoral": "#dc322f", "BgStatusBar": "#268bd2",
	}}
)

func withTheme(th map[string]any) map[string]any {
	d := statusData("main", 0)
	if th != nil {
		d["theme"] = th
	}
	return d
}

func (p *Page) tokens() map[string]string {
	p.T.Helper()
	return Eval[map[string]string](p, `(() => {
		const cs = getComputedStyle(document.documentElement), out = {};
		for (const k of ["--bg","--bg-2","--bg-panel","--bg-float","--bg-hover","--sel","--strip","--on-strip","--ai-on-strip","--on-ai",
			"--text","--text-2","--muted","--dim","--focus","--ai","--blue","--green","--amber","--red",
			"--git-mod","--git-add","--git-del","--git-conf"]) out[k] = cs.getPropertyValue(k).trim().toLowerCase();
		return out;
	})()`)
}

func luminance(t *testing.T, hex string) float64 {
	t.Helper()
	if len(hex) != 7 || hex[0] != '#' {
		t.Fatalf("token %q is not #rrggbb", hex)
	}
	var sum float64
	for i, w := range []float64{0.2126, 0.7152, 0.0722} {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		s := float64(v) / 255
		if s <= 0.03928 {
			s /= 12.92
		} else {
			s = math.Pow((s+0.055)/1.055, 2.4)
		}
		sum += w * s
	}
	return sum
}

func ratio(t *testing.T, a, b string) float64 {
	x, y := luminance(t, a), luminance(t, b)
	return (math.Max(x, y) + 0.05) / (math.Min(x, y) + 0.05)
}

// checkContrast applies the rules of tokens_test.go (TestWebTokensContrast)
// to the tokens the page is showing right now.
func checkContrast(t *testing.T, where string, tk map[string]string) {
	t.Helper()
	surfaces := []string{"--bg", "--bg-2", "--bg-panel", "--bg-float"}
	rows := []string{"--bg", "--bg-panel", "--bg-hover"}
	rules := []struct {
		fg, bg []string
		min    float64
	}{
		{[]string{"--text", "--text-2", "--muted", "--ai", "--blue", "--green", "--amber", "--red"}, surfaces, 4.5},
		{[]string{"--git-mod", "--git-add", "--git-del", "--git-conf"}, rows, 4.5},
		{[]string{"--on-strip"}, []string{"--strip"}, 4.5},
		{[]string{"--text"}, []string{"--sel"}, 4.5},
		{[]string{"--on-ai"}, []string{"--ai"}, 4.5},
		{[]string{"--ai-on-strip"}, []string{"--strip"}, 4.5},
		{[]string{"--dim", "--focus"}, surfaces, 3},
	}
	for _, r := range rules {
		for _, fg := range r.fg {
			for _, bg := range r.bg {
				if c := ratio(t, tk[fg], tk[bg]); c < r.min-0.005 {
					t.Errorf("%s: %s %s on %s %s = %.2f:1, want >= %.1f", where, fg, tk[fg], bg, tk[bg], c, r.min)
				}
			}
		}
	}
}

func setScheme(p *Page, scheme string) {
	p.T.Helper()
	p.GoTab("more")
	n := map[string]string{"dark": "2", "light": "3"}[scheme]
	p.pickPref(1, n)
	p.WaitJS(`document.documentElement.dataset.theme === "` + scheme + `"`)
}

// The page wears the laptop's theme in the laptop's scheme as it is, a
// derived version in the other scheme, and every contrast rule holds for
// a good theme, a light one and a broken one.
func TestThemeFollowsTheLaptopAndStaysReadable(t *testing.T) {
	for _, th := range []map[string]any{oneDark, solarLight, unreadable} {
		name := th["name"].(string)
		t.Run(name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, Phone)
			c := p.Login("c-1")
			c.Send(t, Delta("status", 2, withTheme(th)))
			p.WaitJS(`document.documentElement.dataset.laptopTheme === "` + name + `"`)
			for _, scheme := range []string{"dark", "light"} {
				setScheme(p, scheme)
				tk := p.tokens()
				checkContrast(t, name+"/"+scheme, tk)
				bgLum := luminance(t, tk["--bg"])
				if (scheme == "dark") != (bgLum < 0.2) {
					t.Errorf("%s/%s: --bg %s does not look %s", name, scheme, tk["--bg"], scheme)
				}
				own := (scheme == "light") == (th["light"] == true)
				want := th["colors"].(map[string]string)["BgEditor"]
				if own && tk["--bg"] != want {
					t.Errorf("%s/%s: --bg = %s, want the laptop's %s", name, scheme, tk["--bg"], want)
				}
			}
			p.Holds("true", 100*time.Millisecond)
			if got := p.Problems(); len(got) > 0 {
				t.Errorf("problems: %s", strings.Join(got, "\n"))
			}
		})
	}
}

// Without a theme, or with a malformed one, the page keeps its own
// tokens; when the laptop's theme goes away, so do its colours.
func TestThemeMissingOrMalformedKeepsOwnTokens(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	c := p.Login("c-1")
	setScheme(p, "dark")
	if bg := p.tokens()["--bg"]; bg != "#1c1c1c" {
		t.Fatalf("no theme: --bg = %s, want the built-in #1c1c1c", bg)
	}
	c.Send(t, Delta("status", 2, withTheme(oneDark)))
	p.WaitJS(`getComputedStyle(document.documentElement).getPropertyValue("--bg").trim() === "#282c34"`)
	c.Send(t, Delta("status", 3, withTheme(map[string]any{"name": "x", "colors": map[string]any{"BgEditor": "red", "TextPrimary": 7}})))
	p.WaitJS(`getComputedStyle(document.documentElement).getPropertyValue("--bg").trim() === "#1c1c1c"`)
	if p.Eval(`document.documentElement.dataset.laptopTheme ?? ""`) != "" {
		t.Error("a malformed theme still names itself")
	}
	checkContrast(t, "built-in dark", p.tokens())
}

// Eval is a short form for a string result.
func (p *Page) Eval(js string) string { return Eval[string](p, js) }
