package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/theme"
)

// ── "/" filter over every section (CP-12) ────────────────────────────────
//
// `/` starts typing a filter; every key narrows the rows of every section
// (PROJECT services, CONTAINERS, IMAGES, REGISTRIES, NETWORKS, VOLUMES,
// CONTEXTS) to the ones whose name contains the text, ignoring case. A
// match keeps its group (compose project, image repo, registry host) and
// a matching group keeps all its rows. Folded groups are looked into;
// folded sections stay folded but their header says how many rows match
// ("2/14"). The text shows in the title row. Enter keeps the filter and
// gives the keys back; Esc clears it.
//
// The filter works on the snapshot the view already has: typing never
// starts an engine call.

// filterQuery is the lower-case filter ("" = none).
func (st *servicesState) filterQuery() string { return strings.ToLower(st.filter) }

// containersFilterKey handles the filter keys (ok=false: not ours).
func (m *Model) containersFilterKey(k tea.KeyMsg) (tea.Cmd, bool) {
	st := m.services
	if st == nil {
		return nil, false
	}
	text, typing, handled := inlineFilterStep(st.filter, st.filterTyping, k)
	st.filterTyping = typing
	if !handled {
		return nil, false
	}
	m.setContainersFilter(text)
	return nil, true
}

// setContainersFilter changes the filter and keeps the cursor on the row
// it was on when that row is still shown, else on the first match.
func (m *Model) setContainersFilter(text string) {
	st := m.services
	if st.filter == text {
		st.touch()
		return
	}
	cur, had := m.servicesCurrentRow()
	st.filter = text
	st.touch()
	rows := st.rows()
	if had {
		for i, r := range rows {
			if sameRow(r, cur) {
				st.cursor = i
				return
			}
		}
	}
	for i, r := range rows {
		if r.kind == svcRowService || r.kind == svcRowHostItem {
			st.cursor = i
			return
		}
	}
	m.servicesClampCursor()
}

func sameRow(a, b svcRow) bool {
	return a.kind == b.kind && a.section == b.section && a.hostID == b.hostID && a.name == b.name && a.host == b.host
}

// rowMatches: the row's name contains q (lower case).
func (st *servicesState) rowMatches(r svcRow, q string) bool {
	has := func(s string) bool { return s != "" && strings.Contains(strings.ToLower(s), q) }
	switch r.kind {
	case svcRowService, svcRowGroup:
		return has(r.name)
	case svcRowHostItem:
		if has(r.name) || has(r.imageRef) {
			return true
		}
		if r.host == hostContainer && r.idx >= 0 && r.idx < len(st.containerRows) {
			for _, n := range st.containerRows[r.idx].Names {
				if has(strings.TrimPrefix(n, "/")) {
					return true
				}
			}
		}
	}
	return false
}

func structuralRow(r svcRow) bool {
	return r.kind == svcRowService || r.kind == svcRowGroup || r.kind == svcRowHostItem
}

// filterBody filters one section body. A row is kept when it matches,
// when a row above it in the tree (its group or parent row) matches, or
// when a row below it matches. Hints and notes follow the row they
// belong to; a body without object rows (Loading…, an error) stays as
// it is. any: the body had object rows.
func (st *servicesState) filterBody(body []svcRow, q string) (out []svcRow, any bool) {
	n := len(body)
	parent := make([]int, n)
	var stack []int
	for i, r := range body {
		if !structuralRow(r) {
			parent[i] = -1
			if len(stack) > 0 {
				parent[i] = stack[len(stack)-1]
			}
			continue
		}
		any = true
		for len(stack) > 0 && body[stack[len(stack)-1]].depth >= r.depth {
			stack = stack[:len(stack)-1]
		}
		parent[i] = -1
		if len(stack) > 0 {
			parent[i] = stack[len(stack)-1]
		}
		stack = append(stack, i)
	}
	if !any {
		return body, false
	}
	match := make([]bool, n)
	up := make([]bool, n)   // an ancestor matches
	down := make([]bool, n) // a descendant matches
	for i, r := range body {
		if structuralRow(r) {
			match[i] = st.rowMatches(r, q)
			if p := parent[i]; p >= 0 {
				up[i] = match[p] || up[p]
			}
		}
	}
	for i := n - 1; i >= 0; i-- {
		if structuralRow(body[i]) && (match[i] || down[i]) {
			if p := parent[i]; p >= 0 {
				down[p] = true
			}
		}
	}
	keep := make([]bool, n)
	for i, r := range body {
		switch {
		case structuralRow(r):
			keep[i] = match[i] || up[i] || down[i]
		case parent[i] >= 0:
			p := parent[i]
			keep[i] = keep[p] && (match[p] || up[p])
		default:
			keep[i] = r.kind == svcRowNote || r.kind == svcRowRetry // a section's problem stays
		}
	}
	for i, r := range body {
		if keep[i] {
			out = append(out, r)
		}
	}
	return out, true
}

// applyFilter narrows the view's rows to the filter. Section headers
// stay (Tab still jumps between them); a section without a match has no
// body; when nothing matches anywhere one hint says so.
func (st *servicesState) applyFilter(rows []svcRow) []svcRow {
	q := st.filterQuery()
	if q == "" {
		return rows
	}
	var out []svcRow
	anyObjects, anyKept := false, false
	flush := func(body []svcRow) {
		kept, objects := st.filterBody(body, q)
		if objects {
			anyObjects = true
			for _, r := range kept {
				if structuralRow(r) {
					anyKept = true
					break
				}
			}
		}
		out = append(out, kept...)
	}
	start := 0
	for i, r := range rows {
		if r.kind != svcRowSection {
			continue
		}
		flush(rows[start:i])
		out = append(out, r)
		start = i + 1
	}
	flush(rows[start:])
	if anyObjects && !anyKept {
		note := svcRow{kind: svcRowHint, text: "No rows match \"" + st.filter + "\" · Esc clears"}
		out = append([]svcRow{note}, out...)
	}
	return out
}

// filterCount is a header's count while a filter is on: "matches/all"
// (the plain count when it is not a number: loading, "?").
func (st *servicesState) filterCount(sec, plain string) string {
	if st.filter == "" {
		return plain
	}
	if _, err := strconv.Atoi(plain); err != nil {
		return plain
	}
	body, _ := st.filterBody(st.hostSectionBody(sec), st.filterQuery())
	kind := map[string]hostKind{"containers": hostContainer, "images": hostImage, "registries": hostRegistry,
		"networks": hostNetwork, "volumes": hostVolume, "contexts": hostContext}[sec]
	seen := map[int]bool{}
	for _, r := range body {
		if r.kind == svcRowHostItem && r.host == kind {
			seen[r.idx] = true
		}
	}
	return strconv.Itoa(len(seen)) + "/" + plain
}

// projectFilterCount is the PROJECT / LOCAL header count with a filter.
func (st *servicesState) projectFilterCount(section string, n int) string {
	if st.filter == "" {
		return strconv.Itoa(n)
	}
	q, k := st.filterQuery(), 0
	list := st.compose
	if section == svcSecLocal {
		list = st.local
	}
	for _, s := range list {
		if strings.Contains(strings.ToLower(s.Name), q) {
			k++
		}
	}
	return strconv.Itoa(k) + "/" + strconv.Itoa(n)
}

// filterTitle is the title row's text while a filter is on ("" = none):
// "/text" and a caret while typing.
func (st *servicesState) filterTitle() string {
	if st == nil || (st.filter == "" && !st.filterTyping) {
		return ""
	}
	return inlineFilterTitle(st.filter, st.filterTyping, theme.CurrentIconMode == theme.IconModeASCII)
}
