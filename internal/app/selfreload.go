package app

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/selfwatch"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/update"
)

// Reload on a new build (reload_on_new_build). termocode watches the
// program file it runs from (internal/selfwatch: one stat every 3 s).
// When a complete, working build replaces it, termocode reloads itself
// into it at the next idle moment the way Reload Window does: the
// session is saved, the unsaved text backed up and put back, nvim closed,
// the web panel handed over (web_reload.go), and the process re-execs
// the same path with the same arguments, environment and folder: same
// pid, same terminal, the same web port and browser sessions. While an
// AI turn, an agent, a workflow run or a dialog is busy it waits; the
// status chip ("⬆ new build") and one toast say so, and Reload Now /
// Later are palette commands (also on a controller page).

// selfReloadDeps are what the reload reaches outside (tests swap them).
type selfReloadDeps struct {
	exe    func() (string, error) // the program file
	selfID func() string          // the build id of the running image
	watch  selfwatch.Deps
	// exec replaces the process (spawn.Exec); it returns only on failure.
	exec func(bin string, argv, env []string) error
	// allowed: a re-exec is possible (a terminal: never a test binary).
	allowed func() bool
	// every is the poll interval.
	every time.Duration
}

var selfReloadDepsFor = func() selfReloadDeps {
	return selfReloadDeps{
		exe:     spawn.Executable,
		selfID:  selfBuildID,
		watch:   selfwatch.DefaultDeps(),
		exec:    spawn.Exec,
		allowed: func() bool { return !headless() },
		every:   selfwatch.Interval,
	}
}

// selfBuildID is the Go build id of the running image ("" when it cannot
// be read). On Linux /proc/self/exe is the image even after its file was
// replaced. Read once.
var selfBuildID = sync.OnceValue(func() string {
	path := "/proc/self/exe"
	if _, err := os.Stat(path); err != nil {
		if path, err = spawn.Executable(); err != nil {
			return ""
		}
	}
	id, _ := selfwatch.ReadBuildID(path)
	return id
})

// selfReloadState is the watch of this window.
type selfReloadState struct {
	w       *selfwatch.Watcher
	polling bool
	ready   selfwatch.Build // ID "" while no new build is on disk
	later   string          // the build id Later was chosen for
	toasted string          // the build id the toast was shown for
}

type (
	selfReloadStartMsg  struct{ w *selfwatch.Watcher }
	selfReloadTickMsg   struct{}
	selfReloadPolledMsg struct {
		b   selfwatch.Build
		ok  bool
		why string
	}
)

// reloadBuildRowID is the Settings row of reload_on_new_build.
const reloadBuildRowID = "setting-reload-on-new-build"

func init() {
	settingsRows = append(settingsRows, settingsRow{ID: reloadBuildRowID, Label: "reload_on_new_build", Kind: "bool",
		Default: "true for dev builds, false for releases",
		Desc: "When the termocode program file is replaced by a new build (you rebuilt it), reload into it at the next idle moment, " +
			"as Reload Window does: unsaved text, the session and the web panel's browser sessions are kept. " +
			"It waits while an AI turn, an agent or a dialog is busy. Dev builds: on; releases: off."})
}

// devBuild reports whether v is a developer build: "dev", a "+dirty"
// one or a v0.0.0 pseudo-version (a build from a checkout).
func devBuild(v string) bool {
	return !update.IsRelease(v) || strings.Contains(v, "+dirty") || strings.HasPrefix(v, "v0.0.0-")
}

// reloadOnNewBuildOn reads reload_on_new_build from config.json; unset
// is on for dev builds and off for releases.
func reloadOnNewBuildOn() bool {
	var s struct {
		On *bool `json:"reload_on_new_build"`
	}
	if path, err := settingsConfigPath(); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &s)
		}
	}
	if s.On != nil {
		return *s.On
	}
	return devBuild(termocodeVersion)
}

// reloadBuildSettingValue renders the row. ok=false: not ours.
func reloadBuildSettingValue(id string) (string, bool) {
	if id != reloadBuildRowID {
		return "", false
	}
	return strconv.FormatBool(reloadOnNewBuildOn()), true
}

// applyReloadBuildSetting checks and saves the row. ok=false: not ours.
func (m *Model) applyReloadBuildSetting(id, value string) (tea.Cmd, bool) {
	if id != reloadBuildRowID {
		return nil, false
	}
	b, err := parseBoolLoose(strings.TrimSpace(value))
	if err != nil {
		return m.pushToastDetail(toast.Errr, "reload_on_new_build: expected true or false", ""), true
	}
	if err := saveHTTPSetting("reload_on_new_build", b); err != nil {
		return m.pushToastDetail(toast.Errr, "Settings error", err.Error()), true
	}
	return m.pushToastDetail(toast.Info, "reload_on_new_build = "+strconv.FormatBool(b), "Applies at the next start"), true
}

// selfReloadInitCmd starts the watch at launch, or nil with the setting
// off or no terminal (a test binary is never re-executed).
func (m Model) selfReloadInitCmd() tea.Cmd {
	d := selfReloadDepsFor()
	if !reloadOnNewBuildOn() || !d.allowed() {
		return nil
	}
	return func() tea.Msg {
		exe, err := d.exe()
		if err != nil || exe == "" {
			recordError("[reload] no program file to watch: " + errText(err))
			return nil
		}
		return selfReloadStartMsg{w: selfwatch.New(selfwatch.CleanPath(exe), d.selfID(), d.watch)}
	}
}

func errText(err error) string {
	if err == nil {
		return "empty path"
	}
	return err.Error()
}

// selfReloadTick waits one poll interval.
func selfReloadTick(every time.Duration) tea.Cmd {
	return tea.Tick(every, func(time.Time) tea.Msg { return selfReloadTickMsg{} })
}

// handleSelfReloadMsg handles the watch's messages (any overlay may be
// open). ok=false: not ours.
func (m *Model) handleSelfReloadMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case selfReloadStartMsg:
		m.selfReload = &selfReloadState{w: msg.w}
		return selfReloadTick(selfReloadDepsFor().every), true
	case selfReloadTickMsg:
		s := m.selfReload
		if s == nil || s.w == nil || s.polling {
			return nil, true
		}
		s.polling = true
		w := s.w
		return func() tea.Msg {
			b, ok := w.Poll()
			return selfReloadPolledMsg{b: b, ok: ok, why: w.Why}
		}, true
	case selfReloadPolledMsg:
		s := m.selfReload
		if s == nil {
			return nil, true
		}
		s.polling = false
		next := selfReloadTick(selfReloadDepsFor().every)
		if !msg.ok {
			s.ready = selfwatch.Build{}
			return next, true
		}
		s.ready = msg.b
		if s.later == s.ready.ID {
			return next, true
		}
		if why := m.selfReloadBusy(); why != "" {
			return tea.Batch(m.selfReloadToast(why), next), true
		}
		return tea.Batch(m.execReload(), next), true
	}
	return nil, false
}

// selfReloadReady is the new build waiting to be loaded, if any.
func (m *Model) selfReloadReady() (selfwatch.Build, bool) {
	if s := m.selfReload; s != nil && s.ready.ID != "" {
		return s.ready, true
	}
	return selfwatch.Build{}, false
}

// selfReloadToast says once per build that it waits, and for what.
func (m *Model) selfReloadToast(why string) tea.Cmd {
	s := m.selfReload
	if s.toasted == s.ready.ID {
		return nil
	}
	s.toasted = s.ready.ID
	return m.pushToastDetail(toast.Info, "New build ready — reloading when idle",
		"termocode "+s.ready.Version+". Waits: "+why+". Reload now or later: termocode: Reload Into New Build / Reload New Build Later.")
}

// selfReloadBusy says what the reload waits for ("" for nothing): an AI
// turn, an agent at work or asking, a workflow run, a command in a
// terminal tab (the reload ends the tabs' shells, as Reload Window
// does), a download of an update, or a dialog, card or prompt open on
// the laptop or a page.
func (m *Model) selfReloadBusy() string {
	if s := m.ai; s != nil && (s.streaming || s.inlineBusy || s.actionBusy != "" || s.home.busy) {
		return "an AI turn runs"
	}
	if len(m.agentRollupItems()) > 0 {
		return "an agent works or waits for an answer"
	}
	if m.wfx != nil && len(m.wfx.live) > 0 {
		return "a workflow run goes"
	}
	for _, t := range m.terminalTabs {
		if t.Busy || t.Running { // a reload would end it with the tab's shell
			return "a command runs in a terminal tab"
		}
	}
	if m.upd != nil && m.upd.busy {
		return "an update downloads"
	}
	if m.confirmOpen || m.promptOpen || m.pickerOpen || m.settingsModalOpen || m.connectOpen || m.addAIOpen ||
		m.profileFormOpen || m.importOpen || m.actionsOpen || m.menuOpen {
		return "a dialog is open"
	}
	if m.web != nil && (len(m.web.asks.line) > 0 || m.web.asks.shown != nil || len(m.web.asks.here) > 0) {
		return "a card waits on a page"
	}
	return ""
}

// Palette commands, shown only while a new build is ready.
const (
	paletteReloadBuild      = "reload-new-build"
	paletteReloadBuildLater = "reload-new-build-later"
)

func selfReloadPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: paletteReloadBuild, Title: "termocode: Reload Into New Build"},
		{ID: paletteReloadBuildLater, Title: "termocode: Reload New Build Later"},
	}
}

// selfReloadPaletteFilter hides the two commands with no new build.
func (m Model) selfReloadPaletteFilter(items []picker.Item) []picker.Item {
	if m.selfReload != nil && m.selfReload.ready.ID != "" {
		return items
	}
	out := items[:0:0]
	for _, it := range items {
		if it.ID != paletteReloadBuild && it.ID != paletteReloadBuildLater {
			out = append(out, it)
		}
	}
	return out
}

// dispatchSelfReloadPalette runs the two commands. ok=false: not ours.
func (m *Model) dispatchSelfReloadPalette(id string) (tea.Cmd, bool) {
	switch id {
	case paletteReloadBuild:
		if _, ok := m.selfReloadReady(); !ok {
			return m.pushToastDetail(toast.Info, "No new build yet", "termocode reloads by itself when one is built"), true
		}
		return m.execReload(), true
	case paletteReloadBuildLater:
		return m.selfReloadLater(), true
	}
	return nil, false
}

// selfReloadLater keeps this build running: no reload when idle. Reload
// Into New Build still loads it; a newer build arms the reload again.
func (m *Model) selfReloadLater() tea.Cmd {
	b, ok := m.selfReloadReady()
	if !ok {
		return nil
	}
	m.selfReload.later = b.ID
	return m.pushToastDetail(toast.Info, "Staying on this build", "termocode: Reload Into New Build loads "+b.Version+" when you want.")
}

// confirmKindSelfReload is the chip's card.
const confirmKindSelfReload confirmKindEnum = 3260

// openSelfReloadCard asks: Later (first, focused) or Reload Now.
func (m *Model) openSelfReloadCard() tea.Cmd {
	b, ok := m.selfReloadReady()
	if !ok {
		return nil
	}
	body := "termocode " + b.Version + " was built. Reload into it now? Unsaved text, the session and the web panel are kept, as on Reload Window."
	if why := m.selfReloadBusy(); why != "" && why != "a dialog is open" {
		body += " Now " + why + ": it stops."
	}
	m.confirm = confirm.New("New build ready", body, []confirm.Button{
		{ID: "later", Title: "Later"},
		{ID: "reload", Title: "Reload Now"},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindSelfReload
	return nil
}

// handleSelfReloadConfirm answers the card. ok=false: not ours.
func (m *Model) handleSelfReloadConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindSelfReload {
		return nil, false
	}
	switch id {
	case "reload":
		return m.execReload(), true
	case "later":
		return m.selfReloadLater(), true
	}
	return nil, true
}

// execReload re-execs termocode in place (Reload Window, a new build):
// the session written, the unsaved text backed up for the next image,
// the web panel handed over, nvim closed; then the same program path
// (the new build when one replaced it), arguments and environment.
func (m *Model) execReload() tea.Cmd {
	d := selfReloadDepsFor()
	if !d.allowed() {
		return m.pushToastDetail(toast.Errr, "Reload failed", errHeadless.Error())
	}
	bin := ""
	if s := m.selfReload; s != nil && s.w != nil {
		bin = s.w.Path()
	}
	if bin == "" {
		if exe, err := d.exe(); err == nil && exe != "" {
			bin = selfwatch.CleanPath(exe)
		} else {
			bin = os.Args[0]
		}
	}
	FlushSession() // the last cursor positions (N1)
	// Unsaved text: back it up to the last key and tell the new image to
	// put it back (hot_exit.go). A reload is no unclean quit.
	m.flushHotExitForReload()
	m.webHandoverForReload() // web_reload.go
	if m.nvim != nil {
		_ = m.nvim.Close()
	}
	argv := append([]string{bin}, os.Args[1:]...)
	// exec replaces this process; if it returns, it failed.
	if err := d.exec(bin, argv, os.Environ()); err != nil {
		if s := m.selfReload; s != nil {
			s.later = s.ready.ID // no new try every poll; Reload Into New Build tries again
		}
		recordError("[reload] exec " + bin + ": " + err.Error())
		return m.pushToastDetail(toast.Errr, "Reload failed", err.Error())
	}
	return nil
}
