package app

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

// CTX-33: compose extras — the resolved config page, a one-off
// `compose run`, `compose watch` and the env file picker.

func composeExtrasPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "services-compose-config", Title: "Containers: Show Resolved Compose Config"},
		{ID: "services-compose-run", Title: "Containers: Run One-Off Command in Service…"},
		{ID: "services-compose-watch", Title: "Containers: Watch Project (Rebuild on Change)"},
		{ID: "services-env-file", Title: "Containers: Choose Compose Env File…"},
	}
}

// composeConfigMsg carries the `compose config` text.
type composeConfigMsg struct {
	text string
	err  error
}

// dispatchComposeExtras runs one compose extras palette id.
func (m *Model) dispatchComposeExtras(id string, open tea.Cmd) (tea.Cmd, bool) {
	switch id {
	case "services-compose-config":
		return m.servicesAfterLoad(open, func(m *Model) tea.Cmd { return m.composeConfigPage() }), true
	case "services-compose-run":
		return m.servicesAfterLoad(open, func(m *Model) tea.Cmd { return m.askComposeRun() }), true
	case "services-compose-watch":
		return m.servicesAfterLoad(open, func(m *Model) tea.Cmd { return m.composeWatch() }), true
	case "services-env-file":
		return m.servicesAfterLoad(open, func(m *Model) tea.Cmd { return m.openComposeEnvFilePicker() }), true
	}
	return nil, false
}

func (m *Model) composeOrToast() (*services.Compose, tea.Cmd) {
	c := m.services.composeBackend()
	if c == nil {
		return nil, m.servicesToast(toast.Info, "No compose project", "Add compose.yaml to the workspace, or install docker.")
	}
	return c, nil
}

func (m *Model) composeConfigPage() tea.Cmd {
	c, t := m.composeOrToast()
	if c == nil {
		return t
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		text, err := c.ResolvedConfig(ctx)
		return composeConfigMsg{text: text, err: err}
	}
}

func (m *Model) onComposeConfig(msg composeConfigMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot read the compose config", errLine(msg.err.Error()))
	}
	if err := m.openReadOnlyPage("compose config", msg.text, "", "yaml"); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open the compose config", err.Error())
	}
	return nil
}

func (m *Model) askComposeRun() tea.Cmd {
	if c, t := m.composeOrToast(); c == nil {
		return t
	}
	first := ""
	if len(m.services.compose) > 0 {
		first = m.services.compose[0].Name + " "
	}
	m.prompt = prompt.New("Run one-off command", "Service and command (runs compose run --rm):", first)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindComposeRun
	return nil
}

// svcComposeRunLine splits "service command args…" like a shell.
func svcComposeRunLine(line string) (string, []string, error) {
	words, err := svcShellWords(line)
	if err != nil {
		return "", nil, err
	}
	if len(words) == 0 || strings.HasPrefix(words[0], "-") {
		return "", nil, errors.New("start with a service name")
	}
	return words[0], words[1:], nil
}

// svcShellWords splits a line with single and double quotes and
// backslash escapes.
func svcShellWords(line string) ([]string, error) {
	var words []string
	var cur strings.Builder
	in, quote := false, byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case quote == '"':
			switch {
			case c == '"':
				quote = 0
			case c == '\\' && i+1 < len(line):
				i++
				cur.WriteByte(line[i])
			default:
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, in = c, true
		case c == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
			in = true
		case c == ' ' || c == '\t':
			if in {
				words = append(words, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteByte(c)
			in = true
		}
	}
	if quote != 0 {
		return nil, errors.New("a quote is not closed")
	}
	if in {
		words = append(words, cur.String())
	}
	return words, nil
}

func (m *Model) onComposeRunPrompt(value string) tea.Cmd {
	svc, args, err := svcComposeRunLine(value)
	if err != nil {
		return m.servicesToast(toast.Info, "Not run", err.Error())
	}
	c, t := m.composeOrToast()
	if c == nil {
		return t
	}
	cmd, err := c.RunOneOff(svc, args)
	if err != nil {
		return m.servicesToast(toast.Info, "Not run", err.Error())
	}
	if err := m.servicesRunCommand(cmd, "compose-run:"+svc); err != nil {
		return m.servicesToast(toast.Errr, "Cannot run", err.Error())
	}
	return nil
}

func (m *Model) composeWatch() tea.Cmd {
	c, t := m.composeOrToast()
	if c == nil {
		return t
	}
	const key = "compose-watch"
	if _, ok := m.svcTabRunning(key); ok {
		m.servicesFocusTab(key)
		return nil
	}
	if err := m.servicesRunCommand(c.WatchCommand(), key); err != nil {
		return m.servicesToast(toast.Errr, "Cannot start compose watch", err.Error())
	}
	return nil
}

func (m *Model) openComposeEnvFilePicker() tea.Cmd {
	st := m.services
	files := services.FindEnvFiles(st.root)
	items := []picker.Item{{ID: "svc:envfile:", Title: "Compose default (.env)", Hint: svcEnvFileHint(st.envFile == "")}}
	for _, f := range files {
		items = append(items, picker.Item{ID: "svc:envfile:" + f, Title: f, Hint: svcEnvFileHint(st.envFile == f)})
	}
	m.openTasksPickerItems(" Compose Env File ", items)
	return nil
}

func svcEnvFileHint(current bool) string {
	if current {
		return "in use"
	}
	return ""
}

// onComposeEnvFile keeps the env file for this session's compose calls.
func (m *Model) onComposeEnvFile(name string) tea.Cmd {
	m.services.envFile = name
	m.services.cfgCache = &svcConfigCache{}
	label := name
	if label == "" {
		label = "compose default (.env)"
	}
	return tea.Batch(m.servicesToast(toast.Info, "Compose env file", label), m.servicesAction("refresh"))
}
