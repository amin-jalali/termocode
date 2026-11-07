package assets

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The phone app shell (MOBUX-2): a top bar (project switcher ▾, live dot,
// ☰), the chat stream, and a bottom dock in the thumb zone with the chip
// row above the composer (attach chips, 🎤, Send). The shell draws only
// what its host hands it, so the Home chunk (MOB-4) and the kit drive the
// same component.

var shellExports = []string{"PhoneShell", "TopBar", "Stream", "ProjectSwitcher"}

func readSrc(t *testing.T, f string) string {
	t.Helper()
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPhoneShellComponents(t *testing.T) {
	shell := readSrc(t, "src/phone/Shell.tsx")
	idx := readSrc(t, "src/phone/index.ts")
	kit := readSrc(t, "src/kit/main.tsx")
	for _, name := range shellExports {
		if !regexp.MustCompile(`export function ` + name + `\b`).MatchString(shell) {
			t.Errorf("src/phone/Shell.tsx does not export function %s", name)
		}
		if !regexp.MustCompile(`\b` + name + `\b`).MatchString(idx) {
			t.Errorf("src/phone/index.ts does not export %s", name)
		}
	}
	if !strings.Contains(kit, "<PhoneShell") {
		t.Error("the kit does not show <PhoneShell>")
	}
}

// The shell is drawn from its props: no session, link, store or router
// of its own, so a second copy of the page's state cannot start in it.
func TestPhoneShellHasNoState(t *testing.T) {
	shell := readSrc(t, "src/phone/Shell.tsx")
	for _, bad := range []string{`"../session"`, `"../net/`, `"../lib/router"`, `"../state/store"`, "localStorage"} {
		if strings.Contains(shell, bad) {
			t.Errorf("src/phone/Shell.tsx uses %s; the host hands the shell its data", bad)
		}
	}
}

// Every control of the shell is a 44 px target; the dock keeps clear of
// the home indicator; the shell fills the small viewport (keyboard up).
func TestPhoneShellTargetsAndDock(t *testing.T) {
	css := readPhoneCSS(t)
	for _, cls := range []string{".ph-top-btn", ".ph-switch", ".ph-composer-mic", ".ph-composer-attach", ".ph-switch-row"} {
		body := ruleBody(css, cls)
		if body == "" {
			t.Errorf("phone.css has no %s { … } rule", cls)
			continue
		}
		if !strings.Contains(body, "min-height: var(--p-tap)") {
			t.Errorf("%s has no min-height: var(--p-tap) (44 px target)", cls)
		}
	}
	if body := ruleBody(css, ".ph-shell"); !strings.Contains(body, "100dvh") {
		t.Error(".ph-shell does not fill the dynamic viewport (100dvh)")
	}
	if body := ruleBody(css, ".ph-top"); !strings.Contains(body, "safe-area-inset-top") || !strings.Contains(body, "min-height: var(--p-bar-h)") {
		t.Error(".ph-top does not keep clear of the notch or is not --p-bar-h tall")
	}
	if body := ruleBody(css, ".ph-stream"); !strings.Contains(body, "overflow-y: auto") || !strings.Contains(body, "overscroll-behavior") {
		t.Error(".ph-stream does not scroll on its own (overflow-y: auto, overscroll-behavior)")
	}
}

// The shell's words are English only (owner 2026-11-07): each key is in
// strings.ts and none is in strings.fa.ts.
var shellStrings = []string{"ph.switch", "ph.switch_title", "ph.menu", "ph.live", "ph.offline", "ph.asleep", "ph.mic", "ph.mic_hint", "ph.attach", "ph.stream", "ph.current"}

func TestPhoneShellStringsEnglishOnly(t *testing.T) {
	en := map[string]bool{}
	for _, e := range parseStrings(t, "src/strings.ts", "S") {
		en[e.key] = true
	}
	fa := readSrc(t, "src/strings.fa.ts")
	for _, k := range shellStrings {
		if !en[k] {
			t.Errorf("strings.ts has no %q", k)
		}
		if strings.Contains(fa, `"`+k+`"`) {
			t.Errorf("strings.fa.ts has %q: the phone app is English only", k)
		}
	}
}
