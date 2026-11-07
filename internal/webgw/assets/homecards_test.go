package assets

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Cards inline in the phone's Home (MOB-6), checked in the source next to
// the browser tests (e2e/web/homecards_test.go): the inline cards are the
// kit's Card in inline mode with the sheet's rules, which live once in
// state/cards.ts and are used by both the sheet and the Home.
func TestHomeCardsUseTheSheetRules(t *testing.T) {
	b, err := os.ReadFile("src/home/cards.ts")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		`mode: "inline"`, // the kit's Card, inline: it never takes the focus
		`cardState(`,     // the sheet's open / answered / offline / gone / long rules
		`answerArgs(`,    // the sheet's cards.answer arguments
		`"cards.answer"`, // one request per answer
		`host.openCard(`, // details open the sheet at #/home?card=<id>
	} {
		if !strings.Contains(src, want) {
			t.Errorf("home/cards.ts lost %q", want)
		}
	}
	if regexp.MustCompile(`(?m)^import\s+(?:\{[^}]*\}|\w+)\s+from\s+"(?:preact|@preact/signals|\.\./session|\.\./strings|\.\./ui/[^"]+)"`).MatchString(src) {
		t.Error("home/cards.ts imports page code; the Home gets it from its host")
	}
	sheet, err := os.ReadFile("src/ui/CardSheet.tsx")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`cardState(`, `answerArgs(`} {
		if !strings.Contains(string(sheet), want) {
			t.Errorf("CardSheet.tsx does not use %s: the sheet and the Home must share one rule set", want)
		}
	}
	// One buzz per answer: the kit's Card gives it on the press, so the
	// sheet and the Home must not buzz again when the hub answers.
	for name, s := range map[string]string{"home/cards.ts": src, "ui/CardSheet.tsx": string(sheet)} {
		if strings.Contains(s, "haptic(") {
			t.Errorf("%s buzzes too; the Card already does (two buzzes per answer)", name)
		}
	}
}

// A review card with a diff never shows Accept in the stream: its inline
// state is "peek", whose only other button opens the sheet with the diff.
func TestInlineReviewIsPeekOnly(t *testing.T) {
	b, err := os.ReadFile("src/state/cards.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`inline[^\n]*&&[^\n]*diff[^\n]*\n?[^\n]*"peek"`).MatchString(string(b)) {
		t.Error(`state/cards.ts: an inline card with a diff is not "peek"`)
	}
	card, err := os.ReadFile("src/ui/Card.tsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(card), `state === "long" || state === "peek"`) {
		t.Error(`Card.tsx: a "peek" card does not swap the risky button for the open button`)
	}
}

// #/home?card=<id> keeps the Home under the sheet; every other tab keeps
// #/card/<id>.
func TestRouteCardInHomeQuery(t *testing.T) {
	b, err := os.ReadFile("src/state/route.ts")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{`CARD_IN_QUERY`, `"?card="`, `query.card`} {
		if !strings.Contains(src, want) {
			t.Errorf("route.ts lost %q", want)
		}
	}
}

// The haptic helper uses the app's bridge when there is one, else the
// browser's vibrate, and never throws.
func TestHapticHelper(t *testing.T) {
	b, err := os.ReadFile("src/lib/haptic.ts")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{`Haptics`, `navigator.vibrate`, `try {`} {
		if !strings.Contains(src, want) {
			t.Errorf("lib/haptic.ts lost %q", want)
		}
	}
}
