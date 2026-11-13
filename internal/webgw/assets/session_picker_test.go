package assets

import (
	"os"
	"strings"
	"testing"
)

// WEB-55: after a passkey (or bookmark) sign-in the page shows the
// owner's running sessions and lets them pick one. Checked in the source
// until the e2e runs the page.

func pickerSrc(t *testing.T, f string) string {
	t.Helper()
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("%s: %v", f, err)
	}
	return string(b)
}

// Only running sessions are rows (here and running, never asleep or
// opening: the page cannot create one), each with name, branch and last
// activity; the newest and the ones that need you come first.
func TestSessionPickerRowsAreRunningOnly(t *testing.T) {
	src := pickerSrc(t, "src/lib/sessions.ts")
	for _, want := range []string{
		`p.state === "here" || p.state === "running"`,
		`branch`,
		`openedAt`,
		`export function pickerRows`,
		`export function ago`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("sessions.ts lost %q", want)
		}
	}
	if strings.Contains(src, "project.open") || strings.Contains(src, `"asleep"`) {
		t.Error("the picker must never open or list a sleeping project")
	}
}

// The picker is its own step after the passkey or bookmark sign-in; a
// launch link and a pairing keep going straight to the workspace; a
// single running session skips the picker.
func TestSessionPickerStepAfterSignIn(t *testing.T) {
	main := pickerSrc(t, "src/main.tsx")
	for _, want := range []string{
		`"pick"`,
		`<SignInScreen onDone={startPick} />`,
		`signInWithBookmark(startPick)`,
		`<PairScreen secret={pairSecret ?? ""} onDone={startShell} />`,
		`<SessionPicker onPick={pickSession} />`,
	} {
		if !strings.Contains(main, want) {
			t.Errorf("main.tsx lost %q", want)
		}
	}
	pk := pickerSrc(t, "src/ui/SessionPicker.tsx")
	for _, want := range []string{
		`link.subscribe("projects")`,
		`rows.length === 1`,
		`<ul class="sp-list"`,
		`t("sp.title")`,
		`t("sp.here")`,
	} {
		if !strings.Contains(pk, want) {
			t.Errorf("SessionPicker.tsx lost %q", want)
		}
	}
	if strings.Contains(pk, "project.open") {
		t.Error("the picker creates no session")
	}
}

// A pick of another session opens that project's page; on a desktop it
// stands on its own, since the workbench shows only this session.
func TestSessionPickerOtherSessionOnDesktop(t *testing.T) {
	app := pickerSrc(t, "src/app.tsx")
	for _, want := range []string{`OtherSession`, `projects/`, `t("sp.back")`} {
		if !strings.Contains(app, want) {
			t.Errorf("app.tsx lost %q", want)
		}
	}
}
