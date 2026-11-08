package app

import (
	"context"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The hosts tree from the page, as the laptop's Containers view
// (CONTEXTS, containers_servers.go) and Databases view (SERVERS,
// db_servers.go) draw it. containers.servers lists every host (context)
// at once; containers.server reads one host's containers when the page
// opens it (and on Refresh), with its own time limit: a host that does
// not answer fails its own read with one line, never the others. +
// Add Server… creates a context for ssh://user@host (the page asked
// first, Cancel first). db.add_container adds a database container of a
// listed host to Databases as the laptop's SERVERS row does: the SSH
// tunnel to the host and the container by name and own port, with no
// password (the page's read asks for it; db.secret keeps it in the
// secret store). A host or container is named only by what the last
// listing gave.

func init() {
	webOps[webproto.OpContainersServers] = (*Model).webContainersServers
	webOps[webproto.OpContainersServer] = (*Model).webContainersServer
	webOps[webproto.OpContainersAddServer] = (*Model).webContainersAddServer
	webOps[webproto.OpDBAddContainer] = (*Model).webDBAddContainer
}

// webServerWait bounds one host's list (a remote engine over SSH).
const webServerWait = 20 * time.Second

// webServersSeen is what the hosts tree listed last (Update only).
type webServersSeen struct {
	hosts map[string]containers.Context              // by name
	ctrs  map[string]map[string]containers.Container // host → id → container
}

func (m *Model) webServersSeen() *webServersSeen {
	if m.web.more.servers == nil {
		m.web.more.servers = &webServersSeen{hosts: map[string]containers.Context{}, ctrs: map[string]map[string]containers.Container{}}
	}
	return m.web.more.servers
}

// webServerHost is one host for the page: "where" without the scheme.
func webServerHost(c containers.Context, session string) webproto.ServerHost {
	h := webproto.ServerHost{Name: webName(c.Name), Current: c.Current, Session: session != "" && c.Name == session,
		SSH: containerhost.SSHEndpoint(c.DockerEndpoint)}
	if w := serverTarget(c); w == "this machine" {
		h.Local = true
	} else {
		h.Where = webName(w)
	}
	return h
}

func (m *Model) webContainersServers(call *webCall) tea.Cmd {
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, webContainersWait)
		defer cancel()
		list, err := webHostContexts(ctx, h)
		if err != nil {
			call.fail(webproto.CodeConflict, errLine(aiContainersRedact(err.Error())))
			return nil
		}
		return applyMsg(func(m *Model) tea.Cmd {
			seen := m.webServersSeen()
			seen.hosts = map[string]containers.Context{}
			out := webproto.ServersList{Engine: h.bin(), Hosts: []webproto.ServerHost{}}
			for i, c := range list {
				if i == webproto.MaxServers {
					break
				}
				if c.Name == "" {
					continue
				}
				seen.hosts[c.Name] = c
				out.Hosts = append(out.Hosts, webServerHost(c, h.session))
			}
			call.reply(out)
			return nil
		})
	}
}

// webServerEngine is h's engine on the host name.
func webServerEngine(h aiContainersHost, name string) containers.Engine {
	e := h.e
	e.Context = name
	if name == podmanLocal {
		e.Context = ""
	}
	return e
}

func (m *Model) webContainersServer(call *webCall) tea.Cmd {
	name := call.args.(*webproto.ContextArgs).Name
	seen := m.webServersSeen()
	if _, ok := seen.hosts[name]; !ok {
		call.fail(webproto.CodeNotFound, "That host is not in the list. Open the list again.")
		return nil
	}
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	e, list := webServerEngine(h, name), serverListContainers
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, webServerWait)
		defer cancel()
		rows, err := list(ctx, e)
		if err != nil {
			reason := shortRemoteError(sanitizeOutputLine(errLine(aiContainersRedact(err.Error()))))
			if reason == "" || ctx.Err() != nil {
				reason = "does not answer"
			}
			call.fail(webproto.CodeConflict, reason)
			return nil
		}
		return applyMsg(func(m *Model) tea.Cmd {
			out := webproto.ServerContainers{Name: webName(name), Containers: []webproto.ContainerItem{}}
			raw := map[string]containers.Container{}
			for i, c := range rows {
				if i == webproto.MaxContainers {
					out.More = len(rows) - i
					break
				}
				it := webContainerItem(c, false)
				it.Name = webName(serverContainerName(c))
				it.DB = dbDriverForImage(c.Image)
				out.Containers = append(out.Containers, it)
				raw[c.ID] = c
			}
			m.webServersSeen().ctrs[name] = raw
			call.reply(out)
			return nil
		})
	}
}

func (m *Model) webContainersAddServer(call *webCall) tea.Cmd {
	a := call.args.(*webproto.AddServerArgs)
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	spec := containers.ContextSpec{Name: a.Name, Host: a.Host}
	if _, err := h.e.CreateContextArgs(spec); err != nil {
		call.fail(webproto.CodeBadRequest, sanitizeOutputLine(errLine(err.Error())))
		return nil
	}
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, time.Minute)
		defer cancel()
		if list, err := webHostContexts(ctx, h); err == nil {
			for _, c := range list {
				if c.Name == a.Name {
					call.fail(webproto.CodeConflict, sanitizeOutputLine(a.Name)+" already exists.")
					return nil
				}
			}
		}
		err := h.e.CreateContext(ctx, spec)
		return applyMsg(func(m *Model) tea.Cmd {
			if err != nil {
				call.fail(webproto.CodeConflict, "Could not add it: "+sanitizeOutputLine(errLine(aiContainersRedact(err.Error()))))
				return nil
			}
			m.webAuditOp(call, a.Name+" "+a.Host, "created")
			call.reply(webproto.HostDone{Note: "Open it to see its containers."})
			if m.services != nil && m.services.visible {
				return m.containersRefreshContexts()
			}
			return nil
		})
	}
}

func (m *Model) webDBAddContainer(call *webCall) tea.Cmd {
	a := call.args.(*webproto.DBAddContainerArgs)
	seen := m.webServersSeen()
	host, okHost := seen.hosts[a.Host]
	c, okCtr := seen.ctrs[a.Host][a.ID]
	if !okHost || !okCtr {
		call.fail(webproto.CodeNotFound, "That container is not in the list. Open the host again.")
		return nil
	}
	driver := dbDriverForImage(c.Image)
	if driver == "" {
		call.fail(webproto.CodeConflict, "Not a database container (PostgreSQL, MySQL, MariaDB, Redis or Valkey).")
		return nil
	}
	engine := "docker"
	if st := m.services; st != nil && st.containerEngine.Kind != "" {
		engine = st.containerEngine.Kind
	}
	t := dbRuntimeTarget{driver: driver, name: sanitizeDBText(dbContainerName(c)), cont: c, isCont: true, running: !dbContainerStopped(c),
		context: a.Host, endpoint: host.DockerEndpoint, engine: engine}
	t.user, t.database = dbPrefill(driver, nil)
	conn, ok := dbRemoteConn(t)
	if !ok {
		call.fail(webproto.CodeConflict, "This host is not reached over SSH. Use Open in Databases on the laptop.")
		return nil
	}
	m.ensureDatabasesLoaded()
	s := m.databases
	if saved, ok := s.dbRemoteSavedMatch(conn); ok {
		call.reply(webproto.DBAdded{Conn: saved.ID, Name: webName(dbConnName(saved)), Existing: true})
		return nil
	}
	if conn.Driver == "redis" {
		conn.ID = dbRedisConnectionID(conn)
	} else {
		conn.ID = dbConnectionID(conn)
	}
	if base := conn.ID; s.duplicateConnectionID(base) {
		conn.ID = base + "-" + strconv.Itoa(conn.Port)
		for i := 2; s.duplicateConnectionID(conn.ID); i++ {
			conn.ID = base + "-" + strconv.Itoa(i)
		}
	}
	conn.Name = t.name
	s.pendingConn, s.pendingPassword, s.pendingPasswordRef, s.pendingSSH = conn, "", dbTypedRef{}, dbSSHSecrets{}
	if cmd := m.databaseSavePending(false); cmd != nil {
		s.pendingConn = dbclient.Conn{}
		call.fail(webproto.CodeConflict, "Could not save the connection.")
		return cmd
	}
	m.webAuditOp(call, conn.ID+" "+a.Host+" "+conn.Host, "added")
	call.reply(webproto.DBAdded{Conn: conn.ID, Name: webName(conn.Name)})
	return nil
}
