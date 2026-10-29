package app

import (
	"strings"
)

// ── Kubernetes view: rows ─────────────────────────────────────────────────

type kubeRowKind int

const (
	kubeRowSection kubeRowKind = iota
	kubeRowItem                // a pod, deployment, service or job
	kubeRowNote                // a problem line (red)
	kubeRowHint                // what to do about it, or "No pods"
)

type kubeRow struct {
	kind    kubeRowKind
	section string // pods / deployments / services / jobs
	idx     int    // item rows: index into the section's slice
	name    string // item rows: the object's name
	text    string // note and hint rows
}

func (r kubeRow) selectable() bool { return r.kind == kubeRowSection || r.kind == kubeRowItem }

// kubeSectionTitle is a section header's label.
func kubeSectionTitle(sec string) string { return strings.ToUpper(sec) }

// names is the object names of one section, in kubectl's order.
func (st *kubeState) names(sec string) []string {
	var out []string
	switch sec {
	case "pods":
		for _, p := range st.pods {
			out = append(out, p.Name)
		}
	case "deployments":
		for _, d := range st.deploys {
			out = append(out, d.Name)
		}
	case "services":
		for _, s := range st.services {
			out = append(out, s.Name)
		}
	case "jobs":
		for _, j := range st.jobs {
			out = append(out, j.Name)
		}
	}
	return out
}

// kubeMatch: name passes the filter (case-insensitive, any part).
func kubeMatch(name, filter string) bool {
	return filter == "" || strings.Contains(strings.ToLower(name), strings.ToLower(filter))
}

// rows flattens the view: a problem, or the four sections and their items.
func (st *kubeState) rows() []kubeRow {
	if st == nil || !st.loaded {
		return nil
	}
	if st.err != "" {
		return []kubeRow{{kind: kubeRowNote, text: st.err}}
	}
	var out []kubeRow
	for _, sec := range kubeSections {
		out = append(out, kubeRow{kind: kubeRowSection, section: sec})
		if st.collapsed[sec] {
			continue
		}
		if e := st.errs[sec]; e != "" {
			out = append(out, kubeRow{kind: kubeRowNote, section: sec, text: e})
			continue
		}
		names := st.names(sec)
		shown := 0
		for i, name := range names {
			if kubeMatch(name, st.filter) {
				out = append(out, kubeRow{kind: kubeRowItem, section: sec, idx: i, name: name})
				shown++
			}
		}
		if shown == 0 {
			text := "No " + sec
			if st.filter != "" && len(names) > 0 {
				text = "No match"
			}
			out = append(out, kubeRow{kind: kubeRowHint, section: sec, text: text})
		}
	}
	return out
}

// count is a section header's number: "5", or "2/5" while filtering.
func (st *kubeState) count(sec string) string {
	names := st.names(sec)
	n := len(names)
	if st.filter == "" {
		return itoa(n)
	}
	k := 0
	for _, name := range names {
		if kubeMatch(name, st.filter) {
			k++
		}
	}
	return itoa(k) + "/" + itoa(n)
}

// currentRow is the row under the cursor.
func (st *kubeState) currentRow() (kubeRow, bool) {
	rows := st.rows()
	if st.cursor < 0 || st.cursor >= len(rows) {
		return kubeRow{}, false
	}
	return rows[st.cursor], true
}
