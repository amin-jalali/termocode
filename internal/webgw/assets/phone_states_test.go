package assets

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Phone states everywhere (MOBUX-21): loading skeletons, the offline bar
// ("Offline — answers are not saved", answers greyed), the laptop asleep
// with its last seen time, and an error line with Retry.

func readSrc(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// phoneStateExports are the state parts and the file that exports each.
var phoneStateExports = map[string][]string{
	"src/phone/conn.ts":    {"phoneConn", "canAnswer", "lastSeen", "phoneOnline", "ASLEEP_AFTER_MS", "SHOW_AFTER_MS"},
	"src/phone/States.tsx": {"PhoneSkeleton", "ConnLine", "ErrorLine"},
	"src/phone/live.tsx":   {"startPhoneConn", "ConnBar", "ConnScope"},
}

func TestPhoneStatesExported(t *testing.T) {
	idx := readSrc(t, "src/phone/index.ts")
	for file, names := range phoneStateExports {
		src := readSrc(t, file)
		for _, name := range names {
			if !regexp.MustCompile(`export (function|const|let) ` + name + `\b`).MatchString(src) {
				t.Errorf("%s does not export %s", file, name)
			}
			if !regexp.MustCompile(`\b` + name + `\b`).MatchString(idx) {
				t.Errorf("src/phone/index.ts does not export %s", name)
			}
		}
	}
}

// The words are the owner's: the offline bar says answers are not saved,
// the asleep bar gives the last seen time, the error line has Retry.
func TestPhoneStatesStrings(t *testing.T) {
	en := map[string]string{}
	for _, e := range parseStrings(t, "src/strings.ts", "S") {
		en[e.key] = e.text
	}
	want := map[string]string{
		"ph.conn_offline": "Offline — answers are not saved",
		"ph.retry":   "Retry",
	}
	for k, v := range want {
		if en[k] != v {
			t.Errorf("%s = %q, want %q", k, en[k], v)
		}
	}
	for _, k := range []string{"ph.conn_asleep", "ph.asleep_day"} {
		if !strings.Contains(en[k], "{time}") {
			t.Errorf("%s = %q; it needs the last seen {time}", k, en[k])
		}
	}
	for _, k := range []string{"ph.offline_sr", "ph.asleep_sr", "ph.loading"} {
		if en[k] == "" {
			t.Errorf("strings.ts has no %s", k)
		}
	}
}

// A laptop that stays out of reach while the phone has a network is
// asleep after 30 s; the bar shows after 1 s, as on the web panel.
func TestPhoneConnTimes(t *testing.T) {
	src := readSrc(t, "src/phone/conn.ts")
	for _, want := range []string{"ASLEEP_AFTER_MS = 30_000", "SHOW_AFTER_MS = 1000"} {
		if !strings.Contains(src, want) {
			t.Errorf("conn.ts has no %q", want)
		}
	}
	// Only a live link with a network can take an answer.
	if !regexp.MustCompile(`export function canAnswer\([^)]*\)[^{]*\{\s*return c === "online";`).MatchString(src) {
		t.Error(`canAnswer must be true only for "online"`)
	}
}

// Every state has a colour and a shape, and every control is a 44 px target.
func TestPhoneStatesCSS(t *testing.T) {
	css := readPhoneCSS(t)
	for _, sel := range []string{".ph-conn", ".ph-conn-offline", ".ph-conn-asleep", ".ph-conn-final", ".ph-conn-mark", ".ph-err", ".ph-err-mark", ".ph-sk", ".ph-sk-line", ".ph-sk-block", ".ph-sk-card", ".ph-sk-tile"} {
		if ruleBody(css, sel) == "" {
			t.Errorf("phone.css has no %s { … } rule", sel)
		}
	}
	for _, sel := range []string{".ph-err-retry", ".ph-conn-act"} {
		if !strings.Contains(ruleBody(css, sel), "min-height: var(--p-tap)") {
			t.Errorf("%s has no min-height: var(--p-tap) (44 px target)", sel)
		}
	}
	// Offline, answers are greyed and take no taps, everywhere in the scope.
	if !strings.Contains(css, ".ph-scope.is-offline .ph-answer") {
		t.Error("phone.css does not grey .ph-answer inside an offline .ph-scope")
	}
	// The skeleton shimmer stops for reduced motion.
	if !regexp.MustCompile(`(?s)prefers-reduced-motion: reduce\)\s*\{[^@]*\.ph-sk[^{]*\{[^}]*animation: none`).MatchString(css) {
		t.Error("reduced motion does not stop the .ph-sk animation")
	}
}

// The base components grey their answers offline on their own: Send in
// the composer, and every sheet action that is not local (Cancel).
func TestPhoneAnswersGreyOffline(t *testing.T) {
	for _, f := range []string{"src/phone/Composer.tsx", "src/phone/Sheet.tsx"} {
		if !strings.Contains(readSrc(t, f), "phoneOnline.value") {
			t.Errorf("%s does not read phoneOnline", f)
		}
	}
	if !regexp.MustCompile(`local\?: boolean`).MatchString(readSrc(t, "src/phone/Sheet.tsx")) {
		t.Error("SheetAction has no local flag for actions that work offline")
	}
}

// The error line is an alert with a shape, words and Retry; the bar's
// changing text is not read again and again by a screen reader.
func TestPhoneStatesA11y(t *testing.T) {
	s := readSrc(t, "src/phone/States.tsx")
	for _, want := range []string{`role="alert"`, `t("ph.retry")`, `aria-hidden="true"`, `aria-busy="true"`, `role="status"`} {
		if !strings.Contains(s, want) {
			t.Errorf("States.tsx has no %s", want)
		}
	}
}

// The kit shows every state; it never imports the live link (the kit has
// no session).
func TestPhoneStatesInKit(t *testing.T) {
	kit := readSrc(t, "src/kit/main.tsx")
	for _, c := range []string{"<PhoneSkeleton", "<ConnLine", "<ErrorLine"} {
		if !strings.Contains(kit, c) {
			t.Errorf("the kit does not show %s", c)
		}
	}
	if strings.Contains(kit, "phone/live") {
		t.Error("the kit imports phone/live (the live link)")
	}
	for _, f := range []string{"src/phone/conn.ts", "src/phone/States.tsx", "src/phone/Composer.tsx", "src/phone/Sheet.tsx"} {
		if strings.Contains(readSrc(t, f), "../session") {
			t.Errorf("%s imports the session; only phone/live.tsx may", f)
		}
	}
}
