package assets

import (
	"regexp"
	"strings"
	"testing"
)

// The Home's empty state (MOBUX-4): when the stream is empty the Home
// shows a short hello, three cards (needs you · working · done today)
// and the pinned views below them. Each card is one 44 px button, says
// its state with a shape and a word (never colour alone), and opens the
// list behind it through a chip (no tokens). The counts come from the
// inbox topic (cards while the laptop has none), done counts today only.
// All of it lives in home.js: the phone's first files do not grow.

func TestHomeStartSummary(t *testing.T) {
	st := readSrc(t, "src/state/home.ts")
	if !regexp.MustCompile(`export function homeSummary\(`).MatchString(st) {
		t.Fatal("state/home.ts does not export homeSummary")
	}
	for _, want := range []string{
		"export interface HomeSummary",
		"export function startOfDay(",
		`x.state === "needs_you"`,
		`x.state === "working"`,
		`x.state === "done"`,
		"x.since >= day", // done (and failed) count today only
	} {
		if !strings.Contains(st, want) {
			t.Errorf("state/home.ts: homeSummary lost %q", want)
		}
	}
	// Without an inbox topic the waiting cards still count.
	if !regexp.MustCompile(`inbox\s*\?[^:]*:\s*cards\.length`).MatchString(st) {
		t.Error("homeSummary does not fall back to the waiting cards without an inbox")
	}
}

func TestHomeStartCards(t *testing.T) {
	start := readSrc(t, "src/home/Start.tsx")
	home := readSrc(t, "src/home/Home.tsx")
	strs := readSrc(t, "src/home/strings.ts")
	css := readSrc(t, "src/styles/home.css")

	if !strings.Contains(start, "export function HomeStart(") {
		t.Fatal("src/home/Start.tsx does not export HomeStart")
	}
	// Three cards, in board order: what needs the user first.
	order := []string{`"needs"`, `"working"`, `"done"`}
	at := -1
	for _, k := range order {
		i := strings.Index(start, "card="+k)
		if i < 0 {
			t.Fatalf("Start.tsx has no card=%s", k)
		}
		if i < at {
			t.Errorf("card=%s comes before the card above it: needs you, working, done today", k)
		}
		at = i
	}
	// Status is a shape and a word.
	for _, g := range []string{`"●"`, `"◐"`, `"✓"`} {
		if !strings.Contains(start, g) {
			t.Errorf("Start.tsx has no shape %s", g)
		}
	}
	if !strings.Contains(start, `<button`) || !strings.Contains(start, `class={"home-card`) || !strings.Contains(start, "aria-label=") {
		t.Error("a Home card is not one labelled button")
	}
	// The pinned views come below the cards.
	cards, pins := strings.Index(start, `class="home-cards"`), strings.Index(start, "<PinnedViews")
	if cards < 0 || pins < 0 || pins < cards {
		t.Error("the pinned views are not below the three cards")
	}
	// The hello is short: one line, no more than 60 characters.
	m := regexp.MustCompile(`"start\.hello": "([^"]*)"`).FindStringSubmatch(strs)
	if m == nil {
		t.Fatal(`src/home/strings.ts has no "start.hello"`)
	}
	if n := len([]rune(m[1])); n == 0 || n > 60 {
		t.Errorf("the hello is %d characters; keep it short (≤ 60)", n)
	}
	for _, k := range []string{"start.needs", "start.working", "start.done", "start.card"} {
		if !strings.Contains(strs, `"`+k+`"`) {
			t.Errorf("src/home/strings.ts has no %q", k)
		}
	}
	if regexp.MustCompile(`[\x{0600}-\x{06FF}]`).MatchString(start + strs) {
		t.Error("the Home's empty state carries non-English text: the phone app is English only")
	}

	// The Home shows it when the stream is empty, and hears saved views.
	if !regexp.MustCompile(`stream\.length === 0 && \(?\s*<HomeStart`).MatchString(home) {
		t.Error("Home.tsx does not show <HomeStart> when the stream is empty")
	}
	if !strings.Contains(home, "startSaved()") {
		t.Error("Home.tsx does not start the saved views (pinned views stay empty)")
	}
	// A tap opens the list behind the card through a chip: no tokens.
	if !strings.Contains(home, "onCard=") {
		t.Error("Home.tsx does not hand the cards a tap handler")
	}

	// 44 px targets, and a shape colour per state.
	if !regexp.MustCompile(`\.home-card \{[^}]*min-height: 44px`).MatchString(css) {
		t.Error("home.css: a Home card is not a 44 px target")
	}
	for _, c := range []string{".home-card.is-needs", ".home-card.is-working", ".home-card.is-done"} {
		if !strings.Contains(css, c) {
			t.Errorf("home.css has no %s", c)
		}
	}
}
