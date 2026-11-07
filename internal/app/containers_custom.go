package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Custom row commands (CTX-38): the user lists commands per view in
// container-commands.json (config dir). `!` on a row (or the row menu,
// or the palette) picks one; a command with a "key" also runs from that
// key in its view. Every run shows a Cancel-first card with the command
// as it will run; there is no shell, each argv entry stays one argument.

const customCommandsFile = "container-commands.json"

const customCommandsTemplate = `{
  "_help": "One entry per command. view: containers, images, volumes, networks, compose, pods, deployments, services, jobs. Placeholders: {{id}} {{name}} {{image}} {{context}} {{namespace}}. key is optional (a free key of that view). Each command asks before it runs.",
  "commands": [
    {
      "name": "Top processes",
      "view": "containers",
      "key": "f",
      "argv": ["docker", "top", "{{id}}"]
    },
    {
      "name": "Describe pod (wide)",
      "view": "pods",
      "argv": ["kubectl", "--context", "{{context}}", "-n", "{{namespace}}", "get", "pod", "{{name}}", "-o", "wide"]
    }
  ]
}
`

// customTakenKeys are the keys each view already uses: a custom command
// may not take them.
var customTakenKeys = map[string]string{
	"docker": "k j l s x r t a A R m e o y p z S i D u P n d h b g G v V T F E L I K N w W O c C ! ? / enter space esc tab shift+tab",
	"kube":   "k j l i v E e r s d f y R [ ] w c n g G L o I t H : A N x a O P S F U D ! ? / enter space esc tab shift+tab",
}

func customViewGroup(view string) string {
	switch view {
	case "pods", "deployments", "services", "jobs":
		return "kube"
	}
	return "docker"
}

// parseCustomCommands reads the file body: {"commands": [...]} or a bare
// list. It checks names, views, programs and keys.
func parseCustomCommands(b []byte) ([]containers.CustomCommand, error) {
	var cmds []containers.CustomCommand
	trim := strings.TrimSpace(string(b))
	if strings.HasPrefix(trim, "[") {
		if err := json.Unmarshal(b, &cmds); err != nil {
			return nil, err
		}
	} else {
		var f struct {
			Commands []containers.CustomCommand `json:"commands"`
		}
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, err
		}
		cmds = f.Commands
	}
	if err := containers.ValidateCustomCommands(cmds); err != nil {
		return nil, err
	}
	for _, c := range cmds {
		if c.Key == "" {
			continue
		}
		for _, k := range strings.Fields(customTakenKeys[customViewGroup(c.View)]) {
			if c.Key == k {
				return nil, fmt.Errorf("custom command %q: key %s is taken by the view", c.Name, c.Key)
			}
		}
	}
	return cmds, nil
}

// loadCustomCommands reads container-commands.json; a missing file is
// no commands.
func loadCustomCommands() ([]containers.CustomCommand, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, customCommandsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cmds, err := parseCustomCommands(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", customCommandsFile, err)
	}
	return cmds, nil
}

// customRow is the view and placeholder values of one row.
type customRow struct {
	view   string
	label  string // shown on the card
	fields map[string]string
}

// containersCustomRow is the current row of the Containers view.
func (m *Model) containersCustomRow() (customRow, bool) {
	r, ok := m.servicesCurrentRow()
	if !ok {
		return customRow{}, false
	}
	st := m.services
	ctx := st.sessionEngine().Context
	f := map[string]string{"context": ctx}
	switch {
	case r.kind == svcRowHostItem && r.host == hostContainer && r.idx >= 0 && r.idx < len(st.containerRows):
		c := st.containerRows[r.idx]
		f["id"], f["name"], f["image"] = c.ID, containerShownName(c), c.Image
		return customRow{view: "containers", label: containerShownName(c), fields: f}, true
	case r.kind == svcRowHostItem && r.host == hostImage:
		im, ref, ok := m.selectedImage()
		if !ok {
			return customRow{}, false
		}
		f["id"], f["name"], f["image"] = strings.TrimPrefix(im.ID, "sha256:"), ref, ref
		return customRow{view: "images", label: ref, fields: f}, true
	case r.kind == svcRowHostItem && r.host == hostNetwork && r.idx >= 0 && r.idx < len(st.networkRows):
		n := st.networkRows[r.idx]
		f["id"], f["name"] = n.ID, n.Name
		return customRow{view: "networks", label: n.Name, fields: f}, true
	case r.kind == svcRowHostItem && r.host == hostVolume && r.idx >= 0 && r.idx < len(st.volumeRows):
		v := st.volumeRows[r.idx]
		f["name"] = v.Name
		return customRow{view: "volumes", label: v.Name, fields: f}, true
	case r.kind == svcRowGroup && strings.HasPrefix(r.section, "grp/containers/") && r.name != "other":
		f["name"] = r.name
		return customRow{view: "compose", label: r.name, fields: f}, true
	case r.kind == svcRowService && r.section == svcSecCompose:
		f["name"] = r.name
		return customRow{view: "compose", label: r.name, fields: f}, true
	}
	return customRow{}, false
}

// kubeCustomRow is the current row of the Kubernetes view.
func (m *Model) kubeCustomRow() (customRow, bool) {
	st := m.kube
	if st == nil {
		return customRow{}, false
	}
	r, ok := st.currentRow()
	if !ok || r.kind != kubeRowItem {
		return customRow{}, false
	}
	switch r.section {
	case "pods", "deployments", "services", "jobs":
	default:
		return customRow{}, false
	}
	f := map[string]string{"name": r.name, "context": st.context, "namespace": st.namespace}
	return customRow{view: r.section, label: r.name, fields: f}, true
}

// openCustomCommands is `!`: the commands of the row's view.
func (m *Model) openCustomCommands(row customRow, ok bool) tea.Cmd {
	if !ok {
		return m.servicesToast(toast.Info, "Select a row first", "Custom commands run on a container, image, volume, network, compose project, pod, deployment, service or job.")
	}
	cmds, err := loadCustomCommands()
	if err != nil {
		return m.servicesToast(toast.Errr, "Custom commands not loaded", err.Error())
	}
	list := containers.CustomCommandsFor(cmds, row.view)
	if len(list) == 0 {
		return m.servicesToast(toast.Info, "No custom commands for "+row.view, "Containers: Edit Custom Commands adds them.")
	}
	m.services.customRow = row
	items := make([]picker.Item, 0, len(list))
	for i, c := range list {
		hint := containers.CommandPreview(c.Argv)
		if c.Key != "" {
			hint = c.Key + " · " + hint
		}
		items = append(items, picker.Item{ID: fmt.Sprintf("ctr:custom:%d", i), Title: c.Name, Hint: hint})
	}
	m.openTasksPickerItems(" Custom Commands · "+row.label+" ", items)
	return nil
}

// customCommandKey runs the custom command bound to key in the row's
// view; false when no command has that key.
func (m *Model) customCommandKey(key string, row customRow, ok bool) (tea.Cmd, bool) {
	if !ok || key == "" {
		return nil, false
	}
	cmds, err := loadCustomCommands()
	if err != nil || len(cmds) == 0 {
		return nil, false
	}
	for _, c := range containers.CustomCommandsFor(cmds, row.view) {
		if c.Key == key {
			return m.askCustomCommand(c, row), true
		}
	}
	return nil, false
}

// pickCustomCommand is a picked "ctr:custom:<n>".
func (m *Model) pickCustomCommand(n string) tea.Cmd {
	row := m.services.customRow
	m.services.customRow = customRow{}
	cmds, err := loadCustomCommands()
	if err != nil {
		return m.servicesToast(toast.Errr, "Custom commands not loaded", err.Error())
	}
	list := containers.CustomCommandsFor(cmds, row.view)
	var i int
	if _, err := fmt.Sscan(n, &i); err != nil || i < 0 || i >= len(list) {
		return m.servicesToast(toast.Info, "The custom commands changed", "Pick the command again.")
	}
	return m.askCustomCommand(list[i], row)
}

// askCustomCommand expands c for row and shows the card.
func (m *Model) askCustomCommand(c containers.CustomCommand, row customRow) tea.Cmd {
	argv, err := c.Expand(row.fields)
	if err != nil {
		return m.servicesToast(toast.Info, "Cannot run "+c.Name, err.Error())
	}
	tab := containerTabPrefix + "custom:" + c.Name + ":" + row.label
	if customViewGroup(row.view) == "kube" {
		tab = "kube-custom:" + c.Name + ":" + row.label
	}
	return m.askContainerCommand(c.Name+"?", "Custom command on "+row.view+" "+row.label+". It runs as written, without a shell, in a terminal tab.",
		"Run", services.Command{Label: c.Name + ": " + row.label, Argv: argv}, tab)
}

// customPaletteItems are the CTX-38 palette rows.
func customPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "containers-custom-command", Title: "Containers: Run Custom Command on Row…"},
		{ID: "containers-custom-commands-edit", Title: "Containers: Edit Custom Commands"},
	}
}

// runCustomFromPalette picks the row of the focused view: Kubernetes when
// it is shown, else Containers.
func (m *Model) runCustomFromPalette() tea.Cmd {
	if m.kube != nil && m.kube.visible {
		row, ok := m.kubeCustomRow()
		return m.openCustomCommands(row, ok)
	}
	row, ok := m.containersCustomRow()
	return m.openCustomCommands(row, ok)
}

// customKeyTaken: the key is one of the view's own (or not a single
// printable key), so no custom command is looked up for it.
func customKeyTaken(group, key string) bool {
	if key == " " || len([]rune(key)) != 1 && !strings.HasPrefix(key, "ctrl+") && !strings.HasPrefix(key, "alt+") {
		return true
	}
	for _, k := range strings.Fields(customTakenKeys[group]) {
		if k == key {
			return true
		}
	}
	return false
}
