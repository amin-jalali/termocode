package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Container detail page (CTX-10): I on a container opens a read-only
// Markdown page with its state, exit code, restart count, health log,
// env names (values are never read into the page), mounts, labels and,
// while it runs, its processes (`top`).

// showContainerDetail reads inspect and top, then opens the page.
func (m *Model) showContainerDetail(c containers.Container) tea.Cmd {
	e := m.services.containerEngine
	id, name := c.ID, containerShownName(c)
	running := strings.EqualFold(c.State, "running")
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var d containers.Detail
		value, err := e.Inspect(ctx, "container", id)
		if err == nil {
			var raw []byte
			if raw, err = json.Marshal(value); err == nil {
				if _, isList := value.([]any); !isList {
					raw = append(append([]byte{'['}, raw...), ']')
				}
				d, err = containers.ParseDetail(raw)
			}
		}
		var top containers.TopTable
		topNote := "the container is not running"
		if err == nil && running {
			var terr error
			if top, terr = e.Top(ctx, id); terr != nil {
				topNote = "cannot read the processes: " + errLine(terr.Error())
			} else {
				topNote = ""
			}
		}
		return containerUIDoneMsg{apply: func(m *Model) tea.Cmd {
			if err != nil {
				return m.servicesToast(toast.Errr, "Cannot read "+name, errLine(err.Error()))
			}
			if err := m.openReadOnlyPage("details: "+name, containerDetailPage(d, top, topNote), "", "markdown"); err != nil {
				return m.servicesToast(toast.Errr, "Cannot open the detail page", err.Error())
			}
			return nil
		}}
	}
}

// containerDetailPage is the Markdown of the page. topNote replaces the
// process table when it is empty.
func containerDetailPage(d containers.Detail, top containers.TopTable, topNote string) string {
	var b strings.Builder
	cell := func(s string) string { return strings.ReplaceAll(sanitizeOutputLine(s), "|", `\|`) }
	name := d.Name
	if name == "" {
		name = shortImageID(d.ID)
	}
	fmt.Fprintf(&b, "# %s\n\n", sanitizeOutputLine(name))
	fmt.Fprintf(&b, "- ID: %s\n- Status: %s\n- Exit code: %d\n- Restarts: %d\n", sanitizeOutputLine(d.ID), sanitizeOutputLine(d.Status), d.ExitCode, d.RestartCount)

	b.WriteString("\n## Health\n\n")
	if d.Health.Status == "" {
		b.WriteString("No health check.\n")
	} else {
		fmt.Fprintf(&b, "- Status: %s\n- Failing streak: %d\n", sanitizeOutputLine(d.Health.Status), d.Health.FailingStreak)
		if len(d.Health.Log) > 0 {
			b.WriteString("\n| Start | Exit | Output |\n|---|---|---|\n")
			for _, p := range d.Health.Log {
				fmt.Fprintf(&b, "| %s | %d | %s |\n", cell(p.Start), p.ExitCode, cell(p.Output))
			}
		}
	}

	b.WriteString("\n## Processes\n\n")
	if len(top.Titles) == 0 || len(top.Rows) == 0 {
		if topNote == "" {
			topNote = "no processes"
		}
		b.WriteString(sanitizeOutputLine(topNote) + "\n")
	} else {
		titles := make([]string, len(top.Titles))
		for i, t := range top.Titles {
			titles[i] = cell(t)
		}
		b.WriteString("| " + strings.Join(titles, " | ") + " |\n|" + strings.Repeat("---|", len(titles)) + "\n")
		for _, r := range top.Rows {
			cells := make([]string, len(r))
			for i, c := range r {
				cells[i] = cell(c)
			}
			b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
		}
	}

	b.WriteString("\n## Environment names\n\n")
	if len(d.EnvNames) == 0 {
		b.WriteString("None.\n")
	} else {
		b.WriteString("Values are not shown.\n\n")
		for _, n := range d.EnvNames {
			fmt.Fprintf(&b, "- %s\n", sanitizeOutputLine(n))
		}
	}

	b.WriteString("\n## Mounts\n\n")
	if len(d.Mounts) == 0 {
		b.WriteString("None.\n")
	} else {
		b.WriteString("| Type | Source | Destination | Mode |\n|---|---|---|---|\n")
		for _, mt := range d.Mounts {
			src := mt.Source
			if mt.Name != "" && mt.Type == "volume" {
				src = mt.Name
			}
			mode := "read-only"
			if mt.RW {
				mode = "read-write"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", cell(mt.Type), cell(src), cell(mt.Destination), mode)
		}
	}

	b.WriteString("\n## Labels\n\n")
	if len(d.Labels) == 0 {
		b.WriteString("None.\n")
	} else {
		b.WriteString("| Key | Value |\n|---|---|\n")
		for _, l := range d.Labels {
			fmt.Fprintf(&b, "| %s | %s |\n", cell(l.Key), cell(l.Value))
		}
	}
	return b.String()
}
