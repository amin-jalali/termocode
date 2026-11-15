package assets

import (
	"regexp"
	"strings"
	"testing"
)

// The Home's empty state (MOBUX-4, design sheet 32): when the stream is
// empty the Home shows one summary line, "My work" as one grouped card
// (needs you · working · done today, a row per non-empty state with its
// shape tile, the newest title and its count), "Shortcuts" with live
// counts and the chip strip, then the pinned views. A quiet day is one
// line and chips that only fill the box. A row opens the list behind it
// through a chip (no tokens); a long press offers it to the AI. The
// counts come from the inbox topic (cards while the laptop has none),
// done counts today only. All of it lives in home.js.

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
	// My work in board order: what needs the user first.
	order := []string{`"needs"`, `"working"`, `"done"`}
	at := -1
	for _, k := range order {
		i := strings.Index(start, "card: "+k)
		if i < 0 {
			t.Fatalf("Start.tsx has no row card: %s", k)
		}
		if i < at {
			t.Errorf("card: %s comes before the row above it: needs you, working, done today", k)
		}
		at = i
	}
	for _, k := range []string{`"start.mywork"`, `"start.shortcuts"`, `"start.quiet"`, "openActions(", "onContextMenu", `class="home-tile`} {
		if !strings.Contains(start, k) {
			t.Errorf("Start.tsx has no %s", k)
		}
	}
	// The pinned views come below the sections.
	secs, pins := strings.Index(start, `"start.shortcuts"`), strings.Index(start, "<PinnedViews")
	if secs < 0 || pins < 0 || pins < secs {
		t.Error("the pinned views are not below the sections")
	}
	// The summary line is short: no more than 60 characters.
	m := regexp.MustCompile(`"start\.hello": "([^"]*)"`).FindStringSubmatch(strs)
	if m == nil {
		t.Fatal(`src/home/strings.ts has no "start.hello"`)
	}
	if n := len([]rune(m[1])); n == 0 || n > 60 {
		t.Errorf("the hello is %d characters; keep it short (≤ 60)", n)
	}
	for _, k := range []string{"start.needs", "start.working", "start.done", "start.mywork", "start.shortcuts", "start.quiet", "start.quiet_hint"} {
		if !strings.Contains(strs, `"`+k+`"`) {
			t.Errorf("src/home/strings.ts has no %q", k)
		}
	}
	if regexp.MustCompile(`[\x{0600}-\x{06FF}]`).MatchString(start + strs) {
		t.Error("the Home's empty state carries non-English text: the phone app is English only")
	}

	// The Home shows it when the stream is empty, and hears saved views;
	// the chip strip sits in Shortcuts then, under the box once it is not.
	if !regexp.MustCompile(`stream\.length === 0 && \(?\s*<HomeStart`).MatchString(home) {
		t.Error("Home.tsx does not show <HomeStart> when the stream is empty")
	}
	if !strings.Contains(home, "startSaved()") {
		t.Error("Home.tsx does not start the saved views (pinned views stay empty)")
	}
	if !strings.Contains(home, "onCard=") || !regexp.MustCompile(`stream\.length > 0 && <Chips`).MatchString(home) {
		t.Error("Home.tsx does not hand the rows a tap handler or keeps the strip under the box on an empty Home")
	}

	// 44 px rows and a 28 px colour tile per state.
	if !regexp.MustCompile(`\.home-tile \{[^}]*width: 28px`).MatchString(css) {
		t.Error("home.css: no 28 px tile")
	}
	for _, c := range []string{".home-sec", ".home-quiet", ".home-row"} {
		if !strings.Contains(css, c) {
			t.Errorf("home.css has no %s", c)
		}
	}
}
