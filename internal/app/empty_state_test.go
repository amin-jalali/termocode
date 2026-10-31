package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Spec (UI review #10): every empty panel uses one look: a dim glyph, a
// title, a hint and (optionally) the key to press, centred, filling the
// area exactly; small areas keep at least the title.
func TestEmptyStateFitsAndShowsParts(t *testing.T) {
	es := emptyState{Glyph: theme.IconSearch, Title: "Find in files", Hint: "Search the whole workspace. Ctrl+F finds in the current file.", Key: "f8"}
	for _, sz := range [][2]int{{20, 3}, {24, 8}, {40, 12}, {80, 30}} {
		w, h := sz[0], sz[1]
		rows := renderEmptyState(w, h, theme.BgSidebar, es)
		if len(rows) != h {
			t.Fatalf("%dx%d: %d rows", w, h, len(rows))
		}
		for i, r := range rows {
			if got := ansi.StringWidth(r); got != w {
				t.Errorf("%dx%d: row %d is %d wide", w, h, i, got)
			}
		}
		all := ansi.Strip(strings.Join(rows, "\n"))
		if !strings.Contains(all, "Find in files") {
			t.Errorf("%dx%d: no title:\n%s", w, h, all)
		}
		if h >= 8 && (!strings.Contains(all, "Search the whole") || !strings.Contains(all, "F8")) {
			t.Errorf("%dx%d: hint or key missing:\n%s", w, h, all)
		}
	}
}

// The panels use it.
func TestEmptyPanelsUseEmptyState(t *testing.T) {
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	defer func() { theme.CurrentIconMode = old }()
	out := ansi.Strip(strings.Join(scmNotRepoEmpty(30, 20), "\n"))
	if !strings.Contains(out, "Not a git repository") || !strings.Contains(out, theme.IconGit.String()) {
		t.Errorf("SCM empty state:\n%s", out)
	}
}

// C-5: an empty or error state is a title, one plain sentence and the
// action as a labelled button with its key.
func TestEmptyStateIsTitleSentenceButton(t *testing.T) {
	es := emptyState{Title: "No databases yet", Hint: "Add a connection, or put DATABASE_URL in .env.", Key: "a", Label: "Add connection"}
	rows := renderEmptyState(60, 12, theme.BgSidebar, es)
	var lines []string
	for _, r := range rows {
		if l := strings.TrimSpace(ansi.Strip(r)); l != "" {
			lines = append(lines, l)
		}
	}
	want := []string{"No databases yet", "Add a connection, or put DATABASE_URL in .env.", "[Add connection (a)]"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("empty state = %q, want %q", lines, want)
	}
}

// C-5: Databases without connections uses the pattern; a click on it opens
// the add picker; the footer does not say the same again.
func TestDatabasesEmptyStatePattern(t *testing.T) {
	withConfigDir(t)
	s := newDBState()
	s.loaded = true
	m := dbTestModel(s)
	m.w, m.h, m.explorerWidth = 100, 30, 40
	out := stripSGRForTest(m.renderDatabasesSidebarRaw(39, 20, true))
	flat := strings.Join(strings.Fields(out), " ") // the sentence wraps in a narrow sidebar
	for _, want := range []string{"No databases yet", "Add a connection, or put DATABASE_URL in .env.", "[Add connection (a)]"} {
		if !strings.Contains(flat, want) {
			t.Errorf("empty Databases lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "No databases yet") != 1 {
		t.Errorf("the title is there more than once:\n%s", out)
	}
	clicked, _ := m.handleDatabasesSidebarMouse(10, 12, tea.MouseLeft)
	if !clicked.pickerOpen {
		t.Error("a click on the empty state did not open the add picker")
	}
}

// C-5: no Docker is a title, a sentence and the Retry button with its key,
// in the PROJECT part and in every host section.
func TestContainersErrorStatesAreTitleSentenceButton(t *testing.T) {
	st := svcHostState()
	st.containerEngine = containers.Engine{}
	rows := st.hostSectionBody("containers")
	if len(rows) != 3 || rows[0].kind != svcRowNote || rows[1].kind != svcRowHint || rows[2].kind != svcRowRetry {
		t.Fatalf("engine missing rows: %+v", rows)
	}
	st = svcHostState()
	st.problem = services.Problem{Kind: services.ProblemDaemonDown}
	rows = st.problemRows()
	if len(rows) != 3 || rows[0].text != "Docker is not running" || rows[1].kind != svcRowHint || rows[2].kind != svcRowRetry {
		t.Fatalf("daemon down rows: %+v", rows)
	}
	if got := ansi.Strip(renderServiceRetry(rows[2], false, true, 29)); !strings.Contains(got, "Retry (R)]") || strings.Contains(got, "[ ") {
		t.Errorf("the Retry button is not a labelled button with its key: %q", got)
	}
}

// C-5: the "No services" card is the same pattern.
func TestServicesEmptyCardButton(t *testing.T) {
	st := newServicesState()
	st.loaded = true
	st.containerDetected = true
	m := svcTestModel(st, 100, 30)
	got := ansi.Strip(m.renderServicesSidebarRaw(35, 30, false, svcTestNow, -1, -1))
	if !strings.Contains(got, "[Create services.json (o)]") {
		t.Errorf("button:\n%s", got)
	}
}

// FIX-7: on a Podman machine the PROJECT problem names Podman, even when
// the compose error text does not say which engine answered.
func TestProblemRowsNameTheDetectedEngine(t *testing.T) {
	st := svcHostState()
	st.containerEngine = containers.Engine{Binary: "podman", Kind: "podman", Reachable: false}
	st.problem = services.Problem{Kind: services.ProblemDaemonDown}
	if rows := st.problemRows(); len(rows) == 0 || rows[0].text != "Podman is not running" {
		t.Fatalf("daemon down on podman: %+v", rows)
	}
	st.problem = services.Problem{Kind: services.ProblemNoPermission}
	if rows := st.problemRows(); len(rows) == 0 || rows[0].text != "No access to the Podman socket" {
		t.Fatalf("no permission on podman: %+v", rows)
	}
}
