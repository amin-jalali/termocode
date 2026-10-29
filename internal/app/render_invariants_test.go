package app

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Render invariants on "poor" terminals: 16 colours or no colour at all,
// ASCII / Unicode icon sets. Every frame must be exactly w × h cells, and
// a 16-colour terminal must never receive a 24-bit or 256-colour SGR.
func TestRenderInvariantsPoorTerminals(t *testing.T) {
	withConfigDir(t)
	oldProfile := lipgloss.ColorProfile()
	oldIcons := theme.CurrentIconMode
	defer func() {
		lipgloss.SetColorProfile(oldProfile)
		theme.CurrentIconMode = oldIcons
		theme.ApplyTheme("vscode-dark-plus")
	}()

	const w, h = 100, 30
	m0, _ := newTestModel(t).Update(tea.WindowSizeMsg{Width: w, Height: h})
	base := m0.(Model)

	scenes := map[string]func(m Model) Model{
		"welcome": func(m Model) Model { return m },
		"palette": func(m Model) Model {
			nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyF1})
			return nm.(Model)
		},
		"prompt": func(m Model) Model { m.openGotoLinePrompt(); return m },
		"confirm": func(m Model) Model {
			m.openDeleteConfirm("/tmp/x.go")
			return m
		},
		"settings": func(m Model) Model { m.openSettingsPicker(); return m },
		"toast": func(m Model) Model {
			m.toast, _ = m.toast.PushDetail(toast.Warn, "Something happened", "with a detail line")
			return m
		},
		"theme-picker":  func(m Model) Model { m.openThemePicker(); return m },
		"services":      func(m Model) Model { m.runDevAction("svcdemo"); return m },
		"services-down": func(m Model) Model { m.runDevAction("svcdemo:down"); return m },
		"databases":     func(m Model) Model { m.runDevAction("dbdemo"); return m },
		// HTTP client: the status chip and the "Send anyway?" card.
		"http": func(m Model) Model {
			m.http = newHTTPState()
			m.http.last = &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "GET"}},
				resp: &httpclient.Response{Status: 200, StatusText: "OK", Elapsed: 118e6}}
			_ = m.httpSendRequest("", nil, httpclient.Request{Method: "GET", URL: "http://127.0.0.1:9/{{nope}}"}, "-")
			return m
		},
	}
	sceneMarker := map[string]string{"palette": "Command Palette", "confirm": "Delete", "toast": "Something happened", "theme-picker": "Color Theme", "settings": "icons", "services": "PROJECT", "services-down": "Docker is not running",
		"http": "Send anyway?", "databases": "users"}
	truecolor := regexp.MustCompile(`\x1b\[[0-9;]*[34]8;[25];`)
	anyColor := regexp.MustCompile(`\x1b\[[0-9;]*([34][0-9]|9[0-7]|10[0-7])(;[0-9;]*)?m`)

	for _, prof := range []termenv.Profile{termenv.ANSI, termenv.Ascii} {
		for _, icons := range []theme.IconMode{theme.IconModeASCII, theme.IconModeUnicode, theme.IconModeNerdFont} {
			for _, th := range []string{"vscode-dark-plus", "catppuccin-latte"} {
				lipgloss.SetColorProfile(prof)
				theme.CurrentIconMode = icons
				theme.ApplyTheme(th)
				for name, setup := range scenes {
					m := setup(base)
					out := m.View()
					rows := strings.Split(out, "\n")
					tag := name + "/" + th
					if len(rows) != h {
						t.Fatalf("%s prof=%v icons=%v: %d rows", tag, prof, icons, len(rows))
					}
					for i, r := range rows {
						if got := lipgloss.Width(r); got != w {
							t.Fatalf("%s prof=%v icons=%v: row %d is %d cells:\n%q", tag, prof, icons, i, got, r)
						}
					}
					if want := sceneMarker[name]; want != "" && !strings.Contains(out, want) {
						t.Errorf("%s: scene did not render (missing %q)", tag, want)
					}
					if truecolor.MatchString(out) {
						t.Errorf("%s prof=%v: 24-bit / 256-colour SGR on a 16-colour terminal: %q",
							tag, prof, truecolor.FindString(out))
					}
					if prof == termenv.Ascii && anyColor.MatchString(out) {
						t.Errorf("%s: colour SGR on a no-colour terminal: %q", tag, anyColor.FindString(out))
					}
				}
			}
		}
	}
}
