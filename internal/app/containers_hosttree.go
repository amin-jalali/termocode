package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
)

// CONTAINERS is the hosts tree (the owner's layout):
//
//	▼ CONTAINERS
//	  ▼ default · this machine (docker 27)   4/6
//	    ▼ termocode                          2/3
//	        postgres-1
//	  ▶ baaytona · deploy@65.109.212.14
//	  + Add Server…
//
// The host in use comes first and starts open: its rows are the ones the
// view always had (every container key, menu and action). Every other
// host (docker context, podman connection) follows, closed; it is read
// once when it first opens (r reads it again) and a host that does not
// answer shows its reason under its own header (containers_servers.go).
// A host row keeps what CONTEXTS had: Enter uses it in this session, D
// makes it the default, the right-click menu. + Add Server… is the last
// row. Space folds any host, the one in use too (no engine call).

// hostTreeFold is the fold key of the host in use (open unless set).
func hostTreeFold(name string) string { return "hosttree/" + name }

// activeHostName is the context of the host in use ("" when none is
// known: no contexts, or podman on its local socket before the list).
func (st *servicesState) activeHostName() string {
	a := st.activeContext()
	if a == "" && st.isPodman() && st.hostLoaded["contexts"] {
		return podmanLocal
	}
	return a
}

// isActiveHost: name is the host whose rows are the view's own.
func (st *servicesState) isActiveHost(name string) bool { return name == st.activeHostName() }

// hostOpen: the host's rows are shown under its header.
func (st *servicesState) hostOpen(name string) bool {
	if st.isActiveHost(name) {
		return !st.collapsed[hostTreeFold(name)]
	}
	return st.server(name).open
}

// treeContexts are the hosts of the tree (none while the list is not
// known or failed: the tree then has the host in use only).
func (st *servicesState) treeContexts() []containers.Context {
	if !st.hasContexts() || !st.hostLoaded["contexts"] || st.containerErrors["contexts"] != "" {
		return nil
	}
	return st.contextChoices()
}

// upCount is "running/all".
func upCount(cs []containers.Container, idx []int) string {
	up, n := 0, 0
	each := func(c containers.Container) {
		n++
		if strings.EqualFold(c.State, "running") {
			up++
		}
	}
	if idx == nil {
		for _, c := range cs {
			each(c)
		}
	} else {
		for _, i := range idx {
			each(cs[i])
		}
	}
	return strconv.Itoa(up) + "/" + strconv.Itoa(n)
}

// hostTreeRows is the body of CONTAINERS.
func (st *servicesState) hostTreeRows(key string) []svcRow {
	active := st.activeHostName()
	choices := st.treeContexts()
	idx, here := -1, containers.Context{Name: active}
	for i, c := range choices {
		if c.Name == active {
			idx, here = i, c
		}
	}
	label := "this machine"
	if idx >= 0 {
		label = serverLabel(here)
	} else if active != "" {
		label = contextDisplayName(active)
	}
	if v := st.engineVersionLabel(); v != "" {
		label += " (" + v + ")"
	}
	head := svcRow{kind: svcRowHostItem, section: key, host: hostContext, idx: idx, hostID: active, name: label, current: true}
	if st.containerEngine.Reachable && st.hostLoaded["containers"] && st.containerErrors["containers"] == "" {
		head.count = upCount(st.containerRows, nil)
	}
	out := []svcRow{head}
	if st.hostOpen(active) {
		for _, r := range st.engineSectionBody("containers", key) {
			r.depth++
			out = append(out, r)
		}
	}
	for i, c := range choices {
		if i == idx {
			continue
		}
		r := svcRow{kind: svcRowHostItem, section: key, host: hostContext, idx: i, hostID: c.Name, name: serverLabel(c)}
		if h := st.server(c.Name); h.loaded {
			r.count = upCount(h.rows, nil)
		}
		out = append(out, r)
		out = append(out, st.serverRows(key, c)...)
	}
	if st.hasContexts() {
		out = append(out, addServerRow(key))
	}
	return out
}

// engineVersionLabel is "docker 27": the engine and its major version.
func (st *servicesState) engineVersionLabel() string {
	e := st.containerEngine
	s := e.Kind
	if v := e.Version.Version; v != "" && s != "" {
		s += " " + strings.Split(v, ".")[0]
	}
	return s
}

// toggleActiveHost folds or opens the host in use (no engine call: its
// rows are the view's own list).
func (m *Model) toggleActiveHost(name string) tea.Cmd {
	st := m.services
	k := hostTreeFold(name)
	st.collapsed[k] = !st.collapsed[k]
	st.touch()
	m.servicesClampCursor()
	return nil
}
