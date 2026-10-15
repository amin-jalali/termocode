package app

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// withWelcomeLists is a config dir with n recent files and m workspaces.
func withWelcomeLists(t *testing.T, n, m int) {
	t.Helper()
	withConfigDir(t)
	invalidateWelcome()
	t.Cleanup(invalidateWelcome)
	for i := 0; i < m; i++ {
		pushWorkspace(fmt.Sprintf("/tmp/welcome-ws-%d", i))
	}
	for i := 0; i < n; i++ {
		pushRecent(fmt.Sprintf("/tmp/welcome-fit/dir-%d/file-%02d.go", i, i))
	}
}

// TestWelcome_FitsPane: in every pane size the page is at most h rows,
// every row exactly w cells, and (down to 44×22) nothing is clipped: the
// shrink ladder drops whole rows instead.
func TestWelcome_FitsPane(t *testing.T) {
	oldP, oldI := lipgloss.ColorProfile(), theme.CurrentIconMode
	defer func() { lipgloss.SetColorProfile(oldP); theme.CurrentIconMode = oldI }()
	oldSetup := welcomeSetup
	defer func() { welcomeSetup = oldSetup }()
	for _, setupOn := range []bool{false, true} {
		welcomeSetup = setupCardView{}
		if setupOn {
			welcomeSetup = setupCardFromFacts(baseFacts(ubuntuMono))
		}
		for _, lists := range [][2]int{{0, 0}, {20, 3}} {
			withWelcomeLists(t, lists[0], lists[1])
			for _, sz := range [][2]int{{109, 40}, {64, 28}, {44, 22}, {76, 22}, {40, 12}} {
				for _, prof := range []termenv.Profile{termenv.TrueColor, termenv.ANSI, termenv.Ascii} {
					for _, icons := range []theme.IconMode{theme.IconModeNerdFont, theme.IconModeUnicode, theme.IconModeASCII} {
						lipgloss.SetColorProfile(prof)
						theme.CurrentIconMode = icons
						invalidateWelcome()
						w, h := sz[0], sz[1]
						tag := fmt.Sprintf("setup=%v %v %dx%d prof=%v icons=%v", setupOn, lists, w, h, prof, icons)
						out, hits := renderWelcomeHits(w, h, 0, true)
						rows := strings.Split(out, "\n")
						if len(rows) > h {
							t.Fatalf("%s: %d rows", tag, len(rows))
						}
						for i, r := range rows {
							if got := ansi.StringWidth(r); got != w {
								t.Fatalf("%s: row %d is %d cells: %q", tag, i, got, ansi.Strip(r))
							}
						}
						if h < 22 {
							continue
						}
						plain := ansi.Strip(out)
						for _, a := range welcomeQuickActions() {
							if !strings.Contains(plain, a.title) {
								t.Errorf("%s: START row %q missing", tag, a.title)
							}
						}
						for _, hh := range hits.recents {
							if hh.Row >= h {
								t.Errorf("%s: recent row %d below the pane", tag, hh.Row)
							}
						}
						if setupOn && !strings.Contains(plain, "Hide setup") {
							t.Errorf("%s: setup checklist cut", tag)
						}
						if !setupOn && lists[0] > 0 && !strings.Contains(plain, "RECENT FILES") {
							t.Errorf("%s: recent files dropped although they fit:\n%s", tag, plain)
						}
					}
				}
			}
		}
	}
}

// recentRowsShown counts the recent-file rows (not the All… row).
func recentRowsShown(h welcomeHits) int {
	n := 0
	for _, r := range h.recents {
		if r.Path != welcomeSentinelViewAll {
			n++
		}
	}
	return n
}

// TestWelcome_ShrinkOrder: a shorter pane first shows fewer recent files
// (5 → 3), and only then trades the pixel wordmark for the text one.
func TestWelcome_ShrinkOrder(t *testing.T) {
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	defer func() { theme.CurrentIconMode = old }()
	withWelcomeLists(t, 20, 3)
	sawPixelWith3 := false
	for h := 44; h >= 22; h-- {
		_, hits := renderWelcomeHits(109, h, 0, true)
		pixel, n := welcomeLogoAt.ok, recentRowsShown(hits)
		if h >= welcomePixelMinH && !pixel && n == 5 {
			t.Errorf("h=%d: wordmark dropped while 5 recent files show", h)
		}
		if pixel && n == 3 {
			sawPixelWith3 = true
		}
	}
	if !sawPixelWith3 {
		t.Error("no height shows the pixel wordmark with 3 recent files: the ladder skips step 1")
	}
	// The top of the page never floats: START starts near the top.
	out, _ := renderWelcomeHits(109, 60, 0, true)
	for i, line := range strings.Split(ansi.Strip(out), "\n") {
		if strings.Contains(line, "START") {
			if i > 12 {
				t.Errorf("START on row %d of 60: the page is centred, want it anchored at the top", i)
			}
			break
		}
	}
}

var fgRGB = regexp.MustCompile(`38;2;(\d+);(\d+);(\d+)`)

// TestWelcome_NoSecondAccent: below the wordmark the page is grey; the
// one accent is the cursor bar.
func TestWelcome_NoSecondAccent(t *testing.T) {
	oldP, oldI := lipgloss.ColorProfile(), theme.CurrentIconMode
	lipgloss.SetColorProfile(termenv.TrueColor)
	theme.CurrentIconMode = theme.IconModeUnicode
	defer func() { lipgloss.SetColorProfile(oldP); theme.CurrentIconMode = oldI }()
	withWelcomeLists(t, 0, 0)
	accent := theme.ToRGB(theme.BorderFocus)
	out, _ := renderWelcomeHits(109, 28, 0, true) // h < 30: text wordmark
	for _, m := range fgRGB.FindAllStringSubmatch(out, -1) {
		r, _ := strconv.Atoi(m[1])
		g, _ := strconv.Atoi(m[2])
		b, _ := strconv.Atoi(m[3])
		if max(r, g, b)-min(r, g, b) <= 12 {
			continue // grey
		}
		if abs(r-int(accent.R)) <= 2 && abs(g-int(accent.G)) <= 2 && abs(b-int(accent.B)) <= 2 {
			continue
		}
		t.Errorf("non-grey foreground %v on the start page", m[1:])
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
