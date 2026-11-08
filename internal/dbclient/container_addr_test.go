package dbclient

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

// inspectServer is an SSH server whose `docker inspect` of the
// container answers out (exit code exit).
func inspectServer(t *testing.T, out *string, exit *int) *dbtest.SSHServer {
	t.Helper()
	srv := dbtest.NewSSHServer()
	srv.Exec = func(cmd string) (string, int) {
		if !strings.HasPrefix(cmd, "docker inspect ") {
			return "", 127
		}
		return *out, *exit
	}
	t.Cleanup(srv.Close)
	return srv
}

// A database container on a remote engine has no published port: the
// tunnel asks the server for the container's IP on its network, by the
// container's name, every time it connects.
func TestContainerAddressFindsTheIPByName(t *testing.T) {
	out, exit := "running\nbaaytona_default 172.18.0.5\n", 0
	srv := inspectServer(t, &out, &exit)
	tun := dialThrough(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ref := ContainerRef{Name: "baaytona-db-1", Context: "baaytona"}
	ip, err := ContainerAddress(ctx, tun, ref)
	if err != nil || ip != "172.18.0.5" {
		t.Fatalf("ip = %q, %v", ip, err)
	}
	cmds := srv.Commands()
	if len(cmds) != 1 || !strings.HasSuffix(cmds[0], " baaytona-db-1") || !strings.Contains(cmds[0], "--format '") {
		t.Fatalf("commands = %q", cmds)
	}
	// The container was recreated: a new IP is found again.
	out = "running\nbaaytona_default 172.18.0.9\n"
	if ip, err := ContainerAddress(ctx, tun, ref); err != nil || ip != "172.18.0.9" {
		t.Fatalf("after recreate: ip = %q, %v", ip, err)
	}
	// Several networks: the saved one wins.
	out = "running\nfront 10.0.0.4\nbaaytona_default 172.18.0.9\n"
	ref.Network = "baaytona_default"
	if ip, err := ContainerAddress(ctx, tun, ref); err != nil || ip != "172.18.0.9" {
		t.Fatalf("saved network: ip = %q, %v", ip, err)
	}
	// Host network: the container listens on the server itself.
	out, ref.Network = "running\nhost \n", ""
	if ip, err := ContainerAddress(ctx, tun, ref); err != nil || ip != "127.0.0.1" {
		t.Fatalf("host network: ip = %q, %v", ip, err)
	}
}

func TestContainerAddressSaysWhatIsWrong(t *testing.T) {
	out, exit := "", 1
	srv := inspectServer(t, &out, &exit)
	tun := dialThrough(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ref := ContainerRef{Name: "baaytona-db-1"}
	_, err := ContainerAddress(ctx, tun, ref)
	if err == nil || err.Error() != "No container baaytona-db-1 on bastion (looked for it by name)." {
		t.Fatalf("missing: err = %v", err)
	}
	out, exit = "exited\nbaaytona_default \n", 0
	_, err = ContainerAddress(ctx, tun, ref)
	if err == nil || err.Error() != "The container baaytona-db-1 on bastion is not running (it is exited)." {
		t.Fatalf("stopped: err = %v", err)
	}
	// A name that is not a plain container name never reaches the shell.
	before := len(srv.Commands())
	for _, bad := range []string{"x; rm -rf /", "$(id)", "-f", "a b", ""} {
		if _, err := ContainerAddress(ctx, tun, ContainerRef{Name: bad}); err == nil {
			t.Fatalf("name %q was accepted", bad)
		}
	}
	if _, err := ContainerAddress(ctx, tun, ContainerRef{Name: "db", Engine: "sh -c"}); err == nil {
		t.Fatal("an engine other than docker or podman was accepted")
	}
	if len(srv.Commands()) != before {
		t.Fatalf("a bad name was sent: %q", srv.Commands()[before:])
	}
}

// The container is saved with the connection (and read back).
func TestConnSavesItsContainer(t *testing.T) {
	c := Conn{ID: "baaytona-db", Driver: "postgres", Host: "baaytona-db-1", Port: 5432,
		SSH:       &SSHConfig{Host: "65.109.212.14", User: "deploy", Auth: SSHAuthAgent},
		Container: &ContainerRef{Name: "baaytona-db-1", Context: "baaytona", Engine: "docker", Network: "baaytona_default"}}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"container":{"name":"baaytona-db-1","context":"baaytona","engine":"docker","network":"baaytona_default"}`) {
		t.Fatalf("json = %s", data)
	}
	var back Conn
	if err := json.Unmarshal(data, &back); err != nil || back.Container == nil || *back.Container != *c.Container {
		t.Fatalf("read back = %+v, %v", back.Container, err)
	}
}
