package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Compose: pull, build, up with options, scale, down --volumes (CTX-8) ─
//
// Pull, build, up and scale are visible jobs in the "services: up" style
// terminal tabs. Down with volumes deletes data, so it asks with a
// Cancel-first card that names the volumes.

// confirmKindServicesDownVolumes is the `down --volumes` card (CTX-8 range).
const confirmKindServicesDownVolumes confirmKindEnum = 4808

// svcMaxScale is the largest replica count the scale prompt takes.
const svcMaxScale = 100

// composeMoreMenuItems are the compose-only items of a service row menu.
func composeMoreMenuItems() []menu.Item {
	return []menu.Item{
		{ID: "pull", Title: "Pull image", Icon: "↓"},
		{ID: "build", Title: "Build", Icon: "⚒"},
		{ID: "up-options", Title: "Up with options…", Icon: "▸"},
		{ID: "scale", Title: "Scale…", Icon: "#"},
	}
}

// composeUpOptions maps the options menu ids to their flags.
var composeUpOptions = []struct {
	id, title string
	o         services.UpOptions
}{
	{"up-build", "up --build", services.UpOptions{Build: true}},
	{"up-force", "up --force-recreate", services.UpOptions{ForceRecreate: true}},
	{"up-nodeps", "up --no-deps", services.UpOptions{NoDeps: true}},
	{"up-build-force", "up --build --force-recreate", services.UpOptions{Build: true, ForceRecreate: true}},
	{"up-pull", "up --pull always", services.UpOptions{PullAlways: true}},
}

// openComposeUpOptionsMenu shows the up options for the menu row.
func (m *Model) openComposeUpOptionsMenu() {
	items := make([]menu.Item, 0, len(composeUpOptions))
	for _, o := range composeUpOptions {
		items = append(items, menu.Item{ID: o.id, Title: o.title})
	}
	x, y, _, _ := m.menu.Bounds() // where the row menu was
	m.menu = menu.New(items, x, y)
	m.menu.SetScreenSize(m.w, m.h)
	m.menuOpen = true
	m.menuKind = menuKindServices
}

// handleComposeMoreMenu runs a CTX-8 menu id on the row; false = not ours.
func (m *Model) handleComposeMoreMenu(id string, r svcRow) (tea.Cmd, bool) {
	c := m.services.composeBackend()
	switch id {
	case "pull", "build", "scale", "up-options", "down-volumes":
	default:
		for _, o := range composeUpOptions {
			if o.id == id {
				return m.composeUpWith(c, r.name, o.o), true
			}
		}
		return nil, false
	}
	if c == nil {
		return m.servicesToast(toast.Info, "Docker Compose is not available", m.services.composeProblem().Message()), true
	}
	switch id {
	case "pull":
		if err := m.servicesRunCommand(c.Pull([]string{r.name}), "services:pull"); err != nil {
			return m.servicesToast(toast.Errr, "Cannot pull "+r.name, err.Error()), true
		}
	case "build":
		if err := m.servicesRunCommand(c.Build([]string{r.name}), "services:build"); err != nil {
			return m.servicesToast(toast.Errr, "Cannot build "+r.name, err.Error()), true
		}
	case "up-options":
		m.openComposeUpOptionsMenu()
	case "scale":
		m.askComposeScale(r.name)
	case "down-volumes":
		m.servicesAskDownVolumes()
	}
	return nil, true
}

func (m *Model) composeUpWith(c *services.Compose, name string, o services.UpOptions) tea.Cmd {
	if c == nil {
		return m.servicesToast(toast.Info, "Docker Compose is not available", m.services.composeProblem().Message())
	}
	if _, running := m.svcTabRunning(svcTabUp); running {
		return m.servicesToast(toast.Info, "docker compose up is still running", "see the services: up tab")
	}
	if err := m.servicesRunCommand(c.UpWith([]string{name}, o), svcTabUp); err != nil {
		return m.servicesToast(toast.Errr, "Cannot start services", err.Error())
	}
	st := m.services
	st.busy[svcKey(svcSecCompose, name)] = services.Starting
	st.startedByUs[name] = true
	st.touch()
	m.servicesSaveSession()
	return nil
}

// askComposeScale asks for the replica count of a service.
func (m *Model) askComposeScale(name string) {
	m.services.scaleName = name
	m.prompt = prompt.New("Scale "+name, "Number of containers (0–"+strconv.Itoa(svcMaxScale)+"):", "2")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindComposeScale
}

// onComposeScalePrompt runs the scale job for a valid number.
func (m *Model) onComposeScalePrompt(value string) tea.Cmd {
	name := m.services.scaleName
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 || n > svcMaxScale {
		return m.servicesToast(toast.Warn, "Scale needs a whole number from 0 to "+strconv.Itoa(svcMaxScale), "nothing was changed")
	}
	m.services.scaleName = ""
	c := m.services.composeBackend()
	if c == nil || name == "" {
		return m.servicesToast(toast.Info, "Docker Compose is not available", "")
	}
	if err := m.servicesRunCommand(c.Scale(name, n), svcTabUp); err != nil {
		return m.servicesToast(toast.Errr, "Cannot scale "+name, err.Error())
	}
	return nil
}

// servicesAskDownVolumes asks before `compose down --volumes`.
func (m *Model) servicesAskDownVolumes() {
	body := "This stops and removes this project's containers and networks, and deletes its volumes."
	if v := m.services.volumes; len(v) > 0 {
		body += "\n\nVolumes: " + strings.Join(v, ", ")
	}
	body += "\n\nThe data in the volumes is lost. This cannot be undone."
	m.confirm = confirm.New("Remove containers and volumes?", body,
		[]confirm.Button{{ID: "cancel", Title: "Cancel"},
			{ID: "down-volumes", Title: "Remove with volumes", Style: confirm.StyleDestructive}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindServicesDownVolumes
}

// answerDownVolumes runs the card's answer.
func (m *Model) answerDownVolumes(id string) tea.Cmd {
	if id != "down-volumes" {
		return nil
	}
	st := m.services
	c := st.composeBackend()
	if c == nil {
		return nil
	}
	var keys []string
	for _, s := range st.compose {
		k := svcKey(svcSecCompose, s.Name)
		st.busy[k] = services.Stopping
		keys = append(keys, k)
	}
	st.touch()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return servicesActionDoneMsg{verb: "down", keys: keys, err: c.DownVolumes(ctx)}
	}
}
