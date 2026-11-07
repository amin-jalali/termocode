package app

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// CTX-25: the System page (engine info, disk summary, recent events and
// this session's action log), plus the hub for the other extra
// Containers commands (builds, archive, volumes).

// maxRecentEvents is how many engine events the view keeps for the page.
const maxRecentEvents = 50

// maxActionLog is how many container actions the session log keeps.
const maxActionLog = 100

// svcActionEntry is one finished container action of this session.
type svcActionEntry struct {
	At   time.Time
	What string
	Err  string // first line of the error; "" = ok
}

// containersExtraPaletteItems are the extra "Containers: …" rows.
func containersExtraPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "containers-system", Title: "Containers: Show System Page"},
	}
}

// dispatchContainersExtra runs one extra containers palette id.
func (m *Model) dispatchContainersExtra(id string) (tea.Cmd, bool) {
	switch id {
	case "containers-system":
		open := m.revealServicesView(false)
		return m.servicesAfterLoad(open, func(m *Model) tea.Cmd { return m.containersSystemPage() }), true
	}
	return nil, false
}

// noteEvents keeps the newest engine events for the System page.
func (st *servicesState) noteEvents(evs []containers.Event) {
	st.recentEvents = append(st.recentEvents, evs...)
	if n := len(st.recentEvents); n > maxRecentEvents {
		st.recentEvents = append([]containers.Event(nil), st.recentEvents[n-maxRecentEvents:]...)
	}
}

// noteAction adds one finished action to the session log.
func (st *servicesState) noteAction(what string, err error, at time.Time) {
	e := svcActionEntry{At: at, What: strings.TrimSpace(what)}
	if err != nil {
		e.Err = errLine(err.Error())
	}
	st.actionLog = append(st.actionLog, e)
	if n := len(st.actionLog); n > maxActionLog {
		st.actionLog = append([]svcActionEntry(nil), st.actionLog[n-maxActionLog:]...)
	}
}

// noteActionMsg logs the container action messages (the System page).
func (st *servicesState) noteActionMsg(msg tea.Msg, at time.Time) {
	switch msg := msg.(type) {
	case containerActionDoneMsg:
		st.noteAction(msg.verb+" "+shortImageID(msg.id), msg.err, at)
	case containerGroupDoneMsg:
		st.noteAction("project "+msg.project, msg.err, at)
	case hostActionDoneMsg:
		st.noteAction(strings.TrimSpace(msg.operation+" "+msg.target), msg.err, at)
	case containerBulkDoneMsg:
		what := msg.verb + " " + strings.Join(msg.ids, ", ")
		if len(msg.ids) > 3 {
			what = msg.verb + " " + strings.Join(msg.ids[:3], ", ") + " …"
		}
		var err error
		if len(msg.failed) > 0 {
			err = errString(strings.Join(msg.failed, "; "))
		}
		st.noteAction(what, err, at)
	case containersExtraDoneMsg:
		st.noteAction(msg.label, msg.err, at)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// actionLogText is the "Actions" part of the System page, newest first.
func actionLogText(entries []svcActionEntry, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	var b strings.Builder
	b.WriteString("\nActions (this session)\n")
	if len(entries) == 0 {
		b.WriteString("  No actions yet.\n")
		return b.String()
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		status, what := "ok    ", e.What
		if e.Err != "" {
			status, what = "failed", e.What+": "+e.Err
		}
		b.WriteString("  " + e.At.In(loc).Format("15:04:05") + "  " + status + "  " + what + "\n")
	}
	return b.String()
}

// containersPageMsg carries the text of one extra containers page.
type containersPageMsg struct {
	title, text, filetype string
	err                   error
}

// containersExtraDoneMsg is the end of one extra containers job.
type containersExtraDoneMsg struct {
	label   string // what ran, for the toast and the action log
	section string // the host section to reload ("" = none)
	output  string
	err     error
}

func (m *Model) containersEngineOrToast() (containers.Engine, tea.Cmd, bool) {
	e := m.services.containerEngine
	if e.Binary == "" || !e.Reachable {
		return e, m.servicesToast(toast.Info, "No container engine", "Start docker or podman, then try again."), false
	}
	return e, nil, true
}

// containersSystemPage reads the engine info and disk summary and opens
// the System page.
func (m *Model) containersSystemPage() tea.Cmd {
	e, t, ok := m.containersEngineOrToast()
	if !ok {
		return t
	}
	events := append([]containers.Event(nil), m.services.recentEvents...)
	log := append([]svcActionEntry(nil), m.services.actionLog...)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := e.Info(ctx)
		if err != nil {
			return containersPageMsg{title: "containers system", err: err}
		}
		df, dfErr := e.DiskSummary(ctx)
		text := containers.SystemReport(e.Kind, info, df, events, time.Local)
		if dfErr != nil {
			text += "\n! Disk summary: " + errLine(dfErr.Error()) + "\n"
		}
		text += actionLogText(log, time.Local)
		return containersPageMsg{title: "containers system", text: text}
	}
}

func (m *Model) onContainersPage(msg containersPageMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot open "+msg.title, errLine(msg.err.Error()))
	}
	if err := m.openReadOnlyPage(msg.title, msg.text, "", msg.filetype); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open "+msg.title, err.Error())
	}
	return nil
}

func (m *Model) onContainersExtraDone(msg containersExtraDoneMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Could not "+msg.label, errLine(msg.err.Error()))
	}
	cmds := []tea.Cmd{m.servicesToast(toast.Info, "Done: "+msg.label, errLine(msg.output))}
	if msg.section != "" {
		cmds = append(cmds, m.containersRefresh(msg.section))
	}
	return tea.Batch(cmds...)
}
