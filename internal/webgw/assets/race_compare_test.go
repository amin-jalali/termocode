package assets

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Race compare on the phone (MOBUX-10): one card per lane that swipes
// sideways, a small table on top (files, tests, cost, judge ★), and a
// Merge that only opens a confirm card (Cancel first, Merge arms after
// 600 ms, the merge itself runs on the laptop after its card).

func raceSrc(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile("src/" + rel)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The pure helpers carry the table's columns and the judge.
func TestRaceComparePureHelpers(t *testing.T) {
	src := raceSrc(t, "phone/race.ts")
	for _, want := range []string{
		"export function readRaces(", "export function raceRows(", "export function judgeLane(",
		"export function laneStatus(", "export function canMerge(", "export function costText(",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("phone/race.ts has no %s", want)
		}
	}
	// Pure: no Preact, no session, no DOM.
	for _, bad := range []string{`from "preact`, `from "../session"`, "document.", "window."} {
		if strings.Contains(src, bad) {
			t.Errorf("phone/race.ts is not pure: it uses %s", bad)
		}
	}
	// A lane is merged only when it is finished and not closed.
	if !regexp.MustCompile(`(?s)function canMerge\(.*?(done|Finished|finished).*?\}`).MatchString(src) {
		t.Error("canMerge does not check that the lane is finished")
	}
}

// The view: a table on top, a sideways strip of lane cards, a ★ for the
// judge's pick, and Merge through a confirm sheet with the safe choice first.
func TestRaceCompareView(t *testing.T) {
	src := raceSrc(t, "phone/RaceCompare.tsx")
	for _, want := range []string{
		`class="ph-race-table"`, `class="ph-race-lanes"`, `class="ph-race-lane`, "★",
		"PhoneSheet", "safe: true", "risky: true", "onMerge(",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("phone/RaceCompare.tsx has no %s", want)
		}
	}
	// It never runs an op itself: the host wires Merge to the laptop card.
	for _, bad := range []string{`from "../session"`, "link.request"} {
		if strings.Contains(src, bad) {
			t.Errorf("phone/RaceCompare.tsx calls the link itself (%s); the host does", bad)
		}
	}
	idx := raceSrc(t, "phone/index.ts")
	if !strings.Contains(idx, "RaceCompare") {
		t.Error("phone/index.ts does not export RaceCompare")
	}
}

// The lane strip swipes sideways and snaps to one card; every tap target
// is 44 px.
func TestRaceCompareStyles(t *testing.T) {
	css := readPhoneCSS(t)
	strip := ruleBody(css, ".ph-race-lanes")
	if !strings.Contains(strip, "overflow-x: auto") || !strings.Contains(strip, "scroll-snap-type: x mandatory") {
		t.Errorf(".ph-race-lanes = %q; want a sideways strip that snaps", strip)
	}
	if !strings.Contains(ruleBody(css, ".ph-race-lane"), "scroll-snap-align") {
		t.Error(".ph-race-lane does not snap")
	}
	for _, cls := range []string{".ph-race-row", ".ph-race-act", ".ph-race-dot"} {
		if !strings.Contains(ruleBody(css, cls), "var(--p-tap)") {
			t.Errorf("%s is not a 44 px target (var(--p-tap))", cls)
		}
	}
}

// Its words are English only (the phone app is English only).
func TestRaceCompareStrings(t *testing.T) {
	en := raceSrc(t, "strings.ts")
	for _, k := range []string{"ph.race.files", "ph.race.tests", "ph.race.cost", "ph.race.judge", "ph.race.merge", "ph.race.merge_ask", "ph.race.merge_note"} {
		if !strings.Contains(en, `"`+k+`"`) {
			t.Errorf("strings.ts has no %s", k)
		}
	}
	if strings.Contains(raceSrc(t, "strings.fa.ts"), "ph.race.") {
		t.Error("strings.fa.ts has phone race strings; the phone app is English only")
	}
}
