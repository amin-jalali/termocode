package assets

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Review in chat on the phone, the UX pass (MOBUX-11, on MOB-14's
// views/phone/Review.tsx in phone.js): swipe sideways between files, a
// long press on a line adds a note (Enter does it for a keyboard or a
// screen reader; a plain tap only scrolls), the note shows under its
// line and opens again to change or remove it, and "Send N notes to
// agent" sits in a bottom bar in the thumb zone.

func readRV(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("src/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// rvRule returns the declarations of the first rule whose selector is
// exactly sel.
func rvRule(css, sel string) string {
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(sel) + `\s*\{([^}]*)\}`).FindStringSubmatch(css)
	if m == nil {
		return ""
	}
	return m[1]
}

func TestPhoneReviewSwipesBetweenFiles(t *testing.T) {
	sw := readRV(t, "views/phone/swipe.ts")
	if !strings.Contains(sw, "export function useSwipe(") {
		t.Fatal("views/phone/swipe.ts has no useSwipe")
	}
	if !strings.Contains(sw, `pointerType === "mouse"`) {
		t.Error("useSwipe: a mouse never swipes (the ‹ › buttons are for it)")
	}
	rv := readRV(t, "views/phone/Review.tsx")
	if !strings.Contains(rv, "useSwipe(") {
		t.Error("Review.tsx: no swipe between files")
	}
	css := readRV(t, "styles/review.css")
	if !strings.Contains(rvRule(css, ".rv-body"), "touch-action: pan-y") {
		t.Error("review.css: the swipe area needs touch-action: pan-y")
	}
}

// A long press adds a note; a tap on a line does not (it is a scroll
// on a phone), but Enter or Space on a focused line does.
func TestPhoneReviewLongPressAddsANote(t *testing.T) {
	rv := readRV(t, "views/phone/Review.tsx")
	if !strings.Contains(rv, "useLongPress(") {
		t.Error("Review.tsx: a long press on a line must add a note")
	}
	if !strings.Contains(rv, "data-line=") {
		t.Error("Review.tsx: each line needs data-line for the long press")
	}
	if !strings.Contains(rv, "e.detail === 0") {
		t.Error("Review.tsx: only a keyboard click (detail 0) on a line opens the note, not a tap")
	}
	sp := readRV(t, "strings.phone.ts")
	for _, k := range []string{`"rv.line_add"`, `"rv.line_del"`, `"rv.line_ctx"`} {
		i := strings.Index(sp, k)
		if i < 0 || !strings.Contains(sp[i:i+strings.Index(sp[i:], "\n")], "Long-press") {
			t.Errorf("strings.phone.ts %s must say Long-press, not Tap", k)
		}
	}
}

// A note shows under its line; tapping it opens the sheet again with
// the text, to change or remove it. Cancel comes first.
func TestPhoneReviewNoteUnderItsLine(t *testing.T) {
	rv := readRV(t, "views/phone/Review.tsx")
	if !strings.Contains(rv, `class="rv-inline-note"`) {
		t.Error("Review.tsx: a note shows under its line (rv-inline-note)")
	}
	c, k := strings.Index(rv, `t("cancel")`), strings.Index(rv, `t("rv.note_keep")`)
	if c < 0 || k < 0 || c > k {
		t.Error("NoteSheet: Cancel comes first, then Keep note")
	}
	if !strings.Contains(rv, `t("rv.note_remove")`) {
		t.Error("NoteSheet: an open note can be removed")
	}
	notes := readRV(t, "views/phone/notes.ts")
	if !strings.Contains(notes, "export function noteAt(") || !strings.Contains(notes, "export function dropNote(") {
		t.Error("notes.ts needs noteAt and dropNote (by line, not by index)")
	}
}

// Send N notes to agent is a bottom bar in the thumb zone, with every
// target 44 px.
func TestPhoneReviewSendBarAtTheBottom(t *testing.T) {
	css := readRV(t, "styles/review.css")
	bar := rvRule(css, ".rv-send")
	if !strings.Contains(bar, "position: sticky") || !strings.Contains(bar, "bottom: 0") {
		t.Error("review.css: .rv-send is a sticky bar at the bottom")
	}
	for _, sel := range []string{".rv-inline-note", ".rv-send-btn"} {
		if !strings.Contains(rvRule(css, sel), "min-height: 44px") {
			t.Errorf("review.css: %s needs min-height: 44px", sel)
		}
	}
	rv := readRV(t, "views/phone/Review.tsx")
	if !strings.Contains(rv, `t("rv.offline")`) {
		t.Error("SendBar: offline it says the notes wait here")
	}
}

// The review stays in phone.js; app.js never imports it.
func TestPhoneReviewNotInPageBundle(t *testing.T) {
	for _, f := range []string{"src/main.tsx", "src/app.tsx", "src/workbench.tsx"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "views/phone/") {
			t.Errorf("%s imports a phone view; it belongs in phone.js", f)
		}
	}
}
