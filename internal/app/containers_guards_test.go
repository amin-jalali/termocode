package app

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
	"github.com/amin-jalali/termocode/internal/theme"
)

// CP-11: guards for the Containers view. Render invariants on poor
// terminals, wide / ambiguous-width text, and the cost of the Go paths
// (hidden view, render cache, frame budget for a big host).

// Every Containers scene is exactly w x h cells at the three common sizes
// and sends no 24-bit / 256-colour SGR to a 16-colour terminal and no
// colour SGR at all to a no-colour one; in ASCII icon mode no Nerd Font
// glyph appears.
func TestContainersRenderInvariantsPoorTerminals(t *testing.T) {
	withConfigDir(t)
	oldProfile := lipgloss.ColorProfile()
	oldIcons := theme.CurrentIconMode
	defer func() {
		lipgloss.SetColorProfile(oldProfile)
		theme.CurrentIconMode = oldIcons
		theme.ApplyTheme("vscode-dark-plus")
	}()
	scenes := []struct{ name, arg, marker string }{
		{"host", "", "CONTAINERS"}, {"down", "down", "CONTAINERS"}, {"noengine", "noengine", "CONTAINERS"},
		{"menu", "menu", "Refresh all"},
		{"filter", "filter", "/pos"}, {"tags", "tags", "remote tags"}, // CP-12
	}
	truecolor := regexp.MustCompile(`\x1b\[[0-9;]*[34]8;[25];`)
	anyColor := regexp.MustCompile(`\x1b\[[0-9;]*([34][0-9]|9[0-7]|10[0-7])(;[0-9;]*)?m`)
	nerd := regexp.MustCompile("[\uE000-\uF8FF\U000F0000-\U000FFFFD]")
	for _, size := range [][2]int{{80, 24}, {100, 30}, {150, 42}} {
		w, h := size[0], size[1]
		m0, _ := New().Update(tea.WindowSizeMsg{Width: w, Height: h})
		base := m0.(Model)
		for _, prof := range []termenv.Profile{termenv.ANSI, termenv.Ascii} {
			for _, icons := range []theme.IconMode{theme.IconModeASCII, theme.IconModeUnicode, theme.IconModeNerdFont} {
				for _, th := range []string{"vscode-dark-plus", "catppuccin-latte"} {
					lipgloss.SetColorProfile(prof)
					theme.CurrentIconMode = icons
					theme.ApplyTheme(th)
					for _, sc := range scenes {
						m := base
						m.runDevAction("contdemo:" + sc.arg)
						if sc.arg == "" {
							m.runDevAction("contdemo")
						}
						out := m.View()
						tag := fmt.Sprintf("%s %dx%d/%s prof=%v icons=%v", sc.name, w, h, th, prof, icons)
						rows := strings.Split(out, "\n")
						if len(rows) != h {
							t.Fatalf("%s: %d rows, want %d", tag, len(rows), h)
						}
						for i, r := range rows {
							if got := lipgloss.Width(r); got != w {
								t.Fatalf("%s: row %d is %d cells:\n%q", tag, i, got, r)
							}
						}
						if !strings.Contains(ansi.Strip(out), sc.marker) {
							t.Errorf("%s: scene did not render (missing %q)", tag, sc.marker)
						}
						if truecolor.MatchString(out) {
							t.Errorf("%s: 24-bit / 256-colour SGR on a 16-colour terminal: %q", tag, truecolor.FindString(out))
						}
						if prof == termenv.Ascii && anyColor.MatchString(out) {
							t.Errorf("%s: colour SGR on a no-colour terminal: %q", tag, anyColor.FindString(out))
						}
						if icons == theme.IconModeASCII && nerd.MatchString(ansi.Strip(out)) {
							t.Errorf("%s: Nerd Font glyph %q in ASCII icon mode", tag, nerd.FindString(ansi.Strip(out)))
						}
					}
				}
			}
		}
	}
}

// In ASCII icon mode the sidebar draws no arrow, geometric shape or
// dingbat glyph: every icon has its ASCII form.
func TestContainersAsciiModeSidebarHasNoSymbolGlyphs(t *testing.T) {
	oldIcons := theme.CurrentIconMode
	defer func() { theme.CurrentIconMode = oldIcons }()
	theme.CurrentIconMode = theme.IconModeASCII
	st := svcHostState()
	for _, w := range []int{22, 29, 40, 60} {
		for _, line := range svcRender(st, w, 60) {
			for _, r := range line {
				if (r >= 0x2190 && r <= 0x21ff) || (r >= 0x25a0 && r <= 0x25ff) || (r >= 0x2700 && r <= 0x27bf) || r == '⊘' || r == 'ǁ' {
					t.Fatalf("w=%d: non-ASCII %q in %q", w, r, line)
				}
			}
		}
	}
}

// The sidebar keeps every row exactly w cells with wide text in names,
// images and ports, whether the terminal counts ambiguous characters as
// one cell (default) or two (CJK locales).
func TestContainersSidebarWideAndAmbiguousWidthRows(t *testing.T) {
	oldEA := runewidth.DefaultCondition.EastAsianWidth
	oldIcons := theme.CurrentIconMode
	defer func() { runewidth.DefaultCondition.EastAsianWidth = oldEA; theme.CurrentIconMode = oldIcons }()
	for _, mode := range []theme.IconMode{theme.IconModeUnicode, theme.IconModeASCII, theme.IconModeNerdFont} {
		theme.CurrentIconMode = mode
		for _, ea := range []bool{false, true} {
			runewidth.DefaultCondition.EastAsianWidth = ea
			if ea && mode == theme.IconModeNerdFont {
				continue // Private Use glyphs are ambiguous: Nerd Font in a CJK locale is unsupported
			}
			st := svcHostState()
			for i, n := range wideNames {
				st.containerRows = append(st.containerRows, containers.Container{ID: fmt.Sprintf("w%d", i), Names: []string{n}, Image: n + ":1", State: "running",
					Status: "Up 1 hour", PortsText: "0.0.0.0:80->80/tcp"})
				st.imageRows = append(st.imageRows, containers.Image{ID: fmt.Sprintf("sha256:w%d", i), RepoTags: []string{n + ":latest"}, Size: "1MB"})
				st.networkRows = append(st.networkRows, containers.Network{Name: n, Driver: "bridge", Scope: "local"})
				st.volumeRows = append(st.volumeRows, containers.Volume{Name: n, Driver: "local"})
			}
			for sec := range st.collapsed {
				st.collapsed[sec] = false
			}
			for _, w := range []int{explorerMinWidth, 24, 31, 40, 60, 100} {
				m := svcTestModel(st, 120, 80)
				for cursor := 0; cursor < len(st.rows()); cursor += 3 {
					st.cursor = cursor
					st.touch()
					out := m.renderServicesSidebarRaw(w, 80, cursor%2 == 0, svcTestNow, -1, -1)
					hostPart := false
					for i, r := range strings.Split(out, "\n") {
						if !ea {
							if got := lipgloss.Width(r); got != w {
								t.Fatalf("icons=%v w=%d cursor=%d row %d is %d cells:\n%q", mode, w, cursor, i, got, ansi.Strip(r))
							}
							continue
						}
						// A CJK-locale terminal draws ambiguous glyphs 2 cells wide:
						// no row of the host part may then run past the sidebar. The
						// title, hairline and PROJECT rows are shared chrome / the
						// Services part (box drawing, "…"), not Containers.
						if i < 2 {
							continue
						}
						if i >= 78 { // the two footer lines pad with the shared testsPad
							continue
						}
						if !hostPart {
							hostPart = strings.Contains(ansi.Strip(r), "CONTAINERS")
							continue
						}
						// The selection bar "▌" is the shared gitLead glyph.
						if got := runewidth.StringWidth(strings.Replace(ansi.Strip(r), "▌", " ", 1)); got > w {
							t.Fatalf("icons=%v ea w=%d cursor=%d row %d is %d cells (max %d):\n%q", mode, w, cursor, i, got, w, ansi.Strip(r))
						}
					}
				}
			}
		}
	}
}

// bigHost is a Containers view with n containers in n/5 projects plus
// n/4 images, networks and volumes, every section open.
func bigHost(n int) *servicesState {
	st := svcHostState()
	st.containerRows, st.imageRows, st.networkRows, st.volumeRows = nil, nil, nil, nil
	for i := 0; i < n; i++ {
		proj := fmt.Sprintf("proj%d", i/5)
		st.containerRows = append(st.containerRows, containers.Container{ID: fmt.Sprintf("c%06d", i), Names: []string{fmt.Sprintf("%s-svc%d-1", proj, i%5)},
			Image: fmt.Sprintf("registry.example.com/team/app%d:1.%d", i%40, i%9), State: []string{"running", "exited", "paused"}[i%3], Status: "Up 2 hours",
			Project: proj, Labels: map[string]string{"com.docker.compose.project": proj, "com.docker.compose.service": fmt.Sprintf("svc%d", i%5)},
			PortsText: "0.0.0.0:8080->80/tcp"})
	}
	for i := 0; i < n/4; i++ {
		st.imageRows = append(st.imageRows, containers.Image{ID: fmt.Sprintf("sha256:%064d", i), RepoTags: []string{fmt.Sprintf("registry.example.com/team/app%d:1.%d", i%40, i%9)}, Size: "100MB"})
		st.networkRows = append(st.networkRows, containers.Network{Name: fmt.Sprintf("net%d", i), Driver: "bridge", Scope: "local"})
		st.volumeRows = append(st.volumeRows, containers.Volume{Name: fmt.Sprintf("vol%d", i), Driver: "local"})
	}
	for sec := range st.collapsed {
		st.collapsed[sec] = false
	}
	st.touch()
	return st
}

// Spec: the sidebar is rendered once per state. The same state, size and
// theme returns the cached frame; a change (cursor, size, tree) draws a
// new one.
func TestPerfGuardContainersSidebarRenderCache(t *testing.T) {
	st := svcHostState()
	m := svcTestModel(st, 100, 40)
	first := m.renderServicesSidebar(40, 30)
	if !strings.Contains(ansi.Strip(first), "CONTAINERS") {
		t.Fatalf("sidebar did not render:\n%s", ansi.Strip(first))
	}
	if n := testing.AllocsPerRun(100, func() { _ = m.renderServicesSidebar(40, 30) }); n > maxAllocsContainersCached {
		t.Errorf("cached sidebar: %.0f allocs (max %d)", n, maxAllocsContainersCached)
	}
	poison := func() {
		st.render.Lock()
		st.render.out = "CACHED"
		st.render.Unlock()
	}
	poison()
	if got := m.renderServicesSidebar(40, 30); got != "CACHED" {
		t.Fatal("same state was rendered again")
	}
	st.cursor++
	if got := m.renderServicesSidebar(40, 30); got == "CACHED" {
		t.Error("cursor moved but the cached frame was served")
	}
	poison()
	if got := m.renderServicesSidebar(41, 30); got == "CACHED" {
		t.Error("width changed but the cached frame was served")
	}
	m.renderServicesSidebar(40, 30)
	poison()
	st.touch()
	if got := m.renderServicesSidebar(40, 30); got == "CACHED" {
		t.Error("tree changed (touch) but the cached frame was served")
	}
	m.renderServicesSidebar(40, 30)
	poison()
	old := theme.CurrentIconMode
	defer func() { theme.CurrentIconMode = old }()
	theme.CurrentIconMode = theme.IconModeNerdFont
	if old == theme.IconModeNerdFont {
		theme.CurrentIconMode = theme.IconModeUnicode
	}
	if got := m.renderServicesSidebar(40, 30); got == "CACHED" {
		t.Error("icon mode changed but the cached frame was served")
	}
}

const maxAllocsContainersCached = 24 // the cache key (measured well below)

// Spec: a host with 2000 containers still draws a frame well inside the
// frame budget and the cached frame is free; a keypress (cursor move) is
// a fresh draw, so it is measured too.
func TestPerfGuardContainersBigHostFrameBudget(t *testing.T) {
	st := bigHost(2000)
	m := svcTestModel(st, 120, 50)
	_ = m.renderServicesSidebarRaw(40, 50, true, svcTestNow, -1, -1)
	start := time.Now()
	const n = 10
	for i := 0; i < n; i++ {
		st.cursor = i * 7
		st.touch()
		_ = m.renderServicesSidebar(40, 50)
	}
	if ms := time.Since(start).Milliseconds() / n; ms > maxMillisContainersFrame && !raceEnabled {
		t.Errorf("one sidebar frame of 2000 containers took %d ms (max %d)", ms, maxMillisContainersFrame)
	}
	start = time.Now()
	for i := 0; i < 20; i++ {
		_ = st.rows()
	}
	if ms := time.Since(start).Milliseconds() / 20; ms > maxMillisContainersRows && !raceEnabled {
		t.Errorf("building the rows of 2000 containers took %d ms (max %d)", ms, maxMillisContainersRows)
	}
}

const (
	maxMillisContainersFrame = 100 // measured ~17 ms; CI machines are 3-5x slower
	maxMillisContainersRows  = 100
)

func BenchmarkContainersSidebarBigHost(b *testing.B) {
	st := bigHost(2000)
	m := svcTestModel(st, 120, 50)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		st.cursor = i % 100
		st.touch()
		_ = m.renderServicesSidebar(40, 50)
	}
}

// Spec: a hidden Containers view costs nothing: its events process ends,
// no goroutine is left, nothing is scheduled, and it is never drawn.
func TestPerfGuardContainersViewHiddenCostsNothing(t *testing.T) {
	before := runtime.NumGoroutine()
	m, f := ctxSession(t)
	st := m.services
	pid := f.PID("events.pid")
	if pid == 0 || !servicestest.Alive(pid) {
		t.Fatal("watcher is not running while the view is shown")
	}
	// Visible and idle: ticks and unknown messages ask for no work.
	next0, _ := m.Update(aiActivityTickMsg{}) // the first update may set the mouse mode
	*m = next0.(Model)
	for _, msg := range []tea.Msg{aiActivityTickMsg{}, struct{}{}} {
		next, cmd := m.Update(msg)
		*m = next.(Model)
		if cmd != nil {
			if out := cmd(); out != nil {
				t.Errorf("idle Containers view answered %T with %T", msg, out)
			}
		}
	}
	_ = m.View()
	st.render.Lock()
	drawnVisible := st.render.out != ""
	st.render.Unlock()
	if !drawnVisible {
		t.Fatal("a visible Containers view did not draw (the hidden check below would be empty)")
	}
	seq := st.seq
	m.activity.SetActive(activity.ViewFiles)
	if cmd := m.servicesSyncWatch(); cmd != nil {
		t.Error("hiding the view scheduled work")
	}
	if !servicestest.WaitGone(pid, 3*time.Second) {
		t.Error("the events process is still running while the view is hidden")
	}
	if st.hostWatchStop != nil || st.hostWatchCh != nil || st.watchStop != nil {
		t.Error("watcher state kept while hidden")
	}
	if st.seq == seq {
		t.Error("hiding did not bump the view state")
	}
	// Goroutines settle back to the level before the watcher started.
	settled := false
	for i := 0; i < 150 && !settled; i++ {
		settled = runtime.NumGoroutine() <= before+2
		if !settled {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !settled {
		t.Errorf("goroutines left after hiding: %d -> %d", before, runtime.NumGoroutine())
	}
	// A hidden view schedules and starts nothing, whatever arrives.
	if m.containersLoadVisible() != nil || m.containersReloadAll() != nil || m.startHostWatch() != nil {
		t.Error("hidden view scheduled engine work")
	}
	calls := len(f.Calls())
	m.services.render.Lock()
	m.services.render.out, m.services.render.key = "", ""
	m.services.render.Unlock()
	_ = m.View()
	m.services.render.Lock()
	drawn := m.services.render.out
	m.services.render.Unlock()
	if drawn != "" {
		t.Error("a hidden Containers view was rendered")
	}
	if len(f.Calls()) != calls {
		t.Errorf("engine called while hidden: %q", f.Calls()[calls:])
	}
}

// CP-12: the filtered view (a wide filter text in the title row, filtered
// counts in the headers) and remote tag lists keep every row exactly w
// cells, in every icon mode and at every sidebar width.
func TestContainersFilterAndTagsRowsExactWidth(t *testing.T) {
	oldIcons := theme.CurrentIconMode
	defer func() { theme.CurrentIconMode = oldIcons }()
	for _, mode := range []theme.IconMode{theme.IconModeUnicode, theme.IconModeASCII, theme.IconModeNerdFont} {
		theme.CurrentIconMode = mode
		for _, filter := range []string{"", "pos", "日本語のフィルター", strings.Repeat("x", maxFilterRunes)} {
			st := svcHostState()
			for sec := range st.collapsed {
				st.collapsed[sec] = false
			}
			repo, _ := containers.ParseRemoteRepo("registry.example.com:5000/a-very-long-team-name/with-a-long-repository-name")
			var tags []string
			for i := 0; i < 40; i++ {
				tags = append(tags, fmt.Sprintf("1.%d.%d-alpine-with-a-long-suffix", i, i))
			}
			st.remoteTags = map[string]*remoteTags{
				"postgres":    {repo: repo, ref: "postgres", inImages: true, open: true, loaded: true, tags: tags[:5], truncated: true},
				repo.Ref():    {repo: repo, ref: repo.Ref(), open: true, loaded: true, tags: tags},
				"ghcr.io/x/y": {ref: "ghcr.io/x/y", open: true, loaded: true, err: containers.ErrNeedsSignIn.Error()},
			}
			st.remoteOrder = []string{repo.Ref(), "ghcr.io/x/y"}
			st.filter, st.filterTyping = filter, filter != ""
			for _, w := range []int{explorerMinWidth, 24, 31, 40, 60, 100} {
				m := svcTestModel(st, 120, 80)
				n := len(st.rows())
				for cursor := 0; cursor < n; cursor += 4 {
					st.cursor = cursor
					st.touch()
					out := m.renderServicesSidebarRaw(w, 80, cursor%2 == 0, svcTestNow, -1, -1)
					for i, r := range strings.Split(out, "\n") {
						if got := lipgloss.Width(r); got != w {
							t.Fatalf("icons=%v filter=%q w=%d cursor=%d row %d is %d cells:\n%q", mode, filter, w, cursor, i, got, ansi.Strip(r))
						}
					}
				}
			}
		}
	}
}

// CP-12: the filter over a big host still builds its rows inside the
// frame budget (the filter runs on every keypress).
func TestPerfGuardContainersFilterBigHost(t *testing.T) {
	st := bigHost(2000)
	st.filter = "svc3"
	m := svcTestModel(st, 120, 50)
	_ = m.renderServicesSidebarRaw(40, 50, true, svcTestNow, -1, -1)
	start := time.Now()
	const n = 10
	for i := 0; i < n; i++ {
		st.cursor = i * 3
		st.touch()
		_ = m.renderServicesSidebar(40, 50)
	}
	if ms := time.Since(start).Milliseconds() / n; ms > maxMillisContainersFrame*2 && !raceEnabled {
		t.Errorf("one filtered frame of 2000 containers took %d ms (max %d)", ms, maxMillisContainersFrame*2)
	}
	if rows := st.rows(); len(rows) == 0 {
		t.Fatal("no rows")
	}
}
