package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Debug toolbox (CTX-43): T on a container (or Debug with Toolbox… in its
// menu, or the palette) asks for a toolbox image, then a Cancel-first
// card shows the command: a separate throw-away container that shares
// the target's process and network namespaces. The target is never
// changed: nothing is copied into it and nothing runs inside it. For
// images without a shell (distroless).

// promptKindContainerDebugImage asks for the toolbox image (free number:
// 1700/1720 blocks are the registry and search prompts).
const promptKindContainerDebugImage promptKindEnum = 1760

// askDebugToolbox opens the image prompt for container c.
func (m *Model) askDebugToolbox(c containers.Container) tea.Cmd {
	if !strings.EqualFold(c.State, "running") {
		return m.servicesToast(toast.Info, "Start the container first", "The toolbox joins a running container's processes and network.")
	}
	m.services.debugTarget = c
	m.prompt = prompt.New("Debug "+containerShownName(c), "Toolbox image (empty for "+containers.DefaultDebugImage+"):", containers.DefaultDebugImage)
	m.prompt.SetAllowEmpty(true)
	m.prompt.SetNote("A separate container shares its processes and network. " + containerShownName(c) + " itself is not changed.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindContainerDebugImage
	return nil
}

// onDebugImagePrompt takes the image and shows the card.
func (m *Model) onDebugImagePrompt(value string) tea.Cmd {
	m.promptOpen = false
	c := m.services.debugTarget
	m.services.debugTarget = containers.Container{}
	if c.ID == "" {
		return nil
	}
	e := m.services.containerEngine
	cmd, err := e.DebugToolbox(c.ID, strings.TrimSpace(value))
	if err != nil {
		return m.servicesToast(toast.Info, "Cannot start the toolbox", err.Error())
	}
	cmd.Label = "debug: " + containerShownName(c)
	return m.askContainerCommand("Debug "+containerShownName(c)+"?",
		"Starts a separate toolbox container on the processes and network of "+containerShownName(c)+". The target is not changed; the toolbox is removed when its shell ends.",
		"Start toolbox", cmd, containerTabPrefix+"debug:"+c.ID)
}

// debugMenuItem is the container menu row.
func debugMenuItem() menu.Item {
	return menu.Item{ID: "debug-toolbox", Title: "Debug with Toolbox…", Hint: "T", Icon: "⚒"}
}

// debugPaletteItems is the CTX-43 palette row.
func debugPaletteItems() []picker.Item {
	return []picker.Item{{ID: "containers-debug-toolbox", Title: "Containers: Debug Selected with Toolbox…"}}
}
