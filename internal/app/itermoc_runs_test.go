package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Spec: on a profile's page in Itermoc, the "See runs →" line, the Runs
// (r) pill and the r key open Runs filtered to that profile.
func TestItermocProfileOpensRunsFiltered(t *testing.T) {
	row := imRow{kind: imRowProfile, sec: imSecProfiles, name: "review"}
	for _, how := range []string{"line", "pill", "key"} {
		m := itermocTestModel(t, 70, 40)
		m.itermocShowPage(row, true)
		switch how {
		case "line", "pill": // both are the page hit "runs"
			m.itermocPageVerb("runs", row)
		case "key":
			nm, _, ok := m.routeToItermocPage(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
			if !ok {
				t.Fatalf("%s: r not taken", how)
			}
			*m = nm
		}
		if m.activity.Active() != activity.ViewRuns {
			t.Errorf("%s: view %v, want Runs", how, m.activity.Active())
		}
		if m.runs == nil || m.runs.profile != "review" {
			t.Errorf("%s: Runs not filtered to review", how)
		}
	}
}

// Spec: in Runs, "Profile in Itermoc" (i, and the more list) opens Itermoc
// on the run's profile and shows its page.
func TestRunsProfileInItermoc(t *testing.T) {
	m := runsRig(t, "list", 200, 50)
	it := runsItemByTask(t, m, "fix the double quit confirm")
	m.runs.detail = it.key
	page := runsPageText(m, 165, 48)
	if !strings.Contains(page, "Profile in Itermoc") || strings.Contains(page, "Show profile") {
		t.Errorf("run page does not offer Profile in Itermoc:\n%s", page)
	}
	m.runsMore(it)
	if box := ansi.Strip(m.picker.Box()); !strings.Contains(box, "Profile in Itermoc: "+it.run.Profile) {
		t.Errorf("more list has no Profile in Itermoc:\n%s", box)
	}
	m.pickerOpen = false
	m.focus = FocusEditor
	m.runs.sel = it.key
	if _, ok := m.handleRunsPageKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")}); !ok {
		t.Fatal("i not taken")
	}
	if m.activity.Active() != activity.ViewItermoc {
		t.Fatalf("view %v, want Itermoc", m.activity.Active())
	}
	if p := m.itermocPage; p == nil || p.sec != imSecProfiles || p.name != it.run.Profile {
		t.Errorf("page %+v, want the %s profile", m.itermocPage, it.run.Profile)
	}
	if k := m.itermoc.cursorKey(); !strings.HasSuffix(k, "/"+imSecProfiles+"/"+it.run.Profile) {
		t.Errorf("list cursor on %q, want the %s profile", k, it.run.Profile)
	}
}

// Spec: the empty Runs page has a "Profiles in Itermoc (i)" pill next to
// "Open the chat (c)"; the pill and i open Itermoc on PROFILES.
func TestRunsEmptyProfilesInItermoc(t *testing.T) {
	for _, how := range []string{"click", "key"} {
		m := runsRig(t, "empty", 200, 50)
		page := runsPageText(m, 165, 48)
		wantAll(t, page, "Open the chat (c)", "Profiles in Itermoc (i)", "i Itermoc")
		found := false
		for _, h := range m.runs.cache.pageHits {
			if h.id == "verb:itermoc:" && h.label == "Profiles in Itermoc (i)" {
				found = true
			}
		}
		if !found {
			t.Fatalf("no Profiles in Itermoc pill target: %+v", m.runs.cache.pageHits)
		}
		switch how {
		case "click":
			m.runsClick("verb:itermoc:", true)
		case "key":
			m.focus = FocusEditor
			if _, ok := m.handleRunsPageKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")}); !ok {
				t.Fatal("i not taken")
			}
		}
		if m.activity.Active() != activity.ViewItermoc {
			t.Errorf("%s: view %v, want Itermoc", how, m.activity.Active())
		}
		if len(m.runsCollect(clock.Now())) != 0 {
			t.Error("the empty rig has runs")
		}
	}
}

// Spec: on the empty Runs page "Open the chat (c)" is the one primary
// (lavender) pill; "Profiles in Itermoc (i)" is a quiet, secondary chip.
func TestRunsEmptyItermocPillIsQuiet(t *testing.T) {
	m := runsRig(t, "empty", 200, 50)
	m.runs.cache.pageKy = ""
	page := m.renderRunsPage(165, 48)
	sgrBefore := func(label string) string {
		i := strings.Index(page, label)
		if i < 0 {
			t.Fatalf("no %q on the page", label)
		}
		j := strings.LastIndex(page[:i], "\x1b[")
		k := strings.Index(page[j:], "m")
		return page[j : j+k]
	}
	accent := lgSGR("48", theme.LG(theme.AccentLavender))
	chip := lgSGR("48", theme.LG(theme.BgSectionHdr))
	if s := sgrBefore("Open the chat (c)"); !strings.Contains(s, accent) {
		t.Errorf("Open the chat: %q, want the accent background %q", s, accent)
	}
	s := sgrBefore("Profiles in Itermoc (i)")
	if strings.Contains(s, accent) || !strings.Contains(s, chip) {
		t.Errorf("Profiles in Itermoc: %q, want the chip background %q, not the accent", s, chip)
	}
	if !strings.Contains(s, lgSGR("38", theme.LG(theme.TextSecondary))) {
		t.Errorf("Profiles in Itermoc: %q, want the secondary text colour", s)
	}
}
