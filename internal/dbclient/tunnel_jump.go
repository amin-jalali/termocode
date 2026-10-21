package dbclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Jump hosts (ProxyJump). The chain is resolved before anything is
// dialled: cfg.Jump (the connection's JSON) wins over the ProxyJump of
// ~/.ssh/config, and each jump host is looked up in ~/.ssh/config again
// (HostName, Port, User, IdentityFile and its own ProxyJump, which comes
// before it). ProxyCommand is refused anywhere in the chain: it would run
// a shell command. A jump host logs in with ssh-agent and its key files
// only; a password or an encrypted key file that is not in the agent is
// refused with the hop's name.

// maxJumps is the most jump hosts in one chain.
const maxJumps = 8

func resolveJumps(cfg SSHConfig, hc sshHostConfig, alias, home string) ([]SSHTarget, error) {
	var specs []string
	if len(cfg.Jump) > 0 {
		specs = splitProxyJump(strings.Join(cfg.Jump, ","))
	} else if hc.ProxyCommand != "" {
		return nil, fmt.Errorf("%s: %w", sanitizeLine(alias), ErrProxyCommandUnsupported)
	} else {
		specs = splitProxyJump(hc.ProxyJump)
	}
	r := &jumpResolver{home: home, knownHosts: cfg.KnownHosts}
	return r.list(specs, []string{alias})
}

// splitProxyJump splits a ProxyJump value on commas; "none" is no jump.
func splitProxyJump(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 1 && strings.EqualFold(out[0], "none") {
		return nil
	}
	return out
}

type jumpResolver struct {
	home, knownHosts string
	count            int
}

func (r *jumpResolver) list(specs []string, stack []string) ([]SSHTarget, error) {
	var out []SSHTarget
	for _, spec := range specs {
		hops, err := r.hop(spec, stack)
		if err != nil {
			return nil, err
		}
		out = append(out, hops...)
	}
	return out, nil
}

// hop resolves one jump host and the jump hosts it needs itself (stack:
// the names being resolved, to stop a loop).
func (r *jumpResolver) hop(spec string, stack []string) ([]SSHTarget, error) {
	user, host, port, err := parseJumpSpec(spec)
	if err != nil {
		return nil, err
	}
	for _, s := range stack {
		if strings.EqualFold(s, host) {
			names := make([]string, 0, len(stack)+1)
			for _, n := range append(stack, host) {
				names = append(names, sanitizeLine(n))
			}
			return nil, fmt.Errorf("%w: %s", ErrProxyJumpLoop, strings.Join(names, " → "))
		}
	}
	if len(stack) > maxJumps {
		return nil, fmt.Errorf("%w (at most %d)", ErrTooManyJumps, maxJumps)
	}
	hc := readSSHHostConfig(r.home, host)
	if hc.ProxyCommand != "" {
		return nil, fmt.Errorf("jump host %s: %w", sanitizeLine(host), ErrProxyCommandUnsupported)
	}
	inner := append(append([]string(nil), stack...), host)
	prefix, err := r.list(splitProxyJump(hc.ProxyJump), inner)
	if err != nil {
		return nil, err
	}
	t := SSHTarget{Alias: host, Host: host, Port: port, User: user}
	fillSSHTarget(&t, hc, "", r.knownHosts, r.home)
	if t.User == "" {
		return nil, fmt.Errorf("jump host %s has no user", sanitizeLine(host))
	}
	r.count++
	if r.count > maxJumps {
		return nil, fmt.Errorf("%w (at most %d)", ErrTooManyJumps, maxJumps)
	}
	return append(prefix, t), nil
}

// parseJumpSpec reads [ssh://][user@]host[:port] ([host]:port for IPv6).
func parseJumpSpec(spec string) (user, host string, port int, err error) {
	s := strings.TrimPrefix(strings.TrimSpace(spec), "ssh://")
	if i := strings.LastIndex(s, "@"); i >= 0 {
		user, s = s[:i], s[i+1:]
	}
	host = s
	portText := ""
	switch {
	case strings.HasPrefix(s, "["):
		end := strings.Index(s, "]")
		if end < 0 {
			return "", "", 0, fmt.Errorf("jump host %q: missing ]", sanitizeLine(spec))
		}
		host, portText = s[1:end], strings.TrimPrefix(s[end+1:], ":")
	case strings.Count(s, ":") == 1:
		host, portText, _ = strings.Cut(s, ":")
	}
	if host == "" || strings.IndexFunc(host+user, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return "", "", 0, fmt.Errorf("jump host %q is not a host name", sanitizeLine(spec))
	}
	if portText != "" {
		port, err = strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return "", "", 0, fmt.Errorf("jump host %q: the port is not valid", sanitizeLine(spec))
		}
	}
	return user, host, port, nil
}

func jumpName(t SSHTarget) string { return "jump host " + sanitizeLine(t.Alias) }

// jumpAuthState is what a jump host's login found (for the error).
type jumpAuthState struct {
	keys       int    // keys offered
	note       string // why a key file was not usable
	wantSecret bool   // the server asked for a password
}

var errJumpPassword = errors.New("jump host asks for a password")

// jumpAuth is a jump host's login: the agent's keys and the hop's key
// files that need no passphrase. Keys are read only once the host key is
// checked (the callback runs after it).
func jumpAuth(ctx context.Context, hop SSHTarget, o TunnelOptions) ([]ssh.AuthMethod, *jumpAuthState, func()) {
	st := &jumpAuthState{}
	var agentConn net.Conn
	cleanup := func() {
		if agentConn != nil {
			_ = agentConn.Close()
		}
	}
	var cached []ssh.Signer
	loaded := false
	signers := func() ([]ssh.Signer, error) {
		if loaded {
			return cached, nil
		}
		loaded = true
		var out []ssh.Signer
		sock := o.AgentSocket
		if sock == "" {
			sock = os.Getenv("SSH_AUTH_SOCK")
		}
		if sock != "" {
			var d net.Dialer
			if c, err := d.DialContext(ctx, "unix", sock); err == nil {
				agentConn = c
				if ss, err := agent.NewClient(c).Signers(); err == nil {
					out = append(out, ss...)
				}
			}
		}
		for _, f := range hop.KeyFiles {
			data, err := readPrivateFile(f)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				if st.note == "" {
					st.note = err.Error()
				}
				continue
			}
			signer, err := ssh.ParsePrivateKey(data)
			clear(data)
			var missing *ssh.PassphraseMissingError
			if errors.As(err, &missing) {
				if st.note == "" {
					st.note = "the key " + filepath.Base(f) + " needs its passphrase: add it to ssh-agent with ssh-add"
				}
				continue
			}
			if err != nil {
				if st.note == "" {
					st.note = "could not read the SSH key " + filepath.Base(f)
				}
				continue
			}
			out = append(out, signer)
		}
		st.keys = len(out)
		cached = out
		return out, nil
	}
	refuse := func() (string, error) {
		st.wantSecret = true
		return "", errJumpPassword
	}
	return []ssh.AuthMethod{
		ssh.PublicKeysCallback(signers),
		ssh.PasswordCallback(refuse),
		ssh.KeyboardInteractive(func(string, string, []string, []bool) ([]string, error) {
			st.wantSecret = true
			return nil, errJumpPassword
		}),
	}, st, cleanup
}

// jumpLoginError names the jump host whose login failed.
func jumpLoginError(err error, hop SSHTarget, st *jumpAuthState) error {
	name := jumpName(hop)
	who := sanitizeLine(hop.User) + "@" + hostForDisplay(hop.Addr())
	switch {
	case st != nil && st.wantSecret:
		return fmt.Errorf("SSH login to %s (%s) needs a password: jump hosts log in with ssh-agent or a key file only", name, who)
	case st != nil && st.keys == 0 && st.note != "":
		return fmt.Errorf("SSH login to %s (%s) failed: %s", name, who, sanitizeLine(st.note))
	case st != nil && st.keys == 0:
		return fmt.Errorf("SSH login to %s (%s) failed: no key found; load one into ssh-agent or set IdentityFile for it in ~/.ssh/config", name, who)
	case strings.Contains(err.Error(), "unable to authenticate"):
		return fmt.Errorf("SSH login to %s (%s) failed: the keys were not accepted", name, who)
	}
	msg := strings.TrimPrefix(err.Error(), "ssh: handshake failed: ")
	return fmt.Errorf("SSH: %s: %s", name, sanitizeLine(netErrText(errors.New(msg))))
}
