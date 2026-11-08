package webproto

import (
	"net/url"
	"strconv"
	"strings"
)

// The hosts tree, as the laptop's Containers view (CONTEXTS) and
// Databases view (SERVERS) draw it: every engine host at once (a docker
// context or podman connection: "name · where"), each one's containers
// read once when it is opened (and on Refresh), grouped by compose
// project. A host that does not answer fails its own read only, with
// one line saying why; the others stay. containers.add_server is + Add
// Server…: a new context for ssh://user@host after the page's own
// confirm (Cancel first). db.add_container adds a database container of
// a listed host to Databases as the laptop does: an SSH tunnel to the
// host (its user, the ssh-agent) whose target is the container by its
// name and own port; no password is saved (a read asks for it).

// Hosts tree op names.
const (
	OpContainersServers   = "containers.servers"
	OpContainersServer    = "containers.server"
	OpContainersAddServer = "containers.add_server"
	OpDBAddContainer      = "db.add_container"
)

var opServersRows = []Policy{
	{Op: OpContainersServers, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpContainersServer, New: newOf[ContextArgs](), Role: RoleViewer, Rate: perSec(4), MaxBytes: 1 * kib, Live: 4, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpContainersAddServer, New: newOf[AddServerArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDBAddContainer, New: newOf[DBAddContainerArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opServersRows...); return true }()

func init() {
	registerResult(OpContainersServers, ServersList{})
	registerResult(OpContainersServer, ServerContainers{})
	registerResult(OpContainersAddServer, HostDone{})
	registerResult(OpDBAddContainer, DBAdded{})
}

// Hosts tree limits.
const (
	MaxServers        = 100
	MaxServerEndpoint = 255
)

// AddServerArgs is + Add Server…: a context Name for Host, an ssh://
// address (user@host, a port when not 22).
type AddServerArgs struct {
	Name string `json:"name"`
	Host string `json:"host"`
}

// DBAddContainerArgs adds the database container ID that the last
// containers.server of Host listed.
type DBAddContainerArgs struct {
	Host string `json:"host"`
	ID   string `json:"id"`
}

func (a *AddServerArgs) validate(int) error {
	if !validItemName(a.Name) {
		return invalid("bad name")
	}
	if !ValidSSHServer(a.Host) {
		return invalid("bad host (ssh://user@host)")
	}
	return nil
}

func (a *DBAddContainerArgs) validate(int) error {
	if !validItemName(a.Host) || !validContainerID(a.ID) {
		return invalid("bad host or container")
	}
	return nil
}

// ValidSSHServer reports an ssh://user@host[:port] address with nothing
// else in it (no path, query or password).
func ValidSSHServer(s string) bool {
	if s == "" || len(s) > MaxServerEndpoint || hasControl(s) || strings.ContainsAny(s, " \t\\") || !strings.HasPrefix(s, "ssh://") {
		return false
	}
	u, err := url.Parse(s)
	if err != nil || u.User == nil || u.User.Username() == "" || u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if _, has := u.User.Password(); has {
		return false
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return !strings.HasPrefix(u.User.Username(), "-") && !strings.HasPrefix(u.Hostname(), "-")
}

// ServersList answers containers.servers: every host of the engine.
type ServersList struct {
	Engine string       `json:"engine"` // "docker", "podman"
	Hosts  []ServerHost `json:"hosts"`
}

// ServerHost is one host: a context (podman: a connection). Where is
// user@host[:port] of an ssh:// or tcp:// one ("" with Local: this
// machine); SSH marks an ssh:// one (the Databases view lists those).
type ServerHost struct {
	Name    string `json:"name"`
	Where   string `json:"where,omitempty"`
	Local   bool   `json:"local,omitempty"`
	SSH     bool   `json:"ssh,omitempty"`
	Current bool   `json:"current,omitempty"` // the machine's default
	Session bool   `json:"session,omitempty"` // the one this session uses
}

// ServerContainers answers containers.server: one host's containers.
// DB on a container names its database driver ("postgres", "mysql",
// "redis"): db.add_container takes it.
type ServerContainers struct {
	Name       string          `json:"name"`
	Containers []ContainerItem `json:"containers"`
	More       int             `json:"more,omitempty"`
}

// DBAdded answers db.add_container: the connection's id (Existing: it
// was in Databases already).
type DBAdded struct {
	Conn     string `json:"conn"`
	Name     string `json:"name,omitempty"`
	Existing bool   `json:"existing,omitempty"`
}
