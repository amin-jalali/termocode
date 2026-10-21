package app

import (
	"context"
	"fmt"
	"github.com/amin-jalali/termocode/internal/mcp"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/nerdterm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/setup"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// State, actions and background work of the setup card (setup_card.go).

// setupState is shared by Model copies (like aiState).
type setupState struct {
	visible     bool
	probed      bool
	probe       SetupProbe
	installing  bool
	installLine string
	showSteps   bool
	profileMade bool
	settled     map[string]bool
	plan        *setup.GnomeProfilePlan // waiting for the user's confirm
	doneSaved   bool                    // setup_done written this session
	themeAsked  bool                    // the theme picker was opened from the card
	services    int                     // MCP servers in mcp.json (-1: count again)
}

// setupCardOff hides the card whatever config.json says. This package's
// TestMain sets it, so the many tests that start from an empty config
// keep their Welcome layout; the setup card tests turn it back on.
var setupCardOff bool

func newSetupState() *setupState {
	c := loadSettings()
	s := &setupState{visible: !setupCardOff && setupCardWanted(c), settled: map[string]bool{}, services: -1}
	for _, id := range c.SetupSettled {
		s.settled[id] = true
	}
	return s
}

func (m *Model) setupS() *setupState {
	if m.setup == nil {
		m.setup = newSetupState()
	}
	return m.setup
}

// ─── Seams (tests replace them; no real system reads or writes) ────────

var setupSeams struct {
	sync.Mutex
	probe     func(fresh bool) SetupProbe
	install   func(ctx context.Context, progress func(string)) (string, error)
	gsettings setup.Gsettings
}

func setupProbeFn() func(bool) SetupProbe {
	setupSeams.Lock()
	defer setupSeams.Unlock()
	if setupSeams.probe != nil {
		return setupSeams.probe
	}
	return defaultSetupProbe
}

func setupInstallFn() func(context.Context, func(string)) (string, error) {
	setupSeams.Lock()
	defer setupSeams.Unlock()
	if setupSeams.install != nil {
		return setupSeams.install
	}
	return setup.InstallNerdFont
}

func setupGsettingsFn() setup.Gsettings {
	setupSeams.Lock()
	defer setupSeams.Unlock()
	if setupSeams.gsettings != nil {
		return setupSeams.gsettings
	}
	return setup.RunGsettings
}

// SetSetupProbe replaces the setup card's terminal / font detection until
// restore is called. restore also puts back the icon globals a "Check
// again" or "Use Nerd icons" may have changed. For tests.
func SetSetupProbe(probe func(fresh bool) SetupProbe) (restore func()) {
	setupSeams.Lock()
	old := setupSeams.probe
	setupSeams.probe = probe
	setupSeams.Unlock()
	oldHint, oldMode := nerdFontHint, theme.CurrentIconMode
	return func() {
		setupSeams.Lock()
		setupSeams.probe = old
		setupSeams.Unlock()
		nerdFontHint, theme.CurrentIconMode = oldHint, oldMode
	}
}

// SetNerdFontInstaller replaces the font install of the setup card. For
// tests: no network.
func SetNerdFontInstaller(install func(ctx context.Context, progress func(string)) (string, error)) (restore func()) {
	setupSeams.Lock()
	old := setupSeams.install
	setupSeams.install = install
	setupSeams.Unlock()
	return func() { setupSeams.Lock(); setupSeams.install = old; setupSeams.Unlock() }
}

// SetGnomeGsettings replaces the gsettings runner the GNOME profile
// button reads and writes through. For tests: no real system change.
func SetGnomeGsettings(gs setup.Gsettings) (restore func()) {
	setupSeams.Lock()
	old := setupSeams.gsettings
	setupSeams.gsettings = gs
	setupSeams.Unlock()
	return func() { setupSeams.Lock(); setupSeams.gsettings = old; setupSeams.Unlock() }
}

// defaultSetupProbe reads the real terminal font setting and fc-list
// (both read-only, cached unless fresh).
func defaultSetupProbe(fresh bool) SetupProbe {
	p := SetupProbe{Terminal: setup.DetectTerminalFont(fresh)}
	nf := setup.DetectNerdFont(fresh)
	p.NerdInstalled, p.NerdFamily = nf.Found, nf.Family
	if t := p.Terminal; t.Terminal == nerdterm.GNOME && t.Font != "" && !t.Nerd {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		id, _ := setup.FindGnomeProfile(ctx, setupGsettingsFn(), setup.GnomeProfileName)
		p.ProfileExists = id != ""
	}
	return p
}

// ─── Messages ──────────────────────────────────────────────────────────

type setupProbeMsg struct {
	probe SetupProbe
	fresh bool
}

type setupInstallMsg struct {
	line string
	done bool
	dest string
	err  error
	ch   chan setupInstallMsg
}

type setupPlanMsg struct {
	plan setup.GnomeProfilePlan
	err  error
}

type setupProfileDoneMsg struct {
	plan setup.GnomeProfilePlan
	err  error
}

func setupProbeCmd(fresh bool) tea.Cmd {
	probe := setupProbeFn()
	return func() tea.Msg { return setupProbeMsg{probe: probe(fresh), fresh: fresh} }
}

// setupInitCmd starts the first detection when the card shows.
func (m *Model) setupInitCmd() tea.Cmd {
	if !m.setupS().visible {
		return nil
	}
	return setupProbeCmd(false)
}

// handleSetupMsg handles the card's background results. ok=false: not
// ours.
func (m *Model) handleSetupMsg(msg tea.Msg) (tea.Cmd, bool) {
	s := m.setupS()
	switch msg := msg.(type) {
	case setupProbeMsg:
		s.probed, s.probe = true, msg.probe
		if !msg.fresh {
			return nil, true
		}
		// A fresh check: icons = auto follows the new answer at once.
		nerdFontHint = msg.probe.Terminal.Nerd
		ApplyDisplaySettings(ConfigDisplay())
		m.applyLayout()
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Info, "Terminal font checked", msg.probe.Terminal.Describe())
		return cmd, true
	case setupInstallMsg:
		return m.onSetupInstall(msg), true
	case setupPlanMsg:
		return m.onSetupPlan(msg), true
	case setupProfileDoneMsg:
		var cmd tea.Cmd
		if msg.err != nil {
			m.toast, cmd = m.toast.PushDetail(toast.Errr, "Could not create the terminal profile", msg.err.Error())
			return cmd, true
		}
		s.profileMade = true
		m.toast, cmd = m.toast.PushDetail(toast.Info, "Created GNOME Terminal profile \""+msg.plan.Name+"\"",
			"Your default profile is unchanged. How to open it: see the Welcome page.")
		return cmd, true
	}
	return nil, false
}

// ─── The card as drawn ─────────────────────────────────────────────────

// setupFacts gathers the facts the steps are computed from.
func (m *Model) setupFacts() setupFacts {
	s := m.setupS()
	f := setupFacts{
		probed: s.probed, probe: s.probe,
		iconMode: theme.CurrentIconMode, iconEnv: iconEnvOverride(), goos: setupGOOS,
		installing: s.installing, installLine: s.installLine,
		showSteps: s.showSteps, profileMade: s.profileMade,
		settled: s.settled,
	}
	if !m.aiConfigured() {
		f.secretsLocked = m.aiS().secretsLocked
	}
	if m.aiConfigured() {
		cfg := m.aiS().cfg
		f.aiConfigured = true
		if s.services < 0 || (m.ai != nil && m.ai.mcpWiz != nil) {
			list, _ := mcp.Load(mcpUserPath(), "")
			s.services = len(list)
		}
		f.services = s.services
		f.aiProvider = ai.KindLabel(cfg.Kind)
		f.aiModel = cfg.Model
		if f.aiModel == "" {
			f.aiModel = ai.DefaultModel(cfg.Kind)
		}
	}
	f.themeID = m.themeID
	if f.themeID == "" {
		f.themeID = "vscode-dark-plus"
	}
	f.themeName = f.themeID
	if t, ok := theme.LookupTheme(f.themeID); ok {
		f.themeName = t.Name
	}
	return f
}

// setupCardView is the card for the Welcome page (show=false: none).
func (m *Model) setupCardView() setupCardView {
	if m.setup == nil || !m.setup.visible {
		return setupCardView{}
	}
	return setupCardFromFacts(m.setupFacts())
}

// publishSetupCard hands the card to the Welcome renderer, which is a
// plain function (cached by its own key, see welcome.go). The first time
// every required step is done it saves setup_done: the card stays for
// this session ("SET UP · done") and is gone on the next start.
func (m Model) publishSetupCard() {
	v := m.setupCardView()
	if v.show && v.allDone && !m.setup.doneSaved {
		m.setup.doneSaved = true
		_ = saveSetupConfig(func(c *settingsConfig) { c.SetupDone = boolPtr(true) })
	}
	welcomeSetup = v
}

// pressSetupRow runs checklist row id (a step id or "hide"): a step with
// one choice runs it, a step with several opens the step chooser.
func (m *Model) pressSetupRow(id string) tea.Cmd {
	if id == "hide" {
		return m.closeSetupCard()
	}
	for _, s := range m.setupCardView().steps {
		if s.id != id {
			continue
		}
		switch {
		case s.action == "" || len(s.buttons) == 0:
			return nil
		case s.id == "icons" && m.setupS().showSteps:
			return m.pressSetupButton("font-steps") // "Hide steps"
		case len(s.buttons) == 1:
			return m.pressSetupButton(s.buttons[0].id)
		}
		return m.openSetupChooser(s)
	}
	return nil
}

// openSetupChooser lists step s's choices in a picker, the recommended
// (first available) one first; the step's full sentence is the note.
func (m *Model) openSetupChooser(s setupStep) tea.Cmd {
	items := make([]picker.Item, 0, len(s.buttons))
	rec := false
	for _, b := range s.buttons {
		hint := ""
		switch {
		case b.disabled:
			hint = "not available"
		case !rec:
			hint, rec = "recommended", true
		}
		items = append(items, picker.Item{ID: b.id, Title: b.label, Hint: hint})
	}
	m.picker = picker.NewItems(" "+s.title+" ", items)
	m.picker.SetNote(wrapWords(s.detail, 50))
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindSetupStep
	return m.picker.Init()
}

// ─── Button presses ────────────────────────────────────────────────────

// pressSetupButton runs card button id.
func (m *Model) pressSetupButton(id string) tea.Cmd {
	s := m.setupS()
	var btn setupButton
	found := false
	for _, b := range m.setupCardView().buttons() {
		if b.id == id {
			btn, found = b, true
		}
	}
	if !found {
		return nil
	}
	if btn.disabled {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Warn, btn.label+" is not available", btn.why)
		return cmd
	}
	switch {
	case id == "install-font":
		return m.startSetupInstall()
	case id == "use-nerd":
		cmd, _ := m.applyDisplaySetting("setting-icons", "nerd")
		return cmd
	case id == "keep-unicode":
		var cmd tea.Cmd
		if theme.CurrentIconMode == theme.IconModeNerdFont {
			cmd, _ = m.applyDisplaySetting("setting-icons", "unicode")
		}
		return tea.Batch(cmd, m.settleSetup("icons"))
	case id == "font-steps":
		s.showSteps = !s.showSteps
		return nil
	case id == "check-again":
		return setupProbeCmd(true)
	case id == "gnome-profile":
		return m.planSetupProfile()
	case id == "ai-setup":
		return m.openAIConfigure()
	case id == "secrets-unlock":
		return m.openSecretsUnlock(nil)
	case id == "ai-skip":
		return m.settleSetup("ai")
	case id == "services-connect":
		s.services = -1 // count again when the wizard is done
		return m.openMCPConnect()
	case id == "services-skip":
		return m.settleSetup("services")
	case id == "theme-more":
		// A pick there answers the step (onSetupThemePicked); Esc not.
		s.themeAsked = true
		return m.openThemePicker()
	case id == "hide":
		return m.closeSetupCard()
	}
	return nil
}

// onSetupThemePicked: a theme picked in the picker the card opened
// settles the Theme step. picked=false (Esc) only forgets the question.
func (m *Model) onSetupThemePicked(picked bool) tea.Cmd {
	if m.setup == nil || !m.setup.themeAsked {
		return nil
	}
	m.setup.themeAsked = false
	if !picked {
		return nil
	}
	return m.settleSetup("theme")
}

func (m *Model) setupErr(err error) tea.Cmd {
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushDetail(toast.Errr, "Settings error", err.Error())
	return cmd
}

func (m *Model) settleSetup(step string) tea.Cmd {
	m.setupS().settled[step] = true
	if err := settleSetupStep(step); err != nil {
		return m.setupErr(err)
	}
	return nil
}

// closeSetupCard is "Hide setup": the card is gone until the palette
// brings it back (the toast says how).
func (m *Model) closeSetupCard() tea.Cmd {
	m.setupS().visible = false
	m.welcomeFocus = 0
	if err := saveSetupConfig(func(c *settingsConfig) { c.SetupDismissed = boolPtr(true) }); err != nil {
		return m.setupErr(err)
	}
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushDetail(toast.Info, "Setup card hidden", "Bring it back: F1 → Setup: Show Setup Card")
	return cmd
}

// showSetupCard is the palette command "Setup: Show Setup Card".
func (m *Model) showSetupCard() tea.Cmd {
	if err := showSetupCardConfig(); err != nil {
		return m.setupErr(err)
	}
	s := m.setupS()
	s.visible = true
	cmds := []tea.Cmd{setupProbeCmd(false)}
	if !m.welcomeShowing() {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Info, "Setup card is on the Welcome page", "It shows when no file is open.")
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// ─── Nerd Font install (background, no TUI suspend) ────────────────────

const setupInstallToast = "setup-font-install"

func (m *Model) startSetupInstall() tea.Cmd {
	s := m.setupS()
	if s.installing {
		return nil
	}
	s.installing, s.installLine = true, ""
	install := setupInstallFn()
	ch := make(chan setupInstallMsg, 32)
	go func() {
		defer close(ch)
		dest, err := install(context.Background(), func(line string) {
			select {
			case ch <- setupInstallMsg{line: line, ch: ch}:
			default: // drop progress when the UI lags; the result is never dropped
			}
		})
		ch <- setupInstallMsg{done: true, dest: dest, err: err, ch: ch}
	}()
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushKeyed(setupInstallToast, toast.Info, "Installing "+setup.NerdFontName+"…", "runs in the background")
	return tea.Batch(cmd, waitSetupInstall(ch))
}

func waitSetupInstall(ch chan setupInstallMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *Model) onSetupInstall(msg setupInstallMsg) tea.Cmd {
	s := m.setupS()
	var cmd tea.Cmd
	if !msg.done {
		s.installLine = msg.line
		m.toast, cmd = m.toast.PushKeyed(setupInstallToast, toast.Info, "Installing "+setup.NerdFontName+"…", msg.line)
		return tea.Batch(cmd, waitSetupInstall(msg.ch))
	}
	s.installing, s.installLine = false, ""
	if msg.err != nil {
		m.toast, cmd = m.toast.PushKeyed(setupInstallToast, toast.Errr, "Font install failed", msg.err.Error())
		return cmd
	}
	s.showSteps = true
	m.toast, cmd = m.toast.PushKeyed(setupInstallToast, toast.Info, "✓ "+setup.NerdFontName+" installed",
		"in "+msg.dest+". Now set your terminal to use it (Font steps on the Welcome page).")
	return tea.Batch(cmd, setupProbeCmd(true))
}

// ─── GNOME Terminal profile (opt-in, preview first) ────────────────────

const confirmKindSetupProfile confirmKindEnum = 980

func (m *Model) planSetupProfile() tea.Cmd {
	p := m.setupS().probe
	family := nerdFamilyFor(p)
	size := setup.FontSize(p.Terminal.Font)
	gs := setupGsettingsFn()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		plan, err := setup.PlanGnomeProfile(ctx, gs, family, size, nil)
		return setupPlanMsg{plan: plan, err: err}
	}
}

func (m *Model) onSetupPlan(msg setupPlanMsg) tea.Cmd {
	var cmd tea.Cmd
	if msg.err != nil {
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Could not read GNOME Terminal profiles", msg.err.Error())
		return cmd
	}
	if msg.plan.Exists {
		m.setupS().probe.ProfileExists = true
		m.toast, cmd = m.toast.PushDetail(toast.Info, "A \""+msg.plan.Name+"\" profile exists already", "Nothing changed.")
		return cmd
	}
	plan := msg.plan
	m.setupS().plan = &plan
	m.confirm = confirm.New("Create a terminal profile?", setupProfilePreview(plan, m.setupS().probe.Terminal), []confirm.Button{
		{ID: "create", Title: "Create profile", Style: confirm.StylePrimary},
		{ID: "cancel", Title: "Cancel"},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindSetupProfile
	return nil
}

// setupProfilePreview says exactly what the profile button will change.
// The confirm dialog shows at most 12 body lines: keep it short.
func setupProfilePreview(p setup.GnomeProfilePlan, t nerdterm.Info) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Makes a NEW GNOME Terminal profile \"%s\" with %s.\n", p.Name, p.Font)
	cur := "your default profile"
	if t.Font != "" {
		cur += " (" + t.Font + ")"
	}
	fmt.Fprintf(&b, "Not changed: %s and which profile is the default.\n\n", cur)
	b.WriteString("Sets on the new profile only:\n")
	for _, kv := range p.Settings() {
		fmt.Fprintf(&b, "  %s = %s\n", kv[0], kv[1])
	}
	b.WriteString("and adds it to org.gnome.Terminal.ProfilesList list.\n")
	b.WriteString("Colors: GNOME defaults. Undo: Preferences → " + p.Name + " → ▾ → Delete.")
	return b.String()
}

// handleSetupConfirm answers the profile preview. ok=false: not ours.
func (m *Model) handleSetupConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindSetupProfile {
		return nil, false
	}
	s := m.setupS()
	plan := s.plan
	s.plan = nil
	if id != "create" || plan == nil {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.Push(toast.Info, "Nothing changed")
		return cmd, true
	}
	gs := setupGsettingsFn()
	p := *plan
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return setupProfileDoneMsg{plan: p, err: setup.ApplyGnomeProfile(ctx, gs, p)}
	}, true
}

// onSetupConfirmClosed: Esc on the preview changes nothing.
func (m *Model) onSetupConfirmClosed() tea.Cmd {
	if m.confirmKind != confirmKindSetupProfile || m.setupS().plan == nil {
		return nil
	}
	m.setupS().plan = nil
	var cmd tea.Cmd
	m.toast, cmd = m.toast.Push(toast.Info, "Nothing changed")
	return cmd
}
