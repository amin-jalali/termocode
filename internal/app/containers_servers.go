package app

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The hosts tree (CONTEXTS): every host at once, no Switch needed. Each
// context (the local engine, every docker context / podman connection)
// is a host header that reads "name · where": baaytona ·
// deploy@65.109.212.14. Space opens a host; the first open lists its
// containers once (r / R on it lists again; nothing polls), grouped by
// compose project. A host that does not answer shows its one-line
// reason under its header and never hides the others. Enter on a host
// still uses it in this session; Enter on a database container opens it
// in Databases (through an SSH tunnel for an ssh:// host). The last row,
// + Add Server…, is Create Context: a name, then ssh://user@host, then a
// card, Cancel first.

const (
	hostServerContainer hostKind = 101 // a container under a host of CONTEXTS
	hostAddServer       hostKind = 102 // the "+ Add Server…" row
)

// addServerTitle is the last row of the hosts tree.
const addServerTitle = "+ Add Server…"

// serverListContainers lists one host's containers (tests replace it).
var serverListContainers = func(ctx context.Context, e containers.Engine) ([]containers.Container, error) {
	return e.ListContainers(ctx)
}

// serverHost is what one host of the tree has loaded.
type serverHost struct {
	open    bool
	loading bool
	loaded  bool
	gen     uint64
	rows    []containers.Container
	err     string
}

// serverListMsg is one host's list.
type serverListMsg struct {
	context string
	gen     uint64
	rows    []containers.Container
	err     error
}

func (st *servicesState) server(name string) *serverHost {
	if st.servers == nil {
		st.servers = map[string]*serverHost{}
	}
	h := st.servers[name]
	if h == nil {
		h = &serverHost{}
		st.servers[name] = h
	}
	return h
}

// serverTarget is where a context points, short: "deploy@65.109.212.14"
// for ssh://, "host:2376" for tcp://, "this machine" for a local socket.
func serverTarget(c containers.Context) string {
	e := strings.TrimSpace(c.DockerEndpoint)
	if c.Name == podmanLocal || e == "" && (c.Name == "default" || c.Name == "") {
		return "this machine"
	}
	low := strings.ToLower(e)
	switch {
	case strings.HasPrefix(low, "unix://"), strings.HasPrefix(low, "npipe://"), e == "this machine (no connection)":
		return "this machine"
	case strings.HasPrefix(low, "ssh://"), strings.HasPrefix(low, "tcp://"):
		if u, err := url.Parse(e); err == nil && u.Host != "" {
			s := u.Host
			if u.User != nil && u.User.Username() != "" {
				s = u.User.Username() + "@" + s
			}
			return sanitizeOutputLine(strings.TrimSuffix(s, ":22"))
		}
	}
	return sanitizeOutputLine(e)
}

// serverLabel is the header text of a host.
func serverLabel(c containers.Context) string {
	if t := serverTarget(c); t != "" {
		dot := " · "
		if theme.CurrentIconMode == theme.IconModeASCII {
			dot = " - "
		}
		return contextDisplayName(c.Name) + dot + t
	}
	return contextDisplayName(c.Name)
}

// serverRows are the rows under an open host header.
func (st *servicesState) serverRows(key string, c containers.Context) []svcRow {
	h := st.server(c.Name)
	if !h.open {
		return nil
	}
	switch {
	case h.err != "":
		return []svcRow{{kind: svcRowNote, section: key, text: h.err, depth: 1}}
	case !h.loaded:
		return []svcRow{{kind: svcRowHint, section: key, text: "Loading…", depth: 1}}
	case len(h.rows) == 0:
		return []svcRow{{kind: svcRowHint, section: key, text: "No containers", depth: 1}}
	}
	var out []svcRow
	for _, g := range containers.GroupIndexes(h.rows, "") {
		gkey := hostGroup("servers/"+c.Name, g.Name)
		up := 0
		for _, i := range g.Idx {
			if strings.EqualFold(h.rows[i].State, "running") {
				up++
			}
		}
		out = append(out, svcRow{kind: svcRowGroup, section: gkey, name: g.Name, depth: 1, hostID: c.Name,
			count: strconv.Itoa(up) + "/" + strconv.Itoa(len(g.Idx))})
		if st.collapsed[gkey] {
			continue
		}
		for _, i := range g.Idx {
			out = append(out, svcRow{kind: svcRowHostItem, section: key, host: hostServerContainer, idx: i, depth: 2,
				hostID: c.Name, name: serverContainerName(h.rows[i])})
		}
	}
	return out
}

// serverContainerName is the container's full name, as `docker ps`
// shows it on that host.
func serverContainerName(c containers.Container) string {
	if n := dbContainerName(c); n != "" {
		return sanitizeOutputLine(n)
	}
	return containerShownName(c)
}

// addServerRow is the last row of CONTAINERS.
func addServerRow(key string) svcRow {
	return svcRow{kind: svcRowHostItem, section: key, host: hostAddServer, idx: -1, name: addServerTitle}
}

// toggleServer opens or closes a host; the first open lists it. The
// host in use only folds (its rows are the view's own list).
func (m *Model) toggleServer(name string) tea.Cmd {
	st := m.services
	if st.isActiveHost(name) {
		return m.toggleActiveHost(name)
	}
	h := st.server(name)
	h.open = !h.open
	st.touch()
	m.servicesClampCursor()
	if !h.open || h.loaded || h.loading {
		return nil
	}
	return m.loadServer(name)
}

// loadServer lists one host's containers (once per open or r / R).
func (m *Model) loadServer(name string) tea.Cmd {
	st := m.services
	if st == nil || st.containerEngine.Binary == "" {
		return nil
	}
	h := st.server(name)
	h.gen++
	h.loading, h.err = true, ""
	gen := h.gen
	e := st.containerEngine
	e.Context = name
	if name == podmanLocal {
		e.Context = ""
	}
	list := serverListContainers
	st.touch()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		rows, err := list(ctx, e)
		return serverListMsg{context: name, gen: gen, rows: rows, err: err}
	}
}

func (m *Model) onServerList(msg serverListMsg) tea.Cmd {
	st := m.services
	if st == nil {
		return nil
	}
	h := st.server(msg.context)
	if msg.gen != h.gen {
		return nil
	}
	h.loading = false
	if msg.err != nil {
		h.err, h.loaded, h.rows = shortRemoteError(sanitizeOutputLine(errLine(msg.err.Error()))), false, nil
		if h.err == "" {
			h.err = "does not answer"
		}
	} else {
		h.rows, h.loaded = msg.rows, true
	}
	st.touch()
	m.servicesClampCursor()
	return nil
}

// selectedServerContainer is the container row under the cursor.
func (m *Model) selectedServerContainer() (containers.Container, string, bool) {
	r, ok := m.servicesCurrentRow()
	if !ok || r.kind != svcRowHostItem || r.host != hostServerContainer {
		return containers.Container{}, "", false
	}
	h := m.services.server(r.hostID)
	if r.idx < 0 || r.idx >= len(h.rows) {
		return containers.Container{}, "", false
	}
	return h.rows[r.idx], r.hostID, true
}

// serverContextOf is the context row of a name.
func (st *servicesState) serverContextOf(name string) containers.Context {
	for _, c := range st.contextChoices() {
		if c.Name == name {
			return c
		}
	}
	return containers.Context{Name: name}
}

// serversKey handles the keys of the hosts tree (ok=false: not its row).
func (m *Model) serversKey(key string) (tea.Cmd, bool) {
	r, ok := m.servicesCurrentRow()
	if !ok {
		return nil, false
	}
	switch {
	case r.kind == svcRowHostItem && r.host == hostAddServer:
		if key == "enter" || key == "l" || key == "n" {
			return m.askContextCreate(), true
		}
	case r.kind == svcRowHostItem && r.host == hostContext && (key == " " || key == "right"):
		return m.toggleServer(r.hostID), true
	case r.kind == svcRowHostItem && r.host == hostContext && key == "r" && m.services.isActiveHost(r.hostID):
		return m.containersRefresh("containers"), true
	case r.kind == svcRowHostItem && r.host == hostContext && key == "r" && m.services.server(r.hostID).open:
		return m.loadServer(r.hostID), true
	case r.kind == svcRowGroup && strings.HasPrefix(r.section, "grp/servers/") && (key == " " || key == "enter"):
		m.services.collapsed[r.section] = !m.services.collapsed[r.section]
		m.services.touch()
		return nil, true
	case r.kind == svcRowHostItem && r.host == hostServerContainer:
		c, name, ok := m.selectedServerContainer()
		if !ok {
			return nil, true
		}
		switch key {
		case "enter", "l":
			if t, ok := m.serverDBTarget(c, name); ok {
				return m.databaseOpenTarget(t), true
			}
			return m.servicesToast(toast.Info, "Not a database container", "Enter uses its host: select "+contextDisplayName(name)+" and press Enter"), true
		case "y":
			m.copyToClipboard(c.ID)
			return m.servicesToast(toast.Info, "Copied container id", shortImageID(c.ID)), true
		case "r", "R":
			return m.loadServer(name), true
		case "s":
			return m.serverContainerAction("start", c, name), true
		case "x":
			return m.serverContainerAction("stop", c, name), true
		}
		// The other keys move, fold, filter…; the container keys of the
		// host in use check for its own rows, so they do nothing here.
		return nil, false
	}
	return nil, false
}

// serverDBTarget is the database target of a container of a host.
func (m *Model) serverDBTarget(c containers.Container, host string) (dbRuntimeTarget, bool) {
	st := m.services
	driver := dbDriverForImage(c.Image)
	if driver == "" {
		return dbRuntimeTarget{}, false
	}
	ctx := st.serverContextOf(host)
	t := dbRuntimeTarget{driver: driver, name: sanitizeDBText(dbContainerName(c)), port: dbPublishedContainerPort(c), cont: c, isCont: true,
		running: !dbContainerStopped(c), context: host, endpoint: ctx.DockerEndpoint, engine: st.containerEngine.Kind}
	if t.port == 0 {
		t.port = defaultDBPort(driver)
	}
	t.user, t.database = dbPrefill(driver, nil)
	return t, true
}

// serverFooter is the footer of a hosts-tree row.
func (st *servicesState) serverFooter(r svcRow) (string, string) {
	switch r.host {
	case hostAddServer:
		return "add a host by ssh://user@host", "Enter asks the name, then the host"
	case hostServerContainer:
		h := st.server(r.hostID)
		if r.idx < 0 || r.idx >= len(h.rows) {
			return "", ""
		}
		c := h.rows[r.idx]
		l2 := c.Status
		if dbDriverForImage(c.Image) != "" {
			l2 += " · Enter open in Databases"
		}
		return c.Image + " · " + contextDisplayName(r.hostID), l2
	}
	return "", ""
}

// renderServerRow draws a container of a host or the + Add Server… row.
func (st *servicesState) renderServerRow(r svcRow, active, focused bool, w int) string {
	if r.host == hostAddServer {
		return renderHostLine(active, focused, w, r.depth, "", 0, r.name, theme.TextSecondary, nil)
	}
	h := st.server(r.hostID)
	if r.idx < 0 || r.idx >= len(h.rows) {
		return ""
	}
	c := h.rows[r.idx]
	g, gcol := containerGlyph(c)
	var cols []hostCol
	if w >= 28 {
		up, ucol := containerUptime(c)
		cols = append(cols, hostCol{text: up, width: 6, right: true, col: ucol})
	}
	return renderHostLine(active, focused, w, r.depth, g.String(), gcol, r.name, theme.TextPrimary, cols)
}

// serverStartStop starts or stops a container of another host (tests
// replace it).
var serverStartStop = func(ctx context.Context, e containers.Engine, verb, id string) error {
	if verb == "stop" {
		return e.Stop(ctx, id)
	}
	return e.Start(ctx, id)
}

// serverActionMsg is a start or stop on another host, done.
type serverActionMsg struct {
	context, verb, name string
	err                 error
}

// serverContainerAction is s / x on a container of another host: the
// engine call of the host in use, with that host's context; the host is
// listed again after it.
func (m *Model) serverContainerAction(verb string, c containers.Container, host string) tea.Cmd {
	st := m.services
	if st == nil || st.containerEngine.Binary == "" {
		return nil
	}
	e := st.containerEngine
	e.Context = host
	if host == podmanLocal {
		e.Context = ""
	}
	run, id, name := serverStartStop, c.ID, serverContainerName(c)
	title := "Starting " + name
	if verb == "stop" {
		title = "Stopping " + name
	}
	return tea.Batch(m.servicesToast(toast.Info, title, "on "+contextDisplayName(host)), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return serverActionMsg{context: host, verb: verb, name: name, err: run(ctx, e, verb, id)}
	})
}

// onServerAction says a failure and lists the host again.
func (m *Model) onServerAction(msg serverActionMsg) tea.Cmd {
	var cmds []tea.Cmd
	if msg.err != nil {
		cmds = append(cmds, m.servicesToast(toast.Errr, "Could not "+msg.verb+" "+msg.name, shortRemoteError(sanitizeOutputLine(errLine(msg.err.Error())))))
	}
	return tea.Batch(append(cmds, m.loadServer(msg.context))...)
}
