package dbclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"

	"golang.org/x/crypto/ssh"
)

// ContainerRef is the container behind a connection on a remote engine
// (a docker context or podman connection over ssh://). The container's
// IP changes when it is recreated, so it is found again by Name every
// time the connection opens.
type ContainerRef struct {
	Name    string `json:"name"`
	Context string `json:"context,omitempty"` // the docker context / podman connection
	Engine  string `json:"engine,omitempty"`  // docker (default) or podman
	Network string `json:"network,omitempty"` // the network whose IP is used ("": the first)
}

// containerName is a docker / podman container name: nothing a shell
// reads specially, and no leading dash.
var containerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// containerInspectFormat prints the state, then one "network ip" line
// per network.
const containerInspectFormat = `{{.State.Status}}{{"\n"}}{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{$v.IPAddress}}{{"\n"}}{{end}}`

// maxCommandOutput bounds what one command on the SSH server may print.
const maxCommandOutput = 64 << 10

// ContainerAddress is the IP of ref's container as seen from t's server:
// one `docker inspect` (or podman) of the container by name, run on the
// server over the tunnel's own SSH connection. A container on the host
// network is 127.0.0.1.
func ContainerAddress(ctx context.Context, t *Tunnel, ref ContainerRef) (string, error) {
	engine := ref.Engine
	if engine == "" {
		engine = "docker"
	}
	if engine != "docker" && engine != "podman" {
		return "", fmt.Errorf("unknown container engine %q (use docker or podman)", sanitizeLine(engine))
	}
	if !containerName.MatchString(ref.Name) {
		return "", fmt.Errorf("%q is not a container name", sanitizeLine(ref.Name))
	}
	via := sanitizeLine(t.target.Host)
	out, code, err := t.Output(ctx, engine+" inspect --format '"+containerInspectFormat+"' "+ref.Name)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("No container %s on %s (looked for it by name).", ref.Name, via)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	state := strings.TrimSpace(lines[0])
	if state != "running" {
		return "", fmt.Errorf("The container %s on %s is not running (it is %s).", ref.Name, via, sanitizeLine(state))
	}
	first, host := "", false
	for _, l := range lines[1:] {
		name, ip, _ := strings.Cut(strings.TrimSpace(l), " ")
		ip = strings.TrimSpace(ip)
		if name == "host" {
			host = true
		}
		if net.ParseIP(ip) == nil {
			continue
		}
		if ref.Network != "" && name == ref.Network {
			return ip, nil
		}
		if first == "" {
			first = ip
		}
	}
	switch {
	case first != "":
		return first, nil
	case host:
		return "127.0.0.1", nil
	}
	return "", fmt.Errorf("The container %s on %s has no IP address.", ref.Name, via)
}

// Output runs cmd on the tunnel's server (one SSH session) and returns
// its output (at most 64 KiB) and exit code. ctx bounds it.
func (t *Tunnel) Output(ctx context.Context, cmd string) (string, int, error) {
	if !t.Alive() {
		return "", 0, t.closedErr()
	}
	sess, err := t.client.NewSession()
	if err != nil {
		return "", 0, fmt.Errorf("could not run a command on %s: %s", sanitizeLine(t.target.Host), sanitizeLine(strings.TrimPrefix(err.Error(), "ssh: ")))
	}
	defer sess.Close()
	stop := context.AfterFunc(ctx, func() { _ = sess.Close() })
	defer stop()
	var out bytes.Buffer
	sess.Stdout = &limitWriter{w: &out, n: maxCommandOutput}
	err = sess.Run(cmd)
	if ctx.Err() != nil {
		return "", 0, &ReachError{Addr: "the container engine", Via: SSHLabel(t.target.User, t.target.Host, t.target.Port), Timeout: true, Err: ctx.Err()}
	}
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		return out.String(), exit.ExitStatus(), nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("could not run a command on %s: %s", sanitizeLine(t.target.Host), sanitizeLine(strings.TrimPrefix(err.Error(), "ssh: ")))
	}
	return out.String(), 0, nil
}

// limitWriter keeps the first n bytes and drops the rest.
type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		_, _ = l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}

// MapHost makes dials to host go to addr instead (a container's name to
// its current IP). It lasts as long as the tunnel.
func (t *Tunnel) MapHost(host, addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.hosts == nil {
		t.hosts = map[string]string{}
	}
	t.hosts[host] = addr
}

// HostMapped is where MapHost points host on this tunnel ("": nowhere).
func (t *Tunnel) HostMapped(host string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hosts[host]
}

// mapped is addr with its host mapped by MapHost.
func (t *Tunnel) mapped(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if to, ok := t.hosts[host]; ok {
		return net.JoinHostPort(to, port)
	}
	return addr
}
