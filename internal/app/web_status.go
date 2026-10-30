package app

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/statusbar"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The status topic (WEB-12, web design 2.1): the facts of the laptop's
// status bar for the page's strip. The delta is the whole object (it is
// small); a queued one is replaced by the next, and the hub sends at most
// webStatusEvery apart.

const webStatusEvery = 100 * time.Millisecond

func init() {
	webTopics["status"] = webTopicDef{snapshot: webStatusSnapshot, apply: webApplyReplace[webproto.StatusSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishStatus)
}

// webStatusDelta is a status delta: the whole object; a newer one
// replaces a queued one.
type webStatusDelta webproto.StatusSnap

func (webStatusDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webStatusDelta)
	return n, ok
}

// webStatusPub is what the status topic sent last (Update only).
type webStatusPub struct {
	last     *webproto.StatusSnap // nil: nobody listens
	theme    webproto.StatusTheme // the theme built last, and its palette
	themeKey webThemeKey
	at       time.Time // when the last delta went out
	tick     bool      // a one-shot tick is on its way
}

// webStatusTickMsg: the rate limit's wait is over; the next publish may go.
type webStatusTickMsg struct{}

func webStatusSnapshot(m *Model, _ string) (any, error) {
	if m.web == nil {
		return m.webStatus(&webStatusPub{}), nil
	}
	p := &m.web.shell.status
	snap := m.webStatus(p)
	p.last = &snap // the deltas continue from this snap
	return snap, nil
}

// webPublishStatus sends the status when it changed, at most once per
// webStatusEvery: a change inside that time waits for a one-shot tick.
func (m *Model) webPublishStatus(msg tea.Msg) tea.Cmd {
	h := m.web
	p := &h.shell.status
	if _, ok := msg.(webStatusTickMsg); ok {
		p.tick = false
	}
	if !h.webListens("status") {
		p.last = nil
		return nil
	}
	cur := m.webStatus(p)
	if p.last != nil && webStatusSame(*p.last, cur) {
		return nil
	}
	now := webNow()
	if wait := webStatusEvery - now.Sub(p.at); wait > 0 {
		if p.tick {
			return nil
		}
		p.tick = true
		return tea.Tick(wait, func(time.Time) tea.Msg { return webStatusTickMsg{} })
	}
	p.at = now
	p.last = &cur
	h.publish("status", webStatusDelta(cur))
	return nil
}

// webStatusSame compares two status values; the theme compares by id
// and palette key (the colors map is rebuilt only when those change).
func webStatusSame(a, b webproto.StatusSnap) bool {
	return a.Root == b.Root && a.Workspace == b.Workspace && a.Branch == b.Branch &&
		a.Ahead == b.Ahead && a.Behind == b.Behind && a.DirtyFiles == b.DirtyFiles &&
		a.AI == b.AI && a.Terminals == b.Terminals && a.Runs == b.Runs && a.Web == b.Web &&
		a.Theme.Name == b.Theme.Name && a.Theme.Light == b.Theme.Light && len(a.Theme.Colors) == len(b.Theme.Colors) && webSameMap(a.Theme.Colors, b.Theme.Colors)
}

func webSameMap(a, b map[string]string) bool {
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// webStatus builds the status from the Model: the same sources the status
// bar reads. p keeps the theme's colors between calls.
func (m *Model) webStatus(p *webStatusPub) webproto.StatusSnap {
	label, st := m.aiStatus()
	s := webproto.StatusSnap{
		Root:       aiWorkspaceRoot(),
		Workspace:  strings.ToLower(projectName()), // as the status bar shows it
		Branch:     m.gitBranch.Name,
		Ahead:      m.gitBranch.Ahead,
		Behind:     m.gitBranch.Behind,
		DirtyFiles: len(m.gitFiles),
		AI: webproto.StatusAI{
			State: webAIState(st), Label: label, Busy: st == statusbar.AIStreaming,
			CardsWaiting: m.webCardsWaiting(),
		},
		Terminals: webproto.StatusTerms{Count: len(m.terminalTabs)},
		Theme:     p.themeOf(m.themeID),
	}
	for _, t := range m.terminalTabs {
		if t.Busy || t.Running {
			s.Terminals.Busy++
		}
	}
	if m.ai != nil {
		if m.ai.run != nil {
			s.Runs.Live = 1
		}
		if rm := m.ai.aiRunMessage(); rm != nil && (m.ai.planWait != nil || rm.Run.State == ai.RunStatePaused) {
			s.Runs.NeedsYou = 1
		}
	}
	if m.web != nil {
		s.Web.Clients = len(m.web.live)
	}
	return s
}

func webAIState(s statusbar.AIState) string {
	switch s {
	case statusbar.AIUnconfigured:
		return "setup"
	case statusbar.AIIdle:
		return "idle"
	case statusbar.AIStreaming:
		return "busy"
	}
	return "off"
}

// webCardsWaiting counts the cards that wait for the user (the cards
// topic's list: tool calls, outside agents' calls, a plan, review files,
// trust questions).
func (m *Model) webCardsWaiting() int {
	return len(m.webCards(&webCardsPub{}, false))
}

// webThemeFields are the palette fields the page maps to its tokens (web
// UX 7.1), the Syntax* ones for the file view's code colours.
var webThemeFields = []struct {
	name string
	c    *theme.Color256
}{
	{"BgEditor", &theme.BgEditor}, {"BgSidebar", &theme.BgSidebar}, {"BgPanel", &theme.BgPanel},
	{"BgFloat", &theme.BgFloat}, {"BgHover", &theme.BgHover}, {"BgSelection", &theme.BgSelection},
	{"BgStatusBar", &theme.BgStatusBar}, {"OnAccent", &theme.OnAccent},
	{"TextPrimary", &theme.TextPrimary}, {"TextSecondary", &theme.TextSecondary},
	{"TextMuted", &theme.TextMuted}, {"TextDim", &theme.TextDim},
	{"BorderDefault", &theme.BorderDefault}, {"BorderFocus", &theme.BorderFocus},
	{"AIAccent", &theme.AIAccent}, {"AccentBlue", &theme.AccentBlue}, {"AccentGreen", &theme.AccentGreen},
	{"AccentAmber", &theme.AccentAmber}, {"AccentRedCoral", &theme.AccentRedCoral},
	{"GitModified", &theme.GitModified}, {"GitAdded", &theme.GitAdded},
	{"GitDeleted", &theme.GitDeleted}, {"GitConflict", &theme.GitConflict},
	{"SyntaxKeyword", &theme.SyntaxKeyword}, {"SyntaxControl", &theme.SyntaxControl},
	{"SyntaxFunction", &theme.SyntaxFunction}, {"SyntaxVariable", &theme.SyntaxVariable},
	{"SyntaxType", &theme.SyntaxType}, {"SyntaxString", &theme.SyntaxString},
	{"SyntaxNumber", &theme.SyntaxNumber}, {"SyntaxComment", &theme.SyntaxComment},
	{"SyntaxRegex", &theme.SyntaxRegex}, {"SyntaxConstant", &theme.SyntaxConstant},
	{"SyntaxOperator", &theme.SyntaxOperator},
}

// webThemeKey is the palette of webThemeFields, to see a change cheaply.
type webThemeKey [34]theme.Color256

// themeOf returns the status theme, rebuilding the colors only when the
// palette changed (a theme switch or a user color).
func (p *webStatusPub) themeOf(id string) webproto.StatusTheme {
	var key webThemeKey
	for i, f := range webThemeFields {
		key[i] = *f.c
	}
	if p.theme.Colors != nil && p.themeKey == key && p.theme.Name == id {
		return p.theme
	}
	colors := make(map[string]string, len(webThemeFields))
	for _, f := range webThemeFields {
		colors[f.name] = theme.Hex(*f.c)
	}
	p.theme, p.themeKey = webproto.StatusTheme{Name: id, Light: theme.IsLight(), Colors: colors}, key
	return p.theme
}
