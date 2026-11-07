package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Containers: bulk select + bulk actions (CTX-23) ──────────────────────
//
// v marks the container under the cursor (and moves down), V clears all
// marks. While marks exist, s / x / r / D act on every marked container
// through ONE card that lists them all; Cancel is first and focused.

// confirmKindContainerBulk is the one card of a bulk container action.
const confirmKindContainerBulk confirmKindEnum = 3863

// containerBulkListMax is how many names the card lists before "… and N more".
const containerBulkListMax = 12

type containerBulkDoneMsg struct {
	verb   string
	ids    []string
	failed []string // "name: error" of each container that failed
}

// markedContainers are the marked containers in row order. Marks of
// containers that are gone are dropped.
func (st *servicesState) markedContainers() []containers.Container {
	if len(st.containerMarks) == 0 {
		return nil
	}
	var out []containers.Container
	live := map[string]bool{}
	for _, c := range st.containerRows {
		if st.containerMarks[c.ID] {
			out = append(out, c)
			live[c.ID] = true
		}
	}
	for id := range st.containerMarks {
		if !live[id] {
			delete(st.containerMarks, id)
		}
	}
	return out
}

func (st *servicesState) containerMarked(id string) bool { return st.containerMarks[id] }

// containerBulkKey handles the bulk keys. ok=false: not a bulk key here.
func (m *Model) containerBulkKey(key string) (tea.Cmd, bool) {
	st := m.services
	switch key {
	case "v":
		c, ok := m.selectedContainer()
		if !ok {
			return nil, false
		}
		if st.containerMarks == nil {
			st.containerMarks = map[string]bool{}
		}
		if st.containerMarks[c.ID] {
			delete(st.containerMarks, c.ID)
		} else {
			st.containerMarks[c.ID] = true
		}
		st.touch()
		m.servicesMoveIn(st.rows(), 1)
		return nil, true
	case "V":
		if len(st.markedContainers()) == 0 {
			return nil, false
		}
		st.containerMarks = nil
		st.touch()
		return m.servicesToast(toast.Info, "Marks cleared", ""), true
	case "s", "x", "r", "D":
		marked := st.markedContainers()
		if len(marked) == 0 {
			return nil, false
		}
		verb := map[string]string{"s": "start", "x": "stop", "r": "restart", "D": "remove"}[key]
		m.askContainerBulk(verb, marked)
		return nil, true
	}
	return nil, false
}

// askContainerBulk opens the one card that lists every marked container.
func (m *Model) askContainerBulk(verb string, marked []containers.Container) {
	noun := "containers"
	if len(marked) == 1 {
		noun = "container"
	}
	title := fmt.Sprintf("%s %d %s?", strings.ToUpper(verb[:1])+verb[1:], len(marked), noun)
	var b strings.Builder
	running := 0
	for i, c := range marked {
		if strings.EqualFold(c.State, "running") {
			running++
		}
		if i < containerBulkListMax {
			b.WriteString("  " + containerShownName(c) + "\n")
		}
	}
	if n := len(marked) - containerBulkListMax; n > 0 {
		fmt.Fprintf(&b, "  … and %d more\n", n)
	}
	if verb == "remove" && running > 0 {
		fmt.Fprintf(&b, "\n%d running: they are stopped first.", running)
	}
	risky := confirm.Button{ID: verb, Title: strings.ToUpper(verb[:1]) + verb[1:]}
	if verb == "remove" || verb == "stop" {
		risky.Style = confirm.StyleDestructive
	}
	m.confirm = confirm.New(title, strings.TrimRight(b.String(), "\n"), []confirm.Button{{ID: "cancel", Title: "Cancel"}, risky})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerBulk
	m.pendingContainerBulk = verb
}

// runContainerBulk answers the bulk card: anything but its verb is Cancel.
func (m *Model) runContainerBulk(id string) tea.Cmd {
	st := m.services
	verb := m.pendingContainerBulk
	m.pendingContainerBulk = ""
	if id != verb || verb == "" {
		return nil
	}
	marked := st.markedContainers()
	st.containerMarks = nil
	if len(marked) == 0 || !st.containerEngine.Reachable {
		st.touch()
		return nil
	}
	state := services.Restarting
	switch verb {
	case "start":
		state = services.Starting
	case "stop", "remove":
		state = services.Stopping
	}
	ids := make([]string, 0, len(marked))
	for _, c := range marked {
		st.busy[containerBusyPrefix+c.ID] = state
		ids = append(ids, c.ID)
	}
	st.touch()
	e := st.containerEngine
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var failed []string
		for _, c := range marked {
			var err error
			switch verb {
			case "start":
				err = e.Start(ctx, c.ID)
			case "stop":
				err = e.Stop(ctx, c.ID)
			case "restart":
				err = e.Restart(ctx, c.ID)
			case "remove":
				if strings.EqualFold(c.State, "running") {
					err = e.Stop(ctx, c.ID)
				}
				if err == nil {
					err = e.RemoveContainer(ctx, c.ID)
				}
			}
			if err != nil {
				failed = append(failed, containerShownName(c)+": "+errLine(err.Error()))
			}
		}
		return containerBulkDoneMsg{verb: verb, ids: ids, failed: failed}
	}
}

func (m *Model) onContainerBulkDone(msg containerBulkDoneMsg) tea.Cmd {
	for _, id := range msg.ids {
		delete(m.services.busy, containerBusyPrefix+id)
	}
	m.services.touch()
	cmds := []tea.Cmd{m.containersRefresh("containers")}
	if len(msg.failed) > 0 {
		title := fmt.Sprintf("Could not %s %d of %d containers", msg.verb, len(msg.failed), len(msg.ids))
		cmds = append(cmds, m.servicesToast(toast.Errr, title, msg.failed[0]))
	}
	return tea.Batch(cmds...)
}
