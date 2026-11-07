package assets

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Agent races on the phone (MOB-62): the race block's buttons and the
// start form call the laptop's race ops by their real names, keep the
// safe choice first, use 44 px targets, stay English only and stay out
// of app.js (they come with the phone's lazy chunk).

func readRaceSrc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("src/phone/races/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPhoneRaceOpsAreRealOps(t *testing.T) {
	src := readRaceSrc(t, "raceops.ts")
	want := map[string]string{"start": webproto.OpAIRaceStart, "summary": webproto.OpAIRaceSummary, "verify": webproto.OpAIRaceVerify, "stop": webproto.OpAIRaceStop}
	for k, op := range want {
		if !regexp.MustCompile(`\b` + k + `: "` + regexp.QuoteMeta(op) + `"`).MatchString(src) {
			t.Errorf("raceops.ts: %s is not %q", k, op)
		}
		if _, ok := webproto.Lookup(op); !ok {
			t.Errorf("%s is not an op", op)
		}
	}
	for name, n := range map[string]int{"RACE_TASK_MAX": webproto.MaxAIRaceTask, "RACE_VERIFY_MAX": webproto.MaxAIRaceVerify,
		"RACE_MIN_LANES": webproto.MinAIRaceLanes, "RACE_MAX_LANES": webproto.MaxAIRaceLanes} {
		if !strings.Contains(src, "export const "+name+" = "+strconv.Itoa(n)+";") {
			t.Errorf("raceops.ts: %s is not %d", name, n)
		}
	}
}

// Stop lanes: a sheet with Cancel first and focused, Stop armed; Start
// goes through the laptop card (AskLaptop).
func TestPhoneRaceSafeChoiceFirst(t *testing.T) {
	acts := readRaceSrc(t, "RaceActions.tsx")
	c, s := strings.Index(acts, `{t("cancel")}`), strings.Index(acts, `<span class="arm-label">{rs("race.stop")}`)
	if c < 0 || s < 0 || c > s || !strings.Contains(acts, "useArming(") || !strings.Contains(acts, "safe.current?.focus()") {
		t.Error("RaceActions.tsx: the stop sheet does not put an armed Stop after a focused Cancel")
	}
	if start := readRaceSrc(t, "RaceStart.tsx"); !strings.Contains(start, "<AskLaptop") || !strings.Contains(start, "start(link, ask)") {
		t.Error("RaceStart.tsx: a race starts without the laptop card")
	}
}

func TestPhoneRaceTargetsAndEnglishOnly(t *testing.T) {
	b, err := os.ReadFile("src/styles/race.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	for _, sel := range []string{".race-btn {", ".race-field select {"} {
		i := strings.Index(css, sel)
		if i < 0 {
			i = strings.Index(css, strings.TrimSuffix(sel, " {"))
		}
		if i < 0 || !strings.Contains(css[i:], "min-height: 44px") {
			t.Errorf("race.css: %s is not 44 px", sel)
		}
	}
	fa, _ := os.ReadFile("src/strings.fa.ts")
	if strings.Contains(string(fa), `"race.`) {
		t.Error("the phone race strings are English only")
	}
	main, _ := os.ReadFile("src/main.tsx")
	if strings.Contains(string(main), "phone/races") {
		t.Error("app.js imports the phone race views")
	}
}
