package app

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

func (m *Model) storageCreatePrompt(section string) {
	m.pendingHostTarget = section
	m.prompt = prompt.New("Create "+strings.TrimSuffix(section, "s"), "Name:", "")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindContainerStorageName
}

func (m *Model) storageCreate(name, section string) tea.Cmd {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "-") {
		return m.servicesToast(toast.Info, "Name cannot start with a dash", "Choose a name that starts with a letter or number.")
	}
	if name == "" || strings.ContainsAny(name, " \t\r\n") {
		return m.servicesToast(toast.Info, "Enter a name without spaces", "")
	}
	target := section + "s"
	if section == "network" {
		target = "networks"
	}
	e := m.services.containerEngine
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var err error
		if section == "network" {
			err = e.CreateNetwork(ctx, name)
		} else {
			err = e.CreateVolume(ctx, name)
		}
		return hostActionDoneMsg{operation: "create-" + section, section: target, target: name, err: err}
	}
}

func (m *Model) askStorageRemove(r svcRow) tea.Cmd {
	key := storageInspectKey(r)
	if strings.HasPrefix(m.services.storageInspectState[key], "error:") {
		return m.servicesToast(toast.Errr, "Could not check container use", "Refresh the section and try again.")
	}
	if m.services.storageInspectState[key] == "" || m.services.storageInspectState[key] == "loading" {
		return m.inspectStorageUsersWithIntent(r, true)
	}
	var command []string
	var title, body, target string
	switch r.host {
	case hostNetwork:
		n := m.services.networkRows[r.idx]
		if n.Name == "bridge" || n.Name == "host" || n.Name == "none" {
			return m.servicesToast(toast.Info, "Built-in network", n.Name+" cannot be removed")
		}
		key := storageInspectKey(r)
		users := m.services.storageUsers[key]
		if m.services.storageInspectState[key] == "" {
			users = storageSnapshotUsers(m.services.containerRows, r)
		}
		target = "networks"
		id := n.ID
		if id == "" {
			id = n.Name
		}
		command = []string{m.services.containerEngine.Binary, "network", "rm", id}
		title, body = "Remove network?", "This removes network "+n.Name+"."
		if len(users) > 0 {
			body += "\n\nContainers using it: " + strings.Join(users, ", ")
		}
	case hostVolume:
		v := m.services.volumeRows[r.idx]
		key := storageInspectKey(r)
		users := m.services.storageUsers[key]
		if m.services.storageInspectState[key] == "" {
			users = storageSnapshotUsers(m.services.containerRows, r)
		}
		if len(users) > 0 {
			m.confirm = confirm.New("Volume is in use", "This volume stays while containers use it.\n\nContainers: "+strings.Join(users, ", "), []confirm.Button{{ID: "cancel", Title: "Close"}})
			m.confirm.SetSize(m.w, m.h)
			m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
			m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = nil, "", ""
			return nil
		}
		target = "volumes"
		command = []string{m.services.containerEngine.Binary, "volume", "rm", v.Name}
		title, body = "Remove volume?", "This permanently removes volume "+v.Name+"."
	}
	body += "\n\nCommand: " + svcShellLine(command)
	m.confirm = confirm.New(title, body, []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "remove", Title: "Remove", Style: confirm.StyleDestructive}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
	m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = command, "remove-storage", target
	return nil
}

type storagePrunePreviewMsg struct {
	section string
	volumes []string
	err     error
}

func (m *Model) askStoragePrune(section string) tea.Cmd {
	if section == "volumes" {
		e := m.services.containerEngine
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			rows, err := e.ListDanglingVolumes(ctx)
			var names []string
			if err == nil {
				for _, volume := range rows {
					if isAnonymousVolume(volume.Name) {
						names = append(names, volume.Name)
					}
				}
			}
			return storagePrunePreviewMsg{section: section, volumes: names, err: err}
		}
	}
	m.showStoragePrune(section, nil)
	return nil
}

func (m *Model) onStoragePrunePreview(msg storagePrunePreviewMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot check dangling volumes", errLine(msg.err.Error()))
	}
	m.showStoragePrune(msg.section, msg.volumes)
	return nil
}

func (m *Model) showStoragePrune(section string, volumes []string) {
	verb := strings.TrimSuffix(section, "s")
	args := []string{m.services.containerEngine.Binary, verb, "prune", "-f"}
	body := "This removes unused " + section + ". The engine decides which are unused."
	if section == "volumes" {
		if m.services.containerEngine.Kind == "podman" {
			body = "Podman may remove unused named volumes too. The list below shows only anonymous dangling volumes."
		} else {
			body = "Docker removes only anonymous dangling volumes with this command. Named volumes are kept."
		}
		if len(volumes) == 0 {
			body += "\n\nNo anonymous dangling volumes were found."
		} else {
			wrapped := make([]string, 0, len(volumes))
			for _, name := range volumes {
				wrapped = append(wrapped, wrapStorageName(name, 32))
			}
			body += "\n\nAnonymous dangling volumes:\n" + strings.Join(wrapped, "\n")
		}
	}
	body += "\n\nCommand: " + svcShellLine(args)
	m.confirm = confirm.New("Prune "+section+"?", body, []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "prune", Title: "Prune", Style: confirm.StyleDestructive}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
	m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = args, "prune-"+section, section
}

func wrapStorageName(name string, width int) string {
	if width < 1 || len(name) <= width {
		return name
	}
	var lines []string
	for len(name) > width {
		lines = append(lines, name[:width])
		name = name[width:]
	}
	return strings.Join(append(lines, name), "\n")
}

type diskUsageMsg struct {
	output string
	err    error
}

func (m *Model) containerDiskUsage() tea.Cmd {
	e := m.services.containerEngine
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := e.DiskUsage(ctx)
		return diskUsageMsg{output: out, err: err}
	}
}

func (m *Model) onDiskUsage(msg diskUsageMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot read disk usage", errLine(msg.err.Error()))
	}
	lines := strings.Split(msg.output, "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			for i := range m.services.volumeRows {
				if m.services.volumeRows[i].Name == fields[0] {
					m.services.volumeRows[i].Size = fields[len(fields)-1]
				}
			}
		}
	}
	m.services.touch()
	return nil
}

type storageInspectMsg struct {
	key    string
	gen    uint64
	users  []string
	row    svcRow
	remove bool
	err    error
}

func storageInspectKey(r svcRow) string {
	kind := "network"
	if r.host == hostVolume {
		kind = "volume"
	}
	return "item/" + kind + "/" + r.hostID
}

func storageFoldGlyph(st *servicesState, r svcRow) string {
	key := storageInspectKey(r)
	if collapsed, ok := st.collapsed[key]; !ok || collapsed {
		return theme.GlyphTreeCollapsed.String()
	}
	return theme.GlyphExpand.String()
}

func (st *servicesState) storageUserRows(r svcRow) []svcRow {
	key := storageInspectKey(r)
	collapsed, ok := st.collapsed[key]
	if !ok || collapsed {
		return nil
	}
	if st.storageInspectState[key] == "loading" {
		return []svcRow{{kind: svcRowHint, section: r.section, text: "  Checking containers…"}}
	}
	if state := st.storageInspectState[key]; strings.HasPrefix(state, "error:") {
		return []svcRow{{kind: svcRowHint, section: r.section, text: "  " + strings.TrimPrefix(state, "error:")}}
	}
	users := st.storageUsers[key]
	if len(users) == 0 {
		return []svcRow{{kind: svcRowHint, section: r.section, text: "  No containers use this"}}
	}
	out := make([]svcRow, 0, len(users))
	for _, name := range users {
		out = append(out, svcRow{kind: svcRowHint, section: r.section, text: "  Used by " + name})
	}
	return out
}

func (m *Model) inspectStorageUsers(r svcRow) tea.Cmd {
	return m.inspectStorageUsersWithIntent(r, false)
}

func (m *Model) inspectStorageUsersWithIntent(r svcRow, remove bool) tea.Cmd {
	key := storageInspectKey(r)
	if m.services.storageInspectState[key] != "" && !remove {
		return nil
	}
	m.services.storageInspectState[key] = "loading"
	e := m.services.containerEngine
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if old := m.services.containerCancels[key]; old != nil {
		old()
	}
	m.services.containerCancels[key] = cancel
	gen := m.services.containerGen
	kind, id := "network", r.hostID
	if r.host == hostVolume {
		kind = "volume"
	}
	return func() tea.Msg {
		defer cancel()
		var users []string
		if kind == "volume" {
			rows, err := e.ContainersUsingVolume(ctx, id)
			if err == nil {
				for _, row := range rows {
					users = append(users, containerDisplayName(row, row.Project))
				}
				sort.Strings(users)
			}
			return storageInspectMsg{key: key, gen: gen, users: users, row: r, remove: remove, err: err}
		}
		value, err := e.Inspect(ctx, kind, id)
		if err == nil {
			users, err = networkInspectUsers(value)
		}
		return storageInspectMsg{key: key, gen: gen, users: users, row: r, remove: remove, err: err}
	}
}

func networkInspectUsers(value any) ([]string, error) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("network inspect returned no data")
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("network inspect returned invalid data")
	}
	connections, _ := item["Containers"].(map[string]any)
	var users []string
	for _, raw := range connections {
		container, _ := raw.(map[string]any)
		name, _ := container["Name"].(string)
		if name != "" {
			users = append(users, strings.TrimPrefix(name, "/"))
		}
	}
	sort.Strings(users)
	return users, nil
}

var anonymousVolumeName = regexp.MustCompile(`^[a-f0-9]{64}$`)

func isAnonymousVolume(name string) bool { return anonymousVolumeName.MatchString(name) }

func storageSnapshotUsers(rows []containers.Container, r svcRow) []string {
	var users []string
	for _, c := range rows {
		used := containsString(c.Networks, r.hostID)
		if r.host == hostVolume {
			used = containsString(c.Mounts, r.hostID)
		}
		if used {
			users = append(users, containerDisplayName(c, c.Project))
		}
	}
	return users
}

func (m *Model) onStorageInspect(msg storageInspectMsg) tea.Cmd {
	if !m.services.visible || msg.gen != m.services.containerGen {
		return nil
	}
	if cancel := m.services.containerCancels[msg.key]; cancel != nil {
		cancel()
		delete(m.services.containerCancels, msg.key)
	}
	if msg.err != nil {
		m.services.storageInspectState[msg.key] = "error:Could not check container use"
	} else {
		m.services.storageInspectState[msg.key] = "done"
		m.services.storageUsers[msg.key] = msg.users
	}
	m.services.touch()
	if msg.remove && msg.err == nil {
		return m.askStorageRemove(msg.row)
	}
	if msg.remove && msg.err != nil {
		return m.servicesToast(toast.Errr, "Could not check container use", errLine(msg.err.Error()))
	}
	return nil
}

func (st *servicesState) clearStorageInspect(kind string) {
	prefix := "item/" + kind + "/"
	for key := range st.storageInspectState {
		if strings.HasPrefix(key, prefix) {
			delete(st.storageInspectState, key)
			delete(st.storageUsers, key)
		}
	}
}
