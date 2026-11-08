package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The owner's hosts: this machine, baaytona (ssh://deploy@65.109.212.14,
// Postgres in a container whose port is not published) and a host that
// does not answer.
const webServersContexts = `{"Name":"default","Current":"*","DockerEndpoint":"unix:///var/run/docker.sock"}
{"Name":"baaytona","Current":"","DockerEndpoint":"ssh://deploy@65.109.212.14"}
{"Name":"broken","Current":"","DockerEndpoint":"ssh://ops@10.9.9.9:2222"}
`

var (
	webBaaytonaDB = containers.Container{ID: "c0ffee01aa00", Names: []string{"/baaytona-db-1"}, Image: "postgres:16", State: "running", Status: "Up 3 days",
		Ports: []containers.Port{{PrivatePort: 5432, Type: "tcp"}}, Project: "baaytona", Networks: []string{"baaytona_default"}}
	webBaaytonaWeb = containers.Container{ID: "c0ffee03aa00", Names: []string{"/baaytona-web-1"}, Image: "baaytona/web:latest", State: "running", Project: "baaytona"}
)

// webServersHub is the hub with the fake engine's three hosts; each
// host's containers come from lists (a missing host does not answer).
func webServersHub(t *testing.T) (*Model, *webConn, func() []string) {
	t.Helper()
	m, c, f := webTestContainers(t)
	f.Set(t, "context-ls.json", webServersContexts)
	old := serverListContainers
	serverListContainers = func(_ context.Context, e containers.Engine) ([]containers.Container, error) {
		switch e.Context {
		case "baaytona":
			return []containers.Container{webBaaytonaDB, webBaaytonaWeb}, nil
		case "default":
			return nil, nil
		}
		return nil, errors.New("error during connect: ssh: connect to host 10.9.9.9 port 2222: Connection timed out")
	}
	t.Cleanup(func() { serverListContainers = old })
	return m, c, f.Calls
}

type webTestHosts struct {
	Engine string `json:"engine"`
	Hosts  []struct {
		Name    string `json:"name"`
		Where   string `json:"where"`
		Local   bool   `json:"local"`
		SSH     bool   `json:"ssh"`
		Current bool   `json:"current"`
	} `json:"hosts"`
}

type webTestHostCtrs struct {
	Name       string `json:"name"`
	Containers []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Project string `json:"project"`
		DB      string `json:"db"`
	} `json:"containers"`
}

// Spec: containers.servers lists every host at once as "name · where"
// (this machine, user@host[:port] without the scheme, ssh marked);
// containers.server lists one host's containers (name, compose project,
// the database driver of a database container); a host that does not
// answer fails its own read with one short line and never the others; a
// host not listed is not_found.
func TestWebServersTree(t *testing.T) {
	m, c, _ := webServersHub(t)
	if f := webTestCallBg(t, m, c, "containers.server", `{"name":"baaytona"}`); webTestErr(f) != "not_found" {
		t.Fatalf("a host before the list: %v", f)
	}
	var hs webTestHosts
	webTestData(t, webTestCallBg(t, m, c, "containers.servers", `{}`), &hs)
	if len(hs.Hosts) != 3 {
		t.Fatalf("hosts %+v", hs)
	}
	if h := hs.Hosts[0]; h.Name != "default" || !h.Local || h.Where != "" || h.SSH || !h.Current {
		t.Errorf("default %+v", h)
	}
	if h := hs.Hosts[1]; h.Name != "baaytona" || h.Where != "deploy@65.109.212.14" || !h.SSH || h.Local {
		t.Errorf("baaytona %+v", h)
	}
	if h := hs.Hosts[2]; h.Name != "broken" || h.Where != "ops@10.9.9.9:2222" || !h.SSH {
		t.Errorf("broken %+v", h)
	}
	f := webTestCallBg(t, m, c, "containers.server", `{"name":"broken"}`)
	if webTestErr(f) != "conflict" || f["msg"] != "ssh: Connection timed out" {
		t.Errorf("broken host: %v", f)
	}
	var hc webTestHostCtrs
	webTestData(t, webTestCallBg(t, m, c, "containers.server", `{"name":"baaytona"}`), &hc)
	if hc.Name != "baaytona" || len(hc.Containers) != 2 {
		t.Fatalf("baaytona %+v", hc)
	}
	if db := hc.Containers[0]; db.ID != "c0ffee01aa00" || db.Name != "baaytona-db-1" || db.Project != "baaytona" || db.DB != "postgres" {
		t.Errorf("db %+v", db)
	}
	if web := hc.Containers[1]; web.DB != "" {
		t.Errorf("web %+v is not a database", web)
	}
	if f := webTestCallBg(t, m, c, "containers.server", `{"name":"nope"}`); webTestErr(f) != "not_found" {
		t.Errorf("unlisted host: %v", f)
	}
}

// Spec: + Add Server… creates a context for ssh://user@host with the
// engine's own command (no laptop card: the page asked first), audited;
// a name in use is refused and nothing runs.
func TestWebAddServer(t *testing.T) {
	m, c, calls := webServersHub(t)
	if f := webTestCallBg(t, m, c, "containers.add_server", `{"name":"baaytona","host":"ssh://deploy@65.109.212.14"}`); webTestErr(f) != "conflict" {
		t.Errorf("a name in use: %v", f)
	}
	f := webTestCallBg(t, m, c, "containers.add_server", `{"name":"baaytona2","host":"ssh://deploy@65.109.212.15"}`)
	if webTestErr(f) != "" {
		t.Fatalf("add: %v", f)
	}
	created := 0
	for _, l := range calls() {
		if strings.HasPrefix(l, "context create") {
			created++
			if !strings.Contains(l, "baaytona2") || !strings.Contains(l, "host=ssh://deploy@65.109.212.15") {
				t.Errorf("command %q", l)
			}
		}
	}
	if created != 1 {
		t.Errorf("%d context create calls, want 1: %q", created, calls())
	}
	if a := webTestAudit(m); !strings.Contains(a, "containers.add_server  baaytona2 ssh://deploy@65.109.212.15") {
		t.Errorf("audit:\n%s", a)
	}
}

// webTestAgent serves an ssh-agent with one key on a socket and points
// SSH_AUTH_SOCK at it; its public key is returned.
func webTestAgent(t *testing.T) ssh.PublicKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ring := agent.NewKeyring()
	if err := ring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "ag")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "a.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { _ = agent.ServeAgent(ring, c); _ = c.Close() }()
		}
	}()
	t.Setenv("SSH_AUTH_SOCK", sock)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}

// The owner's path on the page: baaytona's Postgres container (no
// published port) is picked in the SERVERS tree; it is added as the
// laptop adds it (a tunnel to deploy@65.109.212.14 with the agent, the
// container by name and own port, no password saved); the first read
// asks to trust the unknown host, the next asks for the database
// password, db.secret keeps it in the secret store, and the tables are
// read through the tunnel at the container's IP. Picking it again finds
// the saved one.
func TestWebDBAddContainerThenTrustPasswordTables(t *testing.T) {
	const pgPW = "pg-" + "pw-61"
	srv := dbtest.NewSSHServer()
	defer srv.Close()
	srv.Exec = func(cmd string) (string, int) {
		if strings.HasPrefix(cmd, "docker inspect ") && strings.HasSuffix(cmd, " baaytona-db-1") {
			return "running\nbaaytona_default 172.18.0.5\n", 0
		}
		return "", 1
	}
	srv.Forward = func(_ context.Context, addr string) (net.Conn, error) {
		if addr != "172.18.0.5:5432" {
			return nil, errors.New("refused")
		}
		a, b := net.Pipe()
		go func() { _ = b.Close() }()
		return a, nil
	}
	home := sshTestHome(t, srv)
	srv.AuthorizedKeys = []ssh.PublicKey{webTestAgent(t)}
	m, c, _ := webServersHub(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	root := t.TempDir()
	m.ws.Root = root
	m.databases = newDBState()
	m.databases.load(root)
	var mu sync.Mutex
	var pws []string
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(ctx context.Context, c dbclient.Conn, pw string) (dbclient.Session, error) {
		mu.Lock()
		pws = append(pws, pw)
		mu.Unlock()
		if c.Dial == nil {
			return nil, errors.New("no tunnel")
		}
		nc, err := c.Dial(ctx, "tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
		if err != nil {
			return nil, err
		}
		_ = nc.Close()
		if pw != pgPW {
			return nil, errors.New(`FATAL: password authentication failed for user "postgres" (SQLSTATE 28P01)`)
		}
		return &aiDBSession{name: "baaytona"}, nil
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })

	webTestData(t, webTestCallBg(t, m, c, "containers.servers", `{}`), &webTestHosts{})
	if f := webTestCallBg(t, m, c, "db.add_container", `{"host":"baaytona","id":"c0ffee01aa00"}`); webTestErr(f) != "not_found" {
		t.Fatalf("a container before its host was read: %v", f)
	}
	webTestData(t, webTestCallBg(t, m, c, "containers.server", `{"name":"baaytona"}`), &webTestHostCtrs{})
	if f := webTestCallBg(t, m, c, "db.add_container", `{"host":"baaytona","id":"c0ffee03aa00"}`); webTestErr(f) != "conflict" {
		t.Errorf("a web container: %v", f)
	}
	var added struct {
		Conn     string `json:"conn"`
		Existing bool   `json:"existing"`
	}
	webTestData(t, webTestCallBg(t, m, c, "db.add_container", `{"host":"baaytona","id":"c0ffee01aa00"}`), &added)
	if added.Conn == "" || added.Existing {
		t.Fatalf("added %+v", added)
	}
	saved, ok := m.databases.connection(added.Conn)
	if !ok {
		t.Fatalf("%s is not in the laptop's list", added.Conn)
	}
	if saved.SSH == nil || saved.SSH.Host != "65.109.212.14" || saved.SSH.User != "deploy" || saved.SSH.Auth != dbclient.SSHAuthAgent ||
		saved.Host != "baaytona-db-1" || saved.Port != 5432 || saved.Password != "" || saved.User != "postgres" {
		t.Fatalf("saved %+v ssh %+v", saved, saved.SSH)
	}
	if want := (dbclient.ContainerRef{Name: "baaytona-db-1", Context: "baaytona", Engine: "docker", Network: "baaytona_default"}); saved.Container == nil || *saved.Container != want {
		t.Fatalf("container %+v", saved.Container)
	}
	var conns webproto.DBConns
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBConns, `{}`), &conns)
	if len(conns.Conns) != 1 || conns.Conns[0].ID != added.Conn || !conns.Conns[0].SQL {
		t.Fatalf("db.conns %+v", conns)
	}

	args := `{"conn":"` + added.Conn + `"}`
	need := webNeed(t, webTestCallBg(t, m, c, webproto.OpDBTables, args))
	if need["need"] != "host_key" || need["host"] != "65.109.212.14" {
		t.Fatalf("first read: %v", need)
	}
	if f := webTestCallBg(t, m, c, "db.trust_host", `{"conn":"`+added.Conn+`","fingerprint":"`+need["fingerprint"].(string)+`"}`); webTestErr(f) != "" {
		t.Fatalf("trust: %v", f)
	}
	if need := webNeed(t, webTestCallBg(t, m, c, webproto.OpDBTables, args)); need["need"] != "password" {
		t.Fatalf("second read: %v", need)
	}
	if f := webTestCallBg(t, m, c, "db.secret", `{"conn":"`+added.Conn+`","kind":"password","value":"`+pgPW+`"}`); webTestErr(f) != "" {
		t.Fatalf("secret: %v", f)
	}
	var tb webproto.DBTables
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBTables, args), &tb)
	if len(tb.Tables) == 0 {
		t.Fatalf("tables %+v", tb)
	}
	if got := srv.Targets(); got[len(got)-1] != "172.18.0.5:5432" {
		t.Errorf("tunnel targets %q", got)
	}
	webTestData(t, webTestCallBg(t, m, c, "db.add_container", `{"host":"baaytona","id":"c0ffee01aa00"}`), &added)
	if !added.Existing {
		t.Errorf("picked again: %+v, want the saved one", added)
	}
	a := webTestAudit(m)
	if !strings.Contains(a, "db.add_container  "+added.Conn+" baaytona baaytona-db-1") || strings.Contains(a, pgPW) {
		t.Errorf("audit:\n%s", a)
	}
	b, _ := os.ReadFile(filepath.Join(home, "config", "termocode", "db", "connections.json"))
	if strings.Contains(string(b), pgPW) {
		t.Errorf("the password is in the connections file:\n%s", b)
	}
}

// Spec: the Containers view's CONTEXTS section is the hosts tree:
// containers.host gives each context "where" (user@host without the
// scheme, ssh marked, or this machine) and lists the hosts, so a context
// row opens its containers with containers.server at once.
func TestWebContainersHostContextsAreTheHostsTree(t *testing.T) {
	m, c, _ := webServersHub(t)
	var h struct {
		Contexts []struct {
			Name  string `json:"name"`
			Where string `json:"where"`
			Local bool   `json:"local"`
			SSH   bool   `json:"ssh"`
		} `json:"contexts"`
	}
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersHost, `{}`), &h)
	if len(h.Contexts) != 3 {
		t.Fatalf("contexts %+v", h.Contexts)
	}
	if x := h.Contexts[0]; x.Name != "default" || !x.Local || x.Where != "" || x.SSH {
		t.Errorf("default %+v", x)
	}
	if x := h.Contexts[1]; x.Name != "baaytona" || x.Where != "deploy@65.109.212.14" || !x.SSH || x.Local {
		t.Errorf("baaytona %+v", x)
	}
	var hc webTestHostCtrs
	webTestData(t, webTestCallBg(t, m, c, "containers.server", `{"name":"baaytona"}`), &hc)
	if len(hc.Containers) != 2 {
		t.Fatalf("baaytona %+v", hc)
	}
}
