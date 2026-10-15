package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/termimg"
	"github.com/amin-jalali/termocode/internal/theme"
)

func withDashboardLogo(t *testing.T, mode string) {
	t.Helper()
	old := dashboardLogo
	dashboardLogo = mode
	invalidateWelcome()
	t.Cleanup(func() { dashboardLogo = old; invalidateWelcome() })
}

// withRecents is a config dir with n recent files, in unicode icon mode
// (the pixel wordmark; ASCII mode draws the plain banner).
func withRecents(t *testing.T, n int) {
	t.Helper()
	withConfigDir(t)
	oldIcons := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	t.Cleanup(func() { theme.CurrentIconMode = oldIcons; invalidateWelcome() })
	invalidateWelcome()
	for i := 0; i < n; i++ {
		pushRecent(fmt.Sprintf("/tmp/welcome-logo-%d.go", i))
	}
}

// TestWelcomeFitsShortPane: in a 40-row terminal the editor pane is about
// 37 rows. The page used to be 44 rows, so the footer was cut off; now
// the logo makes room and the whole page fits.
func TestWelcomeFitsShortPane(t *testing.T) {
	withRecents(t, 5)
	for _, h := range []int{37, 34} {
		out := renderWelcome(120, h, 0)
		lines := strings.Split(out, "\n")
		if len(lines) > h {
			t.Errorf("h=%d: page is %d rows, taller than the pane", h, len(lines))
		}
		if !strings.Contains(ansi.Strip(out), "for help") {
			t.Errorf("h=%d: footer cut off", h)
		}
		if !strings.Contains(ansi.Strip(out), "RECENT FILES") {
			t.Errorf("h=%d: recent files cut off", h)
		}
	}
}

// TestWelcomeTallPaneKeepsLogo: with room to spare the logo stays.
func TestWelcomeTallPaneKeepsLogo(t *testing.T) {
	withRecents(t, 5)
	renderWelcome(120, 60, 0)
	if !welcomeLogoAt.ok {
		t.Error("tall pane: logo missing")
	}
}

// TestWelcomeLogoOff: dashboard_logo=off drops the logo at any height.
func TestWelcomeLogoOff(t *testing.T) {
	withRecents(t, 5)
	withDashboardLogo(t, "off")
	out := renderWelcome(120, 60, 0)
	if welcomeLogoAt.ok {
		t.Error("logo off: logo still placed")
	}
	if !strings.Contains(ansi.Strip(out), "START") {
		t.Error("logo off: page body missing")
	}
	withDashboardLogo(t, "text")
	if renderWelcome(120, 60, 0); !welcomeLogoAt.ok {
		t.Error("logo text: logo missing")
	}
}

// TestLogoImageOnlyWhenAsked: the image wordmark (kitty / sixel) is drawn
// only with dashboard_logo=image; by default the text logo stays.
func TestLogoImageOnlyWhenAsked(t *testing.T) {
	withRecents(t, 0)
	old := termCaps
	termCaps = termimg.Caps{Protocol: termimg.Kitty}
	t.Cleanup(func() { termCaps = old })
	m := Model{w: 150, h: 60}
	frame := strings.Repeat(strings.Repeat(" ", 150)+"\n", 59) + strings.Repeat(" ", 150)

	for _, c := range []struct {
		mode string
		want bool
	}{{"text", false}, {"off", false}, {"image", true}} {
		withDashboardLogo(t, c.mode)
		renderWelcome(120, 57, 0)
		if _, shown := m.placeLogo(frame); shown != c.want {
			t.Errorf("dashboard_logo=%s: image shown=%v, want %v", c.mode, shown, c.want)
		}
	}
}

// TestDashboardLogoSetting: the Settings modal saves dashboard_logo to
// config.json and applies it at once; a restart reads it back.
func TestDashboardLogoSetting(t *testing.T) {
	withRecents(t, 0)
	withDashboardLogo(t, "text")
	m := Model{w: 150, h: 60}
	if _, ok := m.applyDisplaySetting("setting-dashboard-logo", "off"); !ok {
		t.Fatal("dashboard_logo is not a display setting")
	}
	if dashboardLogo != "off" {
		t.Errorf("applied: dashboardLogo = %q, want off", dashboardLogo)
	}
	if got := ConfigDisplay().Logo; got != "off" {
		t.Errorf("saved: config dashboard_logo = %q, want off", got)
	}
	dashboardLogo = "text"
	ApplyDisplaySettings(ConfigDisplay())
	if dashboardLogo != "off" {
		t.Errorf("restart: dashboardLogo = %q, want off", dashboardLogo)
	}
	if v, _ := displaySettingValue("setting-dashboard-logo", settingsConfig{}); v != "text" {
		t.Errorf("default shown as %q, want text", v)
	}
	if _, ok := m.applyDisplaySetting("setting-dashboard-logo", "big"); !ok || dashboardLogo != "off" {
		t.Errorf("bad value: dashboardLogo = %q, want unchanged off", dashboardLogo)
	}
}
