package assets

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// Accessibility pass of the phone app (MOBUX-22): 44 px targets, AA or
// better contrast in both themes, a screen reader name on every block,
// row and icon button, reduced motion (the system's or the user's own
// switch) and a haptics off switch. These guards read the sources, so a
// new phone view cannot slip past them.

// a11yPhoneSources returns the page sources of the phone app: every .ts
// and .tsx file under src (the kit page aside) that draws a ph- class.
func a11yPhoneSources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir("src", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "kit" {
			return filepath.SkipDir
		}
		if d.IsDir() || filepath.Ext(path) != ".tsx" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if s := string(b); a11yUsesPhoneClass.MatchString(s) {
			out[path] = s
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

var a11yUsesPhoneClass = regexp.MustCompile("class(Name)?=[\"{`][^>]*\\bph-[a-z]")

// a11yTag is one JSX element as written in a source: its name, the raw
// text of its attributes and, for a non-empty element, its children up to
// the matching close tag (nested elements of the same name are counted).
type a11yTag struct {
	name, attrs, body string
	line              int
}

// a11yTags finds every <name …> element in src. Attributes end at the
// first > outside braces, strings and template literals.
func a11yTags(src, name string) []a11yTag {
	var out []a11yTag
	open := "<" + name
	for i := 0; ; {
		j := strings.Index(src[i:], open)
		if j < 0 {
			return out
		}
		start := i + j
		k := start + len(open)
		if k < len(src) && (unicode.IsLetter(rune(src[k])) || unicode.IsDigit(rune(src[k]))) {
			i = k
			continue
		}
		end, selfClose := a11yAttrEnd(src, k)
		if end < 0 {
			return out
		}
		tag := a11yTag{name: name, attrs: src[k:end], line: 1 + strings.Count(src[:start], "\n")}
		i = end + 1
		if !selfClose {
			tag.body = a11yBody(src, i, name)
		}
		out = append(out, tag)
	}
}

// a11yAttrEnd returns the index of the > that closes the open tag at k,
// and whether the tag closes itself (/>).
func a11yAttrEnd(src string, k int) (int, bool) {
	depth := 0
	var quote byte
	for p := k; p < len(src); p++ {
		c := src[p]
		switch {
		case quote != 0:
			if c == quote && src[p-1] != '\\' {
				quote = 0
			}
		case c == '"' || c == '\'' || c == '`':
			if depth > 0 || c == '"' {
				quote = c
			}
		case c == '{':
			depth++
		case c == '}':
			depth--
		case c == '>' && depth == 0:
			return p, p > 0 && src[p-1] == '/'
		}
	}
	return -1, false
}

// a11yBody returns the children of an element whose open tag ended just
// before from, up to its own close tag.
func a11yBody(src string, from int, name string) string {
	depth := 1
	open, closeTag := "<"+name, "</"+name+">"
	for p := from; p < len(src); {
		o := strings.Index(src[p:], open)
		c := strings.Index(src[p:], closeTag)
		if c < 0 {
			return src[from:]
		}
		if o >= 0 && o < c {
			p += o + len(open)
			// <a must not count <aside or <abbr as another <a.
			if p < len(src) && !unicode.IsLetter(rune(src[p])) && !unicode.IsDigit(rune(src[p])) {
				depth++
			}
			continue
		}
		depth--
		if depth == 0 {
			return src[from : p+c]
		}
		p += c + len(closeTag)
	}
	return src[from:]
}

var (
	a11yLabelled   = regexp.MustCompile(`\baria-(label|labelledby)\s*=`)
	a11yJSXExpr    = regexp.MustCompile(`\{[^{}]*\}`)
	a11yInnerTag   = regexp.MustCompile(`<[^<>]*>`)
	a11yTextInExpr = regexp.MustCompile(`\bt\(|\btp\(|\bTXT\.|\blabel\b|\btitle\b|\bname\b|\btext\b|\bchildren\b`)
)

// a11yIconOnly says whether a button's children give a screen reader no
// name: nothing but glyphs (⚙ ☰ ↩ ✕ ▾ 🎤 …) and icon elements, with no
// t(…) text and no expression that names a label, title, name or text.
func a11yIconOnly(body string) bool {
	for _, m := range a11yJSXExpr.FindAllString(body, -1) {
		if a11yTextInExpr.MatchString(m) {
			return false
		}
	}
	rest := a11yInnerTag.ReplaceAllString(a11yJSXExpr.ReplaceAllString(body, ""), "")
	for _, r := range rest {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func TestA11yIconOnlyCatches(t *testing.T) {
	for _, icon := range []string{"⚙", " ☰ ", "<Codicon name=\"close\" />", "<span aria-hidden=\"true\">↩</span>", "🎤", ""} {
		if !a11yIconOnly(icon) {
			t.Errorf("%q has no name, yet passed as text", icon)
		}
	}
	for _, text := range []string{`{t("home.send")}`, `<span>{label}</span>`, `Send`, `{p.title}`, `{children}`, `{tp("ph.forget_yes")}`, `{TXT.retry}`} {
		if a11yIconOnly(text) {
			t.Errorf("%q names the button, yet was taken as icon only", text)
		}
	}
	tags := a11yTags(`<div><button type="button" onClick={() => go(a > b)}>⚙</button><button aria-label={t("x")}>✕</button></div>`, "button")
	if len(tags) != 2 || tags[0].body != "⚙" || a11yLabelled.MatchString(tags[0].attrs) || !a11yLabelled.MatchString(tags[1].attrs) {
		t.Fatalf("tags = %+v", tags)
	}
}

// Every button, and every element acting as one, that shows only an icon
// carries a name for the screen reader (from strings.ts, never hardcoded).
func TestPhoneIconButtonsHaveLabels(t *testing.T) {
	for path, src := range a11yPhoneSources(t) {
		for _, name := range []string{"button", "a"} {
			for _, tag := range a11yTags(src, name) {
				if tag.body == "" && name == "a" {
					continue
				}
				if a11yIconOnly(tag.body) && !a11yLabelled.MatchString(tag.attrs) && !strings.Contains(tag.attrs, "aria-hidden") {
					t.Errorf("%s:%d: <%s> shows only %q; give it aria-label={t(…)}", path, tag.line, name, strings.TrimSpace(tag.body))
				}
			}
		}
		for _, tag := range a11yTags(src, "div") {
			if strings.Contains(tag.attrs, `role="button"`) && a11yIconOnly(tag.body) && !a11yLabelled.MatchString(tag.attrs) {
				t.Errorf("%s:%d: role=button shows only %q; give it aria-label={t(…)}", path, tag.line, strings.TrimSpace(tag.body))
			}
		}
	}
}

// a11yNamedParts are the phone parts a screen reader must announce by
// name: a block in the stream, a card, a sheet, a board tile, a view.
var a11yNamedParts = map[string]bool{"ph-block": true, "ph-card": true, "ph-sheet": true, "ph-tile": true, "ph-view": true}

var (
	a11yClassAttr = regexp.MustCompile("\\bclass(Name)?=(\"[^\"]*\"|\\{`[^`]*`\\}|\\{[^}]*\\})")
	a11yTmplExpr  = regexp.MustCompile(`\$\{[^}]*\}`)
)

// a11yClasses returns the static class names of a tag's attributes.
func a11yClasses(attrs string) []string {
	m := a11yClassAttr.FindStringSubmatch(attrs)
	if m == nil {
		return nil
	}
	v := a11yTmplExpr.ReplaceAllString(m[2], " ")
	return strings.FieldsFunc(v, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_')
	})
}

// Every block, card, sheet, board tile and view of the phone has a name
// (aria-label or aria-labelledby), so a screen reader says what it is.
func TestPhoneBlocksAreNamed(t *testing.T) {
	for path, src := range a11yPhoneSources(t) {
		for _, name := range []string{"div", "section", "article", "li", "aside", "dialog"} {
			for _, tag := range a11yTags(src, name) {
				for _, c := range a11yClasses(tag.attrs) {
					if a11yNamedParts[c] && !a11yLabelled.MatchString(tag.attrs) {
						t.Errorf("%s:%d: <%s class=%s> has no aria-label or aria-labelledby", path, tag.line, name, c)
					}
				}
			}
		}
	}
}

// a11yTapParts are the endings of phone class names that are things to
// tap; each such class sets a 44 px target in its own base rule.
var a11yTapPart = regexp.MustCompile(`^ph-[a-z0-9-]*(-btn|-act|-send|-close|-x|-tool|-chip|-retry|-back|-menu|-switch)$`)

var (
	a11yCSSRule  = regexp.MustCompile(`(?s)([^{}]+)\{([^{}]*)\}`)
	a11yTapValue = regexp.MustCompile(`\b(min-height|height|block-size|min-block-size)\s*:\s*(var\(--p-tap\)|var\(--tap\)|calc\([^;]*var\(--(p-)?tap\)[^;]*\))`)
	a11yComment  = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

// 44 px targets: every phone class that is a thing to tap (…-btn, -act,
// -send, -close, -x, -tool, -chip, -retry, -back, -menu, -switch)
// sets min-height (or height) to the tap token in a rule of its own.
func TestPhoneTapTargets(t *testing.T) {
	b, err := os.ReadFile("src/styles/phone.css")
	if err != nil {
		t.Fatal(err)
	}
	css := a11yComment.ReplaceAllString(string(b), "")
	seen := map[string]bool{}
	sized := map[string]bool{}
	for _, m := range a11yCSSRule.FindAllStringSubmatch(css, -1) {
		for _, sel := range strings.Split(m[1], ",") {
			sel = strings.TrimSpace(sel)
			if !strings.HasPrefix(sel, ".") || strings.ContainsAny(sel[1:], ".: >[+~") {
				continue
			}
			cls := sel[1:]
			if !a11yTapPart.MatchString(cls) {
				continue
			}
			seen[cls] = true
			if a11yTapValue.MatchString(m[2]) {
				sized[cls] = true
			}
		}
	}
	for cls := range seen {
		if !sized[cls] {
			t.Errorf("phone.css: .%s is a thing to tap; set min-height: var(--p-tap) in its rule", cls)
		}
	}
	// Sources may use a tap class that has no rule at all: that is a
	// target the CSS never sized.
	use := regexp.MustCompile(`\bph-[a-z0-9-]+`)
	for path, src := range a11yPhoneSources(t) {
		for _, cls := range use.FindAllString(src, -1) {
			if a11yTapPart.MatchString(cls) && !seen[cls] {
				t.Errorf("%s: .%s is a thing to tap but phone.css has no .%s { min-height: var(--p-tap) } rule", path, cls, cls)
			}
		}
	}
}

// a11yResolve follows var(--x) through the phone tokens to a web token
// and returns its #rrggbb value in theme th ("" when it is not a colour).
func a11yResolve(phone, th map[string]string, k string) string {
	ref := regexp.MustCompile(`^var\((--[a-z0-9-]+)\)$`)
	for i := 0; i < 8; i++ {
		v, ok := phone[k]
		if !ok {
			v = th[k]
		}
		m := ref.FindStringSubmatch(v)
		if m == nil {
			if len(v) == 7 && v[0] == '#' {
				return v
			}
			return ""
		}
		k = m[1]
	}
	return ""
}

// AA or better in both themes, on every phone surface: body text at 7:1
// (AAA), other text at 4.5:1, status marks and the focus ring at 3:1 (UI
// parts; status always comes with a shape too). The accent is a fill
// only, under --p-on-accent text: it never colours text itself.
func TestPhoneContrastBothThemes(t *testing.T) {
	phone := phoneTokens(t, readPhoneCSS(t), "phone:tokens")
	dark, light, _ := readTokens(t)
	surfaces := []string{"--p-bg", "--p-surface", "--p-surface-2", "--p-raised"}
	type rule struct {
		fg  []string
		bg  []string
		min float64
	}
	rules := []rule{
		{[]string{"--p-text"}, []string{"--p-bg", "--p-surface"}, 7},
		{[]string{"--p-text", "--p-text-2", "--p-muted", "--p-ai"}, surfaces, 4.5},
		{[]string{"--p-on-accent"}, []string{"--p-accent"}, 4.5},
		{[]string{"--p-focus", "--p-st-needs", "--p-st-working", "--p-st-review", "--p-st-done", "--p-st-failed", "--p-st-idle", "--p-st-asleep"}, surfaces, 3},
	}
	accentText := regexp.MustCompile(`(^|[;{\s])color:\s*var\(--p-accent\)`)
	for _, f := range []string{"src/styles/phone.css", "src/styles/home.css"} {
		if b, err := os.ReadFile(f); err == nil && accentText.Match(b) {
			t.Errorf("%s colours text with --p-accent; it is below 4.5:1 on dark surfaces, use --p-text or --blue", f)
		}
	}
	for name, th := range map[string]map[string]string{"dark": dark, "light": light} {
		for _, r := range rules {
			for _, fg := range r.fg {
				for _, bg := range r.bg {
					f, g := a11yResolve(phone, th, fg), a11yResolve(phone, th, bg)
					if f == "" || g == "" {
						t.Errorf("%s: %s (%q) or %s (%q) is not a plain colour token", name, fg, f, bg, g)
						continue
					}
					if c := contrast(t, f, g); c < r.min {
						t.Errorf("%s: %s %s on %s %s = %.2f:1, want >= %.1f", name, fg, f, bg, g, c, r.min)
					}
				}
			}
		}
	}
}

func a11yRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Reduced motion: the system's setting, or the user's own switch on This
// phone (data-motion="reduce" on <html>), stops every animation and
// transition, on the web tokens and the phone tokens alike.
func TestPhoneReducedMotionSwitch(t *testing.T) {
	prefs := a11yRead(t, "src/lib/prefs.ts")
	for _, want := range []string{`export type Motion = "system" | "reduce"`, "motion: Motion", "haptics: boolean"} {
		if !strings.Contains(prefs, want) {
			t.Errorf("lib/prefs.ts has no %q", want)
		}
	}
	if !regexp.MustCompile(`html\.dataset\.motion\s*=`).MatchString(a11yRead(t, "src/main.tsx")) {
		t.Error(`main.tsx never sets html.dataset.motion from prefs.motion`)
	}
	const sel = `:root[data-motion="reduce"]`
	base := a11yRead(t, "src/styles/base.css")
	if i := strings.Index(base, sel+" *"); i < 0 || !strings.Contains(base[i:], "animation-duration: 0s") {
		t.Errorf("base.css: %s *, … { animation-duration: 0s … } is missing", sel)
	}
	tokens := a11yRead(t, "src/styles/tokens.css")
	if i := strings.Index(tokens, sel+" {"); i < 0 || !strings.Contains(tokens[i:i+strings.Index(tokens[i:], "}")], "--t-fast: 0ms") {
		t.Errorf("tokens.css: %s { --t-fast: 0ms; … } is missing", sel)
	}
	phone := readPhoneCSS(t)
	for k, v := range phoneTokens(t, phone, "phone:tokens") {
		if !strings.HasPrefix(k, "--p-t-") || strings.HasPrefix(v, "var(--t-") {
			continue
		}
		if i := strings.Index(phone, sel); i < 0 || !strings.Contains(phone[i:], k+": 0ms") {
			t.Errorf("phone.css: %s is not a web motion token; set it to 0ms under %s too", k, sel)
		}
	}
}

// Haptics: one module buzzes, and only while the switch on This phone is
// on (it is on by default; off means never).
func TestPhoneHapticsSwitch(t *testing.T) {
	h := a11yRead(t, "src/lib/haptic.ts")
	if !regexp.MustCompile(`prefs\.(peek\(\)|value)\.haptics`).MatchString(h) {
		t.Error("lib/haptic.ts never reads prefs.haptics; the off switch would not hold")
	}
	vib := regexp.MustCompile(`\.vibrate\b`)
	err := filepath.WalkDir("src", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".ts" && ext != ".tsx" {
			return nil
		}
		if filepath.ToSlash(path) == "src/lib/haptic.ts" {
			return nil
		}
		if vib.MatchString(a11yRead(t, path)) {
			t.Errorf("%s calls vibrate; buzz through lib/haptic.ts so the off switch holds", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	views := a11yRead(t, "src/views/views.tsx")
	for _, k := range []string{`setPref("haptics"`, `setPref("motion"`} {
		if !strings.Contains(views, k) {
			t.Errorf("This phone (views.tsx) has no %s switch", k)
		}
	}
}
