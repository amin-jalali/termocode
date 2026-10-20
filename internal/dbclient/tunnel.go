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

// ErrProxyJumpUnsupported is a host whose ~/.ssh/config entry sets
// ProxyJump or ProxyCommand: one bastion only in this version.
var ErrProxyJumpUnsupported = errors.New("ProxyJump is not supported yet: tunnel to the last bastion directly")

// UnknownHostKeyError is a host that is not in known_hosts. The app shows
// Host and Fingerprint and, when the user trusts it, calls TrustHostKey.
type UnknownHostKeyError struct {
	Host        string // host:port as dialled
	KeyType     string
	Fingerprint string // SHA256:…
	KnownHosts  string // the file TrustHostKey appends to
	Key         ssh.PublicKey
}

func (e *UnknownHostKeyError) Error() string {
	return "unknown SSH host " + hostForDisplay(e.Host) + " · " + e.Fingerprint
}

// HostKeyChangedError is a host whose key differs from known_hosts (or is
// revoked). It is refused; there is no override.
type HostKeyChangedError struct {
	Host        string
	Fingerprint string
	Revoked     bool
}

func (e *HostKeyChangedError) Error() string {
	if e.Revoked {
		return "the host key of " + hostForDisplay(e.Host) + " is revoked; check with your admin"
	}
	return "the host key of " + hostForDisplay(e.Host) + " changed; check with your admin"
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
	ProxyJump  string
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

// ResolveSSH fills cfg from ~/.ssh/config under home.
func ResolveSSH(cfg SSHConfig, home string) (SSHTarget, error) {
	alias := strings.TrimSpace(cfg.Host)
	if alias == "" {
		return SSHTarget{}, errors.New("the SSH tunnel has no host")
	}
	var hc sshHostConfig
	if home != "" {
		if data, err := os.ReadFile(filepath.Join(home, ".ssh", "config")); err == nil {
			hc = parseSSHConfig(data, alias)
		}
	}
	t := SSHTarget{Alias: alias, Host: alias, Port: cfg.Port, User: cfg.User, Auth: strings.ToLower(strings.TrimSpace(cfg.Auth)), ProxyJump: hc.ProxyJump}
	if hc.HostName != "" {
		t.Host = hc.HostName
	}
	if t.Port == 0 {
		t.Port = hc.Port
	}
	if t.Port == 0 {
		t.Port = 22
	}
	if t.Port < 1 || t.Port > 65535 {
		return SSHTarget{}, errors.New("the SSH port is not valid")
	}
	if t.User == "" {
		t.User = hc.User
	}
	if t.User == "" {
		t.User = localUserName()
	}
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
	if cfg.KeyFile != "" {
		t.KeyFiles = []string{expandSSHPath(cfg.KeyFile, home, t.Host, t.User)}
	} else if len(hc.IdentityFiles) > 0 {
		for _, f := range hc.IdentityFiles {
			t.KeyFiles = append(t.KeyFiles, expandSSHPath(f, home, t.Host, t.User))
		}
	} else if home != "" {
		for _, f := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			t.KeyFiles = append(t.KeyFiles, filepath.Join(home, ".ssh", f))
		}
	}
	t.KnownHosts = cfg.KnownHosts
	if t.KnownHosts == "" && home != "" {
		t.KnownHosts = filepath.Join(home, ".ssh", "known_hosts")
	} else {
		t.KnownHosts = expandSSHPath(t.KnownHosts, home, t.Host, t.User)
	}
	return t, nil
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

// Tunnel is one SSH client; DialContext opens a channel to host:port as
// seen from the SSH server.
type Tunnel struct {
	client *ssh.Client
	target SSHTarget
	done   chan struct{}
	once   sync.Once
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

// Close closes the SSH client and every channel through it.
func (t *Tunnel) Close() error {
	if t == nil {
		return nil
	}
	var err error
	t.once.Do(func() {
		err = t.client.Close()
		close(t.done)
	})
	return err
}

// DialContext opens a TCP channel to addr through the tunnel.
func (t *Tunnel) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if !t.Alive() {
		return nil, ErrTunnelClosed
	}
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("the SSH tunnel carries TCP only, not %s", network)
	}
	c, err := t.client.DialContext(ctx, "tcp", addr)
	if err != nil {
		if !t.Alive() {
			return nil, ErrTunnelClosed
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("the SSH server could not reach %s: %s", addr, sanitizeLine(strings.TrimPrefix(err.Error(), "ssh: ")))
	}
	return &tunnelConn{Conn: c}, nil
}

// OpenTunnel resolves cfg, dials the SSH server, checks its host key and
// logs in. ctx bounds the whole handshake.
func OpenTunnel(ctx context.Context, cfg SSHConfig, opts TunnelOptions) (*Tunnel, error) {
	home := opts.home()
	target, err := ResolveSSH(cfg, home)
	if err != nil {
		return nil, err
	}
	if target.ProxyJump != "" {
		return nil, ErrProxyJumpUnsupported
	}
	hostCB, algos, err := hostKeyCheck(target)
	if err != nil {
		return nil, err
	}
	auth, cleanup, err := sshAuth(ctx, target, opts)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	var cbErr error
	config := &ssh.ClientConfig{
		User: target.User,
		Auth: auth,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			cbErr = hostCB(hostname, remote, key)
			return cbErr
		},
		HostKeyAlgorithms: algos,
	}
	dial := opts.Dial
	if dial == nil {
		d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: -1}
		dial = d.DialContext
	}
	addr := target.Addr()
	conn, err := dial(ctx, "tcp", addr)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("SSH: %s did not answer in time", sanitizeLine(addr))
		}
		return nil, fmt.Errorf("SSH: could not reach %s: %s", sanitizeLine(addr), sanitizeLine(netErrText(err)))
	}
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
		return nil, sshLoginError(err, target)
	}
	_ = conn.SetDeadline(time.Time{})
	t := &Tunnel{client: ssh.NewClient(c, chans, reqs), target: target, done: make(chan struct{})}
	go func() { // blocked until the connection ends; no timer, no polling
		_ = t.client.Wait()
		t.once.Do(func() { close(t.done) })
	}()
	return t, nil
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
