package assets

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Phone design tokens and base components (MOBUX-1). The phone app is the
// same family as the web panel: its tokens are built on the web tokens
// (tokens.css), so both themes and the contrast checks of
// TestWebTokensContrast carry over; status is always a colour and a shape.

func readPhoneCSS(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("src/styles/phone.css")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// phoneTokens returns the custom properties of the first {…} block after
// the marker comment in phone.css.
func phoneTokens(t *testing.T, css, marker string) map[string]string {
	t.Helper()
	i := strings.Index(css, "/* "+marker)
	if i < 0 {
		t.Fatalf("phone.css has no %q block", marker)
	}
	rest := css[i:]
	open := strings.Index(rest, "{")
	if strings.HasPrefix(strings.TrimSpace(rest[strings.Index(rest, "*/")+2:]), "@media") {
		rest = rest[open+1:]
		open = strings.Index(rest, "{")
	}
	end := strings.Index(rest[open:], "}")
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`(--[a-z0-9-]+)\s*:\s*([^;]+);`).FindAllStringSubmatch(rest[open+1:open+end], -1) {
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

// phoneTokenNames are the phone tokens every phone view may use: colour
// roles, type scale, spacing, radius, elevation and motion.
var phoneTokenNames = []string{
	// colour roles
	"--p-bg", "--p-surface", "--p-surface-2", "--p-raised", "--p-text", "--p-text-2", "--p-muted",
	"--p-line", "--p-focus", "--p-accent", "--p-on-accent", "--p-ai", "--p-scrim",
	"--p-st-needs", "--p-st-working", "--p-st-review", "--p-st-done", "--p-st-failed", "--p-st-idle", "--p-st-asleep",
	// type
	"--p-font", "--p-mono", "--p-fs-caption", "--p-fs-meta", "--p-fs-body", "--p-fs-title", "--p-fs-head",
	"--p-lh-caption", "--p-lh-meta", "--p-lh-body", "--p-lh-title", "--p-lh-head",
	// space
	"--p-s1", "--p-s2", "--p-s3", "--p-s4", "--p-s6", "--p-gutter", "--p-gap",
	// radius
	"--p-r-chip", "--p-r-row", "--p-r-card", "--p-r-block", "--p-r-sheet",
	// elevation
	"--p-e0", "--p-e1", "--p-e2", "--p-e3",
	// motion
	"--p-t-tap", "--p-t-fast", "--p-t-sheet", "--p-t-arm", "--p-ease", "--p-ease-out",
	// targets and bars
	"--p-tap", "--p-bar-h", "--p-composer-h",
}

func TestPhoneTokensExist(t *testing.T) {
	tok := phoneTokens(t, readPhoneCSS(t), "phone:tokens")
	for _, k := range phoneTokenNames {
		if tok[k] == "" {
			t.Errorf("phone.css has no %s", k)
		}
	}
}

// Every phone colour is a web token, so dark, light, system light and
// more contrast all follow the web panel and its AA checks.
func TestPhoneColoursAreWebTokens(t *testing.T) {
	tok := phoneTokens(t, readPhoneCSS(t), "phone:tokens")
	dark, _, _ := readTokens(t)
	ref := regexp.MustCompile(`^var\((--[a-z0-9-]+)\)$`)
	for k, v := range tok {
		if !strings.HasPrefix(k, "--p-st-") && !colourRole[k] {
			continue
		}
		m := ref.FindStringSubmatch(v)
		if m == nil {
			t.Errorf("%s = %q; a phone colour is var(--web-token)", k, v)
			continue
		}
		if dark[m[1]] == "" {
			t.Errorf("%s uses %s, not a web colour token", k, m[1])
		}
	}
}

var colourRole = map[string]bool{"--p-bg": true, "--p-surface": true, "--p-surface-2": true, "--p-raised": true,
	"--p-text": true, "--p-text-2": true, "--p-muted": true, "--p-line": true, "--p-focus": true,
	"--p-accent": true, "--p-on-accent": true, "--p-ai": true, "--p-scrim": true}

// Touch targets are 44 px (2.75rem) or more; the arm delay of a risky
// choice is the web one (600 ms).
func TestPhoneTargetsAndArm(t *testing.T) {
	tok := phoneTokens(t, readPhoneCSS(t), "phone:tokens")
	if v := tok["--p-tap"]; v != "var(--tap)" {
		if n, err := strconv.ParseFloat(strings.TrimSuffix(v, "rem"), 64); err != nil || !strings.HasSuffix(v, "rem") || n < 2.75 {
			t.Errorf("--p-tap = %q; want var(--tap) or ≥ 2.75rem", v)
		}
	}
	if tok["--p-t-arm"] != "var(--t-arm)" {
		t.Errorf("--p-t-arm = %q; want var(--t-arm)", tok["--p-t-arm"])
	}
	css := readPhoneCSS(t)
	for _, cls := range []string{".ph-chip", ".ph-row", ".ph-tile", ".ph-composer-send", ".ph-block-tool", ".ph-sheet-act", ".ph-card"} {
		body := ruleBody(css, cls)
		if body == "" {
			t.Errorf("phone.css has no %s { … } rule", cls)
			continue
		}
		if !strings.Contains(body, "min-height: var(--p-tap)") {
			t.Errorf("%s has no min-height: var(--p-tap) (44 px target)", cls)
		}
	}
}

// ruleBody returns the declarations of the first rule whose selector is
// exactly sel.
func ruleBody(css, sel string) string {
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(sel) + `\s*\{([^}]*)\}`).FindStringSubmatch(css)
	if m == nil {
		return ""
	}
	return m[1]
}

// Reduced motion stops every phone animation.
func TestPhoneReducedMotion(t *testing.T) {
	tok := phoneTokens(t, readPhoneCSS(t), "phone:reduced-motion")
	for _, k := range []string{"--p-t-tap", "--p-t-fast", "--p-t-sheet"} {
		if tok[k] != "0ms" {
			t.Errorf("reduced motion: %s = %q, want 0ms", k, tok[k])
		}
	}
}

// statusRe reads one row of STATUS in src/phone/status.ts:
//
//	needs: { glyph: "●", tone: "needs", label: "ph.st.needs" },
var statusRe = regexp.MustCompile(`(?m)^\s*([a-z]+):\s*\{\s*glyph:\s*"([^"]+)",\s*tone:\s*"([a-z]+)",\s*label:\s*"([a-z0-9_.]+)"\s*\},?\s*$`)

func phoneStatuses(t *testing.T) [][]string {
	t.Helper()
	b, err := os.ReadFile("src/phone/status.ts")
	if err != nil {
		t.Fatal(err)
	}
	rows := statusRe.FindAllStringSubmatch(string(b), -1)
	if len(rows) == 0 {
		t.Fatal("src/phone/status.ts has no STATUS rows")
	}
	return rows
}

// Status is colour + shape: every status has its own glyph, a colour
// token, a CSS rule and a screen reader label in strings.ts.
func TestPhoneStatusColourAndShape(t *testing.T) {
	css := readPhoneCSS(t)
	tok := phoneTokens(t, css, "phone:tokens")
	keys := map[string]bool{}
	for _, e := range parseStrings(t, "src/strings.ts", "S") {
		keys[e.key] = true
	}
	glyphs := map[string]string{}
	want := map[string]bool{"needs": false, "working": false, "review": false, "done": false, "failed": false, "idle": false, "asleep": false}
	for _, r := range phoneStatuses(t) {
		name, glyph, tone, label := r[1], r[2], r[3], r[4]
		if _, ok := want[name]; ok {
			want[name] = true
		}
		if other, dup := glyphs[glyph]; dup {
			t.Errorf("%s and %s share the shape %q; each status needs its own", name, other, glyph)
		}
		glyphs[glyph] = name
		if tok["--p-st-"+tone] == "" {
			t.Errorf("%s: tone %q has no --p-st-%s", name, tone, tone)
		}
		if ruleBody(css, ".ph-st-"+tone) == "" {
			t.Errorf("%s: phone.css has no .ph-st-%s rule", name, tone)
		}
		if !keys[label] {
			t.Errorf("%s: label %q is not in strings.ts", name, label)
		}
	}
	for name, ok := range want {
		if !ok {
			t.Errorf("STATUS has no %s", name)
		}
	}
}

// phoneComponents are the base components and the file that exports each.
var phoneComponents = map[string]string{
	"StatusMark": "src/phone/Mark.tsx",
	"Block":      "src/phone/Block.tsx",
	"Row":        "src/phone/Row.tsx",
	"PhoneChip":  "src/phone/Chip.tsx",
	"PhoneSheet": "src/phone/Sheet.tsx",
	"PhoneCard":  "src/phone/Card.tsx",
	"Composer":   "src/phone/Composer.tsx",
	"BoardTile":  "src/phone/BoardTile.tsx",
}

// Each base component exists, is exported from src/phone/index.ts and is
// shown in the kit, in both themes.
func TestPhoneBaseComponents(t *testing.T) {
	idx, err := os.ReadFile("src/phone/index.ts")
	if err != nil {
		t.Fatal(err)
	}
	kit, err := os.ReadFile("src/kit/main.tsx")
	if err != nil {
		t.Fatal(err)
	}
	for name, file := range phoneComponents {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !regexp.MustCompile(`export function ` + name + `\b`).Match(b) {
			t.Errorf("%s does not export function %s", file, name)
		}
		if !regexp.MustCompile(`\b` + name + `\b`).Match(idx) {
			t.Errorf("src/phone/index.ts does not export %s", name)
		}
		if !strings.Contains(string(kit), "<"+name) {
			t.Errorf("the kit does not show <%s>", name)
		}
	}
	if !strings.Contains(string(kit), `"../styles/phone.css"`) {
		t.Error("the kit does not load phone.css")
	}
}

// The phone kit is not in the page bundle: app.js stays as it is, and
// the phone views come with their own lazy chunk (MOB-4). Only the kit
// and src/phone itself import it for now.
func TestPhoneKitNotInPageBundle(t *testing.T) {
	for _, f := range []string{"src/main.tsx", "src/app.tsx", "src/workbench.tsx"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "/phone") || strings.Contains(string(b), "phone.css") {
			t.Errorf("%s imports the phone kit; it belongs in the phone's lazy chunk", f)
		}
	}
}
