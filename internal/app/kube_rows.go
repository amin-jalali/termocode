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

// sectionTitle is kubeSectionTitle, with the : kind's own name.
func (st *kubeState) sectionTitle(sec string) string {
	if sec == kubeKindSection {
		return st.kindTitle()
	}
	return kubeSectionTitle(sec)
}

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
	case kubeKindSection:
		for _, o := range st.kindObjs {
			out = append(out, o.Name)
		}
	case kubeForwardsSection:
		for _, k := range st.forwardKeys() {
			out = append(out, st.forwardLabel(k))
		}
	default:
		for _, o := range st.extra[sec] {
			out = append(out, o.Name)
		}
	}
	return out
}

// kubeMatch: name passes the filter (case-insensitive, any part).
func kubeMatch(name, filter string) bool {
	return filter == "" || strings.Contains(strings.ToLower(name), strings.ToLower(filter))
}

// rows flattens the view: a problem, or the four sections and their items
// (and FORWARDS while a port-forward runs).
func (st *kubeState) rows() []kubeRow {
	if st == nil || !st.loaded {
		return nil
	}
	if st.err != "" {
		return []kubeRow{{kind: kubeRowNote, text: st.err}}
	}
	var out []kubeRow
	secs := append(kubeSections[:len(kubeSections):len(kubeSections)], kubeExtraSections...)
	if st.kind != nil { // the : kind, after the folded kinds (kube_kind.go)
		secs = append(secs[:len(secs):len(secs)], kubeKindSection)
	}
	if len(st.forwards) > 0 { // last, so starting one never moves the cursor
		secs = append(secs, kubeForwardsSection)
	}
	for _, sec := range secs {
		out = append(out, kubeRow{kind: kubeRowSection, section: sec})
		if st.collapsed[sec] {
			continue
		}
		if e := st.errs[sec]; e != "" {
			out = append(out, kubeRow{kind: kubeRowNote, section: sec, text: e})
			continue
		}
		if kubeIsExtra(sec) && !st.extraLoaded[sec] {
			out = append(out, kubeRow{kind: kubeRowHint, section: sec, text: "Loading…"})
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
			if sec == kubeKindSection && st.kind != nil {
				text = "No " + st.kind.Name
			}
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
	if kubeIsExtra(sec) && !st.extraLoaded[sec] {
		return "" // folded and never loaded: no number to show
	}
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
