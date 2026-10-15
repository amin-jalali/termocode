package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/nerdterm"
	"github.com/amin-jalali/termocode/internal/theme"
)

// setupText draws the checklist for f at width W, as plain lines with the
// trailing blanks cut.
func setupText(t *testing.T, f setupFacts, W int) []string {
	t.Helper()
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	defer func() { theme.CurrentIconMode = old }()
	var out []string
	for _, l := range setupCardLines(setupCardFromFacts(f), W) {
		var s string
		if l.row != nil {
			s = renderWelcomeRow(*l.row, W, rowRest)
		} else {
			s = l.text(W)
		}
		if got := ansi.StringWidth(s); got != W {
			t.Fatalf("line %q is %d cells, want %d", ansi.Strip(s), got, W)
		}
		out = append(out, strings.TrimRight(ansi.Strip(s), " "))
	}
	return out
}

// TestSetupChecklistRows: one row per step (mark, title, short status,
// action word) and a Hide setup row; no counter, no Done, no chips, no
// "Bring it back" sentence.
func TestSetupChecklistRows(t *testing.T) {
	f := baseFacts(ubuntuMono)
	got := setupText(t, f, 68)
	want := []string{
		"SET UP",
		"   ● Icons  Unicode icons · GNOME Terminal: Ubuntu Mono 13     Fix…",
		"   ● AI     Not set up · termocode works without it         Set up…",
		"   ○ Theme  VSCode Dark+                                    Change…",
		"   Hide setup",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("checklist:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	all := strings.Join(got, "\n")
	for _, gone := range []string{"of 2 done", "Done", "Bring it back", "Tokyo Night", "✓ Dark+"} {
		if strings.Contains(all, gone) {
			t.Errorf("checklist still shows %q", gone)
		}
	}
}

// TestSetupIconsStates: the Icons row for each detection, short.
func TestSetupIconsStates(t *testing.T) {
	cases := []struct {
		name string
		f    func() setupFacts
		want string
	}{
		{"probing", func() setupFacts { f := baseFacts(ubuntuMono); f.probed = false; return f },
			"   ● Icons  Checking your terminal font…"},
		{"kitty", func() setupFacts { f := baseFacts(kittyInfo); f.iconMode = theme.IconModeNerdFont; return f },
			"   ✓ Icons  Nerd icons · kitty"},
		{"ssh", func() setupFacts {
			return baseFacts(nerdterm.Info{Terminal: nerdterm.GNOME, Source: "over SSH"})
		}, "   ● Icons  Unicode icons · font not readable over SSH         Fix…"},
		{"nerd-found", func() setupFacts { return baseFacts(gnomeNerd) },
			"   ● Icons  Unicode icons · GNOME Terminal: JetBrainsMo…  Use Nerd…"},
		{"installing", func() setupFacts {
			f := baseFacts(ubuntuMono)
			f.probe.NerdInstalled = false
			f.installing, f.installLine = true, "42%"
			return f
		}, "   ● Icons  Installing JetBrainsMono Nerd Font · 42%"},
		{"kept", func() setupFacts { f := baseFacts(ubuntuMono); f.settled["icons"] = true; return f },
			"   ✓ Icons  Unicode icons (kept)"},
	}
	for _, c := range cases {
		got := setupText(t, c.f(), 68)
		if len(got) < 2 || got[1] != c.want {
			t.Errorf("%s: icons row\n got %q\nwant %q", c.name, got[1], c.want)
		}
	}
}

// TestSetupNarrowStatusOnOwnLine: below 50 cells a row with an action
// puts its status on the next line, under the title.
func TestSetupNarrowStatusOnOwnLine(t *testing.T) {
	got := setupText(t, baseFacts(ubuntuMono), 40)
	if len(got) < 3 {
		t.Fatalf("narrow checklist: %q", got)
	}
	if got[1] != "   ● Icons                         Fix…" {
		t.Errorf("narrow icons row %q", got[1])
	}
	if got[2] != "     Unicode icons · GNOME Terminal: U…" {
		t.Errorf("narrow status line %q", got[2])
	}
}

// TestSetupFontStepsUnderRow: "Show the font steps" lists the steps under
// the row, indented to the status column.
func TestSetupFontStepsUnderRow(t *testing.T) {
	f := baseFacts(ubuntuMono)
	f.showSteps = true
	got := setupText(t, f, 68)
	if !strings.HasSuffix(got[1], "Hide steps") {
		t.Errorf("icons row with steps open: %q", got[1])
	}
	if !strings.HasPrefix(got[2], "            ") || !strings.Contains(strings.Join(got, "\n"), "Custom font") {
		t.Errorf("steps not under the row:\n%s", strings.Join(got, "\n"))
	}
}

// TestSetupAutoDoneWritesConfig: when every required step is done the
// header says so and setup_done is saved, so the next start is clean.
func TestSetupAutoDoneWritesConfig(t *testing.T) {
	withConfigDir(t)
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeNerdFont
	defer func() { theme.CurrentIconMode = old }()
	m := Model{setup: &setupState{visible: true, probed: true, probe: SetupProbe{Terminal: kittyInfo}, settled: map[string]bool{}}}
	m.publishSetupCard()
	if c := loadSettings(); c.SetupDone != nil && *c.SetupDone {
		t.Fatal("setup_done written with AI still open")
	}
	m.setup.settled["ai"] = true
	m.publishSetupCard()
	if c := loadSettings(); c.SetupDone == nil || !*c.SetupDone {
		t.Fatal("all required steps done, but setup_done not saved")
	}
	lines := setupText(t, m.setupFacts(), 68)
	if lines[0] != "SET UP · done" {
		t.Errorf("header %q", lines[0])
	}
	if !m.setup.visible {
		t.Error("the card vanished at once; it should stay this session")
	}
}
