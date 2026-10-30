package assets

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Safe-first cards (rule AH-1, web UX P1), checked in the source until the
// browser tests of WEB-UX-3 run them: in every choice row of Card.tsx the
// first button is the safe one (it holds the focus ref), the risky button
// goes through the arming gate, Esc maps to Later, and nothing anywhere
// offers "Always allow".
func TestCardPutsTheSafeChoiceFirst(t *testing.T) {
	b, err := os.ReadFile("src/ui/Card.tsx")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	rows := regexp.MustCompile(`(?s)<div class="choices">(.*?)</div>`).FindAllStringSubmatch(src, -1)
	if len(rows) < 2 {
		t.Fatalf("found %d choice rows, want the trust row and the answer row", len(rows))
	}
	for i, r := range rows {
		first := strings.Index(r[1], "<button")
		if first < 0 {
			t.Fatalf("choice row %d has no button", i)
		}
		head := r[1][first:]
		if end := strings.Index(head, ">"); end < 0 || !strings.Contains(head[:end], "ref={safeBtn}") {
			t.Errorf("choice row %d: the first button is not the safe one (no ref={safeBtn})", i)
		}
	}
	for _, want := range []string{
		`if (done || !arm.pressOK()) return;`, // risky clicks pass the arming gate
		`if (e.key === "Escape")`,             // Esc …
		`onLater?.();`,                        // … is Later
		`safeBtn.current?.focus()`,            // the safe button gets the focus
	} {
		if !strings.Contains(src, want) {
			t.Errorf("Card.tsx lost %q", want)
		}
	}
	if strings.Contains(src, `onAnswer?.("risky")`) && strings.Count(src, `onAnswer?.("risky")`) != 1 {
		t.Error("the risky answer is sent from more than one place")
	}
}

func TestNoAlwaysAllowAnywhere(t *testing.T) {
	re := regexp.MustCompile(`(?i)always[ _.-]*allow|allow[ _.-]*always`)
	for _, f := range []string{"src/strings.ts", "src/ui/Card.tsx"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		code := regexp.MustCompile(`(?m)//.*$`).ReplaceAllString(string(b), "") // comments may say it is never offered
		if m := re.FindString(code); m != "" {
			t.Errorf("%s offers %q; the web never may (never-do 21)", f, m)
		}
	}
}
