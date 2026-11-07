package app

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Build an image from a Dockerfile (CTX-7): four short prompts (tag, build
// args one at a time, target, platform), then a Cancel-first card with the
// full command, then the build runs in a terminal tab so the progress shows.

const buildTitle = "Build image"

// askImageBuild starts the build steps for one Dockerfile.
func (m *Model) askImageBuild(dockerfile string) tea.Cmd {
	if !fileExists(dockerfile) {
		return m.servicesToast(toast.Info, "No Dockerfile here", "Open or right-click a Dockerfile, then choose Build Image….")
	}
	m.pendingBuild = containers.BuildOptions{Dockerfile: dockerfile}
	m.openBuildPrompt(promptKindContainerBuildTag, "Image tag (name:tag, empty for none):", defaultBuildTag(dockerfile), "")
	return nil
}

// defaultBuildTag suggests "<folder>:latest" in lower case (image names
// must be lower case).
func defaultBuildTag(dockerfile string) string {
	name := strings.ToLower(filepath.Base(filepath.Dir(dockerfile)))
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, name)
	name = strings.Trim(name, "-._")
	if name == "" {
		name = "image"
	}
	return name + ":latest"
}

func (m *Model) openBuildPrompt(kind promptKindEnum, label, initial, note string) {
	m.prompt = prompt.New(buildTitle, label, initial)
	m.prompt.SetAllowEmpty(true)
	if note != "" {
		m.prompt.SetNote(note)
	}
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, kind
}

// onContainerBuildPrompt takes one answer and opens the next step. Values
// that start with a dash are refused: they would read as engine flags.
func (m *Model) onContainerBuildPrompt(kind promptKindEnum, value string) tea.Cmd {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "-") {
		m.promptOpen = false
		m.pendingBuild = containers.BuildOptions{}
		return m.servicesToast(toast.Info, "Build values cannot start with a dash", "Start the build again and enter a plain value.")
	}
	switch kind {
	case promptKindContainerBuildTag:
		m.pendingBuild.Tag = value
		m.openBuildPrompt(promptKindContainerBuildArgs, "Build argument (KEY=value, empty to finish):", "", "")
	case promptKindContainerBuildArgs:
		if value != "" {
			key, _, ok := strings.Cut(value, "=")
			if !ok || key == "" {
				m.promptOpen = false
				m.pendingBuild = containers.BuildOptions{}
				return m.servicesToast(toast.Info, "Build arguments must use KEY=value", "Start the build again and fix the entry.")
			}
			m.pendingBuild.Args = append(m.pendingBuild.Args, value)
			m.openBuildPrompt(promptKindContainerBuildArgs, "Build argument (KEY=value, empty to finish):", "", "Arguments so far: "+strings.Join(m.pendingBuild.Args, ", "))
			return nil
		}
		m.openBuildPrompt(promptKindContainerBuildTarget, "Target stage (empty for the last stage):", "", "")
	case promptKindContainerBuildTarget:
		m.pendingBuild.Target = value
		m.openBuildPrompt(promptKindContainerBuildPlatform, "Platform (e.g. linux/amd64, empty for default):", "", "")
	case promptKindContainerBuildPlatform:
		m.pendingBuild.Platform = value
		m.promptOpen = false
		return m.askBuildConfirm()
	}
	return nil
}

func (m *Model) askBuildConfirm() tea.Cmd {
	e := m.services.containerEngine
	if e.Binary == "" {
		m.pendingBuild = containers.BuildOptions{}
		return m.servicesToast(toast.Info, "No container engine found", "Install or start Docker or Podman, then try again.")
	}
	args := e.BuildCommand(m.pendingBuild).Argv
	m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = args, "build-image", "images"
	m.confirm = confirm.New("Build image?", "Review the command before it runs. The build shows its progress in a terminal tab.\n\nCommand: "+svcShellLine(args), []confirm.Button{
		{ID: "cancel", Title: "Cancel"}, {ID: "build", Title: "Build", Style: confirm.StylePrimary},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
	return nil
}

// runImageBuild starts the confirmed build in a terminal tab.
func (m *Model) runImageBuild() tea.Cmd {
	o := m.pendingBuild
	m.pendingBuild = containers.BuildOptions{}
	cmd := m.services.containerEngine.BuildCommand(o)
	if err := m.servicesRunCommand(cmd, containerTabPrefix+"build:"+o.Dockerfile); err != nil {
		return m.servicesToast(toast.Errr, "Cannot build image", err.Error())
	}
	return nil
}

// buildDockerfileFromPalette picks the open file when it is a Dockerfile,
// else the Dockerfile at the workspace root.
func (m *Model) buildDockerfileFromPalette() tea.Cmd {
	if p := m.editor.Path(); p != "" && containers.IsDockerfileName(p) {
		return m.askImageBuild(p)
	}
	root, err := m.rootDir()
	if err != nil {
		return m.servicesToast(toast.Errr, "Cannot find the workspace folder", err.Error())
	}
	for _, name := range []string{"Dockerfile", "Containerfile"} {
		if p := filepath.Join(root, name); fileExists(p) {
			return m.askImageBuild(p)
		}
	}
	return m.askImageBuild(filepath.Join(root, "Dockerfile"))
}

// explorerFileMenuItemsFor adds Build Image… to the file menu of a Dockerfile.
func explorerFileMenuItemsFor(path string) []menu.Item {
	items := explorerFileMenuItems()
	if containers.IsDockerfileName(path) {
		items = append(items, menu.Item{Sep: true}, menu.Item{ID: "build-image", Title: "Build Image…", Icon: "▣"})
	}
	return items
}
