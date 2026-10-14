package app

import (
	"os"
	"runtime"
	"slices"
	"strings"

	"github.com/amin-jalali/termocode/internal/nerdterm"
	"github.com/amin-jalali/termocode/internal/setup"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ─── First-run setup card (Welcome page) ───────────────────────────────
//
// A "Set up termocode" card under the START cards with three steps:
// icons (terminal font), AI provider and an optional theme pick. It stays
// until the user presses Done or Hide (setup_done / setup_dismissed in
// config.json); the palette command "Setup: Show Setup Card" brings it
// back. Every status line says only what termocode knows: the terminal's
// font comes from internal/nerdterm (read-only), and nothing outside
// termocode changes before an explicit button press with a preview.
//
// Files: setup_card.go (step logic + config), setup_card_ui.go (state,
// actions, messages), setup_card_render.go (drawing + hit areas).

// SetupProbe is one detection for the card. Tests fake it
// (SetSetupProbe).
type SetupProbe struct {
	Terminal      nerdterm.Info // the terminal's configured font
	NerdInstalled bool          // fc-list found a Nerd Font
	NerdFamily    string        // the installed Nerd family ("" = none)
	ProfileExists bool          // GNOME: a "termocode" profile is there
}

// setupFacts is everything the steps are computed from.
type setupFacts struct {
	probed      bool // the first detection finished
	probe       SetupProbe
	iconMode    theme.IconMode // the icons in use now
	iconEnv     string         // TERMOCODE_ICON_MODE (wins over the setting)
	goos        string
	installing  bool
	installLine string
	showSteps   bool // "Font steps" opened
	profileMade bool // created in this session

	aiConfigured        bool
	aiProvider, aiModel string

	themeID, themeName string
	settled            map[string]bool // steps answered on the card
}

// setupButton is one card button.
type setupButton struct {
	id, label string
	disabled  bool
	why       string // disabled: the reason, told when it is pressed
	active    bool   // theme picks: the current theme
}

// setupStep is one row of the card.
type setupStep struct {
	id, title, status string
	done, optional    bool
	notes             []string
	buttons           []setupButton
}

// setupQuickThemes are the theme step's one-press picks.
var setupQuickThemes = []struct{ id, label string }{
	{"vscode-dark-plus", "Dark+"},
	{"tokyo-night", "Tokyo Night"},
	{"catppuccin-latte", "Light (Latte)"},
}

func iconModeTitle(m theme.IconMode) string {
	switch m {
	case theme.IconModeNerdFont:
		return "Nerd"
	case theme.IconModeUnicode:
		return "Unicode"
	}
	return "ASCII"
}

// nerdFamilyFor is the Nerd Font to tell the user to pick.
func nerdFamilyFor(p SetupProbe) string {
	return setup.SuggestedNerdFont(setup.NerdFontInfo{Found: p.NerdInstalled, Family: p.NerdFamily})
}

// buildSetupSteps computes the three steps.
func buildSetupSteps(f setupFacts) []setupStep {
	return []setupStep{setupIconsStep(f), setupAIStep(f), setupThemeStep(f)}
}

func setupIconsStep(f setupFacts) setupStep {
	s := setupStep{id: "icons", title: "Icons"}
	if !f.probed {
		s.status = "Checking your terminal font…"
		return s
	}
	t := f.probe.Terminal
	mode := iconModeTitle(f.iconMode)
	nerd := nerdFamilyFor(f.probe)
	fontKnownBad := t.Font != "" && !t.Nerd

	switch {
	case f.iconMode == theme.IconModeNerdFont && !fontKnownBad:
		s.done = true
		if t.Known() {
			s.status = "Nerd icons — " + t.Describe() + "."
		} else {
			s.status = "Nerd icons are on."
		}
		return s
	case f.iconMode == theme.IconModeNerdFont:
		s.status = "Nerd icons are on, but " + t.Terminal + " uses " + t.Font + " — icons may show as boxes."
	case t.Nerd:
		s.status = mode + " icons — " + t.Describe() + ". You can turn on Nerd icons."
	case t.Source == "over SSH":
		s.status = mode + " icons — over SSH termocode can't read your local terminal's font."
	case t.Terminal == "":
		s.status = mode + " icons — termocode can't read this terminal's font."
	case t.Font == "":
		s.status = mode + " icons — termocode can't read " + t.Terminal + "'s font."
	default:
		s.status = mode + " icons — " + t.Terminal + " uses " + t.Font + ", not a Nerd Font."
	}
	if f.iconEnv != "" {
		s.notes = append(s.notes, f.iconEnv+" is set: it wins over the buttons here.")
	}
	if f.settled["icons"] && f.iconMode != theme.IconModeNerdFont {
		s.done = true
		return s
	}

	// Notes: what is installed, install progress, steps, the profile.
	switch {
	case f.installing:
		line := f.installLine
		if line == "" {
			line = "starting"
		}
		s.notes = append(s.notes, "Installing "+setup.NerdFontName+"… "+line)
	case f.probe.NerdInstalled && t.Font != "" && !t.Nerd:
		s.notes = append(s.notes, nerd+" is installed, but the terminal does not use it yet.")
	case f.probe.NerdInstalled && !t.Nerd:
		s.notes = append(s.notes, nerd+" is installed. termocode can't see if your terminal uses it.")
	}
	gnome := t.Terminal == nerdterm.GNOME && t.Font != "" && !t.Nerd
	profileReady := gnome && (f.probe.ProfileExists || f.profileMade)
	if f.showSteps && !t.Nerd {
		for _, l := range nerdterm.FixHint(t, nerd) {
			// The card re-checks live: no restart needed.
			s.notes = append(s.notes, strings.Replace(l, "Then restart termocode.", "Then press Check again.", 1))
		}
		if gnome && !profileReady {
			s.notes = append(s.notes, "Or press \"Create termocode profile\": a new profile with the Nerd Font; your default profile stays as it is.")
		}
	}
	if profileReady {
		s.notes = append(s.notes,
			"Profile \""+setup.GnomeProfileName+"\" is ready. Open it with ☰ → New Window → "+setup.GnomeProfileName+", or run:",
			"  gnome-terminal --window --profile="+setup.GnomeProfileName+" -- env TERMOCODE_ICON_MODE=nerd termocode",
			"termocode reads only your default profile, so the command turns Nerd icons on in that window.")
	}

	// Buttons.
	if f.iconMode != theme.IconModeNerdFont {
		if !f.probe.NerdInstalled && !t.Nerd {
			b := setupButton{id: "install-font", label: "Install Nerd Font"}
			switch {
			case f.installing:
				b.label, b.disabled, b.why = "Installing…", true, "The font install is running."
			case f.goos != "linux":
				b.disabled, b.why = true, "Automatic install works on Linux only. Get "+setup.NerdFontName+" from "+setup.NerdFontsURL
			}
			s.buttons = append(s.buttons, b)
		}
		use := setupButton{id: "use-nerd", label: "Use Nerd icons"}
		if !t.Nerd {
			use.disabled = true
			switch {
			case t.Font != "":
				use.why = "Your terminal font (" + t.Font + ") is not a Nerd Font. Switch the terminal to " + nerd + " (Font steps), then press Check again."
			default:
				use.why = "termocode can't see your terminal's font. If it is a Nerd Font, set icons = nerd in Settings."
			}
		}
		s.buttons = append(s.buttons, use)
	}
	if !t.Nerd {
		label := "Font steps"
		if f.showSteps {
			label = "Hide steps"
		}
		s.buttons = append(s.buttons, setupButton{id: "font-steps", label: label})
		if gnome && !profileReady && t.Source != "over SSH" {
			b := setupButton{id: "gnome-profile", label: "Create termocode profile"}
			if !f.probe.NerdInstalled {
				b.disabled, b.why = true, "Install a Nerd Font first: the new profile needs one."
			}
			s.buttons = append(s.buttons, b)
		}
		s.buttons = append(s.buttons, setupButton{id: "check-again", label: "Check again"})
	}
	keep := "Keep Unicode"
	if f.iconMode == theme.IconModeNerdFont {
		keep = "Use Unicode icons"
	}
	s.buttons = append(s.buttons, setupButton{id: "keep-unicode", label: keep})
	return s
}

func setupAIStep(f setupFacts) setupStep {
	s := setupStep{id: "ai", title: "AI"}
	switch {
	case f.aiConfigured:
		s.done = true
		s.status = f.aiProvider
		if f.aiModel != "" {
			s.status += " · " + f.aiModel
		}
	case f.settled["ai"]:
		s.done = true
		s.status = "Skipped. Set it up later: F1 → AI: Configure Provider…"
	default:
		s.status = "Not set up. termocode works fine without AI."
		s.buttons = []setupButton{{id: "ai-setup", label: "Set up AI…"}, {id: "ai-skip", label: "Skip"}}
	}
	return s
}

func setupThemeStep(f setupFacts) setupStep {
	s := setupStep{id: "theme", title: "Theme", optional: true, done: f.settled["theme"]}
	s.status = f.themeName + " (optional)"
	for _, q := range setupQuickThemes {
		s.buttons = append(s.buttons, setupButton{id: "theme:" + q.id, label: q.label, active: q.id == f.themeID})
	}
	s.buttons = append(s.buttons, setupButton{id: "theme-more", label: "More…"})
	return s
}

// setupCardView is what the Welcome page draws.
type setupCardView struct {
	show     bool
	steps    []setupStep
	footer   []setupButton
	progress string
	sig      string // changes whenever the drawing would
}

// buttons lists every button in Tab order.
func (v setupCardView) buttons() []setupButton {
	if !v.show {
		return nil
	}
	var out []setupButton
	for _, s := range v.steps {
		out = append(out, s.buttons...)
	}
	return append(out, v.footer...)
}

func setupCardFromFacts(f setupFacts) setupCardView {
	v := setupCardView{show: true, steps: buildSetupSteps(f)}
	done, need := 0, 0
	for _, s := range v.steps {
		if s.optional {
			continue
		}
		need++
		if s.done {
			done++
		}
	}
	v.progress = itoa(done) + " of " + itoa(need) + " done"
	v.footer = []setupButton{{id: "done", label: "Done"}, {id: "hide", label: "Hide"}}
	var sb strings.Builder
	sb.WriteString(v.progress)
	for _, s := range v.steps {
		sb.WriteString("|" + s.id + s.status + strings.Join(s.notes, "\x00"))
		if s.done {
			sb.WriteString("+")
		}
		for _, b := range s.buttons {
			sb.WriteString("," + b.id + b.label)
			if b.disabled {
				sb.WriteString("-")
			}
			if b.active {
				sb.WriteString("*")
			}
		}
	}
	v.sig = sb.String()
	return v
}

// ─── config.json ───────────────────────────────────────────────────────

// setupCardWanted reports whether the card shows for this config.
func setupCardWanted(c settingsConfig) bool {
	return !(c.SetupDone != nil && *c.SetupDone) && !(c.SetupDismissed != nil && *c.SetupDismissed)
}

func boolPtr(b bool) *bool { return &b }

// saveSetupConfig changes the setup keys of config.json (others kept).
func saveSetupConfig(mut func(*settingsConfig)) error {
	c := loadSettings()
	mut(&c)
	return saveSettings(c)
}

// settleSetupStep records that the user answered step id on the card.
func settleSetupStep(id string) error {
	return saveSetupConfig(func(c *settingsConfig) {
		if !slices.Contains(c.SetupSettled, id) {
			c.SetupSettled = append(c.SetupSettled, id)
		}
	})
}

// showSetupCardConfig clears setup_done / setup_dismissed.
func showSetupCardConfig() error {
	return saveSetupConfig(func(c *settingsConfig) {
		c.SetupDone, c.SetupDismissed = boolPtr(false), boolPtr(false)
	})
}

// iconEnvOverride names the env variable that fixes the icons
// ("TERMOCODE_ICON_MODE=nerd"), "" = none.
func iconEnvOverride() string {
	if v := os.Getenv("TERMOCODE_ICON_MODE"); v != "" {
		return "TERMOCODE_ICON_MODE=" + v
	}
	if os.Getenv("TERMOCODE_NERD_FONT") == "1" {
		return "TERMOCODE_NERD_FONT=1"
	}
	return ""
}

var setupGOOS = runtime.GOOS
