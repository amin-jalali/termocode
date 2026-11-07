package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Several engines/hosts in one view (CTX-41): Containers: Show All
// Contexts Together adds an ALL CONTEXTS section (above HELP). It lists
// the containers of every docker context / podman connection at the same
// time, grouped by context; a context that does not answer is one note
// row and never hides the others. The rows are read-only: Enter uses
// that context in this session (then the usual keys work on its rows),
// y copies the id. It loads when it opens and on r / R, never on a timer.

const allContextsSection = "all contexts"

// hostAllContainer is a container row of ALL CONTEXTS (idx into
// st.multi.Containers).
const hostAllContainer hostKind = 100

// allContextsMsg is one ListAll answer.
type allContextsMsg struct {
	gen uint64
	res containers.MultiResult
}

// toggleAllContexts is the palette command.
func (m *Model) toggleAllContexts() tea.Cmd {
	st := m.services
	st.allContexts = !st.allContexts
	st.touch()
	if !st.allContexts {
		st.multi, st.multiLoaded = containers.MultiResult{}, false
		m.servicesClampCursor()
		return m.servicesToast(toast.Info, "ALL CONTEXTS hidden", "")
	}
	st.collapsed[hostSection(allContextsSection)] = false
	return tea.Batch(m.loadAllContexts(), m.servicesToast(toast.Info, "ALL CONTEXTS shown", "Enter on a row uses its context · run the command again to hide it"))
}

// loadAllContexts lists every context's containers (nil when the section
// is hidden or folded, or no engine is known yet).
func (m *Model) loadAllContexts() tea.Cmd {
	st := m.services
	if st == nil || !st.allContexts || st.collapsed[hostSection(allContextsSection)] || !st.containerDetected || st.containerEngine.Binary == "" {
		return nil
	}
	st.multiGen++
	gen := st.multiGen
	base := st.containerEngine
	base.Context = ""
	known := append([]containers.Context(nil), st.contextRows...)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		ctxs := known
		if len(ctxs) == 0 {
			ctxs, _ = base.ListContexts(ctx)
		}
		sources := containers.SourcesFromContexts(base, ctxs)
		if len(sources) == 0 {
			sources = []containers.Source{{Name: "local", Engine: base}}
		}
		return allContextsMsg{gen: gen, res: containers.ListAll(ctx, sources)}
	}
}

func (m *Model) onAllContexts(msg allContextsMsg) tea.Cmd {
	st := m.services
	if st == nil || msg.gen != st.multiGen || !st.allContexts {
		return nil
	}
	st.multi, st.multiLoaded = msg.res, true
	st.touch()
	m.servicesClampCursor()
	return nil
}

// allContextsRows are the section's rows (none when it is off).
func (st *servicesState) allContextsRows() []svcRow {
	if !st.allContexts {
		return nil
	}
	key := hostSection(allContextsSection)
	out := []svcRow{{kind: svcRowSection, section: key}}
	if st.collapsed[key] {
		return out
	}
	if !st.multiLoaded {
		return append(out, svcRow{kind: svcRowHint, section: key, text: "Loading…"})
	}
	last := "\x00"
	for i, c := range st.multi.Containers {
		if c.Source != last {
			last = c.Source
			out = append(out, svcRow{kind: svcRowGroup, section: hostGroup(allContextsSection, c.Source), name: c.Source})
		}
		if st.collapsed[hostGroup(allContextsSection, c.Source)] {
			continue
		}
		out = append(out, svcRow{kind: svcRowHostItem, section: key, host: hostAllContainer, idx: i, depth: 1, name: containerShownName(c.Container), hostID: c.ID})
	}
	for _, p := range st.multi.Problems {
		out = append(out, svcRow{kind: svcRowNote, section: key, text: p.Source + ": " + errLine(p.Err.Error())})
	}
	if len(st.multi.Containers) == 0 && len(st.multi.Problems) == 0 {
		out = append(out, svcRow{kind: svcRowHint, section: key, text: "No containers in any context"})
	}
	return out
}

// allContextsCount is the header count.
func (st *servicesState) allContextsCount() string {
	if !st.multiLoaded {
		return "…"
	}
	n := strconv.Itoa(len(st.multi.Containers))
	if len(st.multi.Problems) > 0 {
		n += " · " + strconv.Itoa(len(st.multi.Problems)) + " not answering"
	}
	return n
}

// selectedAllContainer is the ALL CONTEXTS row under the cursor.
func (m *Model) selectedAllContainer() (containers.SourceContainer, bool) {
	r, ok := m.servicesCurrentRow()
	if !ok || r.kind != svcRowHostItem || r.host != hostAllContainer || r.idx < 0 || r.idx >= len(m.services.multi.Containers) {
		return containers.SourceContainer{}, false
	}
	return m.services.multi.Containers[r.idx], true
}

// allContextsKey handles the keys of an ALL CONTEXTS row.
func (m *Model) allContextsKey(key string) (tea.Cmd, bool) {
	st := m.services
	if !st.allContexts {
		return nil, false
	}
	r, ok := m.servicesCurrentRow()
	if !ok || r.hostSectionID() != allContextsSection {
		return nil, false
	}
	switch key {
	case "r", "R":
		return m.loadAllContexts(), key == "r"
	}
	c, ok := m.selectedAllContainer()
	if !ok {
		return nil, false
	}
	switch key {
	case "enter", "l":
		return m.switchContext(c.Source), true
	case "y":
		m.copyToClipboard(c.ID)
		return m.servicesToast(toast.Info, "Copied container id", shortImageID(c.ID)), true
	}
	return nil, false // the other row keys act on the active context's rows only
}

// allContextsFooter is the footer of an ALL CONTEXTS row.
func (st *servicesState) allContextsFooter(r svcRow) (string, string) {
	if r.idx < 0 || r.idx >= len(st.multi.Containers) {
		return "", ""
	}
	c := st.multi.Containers[r.idx]
	return c.Image + " · " + c.Source, c.Status + " · Enter use this context"
}

// renderAllContainer draws one ALL CONTEXTS row.
func (st *servicesState) renderAllContainer(r svcRow, active, focused bool, w int) string {
	if r.idx < 0 || r.idx >= len(st.multi.Containers) {
		return ""
	}
	c := st.multi.Containers[r.idx].Container
	g, gcol := containerGlyph(c)
	var cols []hostCol
	if w >= 28 {
		up, ucol := containerUptime(c)
		cols = append(cols, hostCol{text: up, width: 6, right: true, col: ucol})
	}
	return renderHostLine(active, focused, w, r.depth, g.String(), gcol, r.name, theme.TextPrimary, cols)
}

// multiPaletteItems is the CTX-41 palette row.
func multiPaletteItems() []picker.Item {
	return []picker.Item{{ID: "containers-all-contexts", Title: "Containers: Show All Contexts Together"}}
}

// isAllContextsSection: the section key of ALL CONTEXTS.
func isAllContextsSection(section string) bool {
	return strings.TrimPrefix(section, "host/") == allContextsSection
}
