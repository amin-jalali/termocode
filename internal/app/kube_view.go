package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/activity"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Kubernetes view: rendering ───────────────────────────────────────────
//
// Layout (sidebar-local rows):
//
//	0  KUBERNETES · dev
//	1  hairline
//	2  ctx dev · ns shop
//	3
//	4… PODS ──── 5 / item rows / DEPLOYMENTS … / note rows
//	h-2 footer: the cursor row's details
//	h-1 footer: keys

const (
	kubeScopeRow   = 2
	kubeListTop    = 4
	kubeFooterRows = 2
)

func kubeBodyH(h int) int { return max(h-kubeListTop-kubeFooterRows, 1) }

// kubeBodyLines maps the visible list lines to row indexes, scrolled so
// the cursor shows; -1 is the blank line before a section that follows
// an open one.
func kubeBodyLines(rows []kubeRow, cursor, bodyH int) []int {
	var all []int
	pos := 0
	for i, r := range rows {
		if r.kind == kubeRowSection && i > 0 && rows[i-1].kind != kubeRowSection {
			all = append(all, -1)
		}
		if i == cursor {
			pos = len(all)
		}
		all = append(all, i)
	}
	top := 0
	if pos >= bodyH {
		top = pos - bodyH + 1
	}
	end := min(top+bodyH, len(all))
	return all[top:end]
}

func (m Model) renderKubeSidebar(w, h int) string {
	st := m.kube
	focused := m.focus == FocusExplorer
	now := clock.Now()
	hy := -1
	if m.hoverX >= activity.Width && m.hoverX < activity.Width+m.explorerWidth-1 {
		hy = m.hoverY
	}
	key := fmt.Sprintf("%d|%d|%d|%d|%d|%v|%v|%d", st.seq, st.cursor, hy, w, h, focused, theme.CurrentRenderKey(), now.Unix()/60)
	st.render.Lock()
	defer st.render.Unlock()
	if st.render.key == key && st.render.out != "" {
		return st.render.out
	}
	out := st.renderRaw(w, h, focused, now, hy)
	st.render.key, st.render.out = key, out
	return out
}

func (st *kubeState) titleSuffix() string {
	if st.filter != "" || st.filterTyping {
		return inlineFilterTitle(st.filter, st.filterTyping, theme.CurrentIconMode == theme.IconModeASCII)
	}
	return st.context
}

func (st *kubeState) renderRaw(w, h int, focused bool, now time.Time, hy int) string {
	fill := func() string { return theme.Bg(theme.BgSidebar).Render(strings.Repeat(" ", max(w, 0))) }
	muted := func(s string) string { return theme.FgBg(theme.TextMuted, theme.BgSidebar).Render(s) }
	lines := []string{
		sidebarTitle("KUBERNETES", w, focused, st.titleSuffix()),
		theme.PaneHairline(w, theme.BgSidebar, focused),
	}
	done := func() string {
		for len(lines) < h {
			lines = append(lines, fill())
		}
		return strings.Join(lines[:h], "\n")
	}
	if !st.loaded {
		lines = append(lines, testsPad(muted("  Looking for the cluster…"), w))
		return done()
	}
	if st.detected && st.binary == "" {
		lines = append(lines, renderEmptyState(w, h-2, theme.BgSidebar, emptyState{Glyph: theme.IconContainers,
			Title: "kubectl not found", Hint: "Install kubectl to see your cluster here.", Key: "R", Label: "Look again"})...)
		return done()
	}
	// Scope: "ctx dev · ns shop".
	{
		ctx, ns := st.context, st.namespace
		if ctx == "" {
			ctx = "?"
		}
		if ns == "" {
			ns = "?"
		}
		sec := func(s string) string { return theme.FgBg(theme.TextSecondary, theme.BgSidebar).Render(s) }
		room := max(w-2-len("ctx  · ns "), 2)
		ctxW := min(runewidth.StringWidth(ctx), max(room/2, room-runewidth.StringWidth(ns)))
		ctx = runewidth.Truncate(ctx, max(ctxW, 1), "…")
		ns = runewidth.Truncate(ns, max(room-runewidth.StringWidth(ctx), 1), "…")
		s := theme.Bg(theme.BgSidebar).Render(" ") + muted("ctx ") + sec(ctx) + muted(" · ns ") + sec(ns)
		lines = append(lines, testsPad(s, w), fill())
	}
	rows := st.rows()
	var body []string
	for _, i := range kubeBodyLines(rows, st.cursor, kubeBodyH(h)) {
		if i < 0 {
			body = append(body, fill())
			continue
		}
		r := rows[i]
		active := i == st.cursor
		var line string
		switch r.kind {
		case kubeRowSection:
			line = renderServiceSection(kubeSectionTitle(r.section), st.count(r.section), st.collapsed[r.section], active, focused, w)
		case kubeRowItem:
			line = st.renderItem(r, active, focused, w, now)
		case kubeRowNote:
			line = renderServiceNote(svcRow{kind: svcRowNote, text: r.text}, w)
		default:
			line = renderServiceNote(svcRow{kind: svcRowHint, text: r.text}, w)
		}
		if kubeListTop+len(body) == hy && !active && r.selectable() {
			line = sidebarHoverLineBg(line)
		}
		body = append(body, line)
	}
	if st.err != "" {
		for _, hint := range []string{"R tries again", "c picks another context"} {
			body = append(body, renderServiceNote(svcRow{kind: svcRowHint, text: hint}, w))
		}
	}
	for len(body) < kubeBodyH(h) {
		body = append(body, fill())
	}
	lines = append(lines, body[:kubeBodyH(h)]...)
	foot := st.footer(now)
	foot[1] = dbFitFooter(foot[1], max(w-2, 1)) // whole parts go, "? keys" stays
	for _, f := range foot {
		f = runewidth.Truncate(f, max(w-2, 1), "…")
		lines = append(lines, testsPad(theme.Bg(theme.BgSidebar).Render(" ")+muted(f), w))
	}
	return done()
}

// kubePodGlyph is a pod's status mark: green running and ready, yellow
// on its way, red broken, dim done.
func kubePodGlyph(p kube.Pod) (theme.Icon, theme.Color256) {
	switch p.Status {
	case "Running":
		if p.ReadyCount < p.Total {
			return theme.GlyphSvcStarting, theme.GitModified
		}
		return theme.GlyphSvcUp, theme.GitAdded
	case "Completed", "Succeeded":
		return theme.GlyphSvcStopped, theme.TextDim
	case "Pending", "ContainerCreating", "PodInitializing", "Terminating":
		return theme.GlyphSvcStarting, theme.GitModified
	}
	if strings.HasPrefix(p.Status, "Init:") {
		return theme.GlyphSvcStarting, theme.GitModified
	}
	return theme.GlyphSvcExited, theme.DiagError
}

func kubeDeployGlyph(d kube.Deployment) (theme.Icon, theme.Color256) {
	switch {
	case d.Replicas == 0:
		return theme.GlyphSvcStopped, theme.TextDim
	case d.ReadyN >= d.Replicas && d.Updated >= d.Replicas:
		return theme.GlyphSvcUp, theme.GitAdded
	case d.ReadyN == 0:
		return theme.GlyphSvcExited, theme.DiagError
	}
	return theme.GlyphSvcStarting, theme.GitModified
}

func kubeJobGlyph(j kube.Job) (theme.Icon, theme.Color256) {
	switch j.Status() {
	case "Complete":
		return theme.GlyphOK, theme.GitAdded
	case "Failed":
		return theme.GlyphSvcExited, theme.DiagError
	case "Running":
		return theme.GlyphSvcStarting, theme.GitModified
	}
	return theme.GlyphSvcStopped, theme.TextDim
}

// itemParts is an item row's glyph, name and right column.
func (st *kubeState) itemParts(r kubeRow, w int, now time.Time) (theme.Icon, theme.Color256, string) {
	wide := w >= 44
	switch r.section {
	case "pods":
		p := st.pods[r.idx]
		g, c := kubePodGlyph(p)
		right := p.Ready()
		if p.Restarts > 0 {
			right += " " + theme.GlyphSvcRestarting.String() + strconv.Itoa(p.Restarts)
		}
		if fw := st.forward(p.Name); fw != nil {
			right = kubeArrow() + ":" + strconv.Itoa(fw.Local) + " " + right
		}
		if wide {
			right = p.Status + " " + right
		}
		return g, c, right + " " + kube.Age(p.Created, now)
	case "deployments":
		d := st.deploys[r.idx]
		g, c := kubeDeployGlyph(d)
		right := d.Ready()
		if wide {
			right += fmt.Sprintf(" upd %d av %d", d.Updated, d.Available)
		}
		return g, c, right + " " + kube.Age(d.Created, now)
	case "services":
		s := st.services[r.idx]
		right := s.Type
		if wide {
			right += " " + s.PortsText()
		}
		return theme.GlyphSvcStopped, theme.TextDim, right
	case "jobs":
		j := st.jobs[r.idx]
		g, c := kubeJobGlyph(j)
		return g, c, j.Completions() + " " + kube.Age(j.Created, now)
	}
	return theme.Icon{}, theme.TextDim, ""
}

// renderItem draws one object: lead, glyph, name, right column.
func (st *kubeState) renderItem(r kubeRow, active, focused bool, w int, now time.Time) string {
	bg := gitSelBg(active, focused)
	row := lipgloss.NewStyle().Background(bg)
	fg := func(c theme.Color256) lipgloss.Style {
		return lipgloss.NewStyle().Background(bg).Foreground(theme.LG(c))
	}
	glyph, gcol, right := st.itemParts(r, w, now)
	var sb strings.Builder
	sb.WriteString(gitLead(active, focused, bg))
	sb.WriteString(row.Render(" "))
	sb.WriteString(fg(gcol).Render(glyph.String()))
	sb.WriteString(row.Render(" "))
	used := 3 + runewidth.StringWidth(glyph.String())
	room := w - used - 1
	rw := runewidth.StringWidth(right)
	if rw > room-6 { // the name keeps at least 6 cells
		right = runewidth.Truncate(right, max(room-6, 0), "")
		rw = runewidth.StringWidth(right)
	}
	nameW := max(room-rw-1, 1)
	name := svcTruncMiddle(r.name, nameW)
	sb.WriteString(fg(theme.TextPrimary).Render(name))
	used += runewidth.StringWidth(name)
	if rw > 0 {
		gap := max(w-1-used-rw, 1)
		sb.WriteString(row.Render(strings.Repeat(" ", gap)))
		sb.WriteString(fg(theme.TextMuted).Render(right))
		used += gap + rw
	}
	return clampSidebarRow(sb.String(), used, w, row)
}

// kubeArrow is "→", "->" with ASCII icons.
func kubeArrow() string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return "->"
	}
	return "→"
}

// footer is two lines: the cursor row's details and the keys.
func (st *kubeState) footer(now time.Time) []string {
	r, ok := st.currentRow()
	keys := "c context · n namespace · ? keys"
	if !ok || r.kind != kubeRowItem {
		return []string{"", keys}
	}
	switch r.section {
	case "pods":
		p := st.pods[r.idx]
		line := fmt.Sprintf("%s · ready %s · %d restarts", p.Status, p.Ready(), p.Restarts)
		if fw := st.forward(p.Name); fw != nil {
			line = fmt.Sprintf("127.0.0.1:%d %s %d · %s", fw.Local, kubeArrow(), fw.Remote, line)
		}
		return []string{line, "l logs · e shell · f forward · ? keys"}
	case "deployments":
		d := st.deploys[r.idx]
		return []string{fmt.Sprintf("ready %s · updated %d · available %d", d.Ready(), d.Updated, d.Available), "i describe · r restart · ? keys"}
	case "services":
		s := st.services[r.idx]
		return []string{s.Type + " · " + s.ClusterIP + " · " + s.PortsText(), "i describe · y copy name · ? keys"}
	case "jobs":
		j := st.jobs[r.idx]
		return []string{fmt.Sprintf("%s · %s done · %d failed", j.Status(), j.Completions(), j.Failed), "i describe · y copy name · ? keys"}
	}
	return []string{"", keys}
}
