package app

import (
	"context"
	"net/url"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Database containers on a remote engine (a docker context or podman
// connection over ssh://). They rarely publish a port, so the connection
// is an SSH tunnel to that host (its user, the ssh-agent) whose target is
// the container itself: its name and its own port. The container's IP is
// found again by name on every connect (dbclient.ContainerAddress), so a
// recreated container still connects.

// dbRemoteHost is one ssh:// engine host: a context and where it points.
type dbRemoteHost struct {
	context  string // the docker context / podman connection
	endpoint string // ssh://user@host[:port]
	engine   string // docker or podman
}

// dbSSHFromEndpoint is the tunnel of an ssh:// endpoint (user, host and
// port from the URI, auth by the ssh-agent). ok=false: not ssh://.
func dbSSHFromEndpoint(endpoint string) (dbclient.SSHConfig, bool) {
	if !containerhost.SSHEndpoint(endpoint) {
		return dbclient.SSHConfig{}, false
	}
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Hostname() == "" {
		return dbclient.SSHConfig{}, false
	}
	cfg := dbclient.SSHConfig{Host: u.Hostname(), Auth: dbclient.SSHAuthAgent}
	if u.User != nil {
		cfg.User = u.User.Username()
	}
	if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 && p != 22 {
		cfg.Port = p
	}
	return cfg, true
}

// dbSSHLabel is "user@host" (":port" when not 22) of a tunnel.
func dbSSHLabel(cfg dbclient.SSHConfig) string {
	s := cfg.Host
	if cfg.User != "" {
		s = cfg.User + "@" + s
	}
	if cfg.Port != 0 && cfg.Port != 22 {
		s += ":" + strconv.Itoa(cfg.Port)
	}
	return sanitizeDBText(s)
}

// contextEndpoint is the endpoint of a context of the loaded list.
func (st *servicesState) contextEndpoint(name string) string {
	if st == nil || name == "" {
		return ""
	}
	for _, c := range st.contextRows {
		if c.Name == name {
			return c.DockerEndpoint
		}
	}
	return ""
}

// dbContainerName is the container's name without the leading slash.
func dbContainerName(c containers.Container) string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return ""
}

// dbContainerPort is the container's own port for the driver: the
// driver's default when it exposes it, else its first TCP port.
func dbContainerPort(c containers.Container, driver string) int {
	def := defaultDBPort(driver)
	first := 0
	for _, p := range c.Ports {
		if p.Type != "" && p.Type != "tcp" {
			continue
		}
		if p.PrivatePort == def {
			return def
		}
		if first == 0 && p.PrivatePort > 0 {
			first = p.PrivatePort
		}
	}
	if first > 0 {
		return first
	}
	return def
}

// dbRemoteConn is the connection of a database container on an ssh://
// host: the tunnel, the container's name and port, and the container
// itself (found again on connect).
func dbRemoteConn(t dbRuntimeTarget) (dbclient.Conn, bool) {
	cfg, ok := dbSSHFromEndpoint(t.endpoint)
	name := dbContainerName(t.cont)
	if !ok || !t.isCont || name == "" {
		return dbclient.Conn{}, false
	}
	engine := t.engine
	if engine == "" {
		engine = "docker"
	}
	ref := &dbclient.ContainerRef{Name: name, Context: t.context, Engine: engine}
	if len(t.cont.Networks) > 0 {
		ref.Network = t.cont.Networks[0]
	}
	c := dbclient.Conn{Driver: t.driver, Host: name, Port: dbContainerPort(t.cont, t.driver), User: t.user, Database: t.database,
		Source: dbclient.SourceUser, SSH: &cfg, Container: ref}
	if t.driver == "postgres" {
		// The container network is private and the server's own; the
		// SSH tunnel is the encryption.
		c.Params = map[string]string{"sslmode": "disable"}
	}
	return c, true
}

// dbRemoteSavedMatch is a saved connection to the same remote container.
func (s *dbState) dbRemoteSavedMatch(c dbclient.Conn) (dbclient.Conn, bool) {
	for _, saved := range s.conns {
		if saved.Container != nil && c.Container != nil && saved.Container.Name == c.Container.Name && saved.Container.Context == c.Container.Context {
			return saved, true
		}
	}
	return dbclient.Conn{}, false
}

// databaseOpenRemote is Open in Databases for a container of an ssh://
// host: it asks only for the database password (blank if none).
func (m *Model) databaseOpenRemote(t dbRuntimeTarget, c dbclient.Conn, selectPath string) tea.Cmd {
	s := m.databases
	if saved, ok := s.dbRemoteSavedMatch(c); ok {
		s.databaseSelectPath(saved.ID)
		m.databasesSaveSession()
		return m.pushToastDetail(toast.Info, "Already in Databases", sanitizeDBText(dbConnName(saved)))
	}
	if c.Driver == "redis" {
		c.ID = dbRedisConnectionID(c)
	} else {
		c.ID = dbConnectionID(c)
	}
	if base := c.ID; s.duplicateConnectionID(base) {
		c.ID = base + "-" + strconv.Itoa(c.Port)
	}
	c.Name = c.ID
	s.pendingConn, s.pendingPassword, s.addFromURL, s.addLibSQL = c, "", false, false
	if selectPath != "" {
		s.databaseSelectPath(selectPath)
	}
	// The title says where (it fits 80 columns); the label who.
	target := c.Host
	if c.User != "" {
		target = c.User + "@" + target
	}
	cmd := m.databaseAddPrompt(promptKindDBPassword, "Add "+t.name+" · "+dbSSHLabel(*c.SSH),
		"Password for "+sanitizeDBText(target)+" (blank if none):", "", true)
	m.prompt.SetAllowEmpty(true)
	return cmd
}

// dbResolveContainer points tun at c's container: its IP is asked from
// the server by the container's name and dials to c.Host go there.
func dbResolveContainer(ctx context.Context, c dbclient.Conn, tun *dbclient.Tunnel) error {
	if c.Container == nil || tun == nil {
		return nil
	}
	ip, err := dbclient.ContainerAddress(ctx, tun, *c.Container)
	if err != nil {
		return err
	}
	host := c.Host
	if host == "" {
		host = c.Container.Name
	}
	tun.MapHost(host, ip)
	return nil
}

// ── discovery on the ssh:// hosts ──────────────────────────────────────

// dbRemoteFound is the database containers of one ssh:// host.
type dbRemoteFound struct {
	host       dbRemoteHost
	containers []containers.Container
	err        error
}

// dbDiscoverRemote lists the containers of every ssh:// context at the
// same time (one list each, no polling); a host that fails is kept with
// its error and never hides the others.
func dbDiscoverRemote(ctx context.Context, dir string,
	listContexts func(context.Context, string) ([]containers.Context, error),
	listContainers func(context.Context, string, string) ([]containers.Container, error)) []dbRemoteFound {
	ctxs, err := listContexts(ctx, dir)
	if err != nil {
		return nil
	}
	var hosts []dbRemoteHost
	for _, c := range ctxs {
		if c.Error == "" && c.Name != "" && containerhost.SSHEndpoint(c.DockerEndpoint) {
			hosts = append(hosts, dbRemoteHost{context: c.Name, endpoint: c.DockerEndpoint})
		}
	}
	sort.Slice(hosts, func(a, b int) bool { return hosts[a].context < hosts[b].context })
	out := make([]dbRemoteFound, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		crash.Go("the database search", func() {
			defer wg.Done()
			rows, err := listContainers(ctx, dir, h.context)
			out[i] = dbRemoteFound{host: h, containers: rows, err: err}
		})
	}
	wg.Wait()
	return out
}

// dbDetectEngine finds the engine once for the remote lists.
func dbDetectEngine(dir, context string) containers.Engine {
	return containers.Locate(containers.DetectOptions{Exec: servicesExec, Dir: dir, Context: context, Android: runtime.GOOS == "android", Timeout: 10 * time.Second})
}

func init() {
	dbListContexts = func(ctx context.Context, dir string) ([]containers.Context, error) {
		e := dbDetectEngine(dir, "")
		if e.Binary == "" {
			return nil, nil
		}
		return e.ListContexts(ctx)
	}
	dbListContextContainers = func(ctx context.Context, dir, name string) ([]containers.Container, error) {
		e := dbDetectEngine(dir, name)
		if e.Binary == "" {
			return nil, nil
		}
		return e.ListContainers(ctx)
	}
}

// dbRemoteSuggestionPath is the tree path of a remote container row.
func dbRemoteSuggestionPath(context, name string) string {
	return "suggest-remote-" + context + "/" + name
}

// addRemoteSuggestions turns the database containers of the ssh:// hosts
// into suggestion rows (a saved connection to the same container hides
// its row).
func (s *dbState) addRemoteSuggestions() {
	for path := range s.nodes {
		if strings.HasPrefix(path, "suggest-remote-") {
			delete(s.nodes, path)
		}
	}
	s.remoteTargets = map[string]dbRuntimeTarget{}
	for _, f := range s.discoveredRemote {
		cfg, ok := dbSSHFromEndpoint(f.host.endpoint)
		if !ok || f.err != nil {
			continue
		}
		for _, c := range f.containers {
			driver := dbDriverForImage(c.Image)
			name := dbContainerName(c)
			if driver == "" || name == "" {
				continue
			}
			t := dbRuntimeTarget{driver: driver, name: sanitizeDBText(name), cont: c, isCont: true, running: !dbContainerStopped(c),
				context: f.host.context, endpoint: f.host.endpoint, engine: f.host.engine}
			t.user, t.database = dbPrefill(driver, nil)
			conn, ok := dbRemoteConn(t)
			if !ok {
				continue
			}
			if _, saved := s.dbRemoteSavedMatch(conn); saved {
				continue
			}
			path := dbRemoteSuggestionPath(f.host.context, name)
			s.remoteTargets[path] = t
			s.nodes[path] = dbNode{kind: dbNodeSuggestion, name: sanitizeDBText(name), path: path, conn: path,
				detail: driver + "  " + dbSSHLabel(cfg), driver: driver, host: name, port: conn.Port}
		}
	}
}

// databaseAddRemoteSuggestion adds a remote container row: the tunnel,
// container and port are filled in; it asks only for the password.
func (m *Model) databaseAddRemoteSuggestion(path string) tea.Cmd {
	s := m.databases
	t, ok := s.remoteTargets[path]
	if !ok {
		return nil
	}
	c, ok := dbRemoteConn(t)
	if !ok {
		return nil
	}
	return m.databaseOpenRemote(t, c, path)
}
