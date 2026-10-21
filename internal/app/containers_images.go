package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

type imagePageMsg struct {
	ref, title, text string
	err              error
}

type hostActionDoneMsg struct {
	operation, section, target string
	err                        error
}

func (m *Model) selectedImage() (containers.Image, string, bool) {
	r, ok := m.servicesCurrentRow()
	if !ok || r.kind != svcRowHostItem || r.host != hostImage || r.idx < 0 || r.idx >= len(m.services.imageRows) {
		return containers.Image{}, "", false
	}
	im := m.services.imageRows[r.idx]
	ref := r.imageRef
	if ref == "" || ref == "<none>:<none>" {
		ref = strings.TrimPrefix(im.ID, "sha256:")
	}
	return im, ref, true
}

func (m *Model) imagePull(ref string) tea.Cmd {
	if strings.TrimSpace(ref) == "" {
		return nil
	}
	if strings.HasPrefix(strings.TrimSpace(ref), "-") {
		return m.servicesToast(toast.Info, "Image reference cannot start with a dash", "Enter an image name or reference.")
	}
	cmd := m.services.containerEngine.PullCommand(strings.TrimSpace(ref))
	key := containerTabPrefix + "pull:" + strings.TrimSpace(ref)
	if err := m.servicesRunCommand(cmd, key); err != nil {
		return m.servicesToast(toast.Errr, "Cannot pull image", err.Error())
	}
	return nil
}

func (m *Model) askImagePull(ref string) {
	m.prompt = prompt.New("Pull image", "Image reference:", ref)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindContainerPull
}

func (m *Model) imagePush(ref string) tea.Cmd {
	cmd := m.services.containerEngine.PushCommand(ref)
	if err := m.servicesRunCommand(cmd, containerTabPrefix+"push:"+ref); err != nil {
		return m.servicesToast(toast.Errr, "Cannot push image", err.Error())
	}
	return nil
}

func (m *Model) askImagePush(ref string) {
	cmd := m.services.containerEngine.PushCommand(ref)
	m.confirm = confirm.New("Push image?", "This sends the image to its registry.\n\nCommand: "+svcShellLine(cmd.Argv), []confirm.Button{
		{ID: "cancel", Title: "Cancel"}, {ID: "push", Title: "Push", Style: confirm.StylePrimary},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerImagePush
	m.pendingImageRef = ref
}

func (m *Model) askImageTag(ref string) {
	m.pendingImageRef = ref
	m.prompt = prompt.New("Tag image", "New image reference:", ref+"-copy")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindContainerTag
}

func (m *Model) askImageRemove(im containers.Image) tea.Cmd {
	return m.askImageRemoveRef(im, im.ID)
}

func (m *Model) askImageRemoveRef(im containers.Image, ref string) tea.Cmd {
	var users []string
	id := strings.TrimPrefix(im.ID, "sha256:")
	tags := make(map[string]bool, len(im.RepoTags))
	for _, tag := range im.RepoTags {
		tags[tag] = true
	}
	for _, c := range m.services.containerRows {
		if strings.TrimPrefix(c.ImageID, "sha256:") == id || tags[c.Image] {
			users = append(users, containerDisplayName(c, c.Project))
		}
	}
	if len(users) > 0 {
		m.confirm = confirm.New("Image is in use", "This image stays while containers use it.\n\nContainers: "+strings.Join(users, ", "), []confirm.Button{{ID: "cancel", Title: "Close"}})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
		m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = nil, "", ""
		return nil
	}
	cmd := []string{m.services.containerEngine.Binary, "rmi", ref}
	m.confirm = confirm.New("Remove image?", "This removes image reference "+ref+".\n\nCommand: "+svcShellLine(cmd), []confirm.Button{
		{ID: "cancel", Title: "Cancel"}, {ID: "remove", Title: "Remove", Style: confirm.StyleDestructive},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
	m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = cmd, "remove-image", "images"
	return nil
}

func (m *Model) askImagePrune() {
	args := []string{m.services.containerEngine.Binary, "image", "prune", "-f"}
	count := 0
	for _, image := range m.services.imageRows {
		if len(image.RepoTags) == 0 || (len(image.RepoTags) == 1 && image.RepoTags[0] == "<none>:<none>") {
			count++
		}
	}
	body := fmt.Sprintf("Only unused dangling images are removed. Tagged images are kept. Current list: %d dangling.", count)
	m.confirm = confirm.New("Remove dangling images?", body+"\n\nCommand: "+svcShellLine(args), []confirm.Button{
		{ID: "cancel", Title: "Cancel"}, {ID: "prune", Title: "Prune", Style: confirm.StyleDestructive},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
	m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = args, "prune-images", "images"
}

func (m *Model) askImageRun(ref string) {
	m.pendingImageRef = ref
	m.prompt = prompt.New("Run image", "Container name (optional):", "")
	m.prompt.SetAllowEmpty(true)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindContainerRunName
}

func (m *Model) onContainerImagePrompt(kind promptKindEnum, value string) tea.Cmd {
	switch kind {
	case promptKindContainerStorageName:
		section := m.pendingHostTarget
		m.pendingHostTarget = ""
		return m.storageCreate(value, strings.TrimSuffix(section, "s"))
	case promptKindContainerPull:
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		return m.imagePull(value)
	case promptKindContainerTag:
		ref := m.pendingImageRef
		if ref == "" {
			_, ref, _ = m.selectedImage()
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		if strings.HasPrefix(value, "-") {
			return m.servicesToast(toast.Info, "Image reference cannot start with a dash", "Enter a new image name or reference.")
		}
		for _, image := range m.services.imageRows {
			for _, existing := range image.RepoTags {
				if existing == value && existing != ref {
					args := []string{m.services.containerEngine.Binary, "tag", ref, value}
					m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = args, "tag", "images"
					m.confirm = confirm.New("Replace image tag?", "This moves the existing tag "+value+" to the selected image.\n\nCommand: "+svcShellLine(args), []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "tag", Title: "Replace tag", Style: confirm.StyleDestructive}})
					m.confirm.SetSize(m.w, m.h)
					m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
					return nil
				}
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		e := m.services.containerEngine
		return func() tea.Msg {
			defer cancel()
			return hostActionDoneMsg{operation: "tag", section: "images", target: value, err: e.Tag(ctx, ref, strings.TrimSpace(value))}
		}
	case promptKindContainerRunName:
		m.pendingRunName = strings.TrimSpace(value)
		if strings.HasPrefix(m.pendingRunName, "-") {
			return m.servicesToast(toast.Info, "Container name cannot start with a dash", "Choose a name that starts with a letter or number.")
		}
		m.pendingRunPorts, m.pendingRunEnv = nil, nil
		m.prompt = prompt.New("Run image", "Port to publish (empty to finish)", "")
		m.prompt.SetAllowEmpty(true)
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen, m.promptKind = true, promptKindContainerRunPorts
	case promptKindContainerRunPorts:
		value = strings.TrimSpace(value)
		if value != "" {
			if strings.HasPrefix(value, "-") {
				return m.servicesToast(toast.Info, "Port cannot start with a dash", "Enter a port mapping.")
			}
			m.pendingRunPorts = append(m.pendingRunPorts, value)
			m.prompt = prompt.New("Run image", "Port to publish (empty to finish)", "")
			m.prompt.SetAllowEmpty(true)
			m.prompt.SetNote("Ports so far: " + strings.Join(m.pendingRunPorts, ", "))
			m.prompt.SetSize(m.w, m.h)
			m.promptOpen, m.promptKind = true, promptKindContainerRunPorts
			return nil
		}
		m.prompt = prompt.New("Run image", "Environment variable (KEY=value, empty to finish)", "")
		m.prompt.SetAllowEmpty(true)
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen, m.promptKind = true, promptKindContainerRunEnv
	case promptKindContainerRunEnv:
		entry := strings.TrimSpace(value)
		if entry != "" {
			key, _, ok := strings.Cut(entry, "=")
			if !ok || key == "" || strings.HasPrefix(key, "-") {
				return m.servicesToast(toast.Info, "Environment entries must use KEY=value", "Fix the entry before running the image.")
			}
			m.pendingRunEnv = append(m.pendingRunEnv, entry)
			m.prompt = prompt.New("Run image", "Environment variable (KEY=value, empty to finish)", "")
			m.prompt.SetAllowEmpty(true)
			m.prompt.SetNote("Environment so far: " + strings.Join(m.pendingRunEnv, ", "))
			m.prompt.SetSize(m.w, m.h)
			m.promptOpen, m.promptKind = true, promptKindContainerRunEnv
			return nil
		}
		args := m.services.containerEngine.RunCommand(m.pendingImageRef, m.pendingRunName, m.pendingRunPorts, m.pendingRunEnv).Argv
		m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = args, "run-image", "images"
		m.confirm = confirm.New("Run image?", "Review the command before it runs.\n\nCommand: "+svcShellLine(args), []confirm.Button{
			{ID: "cancel", Title: "Cancel"}, {ID: "run", Title: "Run", Style: confirm.StylePrimary},
		})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
		m.promptOpen = false
	}
	return nil
}

func promptList(value string) []string {
	var out []string
	separator := ","
	if strings.ContainsAny(value, "\r\n") {
		separator = "\n"
	}
	for _, item := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), separator) {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func promptLines(value string) []string {
	var out []string
	for _, item := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (m *Model) imageHistory(ref string) tea.Cmd {
	e := m.services.containerEngine
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		rows, err := e.History(ctx, ref)
		b, marshalErr := json.MarshalIndent(rows, "", "  ")
		if err == nil {
			err = marshalErr
		}
		return imagePageMsg{ref: ref, title: "history", text: string(b), err: err}
	}
}

func (m *Model) imageInspect(ref string) tea.Cmd {
	e := m.services.containerEngine
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		value, err := e.Inspect(ctx, "image", ref)
		b, marshalErr := json.MarshalIndent(value, "", "  ")
		if err == nil {
			err = marshalErr
		}
		return imagePageMsg{ref: ref, title: "inspect", text: string(b), err: err}
	}
}

func (m *Model) onImagePage(msg imagePageMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot open image "+msg.title, errLine(msg.err.Error()))
	}
	if err := m.openReadOnlyPage(msg.title+": "+msg.ref, msg.text, "", "json"); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open image page", err.Error())
	}
	return nil
}

func (m *Model) runHostAction(id string) tea.Cmd {
	if id == "cancel" {
		m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = nil, "", ""
		return nil
	}
	args := append([]string(nil), m.pendingHostArgs...)
	operation, section := m.pendingHostOperation, m.pendingHostTarget
	m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = nil, "", ""
	if operation == "default-context" {
		return m.runContextDefault()
	}
	if operation == "run-image" {
		cmd := services.Command{Label: "run: " + m.pendingImageRef, Argv: args}
		if err := m.servicesRunCommand(cmd, containerTabPrefix+"run:"+m.pendingImageRef); err != nil {
			return m.servicesToast(toast.Errr, "Cannot run image", err.Error())
		}
		return nil
	}
	execFor := m.services.hostExec()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, stderr, code, err := execFor.Capture(ctx, "", args...)
		if err == nil && code != 0 {
			err = fmt.Errorf("%s", errLine(stderr))
		}
		return hostActionDoneMsg{operation: operation, section: section, target: strings.Join(args[1:], " "), err: err}
	}
}

func (m *Model) onHostActionDone(msg hostActionDoneMsg) tea.Cmd {
	if msg.operation == "default-context" {
		return m.onContextDefaultDone(msg)
	}
	if msg.err != nil {
		title := "Could not complete container action"
		switch msg.operation {
		case "tag":
			title = "Could not tag image"
		case "remove-image":
			title = "Could not remove image"
		case "prune-images":
			title = "Could not prune images"
		case "remove-storage":
			title = "Could not remove item"
		case "prune-networks":
			title = "Could not prune networks"
		case "prune-volumes":
			title = "Could not prune volumes"
		case "create-network":
			title = "Could not create network"
		case "create-volume":
			title = "Could not create volume"
		}
		return m.servicesToast(toast.Errr, title, errLine(msg.err.Error()))
	}
	if msg.operation == "tag" {
		return tea.Batch(m.servicesToast(toast.Info, "Image tagged", msg.target), m.containersRefresh("images"))
	}
	return m.containersRefresh(msg.section)
}
