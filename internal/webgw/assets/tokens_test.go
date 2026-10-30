package assets

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// tokenBlock returns the custom properties of the first {…} block after
// the marker comment in tokens.css.
func tokenBlock(t *testing.T, css, marker string) map[string]string {
	t.Helper()
	i := strings.Index(css, "/* "+marker)
	if i < 0 {
		t.Fatalf("tokens.css has no %q block", marker)
	}
	rest := css[i:]
	open := strings.Index(rest, "{")
	// The system block nests one level (@media { :root { … } }).
	if marker == "tokens:system-light" {
		rest = rest[open+1:]
		open = strings.Index(rest, "{")
	}
	end := strings.Index(rest[open:], "}")
	body := rest[open+1 : open+end]
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`(--[a-z0-9-]+)\s*:\s*([^;]+);`).FindAllStringSubmatch(body, -1) {
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

func readTokens(t *testing.T) (dark, light, sys map[string]string) {
	t.Helper()
	b, err := os.ReadFile("src/styles/tokens.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	return tokenBlock(t, css, "tokens:dark"), tokenBlock(t, css, "tokens:light"), tokenBlock(t, css, "tokens:system-light")
}

// designTokens are the colour tokens of web UX 7.1; every one must exist.
var designTokens = []string{
	"--bg", "--bg-2", "--bg-panel", "--bg-float", "--bg-hover", "--sel", "--strip", "--on-strip",
	"--text", "--text-2", "--muted", "--dim", "--border", "--focus", "--ai", "--blue", "--green",
	"--amber", "--red", "--git-mod", "--git-add", "--git-del", "--git-conf", "--diff-add-bg", "--diff-del-bg",
}

func TestWebTokensBothThemes(t *testing.T) {
	dark, light, sys := readTokens(t)
	for _, k := range designTokens {
		if dark[k] == "" || light[k] == "" {
			t.Errorf("%s: dark %q light %q; both themes need it", k, dark[k], light[k])
		}
	}
	for k := range dark {
		if _, ok := light[k]; !ok {
			t.Errorf("%s is dark only", k)
		}
	}
	for k, v := range light {
		if sys[k] != v {
			t.Errorf("system light %s = %q, light = %q; the two blocks must match", k, sys[k], v)
		}
	}
	if len(sys) != len(light) {
		t.Errorf("system light has %d tokens, light %d", len(sys), len(light))
	}
}

func hexLum(t *testing.T, hex string) float64 {
	t.Helper()
	if len(hex) != 7 || hex[0] != '#' {
		t.Fatalf("%q is not #rrggbb", hex)
	}
	var c [3]float64
	for i := range c {
		v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			t.Fatal(err)
		}
		s := float64(v) / 255
		if s <= 0.03928 {
			c[i] = s / 12.92
		} else {
			c[i] = math.Pow((s+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}

func contrast(t *testing.T, a, b string) float64 {
	la, lb := hexLum(t, a), hexLum(t, b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// TestWebTokensContrast checks WCAG AA where each token is used: text and
// accents on every surface (4.5:1), git badges on rows, the strip text,
// user bubbles; hints and the focus ring as UI parts (3:1).
func TestWebTokensContrast(t *testing.T) {
	dark, light, _ := readTokens(t)
	surfaces := []string{"--bg", "--bg-2", "--bg-panel", "--bg-float"}
	rows := []string{"--bg", "--bg-panel", "--bg-hover"}
	type rule struct {
		fg  []string
		bg  []string
		min float64
	}
	rules := []rule{
		{[]string{"--text", "--text-2", "--muted", "--ai", "--blue", "--green", "--amber", "--red"}, surfaces, 4.5},
		{[]string{"--git-mod", "--git-add", "--git-del", "--git-conf"}, rows, 4.5},
		{[]string{"--on-strip"}, []string{"--strip"}, 4.5},
		{[]string{"--text"}, []string{"--sel"}, 4.5},
		{[]string{"--on-ai"}, []string{"--ai"}, 4.5},
		{[]string{"--ai-on-strip"}, []string{"--strip"}, 4.5},
		{[]string{"--dim", "--focus"}, surfaces, 3},
	}
	for name, th := range map[string]map[string]string{"dark": dark, "light": light} {
		for _, r := range rules {
			for _, fg := range r.fg {
				for _, bg := range r.bg {
					if c := contrast(t, th[fg], th[bg]); c < r.min {
						t.Errorf("%s: %s %s on %s %s = %.2f:1, want >= %.1f", name, fg, th[fg], bg, th[bg], c, r.min)
					}
				}
			}
		}
	}
}
