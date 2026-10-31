package app

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/activity"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Services sidebar view: rows and rendering ────────────────────────────
//
// Layout (sidebar-local rows):
//
//	0  SERVICES · PROJECT
//	1  hairline
//	2  compose · termocode            3/4 up
//	3
//	4  ▸ Start all  ▪ Stop all  ↻         (action bar)
//	5
//	6… COMPOSE ───────── 4 / service rows / LOCAL … / note rows
//	   (blank)
//	h-2 footer: image · ports of the cursor row
//	h-1 footer: the raw status text
//
// Row anatomy at w = 29: lead, glyph, space, name (12), space, port (6),
// space, uptime (6, right). Below 28 cells the uptime goes, below 22 the
// port.

const (
	svcSummaryRow = 2
	svcActionRow  = 4
	svcListTop    = 6
	svcFooterRows = 2
)

type svcRowKind int

const (
	svcRowSection svcRowKind = iota
	svcRowService
	svcRowNote     // a problem line (red)
	svcRowHint     // what to do about it
	svcRowCode     // a command to copy (y)
	svcRowGroup    // a foldable group in a host section (a compose project, an image repo)
	svcRowHostItem // one host object: container, image tag, network, …
	svcRowRetry    // "Retry": a button row under an engine problem (Enter / click)
	svcRowStats    // one chart line under the selected running container (containers_stats.go); not selectable
)

type svcRow struct {
	kind    svcRowKind
	section string // svcSecCompose / svcSecLocal / host/<id> / grp/<id>/<name>
	name    string // service rows; group and host rows: the display name
	text    string // note / hint / code rows
	hostID  string // host rows: the object's id (container id, image id, name)

	// host rows and groups
	host     hostKind
	idx      int    // index into the section's data slice
	tag      string // image rows: the tag
	imageRef string // image rows: exact repository reference represented by this row
	depth    int    // indent level (2 cells each)
	count    string // group rows: "2/2", "3"
	current  bool   // group of this workspace's project / the active context
}

func hostSection(id string) string          { return "host/" + id }
func hostGroup(section, name string) string { return "grp/" + section + "/" + name }

func (r svcRow) selectable() bool {
	return r.kind == svcRowSection || r.kind == svcRowService || r.kind == svcRowGroup || r.kind == svcRowHostItem || r.kind == svcRowRetry
}

// foldable: Space / click folds it.
func (r svcRow) foldable() bool {
	return r.kind == svcRowSection || r.kind == svcRowGroup || (r.kind == svcRowHostItem && (r.host == hostNetwork || r.host == hostVolume || r.host == hostRegistry || r.host == hostRemoteRepo))
}

// rows flattens the view: sections, problem notes, services.
func (st *servicesState) rows() []svcRow {
	if st == nil {
		return nil
	}
	var out []svcRow
	projectVisible := true
	if shown, ok := st.sections["project"]; ok {
		projectVisible = shown
	}
	if projectVisible && st.det.Empty() {
		out = append(out, svcRow{kind: svcRowSection, section: svcSecCompose})
		if !st.collapsed[svcSecCompose] {
			out = append(out, svcRow{kind: svcRowHint, section: svcSecCompose, text: "No services yet"},
				svcRow{kind: svcRowHint, section: svcSecCompose, text: "o adds services.json"})
		}
	}
	if projectVisible && len(st.det.ComposeFiles) > 0 {
		out = append(out, svcRow{kind: svcRowSection, section: svcSecCompose})
		if !st.collapsed[svcSecCompose] {
			out = append(out, st.problemRows()...)
			for _, s := range st.compose {
				out = append(out, svcRow{kind: svcRowService, section: svcSecCompose, name: s.Name})
			}
			if len(st.compose) == 0 && st.problem.Kind == services.ProblemNone && st.loaded {
				out = append(out, svcRow{kind: svcRowHint, section: svcSecCompose, text: "No services in " + st.det.ComposeFiles[0]})
			}
		}
	}
	if projectVisible && (len(st.local) > 0 || st.det.ConfigErr != nil) {
		out = append(out, svcRow{kind: svcRowSection, section: svcSecLocal})
		if !st.collapsed[svcSecLocal] {
			if err := st.det.ConfigErr; err != nil {
				out = append(out, svcRow{kind: svcRowNote, section: svcSecLocal, text: err.Error()},
					svcRow{kind: svcRowHint, section: svcSecLocal, text: "o opens services.json"})
			}
			for _, s := range st.local {
				out = append(out, svcRow{kind: svcRowService, section: svcSecLocal, name: s.Name})
			}
		}
	}
	out = append(out, st.hostRows()...)
	if st.filter != "" {
		out = st.applyFilter(out)
	}
	return out
}

// problemRows are the note rows of a compose problem (plan §1.3).
func (st *servicesState) problemRows() []svcRow {
	note := func(s string) svcRow { return svcRow{kind: svcRowNote, section: svcSecCompose, text: s} }
	hint := func(s string) svcRow { return svcRow{kind: svcRowHint, section: svcSecCompose, text: s} }
	p := st.composeProblem()
	switch p.Kind {
	case services.ProblemNoDocker:
		return engineMissingRows(svcSecCompose)
	case services.ProblemNoCompose:
		return []svcRow{note("Docker Compose v2 is needed"), hint("Update Docker · R retry")}
	case services.ProblemDaemonDown:
		return []svcRow{note(p.Message()), hint("Start it, then try again."), retryRow(svcSecCompose)}
	case services.ProblemNoPermission:
		return []svcRow{note(p.Message()),
			{kind: svcRowCode, section: svcSecCompose, text: svcPermissionFix},
			hint("y copies it · the docker group is root-equal")}
	case services.ProblemBadConfig:
		return []svcRow{note(st.det.ComposeFiles[0] + ": " + p.Detail), hint("o opens the file · R retry")}
	case services.ProblemOther:
		return []svcRow{note(p.Detail), hint("R retry")}
	}
	return nil
}

// composeProblem is the compose problem with the detected engine filled
// in, so a Podman machine reads "Podman is not running" even when the
// compose error text does not name the engine (FIX-7).
func (st *servicesState) composeProblem() services.Problem {
	p := st.problem
	if p.Engine == "" && st.containerEngine.Kind == "podman" {
		p.Engine = "podman"
	}
	return p
}

// retryRow is the one Retry button under an engine problem.
func retryRow(section string) svcRow {
	return svcRow{kind: svcRowRetry, section: section, text: "Retry"}
}

// composeBlocked: the project has compose services but the engine cannot
// run them, so the actions that need it are off.
func (st *servicesState) composeBlocked() bool {
	if len(st.det.ComposeFiles) == 0 {
		return false
	}
	switch st.problem.Kind {
	case services.ProblemNoDocker, services.ProblemNoCompose, services.ProblemDaemonDown, services.ProblemNoPermission:
		return true
	}
	return false
}

// projectBannerShown: PROJECT already says "Docker is not running", so
// the host sections stay quiet about it.
func (st *servicesState) projectBannerShown() bool {
	visible := true
	if shown, ok := st.sections["project"]; ok {
		visible = shown
	}
	return visible && len(st.det.ComposeFiles) > 0 && !st.collapsed[svcSecCompose] && st.problem.Kind == services.ProblemDaemonDown
}

// svcPermissionFix is shown (never run) when the socket is not ours.
const svcPermissionFix = "sudo usermod -aG docker $USER"

// servicesCurrentRow is the row under the cursor.
func (m Model) servicesCurrentRow() (svcRow, bool) {
	st := m.services
	rows := st.rows()
	if st.cursor < 0 || st.cursor >= len(rows) {
		return svcRow{}, false
	}
	return rows[st.cursor], true
}

// servicesClampCursor keeps the cursor on a selectable row.
func (m *Model) servicesClampCursor() {
	st := m.services
	rows := st.rows()
	if len(rows) == 0 {
		st.cursor = 0
		return
	}
	if st.cursor >= len(rows) {
		st.cursor = len(rows) - 1
	}
	if st.cursor < 0 {
		st.cursor = 0
	}
	if rows[st.cursor].selectable() {
		return
	}
	for i := st.cursor + 1; i < len(rows); i++ {
		if rows[i].selectable() {
			st.cursor = i
			return
		}
	}
	for i := st.cursor - 1; i >= 0; i-- {
		if rows[i].selectable() {
			st.cursor = i
			return
		}
	}
}

// servicesMove moves the cursor by d selectable rows.
func (m *Model) servicesMove(d int) {
	m.servicesMoveIn(m.services.rows(), d)
}

// servicesMoveIn is servicesMove over rows, the view's current rows.
func (m *Model) servicesMoveIn(rows []svcRow, d int) {
	st := m.services
	step := 1
	if d < 0 {
		step, d = -1, -d
	}
	i := st.cursor
	for ; d > 0; d-- {
		j := i + step
		for j >= 0 && j < len(rows) && !rows[j].selectable() {
			j += step
		}
		if j < 0 || j >= len(rows) {
			break
		}
		i = j
	}
	st.cursor = i
	st.touch()
}

// ── Look of one row ──────────────────────────────────────────────────────

// svcGlyph is a state's glyph and colour.
func svcGlyph(s services.Status) (theme.Icon, theme.Color256) {
	switch s.State {
	case services.Healthy:
		return theme.GlyphSvcUp, theme.GitAdded
	case services.Running:
		if s.PortOpen != nil && !*s.PortOpen {
			return theme.GlyphSvcUp, theme.GitModified
		}
		return theme.GlyphSvcUp, theme.GitAdded
	case services.Unhealthy:
		return theme.GlyphSvcBad, theme.DiagError
	case services.Starting, services.Pulling, services.Stopping:
		return theme.GlyphSvcStarting, theme.GitModified
	case services.Restarting:
		return theme.GlyphSvcRestarting, theme.GitModified
	case services.Exited:
		if s.ExitCode != 0 {
			return theme.GlyphSvcExited, theme.DiagError
		}
	case services.Unknown:
		return theme.GlyphSvcUnknown, theme.TextDim
	}
	return theme.GlyphSvcStopped, theme.TextDim
}

// svcUptime is the right column: uptime, exit code or a short state word.
func svcUptime(s services.Status, now time.Time) (string, theme.Color256) {
	switch s.State {
	case services.Running, services.Healthy, services.Unhealthy:
		if !s.StartedAt.IsZero() {
			return services.FormatUptime(now.Sub(s.StartedAt)), theme.TextMuted
		}
	case services.Starting:
		if !s.StartedAt.IsZero() {
			return services.FormatUptime(now.Sub(s.StartedAt)), theme.TextMuted
		}
	case services.Pulling:
		return "pull", theme.TextMuted
	case services.Stopping:
		return "stop", theme.TextMuted
	case services.Restarting:
		return "—", theme.TextMuted
	case services.Exited:
		if s.ExitCode != 0 {
			return "exit " + strconv.Itoa(s.ExitCode), theme.DiagError
		}
		return "done", theme.TextMuted
	case services.Stopped:
		if strings.HasPrefix(s.Text, "Exited (0)") {
			return "done", theme.TextMuted
		}
	}
	return "", theme.TextMuted
}

// svcPortText is the port column: the first published port, else the
// declared one; "—" for a running service with none.
func svcPortText(svc services.Service, s services.Status) string {
	for _, p := range s.Ports {
		if p.Published > 0 {
			return ":" + strconv.Itoa(p.Published)
		}
	}
	if s.State.Up() && svc.Backend == svcSecCompose {
		return "—"
	}
	for _, p := range svc.Ports {
		if p.Published > 0 {
			return ":" + strconv.Itoa(p.Published)
		}
	}
	return ""
}

// svcTruncMiddle cuts s to w cells with … in the middle.
func svcTruncMiddle(s string, w int) string {
	if runewidth.StringWidth(s) <= w {
		return s
	}
	if w <= 1 {
		return runewidth.Truncate(s, w, "")
	}
	keep := w - runewidth.StringWidth("…")
	if keep < 0 {
		return runewidth.Truncate(s, w, "")
	}
	head := (keep + 1) / 2
	tail := keep - head
	// Cut by cells, not runes: a wide or combining rune must not push the
	// result past w.
	h := runewidth.Truncate(s, head, "")
	r := []rune(s)
	t := ""
	for i := len(r) - 1; i >= 0; i-- {
		if runewidth.StringWidth(string(r[i:])) > tail {
			break
		}
		t = string(r[i:])
	}
	return h + "…" + t
}

// svcRowColumns is the width ladder: name, port and uptime widths for w.
func svcRowColumns(w int) (nameW, portW, upW int) {
	switch {
	case w >= 28:
		return w - 3 - 1 - 6 - 1 - 6, 6, 6
	case w >= 22:
		return w - 3 - 1 - 6, 6, 0
	}
	return max(w-4, 1), 0, 0
}

// renderServiceRow draws one service row exactly w cells wide.
func renderServiceRow(svc services.Service, s services.Status, active, focused bool, w int, now time.Time) string {
	bg := gitSelBg(active, focused)
	row := func(s string) string { return sgrPaint("", bg, s) }
	fg := func(c theme.Color256, s string) string { return sgrPaint(theme.LG(c), bg, s) }
	glyph, gcol := svcGlyph(s)
	nameW, portW, upW := svcRowColumns(w)
	var sb strings.Builder
	sb.WriteString(hostLead(active, focused, bg))
	sb.WriteString(fg(gcol, glyph.String()))
	sb.WriteString(row(" "))
	nameCol := theme.TextPrimary
	if s.State == services.Unknown {
		nameCol = theme.TextDim
	}
	name := svcTruncMiddle(svc.Name, nameW)
	sb.WriteString(fg(nameCol, name))
	used := 3 + runewidth.StringWidth(name)
	if pad := 3 + nameW + 1 - used; pad > 0 {
		sb.WriteString(row(strings.Repeat(" ", pad)))
		used += pad
	}
	if portW > 0 {
		port := runewidth.Truncate(svcPortText(svc, s), portW, "")
		sb.WriteString(fg(theme.TextSecondary, port))
		sb.WriteString(row(strings.Repeat(" ", portW-runewidth.StringWidth(port))))
		used += portW
	}
	if upW > 0 {
		up, ucol := svcUptime(s, now)
		if s.State == services.Unknown {
			up = ""
		}
		up = runewidth.Truncate(up, upW, "")
		sb.WriteString(row(strings.Repeat(" ", 1+upW-runewidth.StringWidth(up))))
		sb.WriteString(fg(ucol, up))
		used += 1 + upW
	}
	return hostClamp(sb.String(), used, w, bg)
}

// showEmptyCard: the project has no services and no host section can
// show anything, so the whole view is the "No services" card (key o).
func (st *servicesState) showEmptyCard() bool {
	return st.loaded && st.det.Empty() && (!st.hostPartShown() || st.engineAbsent())
}

// renderServiceSection is a section header: "COMPOSE ──── 4" (only the
// count is bold: one bold element per region).
func renderServiceSection(label, count string, collapsed, active, focused bool, w int) string {
	bg := gitSelBg(active, focused)
	row := func(s string) string { return sgrPaint("", bg, s) }
	fg := func(c theme.Color256, s string) string { return sgrPaint(theme.LG(c), bg, s) }
	labelCol := theme.TextSecondary
	if active && focused {
		labelCol = theme.BorderFocus
	}
	var sb strings.Builder
	sb.WriteString(hostLead(active, focused, bg))
	used := 1
	foldGlyph := theme.GlyphExpand
	if collapsed {
		foldGlyph = theme.GlyphTreeCollapsed
	}
	sb.WriteString(fg(theme.TextDim, foldGlyph.String()))
	sb.WriteString(row(" "))
	used += 2
	sb.WriteString(fg(labelCol, label))
	sb.WriteString(row(" "))
	used += runewidth.StringWidth(label) + 1
	// Cells, not glyphs: "─" is two cells wide where ambiguous width is wide.
	rw := max(runewidth.StringWidth("─"), 1)
	if rule := (w - used - len(count) - 2) / rw; rule > 0 {
		sb.WriteString(fg(theme.BorderSubtle, strings.Repeat("─", rule)))
		used += rule * rw
	}
	sb.WriteString(row(" "))
	sb.WriteString(lipgloss.NewStyle().Background(bg).Foreground(theme.LG(theme.TextMuted)).Bold(true).Render(count))
	used += 1 + len(count)
	return hostClamp(sb.String(), used, w, bg)
}

// renderServiceNote draws a note / hint / code row.
func renderServiceNote(r svcRow, w int) string {
	row := lipgloss.NewStyle().Background(theme.LG(theme.BgSidebar))
	col, lead := theme.TextMuted, "   "
	switch r.kind {
	case svcRowNote:
		col = theme.DiagError
	case svcRowCode:
		col, lead = theme.TextSecondary, "   $ "
	}
	text := runewidth.Truncate(r.text, max(w-runewidth.StringWidth(lead)-1, 1), "…")
	s := row.Render(lead) + lipgloss.NewStyle().Background(theme.LG(theme.BgSidebar)).Foreground(theme.LG(col)).Render(text)
	return clampSidebarRow(s, runewidth.StringWidth(lead)+runewidth.StringWidth(text), w, row)
}

// renderServiceRetry draws the Retry button row: "[↻ Retry (R)]".
func renderServiceRetry(r svcRow, active, focused bool, w int) string {
	bg := gitSelBg(active, focused)
	row := lipgloss.NewStyle().Background(bg)
	glyph := theme.GlyphRefresh.String()
	if glyph != "" {
		glyph += " "
	}
	text := "[" + glyph + r.text + " (R)]"
	text = runewidth.Truncate(text, max(w-4, 1), "…")
	s := row.Render("   ") + lipgloss.NewStyle().Background(bg).Foreground(theme.LG(theme.TextPrimary)).Render(text)
	return clampSidebarRow(s, 3+runewidth.StringWidth(text), w, row)
}

// svcAction is one clickable label on the action bar.
type svcAction struct {
	id, label  string
	start, end int
	disabled   bool // greyed out: needs an engine that is not there
}

// svcActionLayout lays out the action bar for width w (labels that do
// not fit are dropped; the refresh icon sits at the right edge).
func svcActionLayout(w int) []svcAction {
	labels := [][2]string{{"start-all", theme.GlyphStart.String() + " Start all"}, {"stop-all", theme.GlyphStop.String() + " Stop all"}}
	iconAt := w - 2
	var out []svcAction
	col := 1
	for _, l := range labels {
		lw := runewidth.StringWidth(l[1])
		if col+lw > iconAt-1 {
			break
		}
		out = append(out, svcAction{id: l[0], label: l[1], start: col, end: col + lw})
		col += lw + 2
	}
	if iconAt >= col-1 && iconAt > 0 {
		out = append(out, svcAction{id: "refresh", label: theme.GlyphRefresh.String(), start: iconAt, end: iconAt + 1})
	}
	return out
}

// actionLayout is the action bar of this state: an empty project has no
// Start all / Stop all (nothing to start), only refresh.
func (st *servicesState) actionLayout(w int) []svcAction {
	all := svcActionLayout(w)
	if !st.det.Empty() {
		if st.composeBlocked() {
			for i := range all {
				all[i].disabled = all[i].id == "start-all" || all[i].id == "stop-all"
			}
		}
		return all
	}
	var out []svcAction
	for _, a := range all {
		if a.id == "refresh" {
			out = append(out, a)
		}
	}
	return out
}

// svcFooterFor is the two footer lines for the cursor row r (plan §2.7;
// ok false: no row).
func (m Model) svcFooterFor(r svcRow, ok bool) (string, string) {
	st := m.services
	if !ok {
		return "", ""
	}
	if r.kind == svcRowGroup || r.kind == svcRowHostItem || (r.kind == svcRowSection && strings.HasPrefix(r.section, "host/")) {
		return st.hostFooter(r)
	}
	if r.kind == svcRowRetry {
		return "Enter or R checks again", ""
	}
	if r.kind == svcRowSection {
		if r.section == svcSecCompose && st.det.Empty() {
			return "Add compose.yaml, Procfile", "o adds services.json"
		}
		if r.section == svcSecCompose {
			l1 := strings.Join(st.det.ComposeFiles, ", ")
			if st.project != "" {
				l1 += " · " + st.project
			}
			return l1, "⏎ logs of all · space fold"
		}
		return "local commands", "space fold"
	}
	svc, ok := st.findService(r.section, r.name)
	if !ok {
		return "", ""
	}
	s := st.statusOf(r.section, r.name)
	var l1 string
	if svc.Backend == svcSecLocal {
		l1 = svc.Command
	} else {
		img := s.Image
		if img == "" {
			img = svc.Image
		}
		if img == "" {
			img = "build: " + svc.Name
		}
		ports := s.Ports
		if len(ports) == 0 {
			ports = svc.Ports
		}
		var ps []string
		for _, p := range ports {
			if p.Published > 0 {
				ps = append(ps, p.String())
			}
		}
		l1 = img
		if len(ps) > 0 {
			l1 += " · " + strings.Join(ps, ", ")
		} else if s.State.Up() {
			l1 += " · no published ports"
		}
	}
	return l1, svcStatusLine(svc, s)
}

// svcFooterFit drops the host part of "0.0.0.0:5432→5432" when line 1
// does not fit w cells.
func svcFooterFit(l1 string, w int) string {
	if runewidth.StringWidth(l1) <= w {
		return l1
	}
	return svcHostRe.ReplaceAllString(l1, ":$1→")
}

// svcFooterKeys ends the footer with "? keys" (like the Databases footer),
// so the key list is one key away. It goes after the last part of line 2;
// whole " · " parts are dropped from the end of line 2 to make room (a
// footer that already says "? keys" is left alone). When line 2 cannot
// carry it even alone, line 1 gets it if that fits; else nothing is added.
func svcFooterKeys(l1, l2 string, width int) (string, string) {
	if l1 == "" && l2 == "" {
		return l1, l2
	}
	const tail = "? keys"
	if strings.Contains(l1, tail) || strings.Contains(l2, tail) {
		return l1, l2
	}
	if l2 == "" {
		return l1, tail
	}
	if got := dbFitFooter(l2+" · "+tail, width); strings.HasSuffix(got, tail) {
		return l1, got
	}
	if runewidth.StringWidth(l1+" · "+tail) <= width {
		return l1 + " · " + tail, l2
	}
	return l1, l2
}

var svcHostRe = lazyre.New(`(?:\d{1,3}(?:\.\d{1,3}){3}|\[?[0-9a-fA-F:]*::[0-9a-fA-F:]*\]?):(\d+)→`)

// svcStatusLine is footer line 2.
func svcStatusLine(svc services.Service, s services.Status) string {
	port := func() string {
		if s.PortOpen == nil {
			return ""
		}
		p := 0
		for _, pp := range s.Ports {
			p = pp.Published
			break
		}
		if p == 0 {
			p = svc.Health.TCP
		}
		if p == 0 && len(svc.Ports) > 0 {
			p = svc.Ports[0].Published
		}
		if *s.PortOpen {
			return fmt.Sprintf(" · port %d open", p)
		}
		return fmt.Sprintf(" · port %d not open", p)
	}
	text := s.Text
	if svc.Backend == svcSecLocal {
		switch s.State {
		case services.Running:
			return "running" + port()
		case services.Exited:
			return fmt.Sprintf("exited (%d) · l logs · r restart", s.ExitCode)
		case services.Starting:
			return "starting…"
		case services.Stopping:
			return "stopping…"
		}
		return "stopped · s start"
	}
	switch s.State {
	case services.NotCreated:
		return "not created · s start"
	case services.Starting:
		if text != "" && s.Health == "starting" {
			return text
		}
		return "starting…"
	case services.Pulling:
		return "pulling " + svc.Image + " — see the services: up tab"
	case services.Stopping:
		return "stopping…"
	case services.Running:
		return text + port()
	case services.Unhealthy:
		return text + " · l logs"
	case services.Exited:
		if s.ExitCode != 0 {
			return text + " · l logs · r restart"
		}
	case services.Unknown:
		return "Docker is off · R retry"
	}
	return text
}

// servicesHits are the view's click targets, sidebar-local x: the action
// bar labels and the service rows.
func (m Model) servicesHits(w int) []hitTarget {
	st := m.services
	if st == nil {
		return nil
	}
	out := []hitTarget{{ID: "cont:more", Label: theme.GlyphMore.String(), X: w - 1, Y: 0, W: 1, H: 1}}
	if !st.loaded {
		return out
	}
	if st.showEmptyCard() {
		return out
	}
	for _, a := range st.actionLayout(w) {
		if a.disabled {
			continue
		}
		out = append(out, hitTarget{ID: "svc:btn:" + a.id, Label: a.label, X: a.start, Y: svcActionRow, W: a.end - a.start, H: 1})
	}
	rows := st.rows()
	for i, idx := range svcBodyLines(rows, st.cursor, svcBodyH(m.h)) {
		if idx < 0 {
			continue
		}
		r := rows[idx]
		if r.kind == svcRowService {
			out = append(out, hitTarget{ID: "svc:row:" + svcKey(r.section, r.name), Label: r.name, X: 0, Y: svcListTop + i, W: w, H: 1})
		} else if r.kind == svcRowHostItem {
			out = append(out, hitTarget{ID: "cont:row:" + strings.TrimPrefix(r.section, "host/") + "/" + r.hostID, Label: r.name, X: 0, Y: svcListTop + i, W: w, H: 1})
		} else if r.kind == svcRowGroup {
			out = append(out, hitTarget{ID: "cont:grp:" + strings.TrimPrefix(r.section, "grp/"), Label: r.name, X: 0, Y: svcListTop + i, W: w, H: 1})
		} else if r.kind == svcRowSection && strings.HasPrefix(r.section, "host/") {
			out = append(out, hitTarget{ID: "cont:sec:" + strings.TrimPrefix(r.section, "host/"), Label: hostSectionTitle(strings.TrimPrefix(r.section, "host/")), X: 0, Y: svcListTop + i, W: w, H: 1})
		}
	}
	return out
}

// ── The whole view ───────────────────────────────────────────────────────

// svcBodyH is the list height for sidebar height h.
func svcBodyH(h int) int { return max(h-svcListTop-svcFooterRows, 1) }

// svcBodyLines maps the visible list lines to row indexes, scrolled so
// the cursor shows; -1 is the blank line before a section header that
// follows an open section (folded headers stack without blanks).
func svcBodyLines(rows []svcRow, cursor, bodyH int) []int {
	var all []int
	pos := 0
	for i, r := range rows {
		if r.kind == svcRowSection && i > 0 && rows[i-1].kind != svcRowSection {
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

type svcRenderCache struct {
	sync.Mutex
	key string
	out string
}

func (m Model) renderServicesSidebar(w, h int) string {
	st := m.services
	focused := m.focus == FocusExplorer
	now := clock.Now()
	hy := -1
	if m.hoverX >= activity.Width && m.hoverX < activity.Width+m.explorerWidth-1 {
		hy = m.hoverY
	}
	hx := m.hoverX - activity.Width
	key := fmt.Sprintf("%d|%d|%d|%d|%d|%d|%v|%v|%d", st.seq, st.cursor, hy, hx, w, h, focused, theme.CurrentRenderKey(), now.Unix()/60)
	st.render.Lock()
	defer st.render.Unlock()
	if st.render.key == key && st.render.out != "" {
		return st.render.out
	}
	out := m.renderServicesSidebarRaw(w, h, focused, now, hy, hx)
	st.render.key, st.render.out = key, out
	return out
}

func (m Model) renderServicesSidebarRaw(w, h int, focused bool, now time.Time, hy, hx int) string {
	st := m.services
	fill := func() string { return theme.Bg(theme.BgSidebar).Render(strings.Repeat(" ", max(w, 0))) }
	muted := func(s string) string { return theme.FgBg(theme.TextMuted, theme.BgSidebar).Render(s) }
	lines := []string{
		containersSidebarTitle(w, focused, st.filterTitle()),
		theme.PaneHairline(w, theme.BgSidebar, focused),
	}
	if !st.loaded {
		lines = append(lines, testsPad(muted("  Looking for services…"), w))
		for len(lines) < h {
			lines = append(lines, fill())
		}
		return strings.Join(lines[:h], "\n")
	}
	if st.showEmptyCard() {
		hint := "Add compose.yaml, a Procfile or .termocode/services.json"
		if st.engineAbsent() && st.hostPartShown() {
			hint += ". Install Docker or Podman to see containers."
		}
		lines = append(lines, renderEmptyState(w, h-2, theme.BgSidebar, emptyState{Glyph: theme.IconContainers,
			Title: "No services", Hint: hint, Key: "o", Label: "Create services.json"})...)
		return strings.Join(lines[:h], "\n")
	}
	// Summary: backend · project, then "3/4 up" on the right.
	{
		left := "local"
		if len(st.det.ComposeFiles) > 0 {
			left = "compose"
			if st.project != "" {
				left += " · " + st.project
			}
		}
		up, total, _ := st.counts()
		right := fmt.Sprintf("%d/%d up", up, total)
		if st.det.Empty() {
			left, right = "No compose.yaml, Procfile", ""
		}
		if st.stale {
			right = "? " + right
		}
		gap := w - 2 - runewidth.StringWidth(left) - runewidth.StringWidth(right)
		if gap < 1 {
			left = runewidth.Truncate(left, max(w-3-runewidth.StringWidth(right), 1), "…")
			gap = max(w-2-runewidth.StringWidth(left)-runewidth.StringWidth(right), 1)
		}
		s := theme.Bg(theme.BgSidebar).Render(" ") + muted(left) + theme.Bg(theme.BgSidebar).Render(strings.Repeat(" ", gap)) +
			theme.FgBg(theme.TextSecondary, theme.BgSidebar).Render(right)
		lines = append(lines, testsPad(s, w), fill())
	}
	// Action bar.
	{
		var b strings.Builder
		col := 0
		for _, a := range st.actionLayout(w) {
			b.WriteString(testsFill(a.start - col))
			st := theme.FgBg(theme.TextSecondary, theme.BgSidebar)
			if a.disabled {
				st = theme.FgBg(theme.TextDim, theme.BgSidebar)
			} else if hy == svcActionRow && hx >= a.start && hx < a.end {
				st = lipgloss.NewStyle().Background(theme.Wash(theme.BgSidebar, theme.WashHover)).Foreground(theme.LG(theme.TextPrimary))
			}
			b.WriteString(st.Render(a.label))
			col = a.end
		}
		lines = append(lines, testsPad(b.String(), w), fill())
	}
	rows := st.rows()
	var body []string
	for _, i := range svcBodyLines(rows, st.cursor, svcBodyH(h)) {
		if i < 0 {
			body = append(body, fill())
			continue
		}
		r := rows[i]
		active := i == st.cursor
		var line string
		switch r.kind {
		case svcRowSection:
			if strings.HasPrefix(r.section, "host/") {
				sec := strings.TrimPrefix(r.section, "host/")
				line = renderServiceSection(hostSectionTitle(sec), st.filterCount(sec, st.hostSectionCount(sec)), st.collapsed[r.section], active, focused, w)
				break
			}
			// PROJECT is the old COMPOSE header: it counts the compose
			// services; LOCAL counts its own.
			label, n := "PROJECT", len(st.compose)
			if r.section == svcSecLocal {
				label, n = "LOCAL", len(st.local)
			}
			line = renderServiceSection(label, st.projectFilterCount(r.section, n), st.collapsed[r.section], active, focused, w)
		case svcRowGroup:
			line = renderHostGroup(r, st.collapsed[r.section], active, focused, w)
		case svcRowService:
			svc, _ := st.findService(r.section, r.name)
			line = renderServiceRow(svc, st.statusOf(r.section, r.name), active, focused, w, now)
		case svcRowHostItem:
			line = st.renderHostItem(r, active, focused, w)
		case svcRowRetry:
			line = renderServiceRetry(r, active, focused, w)
		case svcRowStats:
			line = st.renderStatsRow(r, w)
		default:
			line = renderServiceNote(r, w)
		}
		if svcListTop+len(body) == hy && !active && r.selectable() {
			line = sidebarHoverLineBg(line)
		}
		body = append(body, line)
	}
	for len(body) < svcBodyH(h) {
		body = append(body, fill())
	}
	lines = append(lines, body...)
	var cur svcRow
	have := st.cursor >= 0 && st.cursor < len(rows)
	if have {
		cur = rows[st.cursor]
	}
	f1, f2 := m.svcFooterFor(cur, have) // the rows drawn above: no second build
	f1 = svcFooterFit(f1, w-2)
	if theme.CurrentIconMode == theme.IconModeASCII {
		f1 = strings.ReplaceAll(f1, "→", "->")
	}
	f1, f2 = svcFooterKeys(f1, f2, w-2)
	for _, f := range []string{f1, f2} {
		f = runewidth.Truncate(f, max(w-2, 1), "…")
		lines = append(lines, testsPad(theme.Bg(theme.BgSidebar).Render(" ")+muted(f), w))
	}
	for len(lines) < h {
		lines = append(lines, fill())
	}
	return strings.Join(lines[:h], "\n")
}

// containersSidebarTitle is "CONTAINERS · project ⋯"; while a filter is on
// the filter text ("/pg") takes the project's place.
func containersSidebarTitle(w int, focused bool, filter string) string {
	if w <= 0 {
		return ""
	}
	if w == 1 {
		return theme.Bg(theme.BgSidebar).Render("⋯")
	}
	suffix := projectName()
	if filter != "" {
		suffix = filter
	}
	title := sidebarTitle("CONTAINERS", w-2, focused, suffix)
	return title + theme.Bg(theme.BgSidebar).Render(" ") + theme.FgBg(theme.TextMuted, theme.BgSidebar).Render(theme.GlyphMore.String())
}
