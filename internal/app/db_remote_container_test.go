package app

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

// The owner's server: docker context baaytona = ssh://deploy@65.109.212.14,
// Postgres in a container whose port is not published.
var (
	baaytonaContext = containers.Context{Name: "baaytona", DockerEndpoint: "ssh://deploy@65.109.212.14"}
	baaytonaDB      = containers.Container{ID: "c0ffee01", Names: []string{"/baaytona-db-1"}, Image: "postgres:16", State: "running", Status: "Up 3 days",
		Ports: []containers.Port{{PrivatePort: 5432, Type: "tcp"}}, Project: "baaytona", Networks: []string{"baaytona_default"},
		Labels: map[string]string{"com.docker.compose.project": "baaytona", "com.docker.compose.service": "db"}}
	baaytonaRedis = containers.Container{ID: "c0ffee02", Names: []string{"/baaytona-scraper-redis"}, Image: "redis:7", State: "running",
		Ports: []containers.Port{{PrivatePort: 6379, Type: "tcp"}}, Project: "baaytona"}
	baaytonaApp = containers.Container{ID: "c0ffee03", Names: []string{"/baaytona-web-1"}, Image: "baaytona/web:latest", State: "running", Project: "baaytona"}
)

// remoteContainersModel is the Containers view on the baaytona context.
func remoteContainersModel(t *testing.T) *Model {
	t.Helper()
	m := filterDemo(t)
	st := m.services
	st.sessionContext = "baaytona"
	st.contextRows = []containers.Context{{Name: "default", Current: true, DockerEndpoint: "unix:///var/run/docker.sock"}, baaytonaContext}
	st.containerRows = []containers.Container{baaytonaDB, baaytonaApp}
	st.touch()
	if m.databases == nil {
		m.databases = newDBState()
	}
	m.databases.root, m.databases.loaded = t.TempDir(), true
	return m
}

// "Open in Databases" on a container of an ssh:// context: a tunnel to
// that host (its user, the agent) and the container itself as the
// target, by name, with its own port; no published port is needed.
func TestDBOpenRemoteContainerBuildsTunnelToTheContainer(t *testing.T) {
	m := remoteContainersModel(t)
	if !m.servicesCursorTo(func(r svcRow) bool { return r.host == hostContainer && r.hostID == "c0ffee01" }) {
		t.Fatal("no baaytona-db-1 row")
	}
	r, _ := m.servicesCurrentRow()
	tg, ok := m.dbTargetFromRow(r)
	if !ok {
		t.Fatal("baaytona-db-1 is not a database target")
	}
	_ = m.databaseOpenTargetRevealed(tg)
	c := m.databases.pendingConn
	if c.SSH == nil || c.SSH.Host != "65.109.212.14" || c.SSH.User != "deploy" || c.SSH.Auth != dbclient.SSHAuthAgent || c.SSH.Port != 0 {
		t.Fatalf("ssh = %+v, want deploy@65.109.212.14 with the agent", c.SSH)
	}
	if c.Host != "baaytona-db-1" || c.Port != 5432 || c.Driver != "postgres" {
		t.Fatalf("target = %s %s:%d, want postgres baaytona-db-1:5432", c.Driver, c.Host, c.Port)
	}
	want := dbclient.ContainerRef{Name: "baaytona-db-1", Context: "baaytona", Engine: "docker", Network: "baaytona_default"}
	if c.Container == nil || *c.Container != want {
		t.Fatalf("container = %+v, want %+v", c.Container, want)
	}
	if !m.promptOpen {
		t.Fatal("no password prompt")
	}
	view := ansi.Strip(m.prompt.View())
	if !strings.Contains(view, "baaytona-db-1") || !strings.Contains(view, "deploy@65.109.212.14") {
		t.Fatalf("the prompt does not say where it connects:\n%s", view)
	}
}

// A saved connection of a remote container finds the container's IP
// again (by name) every time it connects, through its own tunnel.
func TestDBRemoteContainerFindsItsIPAgainOnConnect(t *testing.T) {
	ip := "172.18.0.5"
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-41"
	srv.Exec = func(cmd string) (string, int) {
		if strings.HasPrefix(cmd, "docker inspect ") && strings.HasSuffix(cmd, " baaytona-db-1") {
			return "running\nbaaytona_default " + ip + "\n", 0
		}
		return "", 1
	}
	srv.Forward = func(_ context.Context, addr string) (net.Conn, error) {
		if addr != ip+":5432" {
			return nil, errors.New("refused")
		}
		a, b := net.Pipe()
		go func() { _ = b.Close() }()
		return a, nil
	}
	defer srv.Close()
	home := sshTestHome(t, srv)
	if err := dbclient.TrustHostKey(filepath.Join(home, ".ssh", "known_hosts"), "65.109.212.14:22", srv.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	m := unreachableModel(t, &dbclient.SSHConfig{Host: "65.109.212.14", User: "deploy", Auth: "password"})
	c := m.databases.conns[0]
	c.Host = "baaytona-db-1"
	c.Container = &dbclient.ContainerRef{Name: "baaytona-db-1", Context: "baaytona", Engine: "docker"}
	m.databases.conns[0], m.databases.userConns[0] = c, c
	dbStoreSet(t, "db.baaytona.ssh", "tunnel-pw-41")

	var dialled []string
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(ctx context.Context, c dbclient.Conn, _ string) (dbclient.Session, error) {
		addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
		conn, err := c.Dial(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		_ = conn.Close()
		dialled = append(dialled, addr)
		return &pingSession{}, nil
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })

	dbConnectAndDeliver(t, m, m.databaseConnect("baaytona"))
	if m.databases.sessions["baaytona"] == nil {
		t.Fatalf("not connected: %q", m.databases.nodes["baaytona"].problem)
	}
	ip = "172.18.0.9" // the container was recreated
	_ = m.databaseDisconnectNow("baaytona")
	dbConnectAndDeliver(t, m, m.databaseConnect("baaytona"))
	if m.databases.sessions["baaytona"] == nil {
		t.Fatalf("not connected after the IP changed: %q", m.databases.nodes["baaytona"].problem)
	}
	tg := srv.Targets()
	if len(tg) != 2 || tg[0] != "172.18.0.5:5432" || tg[1] != "172.18.0.9:5432" {
		t.Fatalf("tunnel targets = %q, want the old IP, then the new one", tg)
	}
	if len(dialled) != 2 {
		t.Fatalf("dialled = %q", dialled)
	}
	// The container is gone: one plain line, no Loading… left.
	ip = ""
	srv.Exec = func(string) (string, int) { return "", 1 }
	_ = m.databaseDisconnectNow("baaytona")
	dbConnectAndDeliver(t, m, m.databaseConnect("baaytona"))
	if n := m.databases.nodes["baaytona"]; n.loading || n.problem != "No container baaytona-db-1 on 65.109.212.14 (looked for it by name)." {
		t.Fatalf("gone: loading=%v problem=%q", n.loading, n.problem)
	}
}

// Discovery lists the database containers of every ssh:// context too
// (local discovery is unchanged); picking one adds it with the tunnel
// and the container filled in and asks only for the password.
func TestDBDiscoverListsDatabaseContainersOfSSHContexts(t *testing.T) {
	t.Setenv("TERMOCODE_NO_DB_DISCOVERY", "")
	oldList, oldCtx, oldRemote, oldTargets := dbListContainers, dbListContexts, dbListContextContainers, dbProbeTargets
	dbListContainers = func(context.Context, string) ([]containers.Container, error) { return nil, nil }
	dbListContexts = func(context.Context, string) ([]containers.Context, error) {
		return []containers.Context{{Name: "default", Current: true, DockerEndpoint: "unix:///var/run/docker.sock"}, baaytonaContext,
			{Name: "rentcar", DockerEndpoint: "ssh://ops@rentcar.example.com:2222"}, {Name: "broken", DockerEndpoint: "ssh://me@down.example.com"}}, nil
	}
	var asked []string
	var mu sync.Mutex // the hosts are asked at the same time
	dbListContextContainers = func(_ context.Context, _ string, name string) ([]containers.Container, error) {
		mu.Lock()
		asked = append(asked, name)
		mu.Unlock()
		switch name {
		case "baaytona":
			return []containers.Container{baaytonaDB, baaytonaRedis, baaytonaApp}, nil
		case "rentcar":
			return []containers.Container{{ID: "r1", Names: []string{"/rentcar-db-1"}, Image: "mariadb:11", State: "running", Ports: []containers.Port{{PrivatePort: 3306, Type: "tcp"}}, Project: "rentcar"}}, nil
		}
		return nil, errors.New("ssh: connect to host down.example.com port 22: Connection timed out")
	}
	dbProbeTargets = nil
	t.Cleanup(func() {
		dbListContainers, dbListContexts, dbListContextContainers, dbProbeTargets = oldList, oldCtx, oldRemote, oldTargets
	})
	s := newDBState()
	s.root, s.visible, s.loaded = t.TempDir(), true, true
	m := dbTestModel(s)
	m.w, m.h = 80, 24
	msg, ok := dbRunFor[dbDiscoverMsg](t, m.databaseDiscover())
	if !ok {
		t.Fatal("no discovery result")
	}
	_ = m.onDBDiscover(msg)
	sort.Strings(asked)
	if strings.Join(asked, ",") != "baaytona,broken,rentcar" {
		t.Fatalf("asked contexts %q, want the three ssh:// ones (not default)", asked)
	}
	var found []string
	for path, n := range s.nodes {
		if n.kind == dbNodeSuggestion && strings.HasPrefix(path, "suggest-remote-") {
			found = append(found, n.name+" | "+n.detail)
		}
	}
	joined := strings.Join(found, "\n")
	for _, want := range []string{"baaytona-db-1", "baaytona-scraper-redis", "rentcar-db-1", "deploy@65.109.212.14", "ops@rentcar.example.com"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("no %q in the remote suggestions:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "baaytona-web-1") || len(found) != 3 {
		t.Fatalf("remote suggestions:\n%s", joined)
	}

	cmd := m.databaseAddSuggestion("suggest-remote-baaytona/baaytona-db-1")
	_ = cmd
	c := s.pendingConn
	if c.SSH == nil || c.SSH.Host != "65.109.212.14" || c.SSH.User != "deploy" || c.Host != "baaytona-db-1" || c.Port != 5432 || c.Container == nil || c.Container.Context != "baaytona" {
		t.Fatalf("picked: %+v ssh %+v container %+v", c, c.SSH, c.Container)
	}
	if !m.promptOpen || m.promptKind != promptKindDBPassword {
		t.Fatal("picking a remote container does not ask for the password")
	}
	// rentcar's SSH port comes from the URI.
	_ = m.databaseAddSuggestion("suggest-remote-rentcar/rentcar-db-1")
	if c := s.pendingConn; c.SSH == nil || c.SSH.Port != 2222 || c.SSH.User != "ops" || c.Driver != "mysql" || c.Port != 3306 {
		t.Fatalf("rentcar: %+v ssh %+v", c, c.SSH)
	}
}

// pingSession is a fake session that answers its ping.
type pingSession struct{ fakeDBSession }

func (*pingSession) Ping(context.Context) error { return nil }
