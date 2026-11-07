package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Logs options (CTX-11): L on a container opens a picker to follow its
// logs with a tail or since limit and timestamps on or off, to save the
// last lines to a file in Downloads, or to search them.

const (
	pickerKindContainerLogs       pickerKindEnum = 4905
	promptKindContainerLogsSince  promptKindEnum = 4906
	promptKindContainerLogsTail   promptKindEnum = 4907
	promptKindContainerLogsSearch promptKindEnum = 4908
)

// containerLogsCaptureTail is how many lines save and search read.
const containerLogsCaptureTail = 10000

// containerLogsState is the container of the open logs picker.
type containerLogsState struct {
	c  containers.Container
	ts bool // timestamps
}

// containerLogsItems are the picker rows; ts is the timestamps toggle.
func containerLogsItems(ts bool) []picker.Item {
	tsWord := "off"
	if ts {
		tsWord = "on"
	}
	return []picker.Item{
		{ID: "follow:200", Title: "Follow, last 200 lines"},
		{ID: "follow:1000", Title: "Follow, last 1000 lines"},
		{ID: "follow:all", Title: "Follow, all lines"},
		{ID: "since:10m", Title: "Follow, last 10 minutes"},
		{ID: "since:1h", Title: "Follow, last hour"},
		{ID: "since:custom", Title: "Follow since…", Hint: "a duration (30m) or a date"},
		{ID: "tail:custom", Title: "Follow, last N lines…"},
		{ID: "ts", Title: "Timestamps: " + tsWord, Hint: "Enter switches"},
		{ID: "save", Title: "Save logs to a file", Hint: "last " + strconv.Itoa(containerLogsCaptureTail) + " lines, in Downloads"},
		{ID: "search", Title: "Search the logs…", Hint: "last " + strconv.Itoa(containerLogsCaptureTail) + " lines"},
	}
}

// containerLogsChoice is the follow options of a fixed picker row.
func containerLogsChoice(id string, ts bool) (containers.LogsOptions, bool) {
	o := containers.LogsOptions{Timestamps: ts}
	switch {
	case id == "follow:all":
		o.Tail = -1
	case strings.HasPrefix(id, "follow:"):
		n, err := strconv.Atoi(strings.TrimPrefix(id, "follow:"))
		if err != nil {
			return o, false
		}
		o.Tail = n
	case strings.HasPrefix(id, "since:") && id != "since:custom":
		o.Since, o.Tail = strings.TrimPrefix(id, "since:"), -1
	default:
		return o, false
	}
	return o, true
}

// filterLogLines keeps the lines that contain term (any case).
func filterLogLines(text, term string) []string {
	term = strings.ToLower(term)
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(strings.ToLower(l), term) {
			out = append(out, l)
		}
	}
	return out
}

// containerLogsFileName is a safe local file name for saved logs.
func containerLogsFileName(name string, t time.Time) string {
	safe := strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == '.' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, name)
	safe = strings.Trim(safe, ".-")
	if safe == "" {
		safe = "container"
	}
	return safe + "-logs-" + t.Format("20060102-150405") + ".log"
}

// openContainerLogsOptions opens the picker for c.
func (m *Model) openContainerLogsOptions(c containers.Container) {
	ts := m.services.logsOpts.ts
	if m.services.logsOpts.c.ID != c.ID {
		ts = false
	}
	m.services.logsOpts = containerLogsState{c: c, ts: ts}
	m.picker = picker.NewItems(" Logs: "+sanitizeOutputLine(containerShownName(c))+" ", containerLogsItems(ts))
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen, m.pickerKind = true, pickerKindContainerLogs
}

// onContainerLogsPicked handles one picker row.
func (m *Model) onContainerLogsPicked(id string) tea.Cmd {
	st := &m.services.logsOpts
	if st.c.ID == "" {
		return nil
	}
	if o, ok := containerLogsChoice(id, st.ts); ok {
		return m.followContainerLogs(st.c, o)
	}
	ask := func(kind promptKindEnum, title, label string) {
		m.prompt = prompt.New(title, label, "")
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen, m.promptKind = true, kind
	}
	switch id {
	case "ts":
		st.ts = !st.ts
		m.openContainerLogsOptions(st.c)
	case "since:custom":
		ask(promptKindContainerLogsSince, "Follow logs since", "Duration (30m, 2h) or date (2026-11-07T10:00:00Z):")
	case "tail:custom":
		ask(promptKindContainerLogsTail, "Follow logs", "Last how many lines:")
	case "search":
		ask(promptKindContainerLogsSearch, "Search logs", "Text to find:")
	case "save":
		return m.saveContainerLogs(st.c)
	}
	return nil
}

// onContainerLogsPrompt handles the since, tail and search prompts.
func (m *Model) onContainerLogsPrompt(kind promptKindEnum, value string) tea.Cmd {
	st := m.services.logsOpts
	if st.c.ID == "" {
		return nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	switch kind {
	case promptKindContainerLogsSince:
		return m.followContainerLogs(st.c, containers.LogsOptions{Since: value, Tail: -1, Timestamps: st.ts})
	case promptKindContainerLogsTail:
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return m.servicesToast(toast.Info, "Type a number of lines", "for example 500")
		}
		return m.followContainerLogs(st.c, containers.LogsOptions{Tail: n, Timestamps: st.ts})
	case promptKindContainerLogsSearch:
		return m.searchContainerLogs(st.c, value)
	}
	return nil
}

// followContainerLogs opens a terminal tab that follows the logs.
func (m *Model) followContainerLogs(c containers.Container, o containers.LogsOptions) tea.Cmd {
	e := m.services.containerEngine
	cmd, err := e.LogsFollowWith(c.ID, o)
	if err != nil {
		return m.servicesToast(toast.Info, "Cannot follow logs", err.Error())
	}
	key := containerTabPrefix + "logsx:" + c.ID + ":" + strings.Join(cmd.Argv[1:], " ")
	if m.servicesFocusTab(key) {
		return nil
	}
	cmd.Label = "logs: " + containerShownName(c)
	if err := m.servicesRunCommand(cmd, key); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open logs", err.Error())
	}
	return nil
}

// saveContainerLogs writes the last lines into a new file in Downloads.
func (m *Model) saveContainerLogs(c containers.Container) tea.Cmd {
	e := m.services.containerEngine
	name := containerShownName(c)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		text, err := e.Logs(ctx, c.ID, containerLogsCaptureTail, "")
		var dst string
		if err == nil {
			var dir string
			if dir, err = downloadsDir(); err == nil {
				dst = filepath.Join(dir, containerLogsFileName(name, time.Now()))
				var f *os.File
				f, err = os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
				if errors.Is(err, fs.ErrExist) {
					err = fmt.Errorf("%s is already there; try again in a second", dst)
				}
				if err == nil {
					_, werr := f.WriteString(text + "\n")
					err = errors.Join(werr, f.Close())
				}
			}
		}
		return containerUIDoneMsg{apply: func(m *Model) tea.Cmd {
			if err != nil {
				return m.servicesToast(toast.Errr, "Cannot save the logs of "+name, errLine(err.Error()))
			}
			return m.servicesToast(toast.Info, "Saved the logs of "+name, dst)
		}}
	}
}

// searchContainerLogs opens a read-only page with the matching lines.
func (m *Model) searchContainerLogs(c containers.Container, term string) tea.Cmd {
	e := m.services.containerEngine
	name := containerShownName(c)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		text, err := e.Logs(ctx, c.ID, containerLogsCaptureTail, "")
		return containerUIDoneMsg{apply: func(m *Model) tea.Cmd {
			if err != nil {
				return m.servicesToast(toast.Errr, "Cannot read the logs of "+name, errLine(err.Error()))
			}
			lines := filterLogLines(text, term)
			if len(lines) == 0 {
				return m.servicesToast(toast.Info, "No log line has "+strconv.Quote(term), "searched the last "+strconv.Itoa(containerLogsCaptureTail)+" lines")
			}
			if err := m.openReadOnlyPage("logs: "+name+" /"+term, strings.Join(lines, "\n"), "", "log"); err != nil {
				return m.servicesToast(toast.Errr, "Cannot open the search page", err.Error())
			}
			return m.servicesToast(toast.Info, fmt.Sprintf("%d lines have %q", len(lines), term), "in the last "+strconv.Itoa(containerLogsCaptureTail)+" lines")
		}}
	}
}
