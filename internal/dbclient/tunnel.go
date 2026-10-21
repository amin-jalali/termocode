package dbclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// The SSH tunnel is in-process: one *ssh.Client per connection, the
// drivers dial the database through it (no local listening port). Host
// keys are checked against known_hosts: an unknown key fails with
// UnknownHostKeyError (the app shows the fingerprint and asks), a changed
// key fails with HostKeyChangedError and is never accepted. There is no
// keepalive timer: a dead tunnel shows on the next dial as ErrTunnelClosed.

// DialFunc opens a network connection (the SSH tunnel's DialContext).
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// SSH auth kinds.
const (
	SSHAuthAgent    = "agent"
	SSHAuthKey      = "key"
	SSHAuthPassword = "password"
)

// ErrTunnelClosed is a dial through a tunnel that has gone away.
var ErrTunnelClosed = errors.New("SSH tunnel closed · R reconnect")

// ErrProxyCommandUnsupported is a host whose ~/.ssh/config entry sets
// ProxyCommand: it would run a shell command, so it is refused.
var ErrProxyCommandUnsupported = errors.New("ProxyCommand is not supported (it runs a shell command); use ProxyJump")

// ErrProxyJumpLoop is a ProxyJump chain that comes back to a host
// already on it.
var ErrProxyJumpLoop = errors.New("the ProxyJump chain loops")

// ErrTooManyJumps is a ProxyJump chain longer than maxJumps hops.
var ErrTooManyJumps = errors.New("the ProxyJump chain has too many jump hosts")

// UnknownHostKeyError is a host that is not in known_hosts. The app shows
// Host and Fingerprint and, when the user trusts it, calls TrustHostKey.
type UnknownHostKeyError struct {
	Host        string // host:port as dialled
	KeyType     string
	Fingerprint string // SHA256:…
	KnownHosts  string // the file TrustHostKey appends to
	Key         ssh.PublicKey
	Jump        string // the jump host's name ("": the tunnel's own server)
}

func (e *UnknownHostKeyError) Error() string {
	if e.Jump != "" {
		return "unknown SSH jump host " + hostForDisplay(e.Host) + " · " + e.Fingerprint
	}
	return "unknown SSH host " + hostForDisplay(e.Host) + " · " + e.Fingerprint
}

// HostKeyChangedError is a host whose key differs from known_hosts (or is
// revoked). It is refused; there is no override.
type HostKeyChangedError struct {
	Host        string
	Fingerprint string
	Revoked     bool
	Jump        string // the jump host's name ("": the tunnel's own server)
}

func (e *HostKeyChangedError) Error() string {
	what := "the host key of "
	if e.Jump != "" {
		what += "jump host "
	}
	if e.Revoked {
		return what + hostForDisplay(e.Host) + " is revoked; check with your admin"
	}
	return what + hostForDisplay(e.Host) + " changed; check with your admin"
}

// SSHSecretError asks for the SSH password or the key passphrase (Wrong:
// the one given did not work).
type SSHSecretError struct {
	Kind    string // "password" | "passphrase"
	Wrong   bool
	KeyFile string
}

func (e *SSHSecretError) Error() string {
	switch {
	case e.Kind == "passphrase" && e.Wrong:
		return "wrong passphrase for the SSH key " + filepath.Base(e.KeyFile)
	case e.Kind == "passphrase":
		return "the SSH key " + filepath.Base(e.KeyFile) + " needs its passphrase"
	case e.Wrong:
		return "SSH login failed: the password was not accepted"
	default:
		return "the SSH tunnel needs a password"
	}
}

// SSHTarget is the resolved tunnel endpoint: the JSON object first, then
// ~/.ssh/config for the blanks (HostName, Port, User, IdentityFile).
type SSHTarget struct {
	Alias      string
	Host       string
	Port       int
	User       string
	Auth       string
	KeyFiles   []string // candidates in order (Auth key)
	KnownHosts string
	// Jumps are the jump hosts before this one, first one first.
	Jumps []SSHTarget
}

// Addr is host:port of the SSH server.
func (t SSHTarget) Addr() string { return net.JoinHostPort(t.Host, strconv.Itoa(t.Port)) }

// TunnelOptions are the secrets and the environment of OpenTunnel.
type TunnelOptions struct {
	// Home is the user's home: ~ expansion, ~/.ssh/config, the default
	// known_hosts and key files. Empty: os.UserHomeDir.
	Home string
	// Password (auth password) and Passphrase (auth key) come from the
	// secret store; never from a file.
	Password   string
	Passphrase string
	// AgentSocket is the ssh-agent socket (empty: $SSH_AUTH_SOCK).
	AgentSocket string
	// Dial opens the TCP connection to the SSH server (nil: net.Dialer
	// without keepalive).
	Dial DialFunc
}

func (o TunnelOptions) home() string {
	if o.Home != "" {
		return o.Home
	}
	h, _ := os.UserHomeDir()
	return h
}

// ResolveSSH fills cfg from ~/.ssh/config under home, then resolves the
// jump hosts (cfg.Jump, else the host's ProxyJump). On a jump error the
// returned target still has its own fields filled (the prompts use them
// as defaults).
func ResolveSSH(cfg SSHConfig, home string) (SSHTarget, error) {
	alias := strings.TrimSpace(cfg.Host)
	if alias == "" {
		return SSHTarget{}, errors.New("the SSH tunnel has no host")
	}
	hc := readSSHHostConfig(home, alias)
	t := SSHTarget{Alias: alias, Host: alias, Port: cfg.Port, User: cfg.User, Auth: strings.ToLower(strings.TrimSpace(cfg.Auth))}
	if t.Port < 0 || t.Port > 65535 {
		return SSHTarget{}, errors.New("the SSH port is not valid")
	}
	fillSSHTarget(&t, hc, cfg.KeyFile, cfg.KnownHosts, home)
	if t.User == "" {
		return SSHTarget{}, errors.New("the SSH tunnel has no user")
	}
	switch t.Auth {
	case "":
		t.Auth = SSHAuthAgent
		if os.Getenv("SSH_AUTH_SOCK") == "" && (cfg.KeyFile != "" || len(hc.IdentityFiles) > 0) {
			t.Auth = SSHAuthKey
		}
	case SSHAuthAgent, SSHAuthKey, SSHAuthPassword:
	default:
		return SSHTarget{}, fmt.Errorf("unknown SSH auth %q (use agent, key or password)", sanitizeLine(cfg.Auth))
	}
	jumps, err := resolveJumps(cfg, hc, alias, home)
	if err != nil {
		return t, err
	}
	t.Jumps = jumps
	return t, nil
}

// readSSHHostConfig is the ~/.ssh/config subset for alias (empty when
// there is no home or no file).
func readSSHHostConfig(home, alias string) sshHostConfig {
	if home == "" {
		return sshHostConfig{}
	}
	data, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return sshHostConfig{}
	}
	return parseSSHConfig(data, alias)
}

// fillSSHTarget fills the blanks of t (Host is the alias, Port and User
// may be set) from hc and the defaults: HostName, Port 22, User, the key
// files and known_hosts.
func fillSSHTarget(t *SSHTarget, hc sshHostConfig, keyFile, knownHosts, home string) {
	if hc.HostName != "" {
		t.Host = hc.HostName
	}
	if t.Port == 0 {
		t.Port = hc.Port
	}
	if t.Port == 0 {
		t.Port = 22
	}
	if t.User == "" {
		t.User = hc.User
	}
	if t.User == "" {
		t.User = localUserName()
	}
	if keyFile != "" {
		t.KeyFiles = []string{expandSSHPath(keyFile, home, t.Host, t.User)}
	} else if len(hc.IdentityFiles) > 0 {
		for _, f := range hc.IdentityFiles {
			t.KeyFiles = append(t.KeyFiles, expandSSHPath(f, home, t.Host, t.User))
		}
	} else if home != "" {
		for _, f := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			t.KeyFiles = append(t.KeyFiles, filepath.Join(home, ".ssh", f))
		}
	}
	t.KnownHosts = knownHosts
	if t.KnownHosts == "" && home != "" {
		t.KnownHosts = filepath.Join(home, ".ssh", "known_hosts")
	} else {
		t.KnownHosts = expandSSHPath(t.KnownHosts, home, t.Host, t.User)
	}
}

func localUserName() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		name := u.Username
		if i := strings.LastIndexAny(name, `\`); i >= 0 { // DOMAIN\user
			name = name[i+1:]
		}
		return name
	}
	for _, k := range []string{"USER", "LOGNAME", "USERNAME"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// Tunnel is the SSH client of the tunnel's server, reached directly or
// through jump hosts (one client per hop); DialContext opens a channel
// to host:port as seen from the last server.
type Tunnel struct {
	client  *ssh.Client   // the tunnel's own server (the last hop)
	hops    []*ssh.Client // every client, first hop first (client is last)
	names   []string      // what errors call each hop
	target  SSHTarget
	done    chan struct{}
	once    sync.Once
	closing atomic.Bool
	mu      sync.Mutex
	cause   error // why the chain broke (nil: closed, or a single hop)
}

// Target is the resolved endpoint (for the row and the footer).
func (t *Tunnel) Target() SSHTarget { return t.target }

// Alive reports whether the SSH connection is still up.
func (t *Tunnel) Alive() bool {
	if t == nil {
		return false
	}
	select {
	case <-t.done:
		return false
	default:
		return true
	}
}

// Close closes every hop, the last one first, and every channel through
// them.
func (t *Tunnel) Close() error {
	if t == nil {
		return nil
	}
	t.closing.Store(true)
	var err error
	t.once.Do(func() {
		close(t.done)
		err = closeClients(t.hops)
	})
	return err
}

// closeClients closes cs in reverse order; the first error is returned
// (an inner hop already gone with its outer one is not an error).
func closeClients(cs []*ssh.Client) error {
	var first error
	for i := len(cs) - 1; i >= 0; i-- {
		if err := cs[i].Close(); err != nil && first == nil && i == len(cs)-1 {
			first = err
		}
	}
	return first
}

// watch tears the whole chain down when hop i's connection ends on its
// own. The hop named in the error is the first one (from the client)
// that no longer answers: a dead outer hop takes the inner ones with it.
func (t *Tunnel) watch(i int) {
	_ = t.hops[i].Wait() // blocked until the connection ends; no timer
	if t.closing.Load() {
		return
	}
	t.once.Do(func() {
		if len(t.hops) > 1 {
			dead := i
			for j := 0; j < i; j++ {
				if !hopAnswers(t.hops[j]) {
					dead = j
					break
				}
			}
			t.mu.Lock()
			t.cause = &tunnelClosedError{hop: t.names[dead]}
			t.mu.Unlock()
		}
		close(t.done)
		_ = closeClients(t.hops)
	})
}

// hopAnswers asks c for a reply (1 s at most; a hop that is slow to
// answer is taken as alive).
func hopAnswers(c *ssh.Client) bool {
	res := make(chan error, 1)
	go func() {
		_, _, err := c.SendRequest("keepalive@openssh.com", true, nil)
		res <- err
	}()
	select {
	case err := <-res:
		return err == nil
	case <-time.After(time.Second):
		return true
	}
}

// tunnelClosedError is ErrTunnelClosed with the hop that went away.
type tunnelClosedError struct{ hop string }

func (e *tunnelClosedError) Error() string {
	return "SSH tunnel closed: " + e.hop + " went away · R reconnect"
}

func (e *tunnelClosedError) Is(target error) bool { return target == ErrTunnelClosed }

func (t *Tunnel) closedErr() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cause != nil {
		return t.cause
	}
	return ErrTunnelClosed
}

// DialContext opens a TCP channel to addr through the tunnel.
func (t *Tunnel) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if !t.Alive() {
		return nil, t.closedErr()
	}
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("the SSH tunnel carries TCP only, not %s", network)
	}
	c, err := t.client.DialContext(ctx, "tcp", addr)
	if err != nil {
		if !t.Alive() {
			return nil, t.closedErr()
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("the SSH server could not reach %s: %s", addr, sanitizeLine(strings.TrimPrefix(err.Error(), "ssh: ")))
	}
	return &tunnelConn{Conn: c}, nil
}

// OpenTunnel resolves cfg, then dials each jump host through the one
// before it and the SSH server last. Every hop's host key is checked
// against known_hosts before anything is sent to it. ctx bounds the
// whole handshake.
func OpenTunnel(ctx context.Context, cfg SSHConfig, opts TunnelOptions) (*Tunnel, error) {
	home := opts.home()
	target, err := ResolveSSH(cfg, home)
	if err != nil {
		return nil, err
	}
	chain := append(append([]SSHTarget(nil), target.Jumps...), target)
	dial := opts.Dial
	if dial == nil {
		d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: -1}
		dial = d.DialContext
	}
	var clients []*ssh.Client
	var names []string
	fail := func(err error) (*Tunnel, error) {
		_ = closeClients(clients)
		return nil, err
	}
	for i, hop := range chain {
		jump := i < len(chain)-1
		name := jumpName(hop)
		if !jump {
			name = "SSH server " + sanitizeLine(hop.Alias)
		}
		var auth []ssh.AuthMethod
		var authErr *jumpAuthState
		cleanup := func() {}
		if jump {
			auth, authErr, cleanup = jumpAuth(ctx, hop, opts)
		} else {
			auth, cleanup, err = sshAuth(ctx, hop, opts)
			if err != nil {
				return fail(err)
			}
		}
		var conn net.Conn
		addr := hop.Addr()
		if i == 0 {
			conn, err = dial(ctx, "tcp", addr)
		} else {
			conn, err = clients[i-1].DialContext(ctx, "tcp", addr)
		}
		if err != nil {
			cleanup()
			if ctx.Err() != nil {
				return fail(fmt.Errorf("SSH: %s did not answer in time", sanitizeLine(addr)))
			}
			if i > 0 {
				return fail(fmt.Errorf("SSH: %s could not reach %s: %s", names[i-1], sanitizeLine(addr), sanitizeLine(strings.TrimPrefix(netErrText(err), "ssh: "))))
			}
			return fail(fmt.Errorf("SSH: could not reach %s: %s", sanitizeLine(addr), sanitizeLine(netErrText(err))))
		}
		c, err := sshHandshake(ctx, conn, hop, auth, authErr)
		cleanup()
		if err != nil {
			return fail(err)
		}
		clients = append(clients, c)
		names = append(names, name)
	}
	t := &Tunnel{client: clients[len(clients)-1], hops: clients, names: names, target: target, done: make(chan struct{})}
	for i := range clients {
		go t.watch(i)
	}
	return t, nil
}

// sshHandshake checks hop's host key and logs in over conn. A host key
// error wins over the login error. jump is the login state of a jump
// host (nil: the tunnel's own server).
func sshHandshake(ctx context.Context, conn net.Conn, hop SSHTarget, auth []ssh.AuthMethod, jump *jumpAuthState) (*ssh.Client, error) {
	hostCB, algos, err := hostKeyCheck(hop)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	var cbErr error
	config := &ssh.ClientConfig{
		User: hop.User,
		Auth: auth,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			cbErr = hostCB(hostname, remote, key)
			if cbErr != nil && jump != nil {
				var unknown *UnknownHostKeyError
				var changed *HostKeyChangedError
				switch {
				case errors.As(cbErr, &unknown):
					unknown.Jump = sanitizeLine(hop.Alias)
				case errors.As(cbErr, &changed):
					changed.Jump = sanitizeLine(hop.Alias)
				}
			}
			return cbErr
		},
		HostKeyAlgorithms: algos,
	}
	addr := hop.Addr()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if !stop() {
		if err == nil {
			_ = c.Close()
		}
		_ = conn.Close()
		return nil, fmt.Errorf("SSH: %s did not answer in time", sanitizeLine(addr))
	}
	if err != nil {
		_ = conn.Close()
		if cbErr != nil {
			return nil, cbErr
		}
		if jump != nil {
			return nil, jumpLoginError(err, hop, jump)
		}
		return nil, sshLoginError(err, hop)
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

func sshLoginError(err error, t SSHTarget) error {
	msg := err.Error()
	if strings.Contains(msg, "unable to authenticate") {
		switch t.Auth {
		case SSHAuthPassword:
			return &SSHSecretError{Kind: "password", Wrong: true}
		case SSHAuthAgent:
			return fmt.Errorf("SSH login as %s@%s failed: the agent's keys were not accepted", sanitizeLine(t.User), sanitizeLine(t.Host))
		default:
			return fmt.Errorf("SSH login as %s@%s failed: the key was not accepted", sanitizeLine(t.User), sanitizeLine(t.Host))
		}
	}
	msg = strings.TrimPrefix(msg, "ssh: handshake failed: ")
	return fmt.Errorf("SSH: %s", sanitizeLine(netErrText(errors.New(msg))))
}

func netErrText(err error) string {
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		return op.Err.Error()
	}
	return err.Error()
}

// hostKeyCheck is the known_hosts callback plus the key algorithms known
// for the host (so the server presents a key type we can compare).
func hostKeyCheck(t SSHTarget) (ssh.HostKeyCallback, []string, error) {
	var files []string
	if t.KnownHosts != "" {
		if _, err := os.Stat(t.KnownHosts); err == nil {
			files = append(files, t.KnownHosts)
		} else if !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("could not read %s: %s", t.KnownHosts, sanitizeLine(err.Error()))
		}
	}
	base, err := knownhosts.New(files...)
	if err != nil {
		return nil, nil, fmt.Errorf("could not read known_hosts: %s", sanitizeLine(err.Error()))
	}
	dialled := t.Addr()
	check := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if remote == nil || !splittable(remote.String()) {
			remote = hostAddr(dialled)
		}
		err := base(dialled, remote, key)
		if err == nil {
			return nil
		}
		var revoked *knownhosts.RevokedError
		if errors.As(err, &revoked) {
			return &HostKeyChangedError{Host: dialled, Fingerprint: ssh.FingerprintSHA256(key), Revoked: true}
		}
		var ke *knownhosts.KeyError
		if errors.As(err, &ke) {
			if len(ke.Want) == 0 {
				return &UnknownHostKeyError{Host: dialled, KeyType: key.Type(), Fingerprint: ssh.FingerprintSHA256(key), KnownHosts: t.KnownHosts, Key: key}
			}
			return &HostKeyChangedError{Host: dialled, Fingerprint: ssh.FingerprintSHA256(key)}
		}
		return fmt.Errorf("SSH host key check failed: %s", sanitizeLine(err.Error()))
	}
	// Ask with a key nobody has to learn the known key types of the host.
	var algos []string
	probe, _ := ssh.NewPublicKey(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)))
	var ke *knownhosts.KeyError
	if err := base(dialled, hostAddr(dialled), probe); errors.As(err, &ke) {
		seen := map[string]bool{}
		for _, k := range ke.Want {
			for _, a := range hostKeyAlgos(k.Key.Type()) {
				if !seen[a] {
					seen[a] = true
					algos = append(algos, a)
				}
			}
		}
	}
	return check, algos, nil
}

func hostKeyAlgos(keyType string) []string {
	if keyType == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}
	return []string{keyType}
}

type hostAddr string

func (a hostAddr) Network() string { return "tcp" }
func (a hostAddr) String() string  { return string(a) }

func splittable(s string) bool {
	_, _, err := net.SplitHostPort(s)
	return err == nil
}

// TrustHostKey appends host's key to the known_hosts file (created 0600
// in a 0700 directory when missing).
func TrustHostKey(file, host string, key ssh.PublicKey) error {
	if file == "" || key == nil {
		return errors.New("no known_hosts file to save the host key to")
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	prefix := ""
	if data, err := os.ReadFile(file); err == nil && len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		prefix = "\n"
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(prefix + knownhosts.Line([]string{knownhosts.Normalize(host)}, key) + "\n")
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func sshAuth(ctx context.Context, t SSHTarget, o TunnelOptions) ([]ssh.AuthMethod, func(), error) {
	noop := func() {}
	switch t.Auth {
	case SSHAuthPassword:
		if o.Password == "" {
			return nil, noop, &SSHSecretError{Kind: "password"}
		}
		pw := o.Password
		return []ssh.AuthMethod{
			ssh.Password(pw),
			ssh.KeyboardInteractive(func(_, _ string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					if !echos[i] {
						answers[i] = pw
					}
				}
				return answers, nil
			}),
		}, noop, nil
	case SSHAuthKey:
		signer, err := loadSSHKey(t.KeyFiles, o.Passphrase)
		if err != nil {
			return nil, noop, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, noop, nil
	default:
		sock := o.AgentSocket
		if sock == "" {
			sock = os.Getenv("SSH_AUTH_SOCK")
		}
		if sock == "" {
			return nil, noop, errors.New("ssh-agent is not running (SSH_AUTH_SOCK is not set); start it or use a key file")
		}
		var d net.Dialer
		conn, err := d.DialContext(ctx, "unix", sock)
		if err != nil {
			return nil, noop, errors.New("could not reach ssh-agent; start it or use a key file")
		}
		signers, err := agent.NewClient(conn).Signers()
		if err != nil {
			_ = conn.Close()
			return nil, noop, errors.New("could not read the keys of ssh-agent")
		}
		if len(signers) == 0 {
			_ = conn.Close()
			return nil, noop, errors.New("ssh-agent has no keys; add one with ssh-add")
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signers...)}, func() { _ = conn.Close() }, nil
	}
}

// maxKeyFile is the largest private key file read.
const maxKeyFile = 1 << 20

// loadSSHKey reads the first existing key file of files.
func loadSSHKey(files []string, passphrase string) (ssh.Signer, error) {
	for _, f := range files {
		data, err := readPrivateFile(f)
		if os.IsNotExist(err) && len(files) > 1 {
			continue
		}
		if err != nil {
			return nil, err
		}
		defer clear(data)
		signer, err := ssh.ParsePrivateKey(data)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			if passphrase == "" {
				return nil, &SSHSecretError{Kind: "passphrase", KeyFile: f}
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(passphrase))
			if errors.Is(err, x509.IncorrectPasswordError) {
				return nil, &SSHSecretError{Kind: "passphrase", Wrong: true, KeyFile: f}
			}
		}
		if err != nil {
			return nil, fmt.Errorf("could not read the SSH key %s: %s", filepath.Base(f), sanitizeLine(strings.TrimPrefix(err.Error(), "ssh: ")))
		}
		return signer, nil
	}
	return nil, errors.New("no SSH key file found; set \"keyFile\" in the tunnel")
}

// readPrivateFile reads a key file: a regular file under 1 MiB that only
// its owner can read (as OpenSSH and libpq require on Unix).
func readPrivateFile(p string) ([]byte, error) {
	fi, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("could not read %s: %s", p, sanitizeLine(err.Error()))
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", p)
	}
	if fi.Size() > maxKeyFile {
		return nil, fmt.Errorf("%s is too large for a key file", p)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("the key file %s can be read by other users; run chmod 600 on it", p)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %s", p, sanitizeLine(err.Error()))
	}
	return data, nil
}

// tunnelConn adds deadlines to an SSH channel (the drivers set them;
// x/crypto's channel refuses them). A deadline that passes closes the
// channel: the driver sees a timeout and drops the connection. The timer
// exists only while a deadline is set.
type tunnelConn struct {
	net.Conn
	mu      sync.Mutex
	rt, wt  *time.Timer
	expired atomic.Bool
}

type tunnelTimeout struct{}

func (tunnelTimeout) Error() string   { return "i/o timeout" }
func (tunnelTimeout) Timeout() bool   { return true }
func (tunnelTimeout) Temporary() bool { return true }

func (c *tunnelConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if err != nil && c.expired.Load() {
		return n, tunnelTimeout{}
	}
	return n, err
}

func (c *tunnelConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if err != nil && c.expired.Load() {
		return n, tunnelTimeout{}
	}
	return n, err
}

func (c *tunnelConn) arm(slot **time.Timer, t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if *slot != nil {
		(*slot).Stop()
		*slot = nil
	}
	if t.IsZero() {
		return
	}
	expire := func() {
		c.expired.Store(true)
		_ = c.Conn.Close()
	}
	d := time.Until(t)
	if d <= 0 {
		expire()
		return
	}
	*slot = time.AfterFunc(d, expire)
}

func (c *tunnelConn) SetDeadline(t time.Time) error {
	c.arm(&c.rt, t)
	c.arm(&c.wt, t)
	return nil
}
func (c *tunnelConn) SetReadDeadline(t time.Time) error  { c.arm(&c.rt, t); return nil }
func (c *tunnelConn) SetWriteDeadline(t time.Time) error { c.arm(&c.wt, t); return nil }

func (c *tunnelConn) Close() error {
	c.mu.Lock()
	for _, t := range []*time.Timer{c.rt, c.wt} {
		if t != nil {
			t.Stop()
		}
	}
	c.rt, c.wt = nil, nil
	c.mu.Unlock()
	return c.Conn.Close()
}

func hostForDisplay(hostport string) string {
	h, p, err := net.SplitHostPort(hostport)
	if err != nil {
		return sanitizeLine(hostport)
	}
	if p == "22" {
		return sanitizeLine(h)
	}
	return sanitizeLine(hostport)
}

// sanitizeLine keeps the first line of s without control characters (text
// from servers, files and the network).
func sanitizeLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == '‮' || r == '‭' || r == '⁦' || r == '⁧' || r == '⁨' || r == '⁩' || r == '‪' || r == '‫' || r == '‬' {
			return -1
		}
		return r
	}, strings.TrimSpace(line))
}
