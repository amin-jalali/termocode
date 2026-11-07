package app

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Containers: write actions (CTX-19) ───────────────────────────────────
//
// On a container row: K kills it (card: Cancel, Stop gently (TERM),
// Kill), N renames it (a prompt), w opens the "Change container" list:
// restart policy, CPU and memory limits, connect to or disconnect from a
// network. Every change but the rename shows the exact command on a card
// with Cancel first and focused. The same actions are in the row's
// right-click menu and in the palette (on the selected container).

const (
	confirmKindContainerWrite    confirmKindEnum = 5191
	promptKindContainerRename    promptKindEnum  = 5192
	promptKindContainerResources promptKindEnum  = 5193
)

// containerWrite is the change waiting on its prompt or card.
type containerWrite struct {
	verb  string // kill | rename | policy | resources | connect | disconnect
	id    string
	value string // signal, name, policy or network
	res   containers.Resources
}

func shortContainerID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// containerWriteMenuItems are the write actions of the row menu.
func containerWriteMenuItems() []menu.Item {
	return []menu.Item{
		{ID: "ctr-kill", Title: "Kill…", Hint: "K", Icon: "✕"},
		{ID: "ctr-rename", Title: "Rename…", Hint: "N", Icon: "✎"},
		{ID: "ctr-policy", Title: "Restart policy…", Icon: "↻"},
		{ID: "ctr-resources", Title: "CPU and memory limits…", Icon: "▤"},
		{ID: "ctr-net-connect", Title: "Connect to network…", Icon: "+"},
		{ID: "ctr-net-disconnect", Title: "Disconnect from network…", Icon: "-"},
	}
}

// containerWritePick runs one write action by its menu id.
func (m *Model) containerWritePick(id string, c containers.Container) tea.Cmd {
	st := m.services
	if st == nil || !st.containerEngine.Reachable {
		return m.servicesToast(toast.Info, "Container engine is not ready", "Refresh the Containers view and try again")
	}
	switch id {
	case "ctr-kill":
		return m.askContainerKill(c)
	case "ctr-rename":
		m.askContainerRename(c)
	case "ctr-policy":
		m.openTasksPickerItems(" Restart Policy: "+containerShownName(c)+" ", containerPolicyItems(c.ID))
	case "ctr-resources":
		m.askContainerResources(c)
	case "ctr-net-connect", "ctr-net-disconnect":
		connect := id == "ctr-net-connect"
		items := m.containerNetworkItems(c, connect)
		if len(items) == 0 {
			if connect {
				return m.servicesToast(toast.Info, "No other network", containerShownName(c)+" is on every network; n on NETWORKS creates one")
			}
			return m.servicesToast(toast.Info, "Not on a network", containerShownName(c))
		}
		title := " Connect " + containerShownName(c) + " to "
		if !connect {
			title = " Disconnect " + containerShownName(c) + " from "
		}
		m.openTasksPickerItems(title, items)
	}
	return nil
}

// openContainerWriteList is w: the "Change container" list.
func (m *Model) openContainerWriteList(c containers.Container) {
	var items []picker.Item
	for _, it := range containerWriteMenuItems() {
		items = append(items, picker.Item{ID: "svc:ctr-do:" + c.ID + "|" + it.ID, Title: it.Title, Hint: it.Hint})
	}
	m.openTasksPickerItems(" Change "+containerShownName(c)+" ", items)
}

func containerPolicyItems(id string) []picker.Item {
	rows := [][2]string{
		{"no", "never restart"},
		{"always", "always restart, also after the engine starts"},
		{"unless-stopped", "restart unless you stopped it"},
		{"on-failure", "restart when it exits with an error"},
		{"on-failure:3", "restart on error, at most 3 times"},
	}
	var items []picker.Item
	for _, r := range rows {
		items = append(items, picker.Item{ID: "svc:ctr-policy:" + id + "|" + r[0], Title: r[0], Hint: r[1]})
	}
	return items
}

// containerNetworkItems lists the networks c can join (connect) or the
// ones it is on (disconnect).
func (m *Model) containerNetworkItems(c containers.Container, connect bool) []picker.Item {
	on := map[string]bool{}
	for _, n := range c.Networks {
		on[n] = true
	}
	verb := "ctr-disconnect"
	if connect {
		verb = "ctr-connect"
	}
	var items []picker.Item
	if connect {
		for _, n := range m.services.networkRows {
			if !on[n.Name] && n.Name != "host" && n.Name != "none" {
				items = append(items, picker.Item{ID: "svc:" + verb + ":" + c.ID + "|" + n.Name, Title: n.Name, Hint: n.Driver})
			}
		}
		return items
	}
	for _, n := range c.Networks {
		items = append(items, picker.Item{ID: "svc:" + verb + ":" + c.ID + "|" + n, Title: n})
	}
	return items
}

// onContainerWritePicked answers the write pickers ("svc:ctr-…").
func (m *Model) onContainerWritePicked(verb, key string) tea.Cmd {
	id, value, _ := strings.Cut(key, "|")
	c, ok := m.containerByID(id)
	if !ok {
		return m.servicesToast(toast.Info, "Container is gone", shortContainerID(id))
	}
	switch verb {
	case "ctr-do":
		return m.containerWritePick(value, c)
	case "ctr-policy":
		m.askContainerWrite(containerWrite{verb: "policy", id: id, value: value})
	case "ctr-connect":
		m.askContainerWrite(containerWrite{verb: "connect", id: id, value: value})
	case "ctr-disconnect":
		m.askContainerWrite(containerWrite{verb: "disconnect", id: id, value: value})
	}
	return nil
}

func (m *Model) askContainerKill(c containers.Container) tea.Cmd {
	if !strings.EqualFold(c.State, "running") && !strings.EqualFold(c.State, "paused") {
		return m.servicesToast(toast.Info, "Container is not running", containerShownName(c))
	}
	m.askContainerWrite(containerWrite{verb: "kill", id: c.ID, value: "KILL"})
	return nil
}

func (m *Model) askContainerRename(c containers.Container) {
	m.pendingContainerWrite = containerWrite{verb: "rename", id: c.ID}
	m.prompt = prompt.New("Rename container", "New name for "+containerShownName(c)+":", containerShownName(c))
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindContainerRename
}

func (m *Model) askContainerResources(c containers.Container) {
	m.pendingContainerWrite = containerWrite{verb: "resources", id: c.ID}
	m.prompt = prompt.New("CPU and memory limits", "CPUs, memory, swap (like 1.5 512m 1g; - keeps one):", "")
	m.prompt.SetNote("Changes " + containerShownName(c) + " now; the next card shows the command first.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindContainerResources
}

// parseResourcesAnswer reads "CPUs memory swap"; "-" or a missing value
// keeps that limit.
func parseResourcesAnswer(s string) (containers.Resources, error) {
	f := strings.Fields(s)
	if len(f) > 3 {
		return containers.Resources{}, errors.New("give at most three values: CPUs, memory, swap")
	}
	var v [3]string
	for i, x := range f {
		if x != "-" {
			v[i] = x
		}
	}
	r := containers.Resources{CPUs: v[0], Memory: v[1], MemorySwap: v[2]}
	if r == (containers.Resources{}) {
		return r, errors.New("no limit to change")
	}
	return r, nil
}

// onContainerWritePrompt takes the rename or limits answer.
func (m *Model) onContainerWritePrompt(kind promptKindEnum, value string) tea.Cmd {
	w := m.pendingContainerWrite
	value = strings.TrimSpace(value)
	switch kind {
	case promptKindContainerRename:
		m.pendingContainerWrite = containerWrite{}
		c, ok := m.containerByID(w.id)
		if !ok {
			return m.servicesToast(toast.Info, "Container is gone", shortContainerID(w.id))
		}
		if value == "" || value == containerShownName(c) {
			return nil
		}
		if strings.HasPrefix(value, "-") || strings.ContainsAny(value, " \t/\\:") {
			return m.servicesToast(toast.Info, "Not a container name", "Use letters, digits, _ . and -")
		}
		return m.runContainerWriteNow(containerWrite{verb: "rename", id: w.id, value: value})
	case promptKindContainerResources:
		r, err := parseResourcesAnswer(value)
		if err != nil {
			m.pendingContainerWrite = containerWrite{}
			return m.servicesToast(toast.Info, "Limits not changed", err.Error())
		}
		w.verb, w.res = "resources", r // the prompt kind is the verb
		m.askContainerWrite(w)
	}
	return nil
}

// containerWriteArgs is the engine argv (without the binary) of w.
func containerWriteArgs(w containerWrite) []string {
	id := shortContainerID(w.id)
	switch w.verb {
	case "kill":
		return []string{"kill", "--signal", w.value, id}
	case "rename":
		return []string{"rename", id, w.value}
	case "policy":
		return []string{"update", "--restart", w.value, id}
	case "resources":
		args := []string{"update"}
		if w.res.CPUs != "" {
			args = append(args, "--cpus", w.res.CPUs)
		}
		if w.res.Memory != "" {
			args = append(args, "--memory", w.res.Memory)
		}
		if w.res.MemorySwap != "" {
			args = append(args, "--memory-swap", w.res.MemorySwap)
		}
		return append(args, id)
	case "connect", "disconnect":
		return []string{"network", w.verb, w.value, id}
	}
	return nil
}

// askContainerWrite opens the card of one change: Cancel first.
func (m *Model) askContainerWrite(w containerWrite) {
	name := shortContainerID(w.id)
	if c, ok := m.containerByID(w.id); ok {
		name = containerShownName(c)
	}
	command := m.services.containerEngine.Binary + " " + strings.Join(containerWriteArgs(w), " ")
	buttons := []confirm.Button{{ID: "cancel", Title: "Cancel"}}
	var title, body string
	switch w.verb {
	case "kill":
		title = "Kill container?"
		body = "This ends " + name + " at once; unsaved work in it is lost. Stop gently sends TERM first."
		buttons = append(buttons,
			confirm.Button{ID: "term", Title: "Stop gently (TERM)"},
			confirm.Button{ID: "apply", Title: "Kill", Style: confirm.StyleDestructive})
	case "policy":
		title = "Change restart policy?"
		body = "Restart policy of " + name + " becomes " + w.value + "."
		buttons = append(buttons, confirm.Button{ID: "apply", Title: "Change"})
	case "resources":
		title = "Change limits?"
		body = "This changes the CPU and memory limits of " + name + " now. Too little memory can stop it."
		buttons = append(buttons, confirm.Button{ID: "apply", Title: "Change"})
	case "connect":
		title = "Connect to network?"
		body = name + " joins " + w.value + "."
		buttons = append(buttons, confirm.Button{ID: "apply", Title: "Connect"})
	case "disconnect":
		title = "Disconnect from network?"
		body = name + " leaves " + w.value + "; it cannot reach that network's containers after this."
		buttons = append(buttons, confirm.Button{ID: "apply", Title: "Disconnect", Style: confirm.StyleDestructive})
	default:
		return
	}
	m.confirm = confirm.New(title, body+"\n\nCommand: "+command, buttons)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerWrite
	m.pendingContainerWrite = w
}

// runContainerWrite answers the card: anything but apply / term is Cancel.
func (m *Model) runContainerWrite(id string) tea.Cmd {
	w := m.pendingContainerWrite
	m.pendingContainerWrite = containerWrite{}
	if w.verb == "" || (id != "apply" && !(id == "term" && w.verb == "kill")) {
		return nil
	}
	if id == "term" {
		w.value = "TERM"
	}
	return m.runContainerWriteNow(w)
}

func (m *Model) runContainerWriteNow(w containerWrite) tea.Cmd {
	st := m.services
	if st == nil || !st.containerEngine.Reachable {
		return m.servicesToast(toast.Info, "Container engine is not ready", "Refresh the Containers view and try again")
	}
	key := containerBusyPrefix + w.id
	if st.busy[key].Busy() {
		return m.servicesToast(toast.Info, "Container is busy", shortContainerID(w.id))
	}
	e := st.containerEngine
	var run func(context.Context) error
	switch w.verb {
	case "kill":
		run = func(ctx context.Context) error { return e.Kill(ctx, w.id, w.value) }
	case "rename":
		run = func(ctx context.Context) error { return e.Rename(ctx, w.id, w.value) }
	case "policy":
		run = func(ctx context.Context) error { return e.SetRestartPolicy(ctx, w.id, w.value) }
	case "resources":
		run = func(ctx context.Context) error { return e.SetResources(ctx, w.id, w.res) }
	case "connect":
		run = func(ctx context.Context) error { return e.NetworkConnect(ctx, w.value, w.id) }
	case "disconnect":
		run = func(ctx context.Context) error { return e.NetworkDisconnect(ctx, w.value, w.id) }
	default:
		return nil
	}
	state := services.Restarting
	if w.verb == "kill" {
		state = services.Stopping
	}
	st.busy[key] = state
	st.touch()
	verb := map[string]string{"policy": "change the restart policy of", "resources": "change the limits of",
		"connect": "connect", "disconnect": "disconnect"}[w.verb]
	if verb == "" {
		verb = w.verb
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
		defer cancel()
		return containerActionDoneMsg{id: w.id, verb: verb, err: run(ctx)}
	}
}

// containerWritePalette runs a write palette command on the selected container.
func (m *Model) containerWritePalette(id string, c containers.Container) (tea.Cmd, bool) {
	pick := map[string]string{
		"containers-kill": "ctr-kill", "containers-rename": "ctr-rename", "containers-restart-policy": "ctr-policy",
		"containers-resources": "ctr-resources", "containers-network-connect": "ctr-net-connect",
		"containers-network-disconnect": "ctr-net-disconnect",
	}[id]
	if pick == "" {
		return nil, false
	}
	return m.containerWritePick(pick, c), true
}
