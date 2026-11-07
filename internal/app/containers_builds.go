package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// CTX-26: the Builds page (history, builders, cache usage), one build's
// detail, the build cache prune card and the Dockerfile build checks.

func containersBuildsPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "containers-builds", Title: "Containers: Show Builds (History, Builders, Cache)"},
		{ID: "containers-build-detail", Title: "Containers: Show Build Detail…"},
		{ID: "containers-build-cache-prune", Title: "Containers: Prune Build Cache…"},
		{ID: "containers-build-check", Title: "Containers: Check Dockerfile (Build Checks)"},
	}
}

func (m *Model) dispatchContainersBuilds(id string) (tea.Cmd, bool) {
	var f func(m *Model) tea.Cmd
	switch id {
	case "containers-builds":
		f = func(m *Model) tea.Cmd { return m.containersBuildsPage() }
	case "containers-build-detail":
		f = func(m *Model) tea.Cmd { return m.containersBuildDetailPicker() }
	case "containers-build-cache-prune":
		f = func(m *Model) tea.Cmd { return m.askBuildCachePrune() }
	case "containers-build-check":
		f = func(m *Model) tea.Cmd { return m.containersBuildCheck() }
	default:
		return nil, false
	}
	return m.servicesAfterLoad(m.revealServicesView(false), f), true
}

// buildsPageText is the Builds page: history (newest first), builders
// and the build cache row. A nil error with no rows says "none".
func buildsPageText(hist []containers.BuildRecord, histErr error, builders []containers.Builder, buildersErr error, cache containers.BuildCacheUsage, cacheErr error, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	var b strings.Builder
	b.WriteString("Build history\n")
	switch {
	case histErr != nil:
		b.WriteString("  " + errLine(histErr.Error()) + "\n")
	case len(hist) == 0:
		b.WriteString("  No builds yet.\n")
	default:
		h := append([]containers.BuildRecord(nil), hist...)
		sort.SliceStable(h, func(i, j int) bool { return h[i].Created.After(h[j].Created) })
		for _, r := range h {
			line := "  " + r.Ref + "  " + orDashText(r.Name)
			if !r.Created.IsZero() {
				line += "  " + r.Created.In(loc).Format("2006-01-02 15:04")
			}
			if r.Duration != "" {
				line += "  " + r.Duration
			}
			if r.Steps > 0 {
				line += fmt.Sprintf("  %d/%d cached", r.Cached, r.Steps)
			}
			if r.Failed() {
				reason := r.Error
				if reason == "" {
					reason = strings.ToLower(r.Status)
				}
				line += "  failed: " + reason
			} else if r.Status != "" {
				line += "  " + strings.ToLower(r.Status)
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\nBuilders\n")
	switch {
	case buildersErr != nil:
		b.WriteString("  " + errLine(buildersErr.Error()) + "\n")
	case len(builders) == 0:
		b.WriteString("  No builders.\n")
	default:
		for _, bl := range builders {
			mark := " "
			if bl.Current {
				mark = "*"
			}
			line := "  " + mark + " " + bl.Name + "  " + orDashText(bl.Driver) + "  " + orDashText(bl.Status)
			if len(bl.Platforms) > 0 {
				line += "  " + strings.Join(bl.Platforms, ", ")
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\nBuild cache\n")
	if cacheErr != nil {
		b.WriteString("  " + errLine(cacheErr.Error()) + "\n")
	} else {
		b.WriteString(fmt.Sprintf("  %d entries (%d in use)  size %s  reclaimable %s\n", cache.Count, cache.Active, orDashText(cache.Size), orDashText(cache.Reclaimable)))
	}
	b.WriteString("\nDetail of one build: Containers: Show Build Detail…  Prune: Containers: Prune Build Cache…\n")
	return b.String()
}

func orDashText(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// buildCachePruneBody is the prune card text.
func buildCachePruneBody(u containers.BuildCacheUsage, command string) string {
	return fmt.Sprintf("The build cache has %d entries (%s); %s can be reclaimed.\n\nPrune unused removes the cache no build uses now. Prune all removes every entry: the next builds start cold.\n\nCommand: %s",
		u.Count, orDashText(u.Size), orDashText(u.Reclaimable), command)
}

func (m *Model) containersBuildsPage() tea.Cmd {
	e, t, ok := m.containersEngineOrToast()
	if !ok {
		return t
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		hist, hErr := e.BuildHistory(ctx)
		builders, bErr := e.Builders(ctx)
		cache, cErr := e.BuildCache(ctx)
		return containersPageMsg{title: "containers builds", text: buildsPageText(hist, hErr, builders, bErr, cache, cErr, time.Local)}
	}
}

// buildsListMsg carries the history for the detail picker.
type buildsListMsg struct {
	rows []containers.BuildRecord
	err  error
}

func (m *Model) containersBuildDetailPicker() tea.Cmd {
	e, t, ok := m.containersEngineOrToast()
	if !ok {
		return t
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rows, err := e.BuildHistory(ctx)
		return buildsListMsg{rows: rows, err: err}
	}
}

func (m *Model) onBuildsList(msg buildsListMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot list builds", errLine(msg.err.Error()))
	}
	if len(msg.rows) == 0 {
		return m.servicesToast(toast.Info, "No builds yet", "Build an image first.")
	}
	rows := append([]containers.BuildRecord(nil), msg.rows...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Created.After(rows[j].Created) })
	items := make([]picker.Item, 0, len(rows))
	for _, r := range rows {
		hint := strings.ToLower(r.Status)
		if r.Failed() {
			hint = "failed"
		}
		items = append(items, picker.Item{ID: "ctx-x:build:" + r.Ref, Title: orDashText(r.Name) + "  " + r.Ref, Hint: hint})
	}
	m.openTasksPickerItems(" Build Detail ", items)
	return nil
}

// openBuildDetail shows one build's detail in a terminal tab.
func (m *Model) openBuildDetail(ref string) tea.Cmd {
	e, t, ok := m.containersEngineOrToast()
	if !ok {
		return t
	}
	cmd, err := e.BuildDetailCommand(ref)
	if err != nil {
		return m.servicesToast(toast.Info, "Not shown", err.Error())
	}
	if err := m.servicesRunCommand(cmd, containerTabPrefix+"build:"+ref); err != nil {
		return m.servicesToast(toast.Errr, "Cannot show the build", err.Error())
	}
	return nil
}

// buildCacheUsageMsg starts the prune card with the current usage.
type buildCacheUsageMsg struct {
	usage containers.BuildCacheUsage
	err   error
}

func (m *Model) askBuildCachePrune() tea.Cmd {
	e, t, ok := m.containersEngineOrToast()
	if !ok {
		return t
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		u, err := e.BuildCache(ctx)
		return buildCacheUsageMsg{usage: u, err: err}
	}
}

func (m *Model) onBuildCacheUsage(msg buildCacheUsageMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot read the build cache", errLine(msg.err.Error()))
	}
	e := m.services.containerEngine
	unused := e.BuildCachePruneArgs(false)
	all := e.BuildCachePruneArgs(true)
	jobs := map[string]containersExtraJob{
		"prune": {label: "prune build cache", args: unused, timeout: 5 * time.Minute},
	}
	buttons := []confirm.Button{{ID: "prune", Title: "Prune unused", Style: confirm.StyleDestructive}}
	if strings.Join(all, " ") != strings.Join(unused, " ") {
		jobs["prune-all"] = containersExtraJob{label: "prune all build cache", args: all, timeout: 5 * time.Minute}
		buttons = append(buttons, confirm.Button{ID: "prune-all", Title: "Prune all", Style: confirm.StyleDestructive})
	}
	return m.askContainersExtra("Prune build cache?", buildCachePruneBody(msg.usage, m.containersCommandLine(unused)), buttons, jobs)
}

// containersBuildCheck runs the build checks of the workspace Dockerfile
// in a terminal tab (no image is built).
func (m *Model) containersBuildCheck() tea.Cmd {
	e, t, ok := m.containersEngineOrToast()
	if !ok {
		return t
	}
	cmd := e.BuildCheckCommand(m.services.root, "")
	if err := m.servicesRunCommand(cmd, containerTabPrefix+"build-check"); err != nil {
		return m.servicesToast(toast.Errr, "Cannot run the build checks", err.Error())
	}
	return nil
}

// handleContainersExtraPick runs a picked "ctx-x:<kind>:<value>".
func (m *Model) handleContainersExtraPick(id string) tea.Cmd {
	kind, value, _ := strings.Cut(strings.TrimPrefix(id, "ctx-x:"), ":")
	switch kind {
	case "build":
		return m.openBuildDetail(value)
	}
	return nil
}
