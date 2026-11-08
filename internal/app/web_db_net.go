package app

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The page's own way to a database behind an SSH tunnel. When the laptop
// has no tunnel open for the connection, a read from the page opens one
// (the saved SSH settings and secrets, the same known_hosts check as the
// laptop's) and keeps it for the next reads until the hub stops. An
// unknown host key is never trusted on its own: the read fails with the
// key (a DBNeed), and db.trust_host saves exactly that key once the user
// said yes on the page. A secret the laptop does not have (the SSH
// password or key passphrase, the database's password) fails the same
// way; db.secret keeps the one the user typed in the secret store, where
// the next read finds it.

func init() {
	webOps[webproto.OpDBTrustHost] = (*Model).webDBTrustHost
	webOps[webproto.OpDBSecret] = (*Model).webDBSecret
}

// webDBNet is the hub's tunnels and the host keys it asked about (own
// lock: the reads run off Update).
type webDBNet struct {
	mu       sync.Mutex
	tunnels  map[string]*dbclient.Tunnel              // by connection id and SSH settings
	hostKeys map[string]*dbclient.UnknownHostKeyError // the key last asked about, by connection
}

// webDBTunnelKey names c's tunnel: new SSH settings get a new one.
func webDBTunnelKey(c dbclient.Conn) string {
	b, _ := json.Marshal(c.SSH)
	return c.ID + "\x00" + string(b)
}

// dial is the dialer of c's open tunnel, opened first when there is none.
// A connection of a container on the server asks for the container's IP
// by its name once per tunnel (docker inspect is a new SSH session and
// the docker CLI starting, often a second); a dial that fails later asks
// again once and dials the new IP (a recreated container has a new one).
func (n *webDBNet) dial(ctx context.Context, c dbclient.Conn, root string, sec dbSSHSecrets) (dbclient.DialFunc, error) {
	t, err := n.tunnel(ctx, c, root, sec)
	if err != nil {
		return nil, err
	}
	if c.Container == nil {
		return t.DialContext, nil
	}
	if !t.HostMapped(webDBContainerHost(c)) {
		if err := dbResolveContainer(ctx, c, t); err != nil {
			return nil, err
		}
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		nc, err := t.DialContext(ctx, network, addr)
		if err == nil || ctx.Err() != nil || !t.Alive() {
			return nc, err
		}
		if rerr := dbResolveContainer(ctx, c, t); rerr != nil {
			return nil, rerr // the container is gone or stopped: say so
		}
		return t.DialContext(ctx, network, addr)
	}, nil
}

// webDBContainerHost is the name dials to c's container use (as
// dbResolveContainer maps it).
func webDBContainerHost(c dbclient.Conn) string {
	if c.Host != "" {
		return c.Host
	}
	return c.Container.Name
}

// tunnel is c's open tunnel, opened first when there is none.
func (n *webDBNet) tunnel(ctx context.Context, c dbclient.Conn, root string, sec dbSSHSecrets) (*dbclient.Tunnel, error) {
	key := webDBTunnelKey(c)
	n.mu.Lock()
	if t := n.tunnels[key]; t.Alive() {
		n.mu.Unlock()
		return t, nil
	}
	n.mu.Unlock()
	t, err := dbOpenTunnel(ctx, dbResolveConnPaths(c, root, dbSSHHome()), sec)
	if err != nil {
		return nil, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if old := n.tunnels[key]; old.Alive() { // another read was quicker
		_ = t.Close()
		return old, nil
	} else if old != nil {
		_ = old.Close()
	}
	if n.tunnels == nil {
		n.tunnels = map[string]*dbclient.Tunnel{}
	}
	n.tunnels[key] = t
	return t, nil
}

// closeAll closes every tunnel (the hub stops).
func (n *webDBNet) closeAll() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for key, t := range n.tunnels {
		_ = t.Close()
		delete(n.tunnels, key)
	}
}

// askHostKey remembers the key the page is asked to trust for conn.
func (n *webDBNet) askHostKey(conn string, e *dbclient.UnknownHostKeyError) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.hostKeys == nil {
		n.hostKeys = map[string]*dbclient.UnknownHostKeyError{}
	}
	n.hostKeys[conn] = e
}

// takeHostKey is the key asked about for conn when its fingerprint is fp
// (it is then forgotten); nil otherwise.
func (n *webDBNet) takeHostKey(conn, fp string) *dbclient.UnknownHostKeyError {
	n.mu.Lock()
	defer n.mu.Unlock()
	e := n.hostKeys[conn]
	if e == nil || e.Fingerprint != fp {
		return nil
	}
	delete(n.hostKeys, conn)
	return e
}

// webDBHostName is host:port as the user names it (no :22).
func webDBHostName(hostport string) string {
	if h, p, err := netSplit(hostport); err == nil && p == "22" {
		return sanitizeDBText(h)
	}
	return sanitizeDBText(hostport)
}

// webDBRoute lets w open the page's own tunnel when the laptop has none
// open for it (read on Update: the secrets come from the store).
func (m *Model) webDBRoute(w *webDBConn) {
	if m.web == nil {
		return
	}
	w.net = &m.web.dbNet
	if w.c.SSH != nil && w.snap.dial == nil && w.c.Driver != "sqlite" && w.c.Driver != "duckdb" {
		w.sec = dbStoredSSHSecrets(w.c)
		w.tunnel = true
	}
}

// route opens (or reuses) the page's tunnel of w when it needs one.
func (w *webDBConn) route(ctx context.Context) error {
	if !w.tunnel || w.snap.dial != nil {
		return nil
	}
	dial, err := w.net.dial(ctx, w.c, w.root, w.sec)
	if err != nil {
		return err
	}
	w.snap.dial = dial
	return nil
}

// session is a session of w's connection (read-only or a plain
// auto-commit one) through the laptop's or the page's tunnel.
//
// Opening it is bounded as the laptop's connect is (dbOpenBounded): the
// answer comes by dbTestTimeout + dbConnectGrace, so the page never
// stays on Loading… when a driver or the server ignores the deadline.
func (w *webDBConn) session(ctx context.Context, readOnly bool) (dbclient.Session, error) {
	octx, cancel := context.WithTimeout(ctx, dbTestTimeout)
	type opened struct {
		snap dbAISnapshot
		sess dbclient.Session
		err  error
		ok   bool
	}
	done := make(chan opened, 1)
	cp := *w // the open works on its own copy: a late one touches nothing
	crash.GoWith("the database read", func() {
		err := cp.route(octx)
		var sess dbclient.Session
		if err == nil {
			sess, err = dbAIOpen(octx, cp.root, cp.c, &cp.snap, readOnly)
		}
		done <- opened{snap: cp.snap, sess: sess, err: err, ok: true}
	}, func() {
		done <- opened{err: errors.New("the read stopped on an internal error (a crash report was saved)")}
	})
	give := time.NewTimer(dbTestTimeout + dbConnectGrace)
	defer give.Stop()
	select {
	case r := <-done:
		cancel()
		if r.ok {
			w.snap = r.snap
		}
		if r.err != nil && ctx.Err() == nil && errors.Is(octx.Err(), context.DeadlineExceeded) && dbclient.ReachText(r.err) == "" {
			r.err = dbNoAnswer(w.c, w.c.SSH != nil) // the database said nothing in time
		}
		return r.sess, r.err
	case <-give.C:
		cancel()
		crash.Go("the database read", func() {
			if r := <-done; r.sess != nil {
				_ = r.sess.Close()
			}
		})
		return nil, dbNoAnswer(w.c, false)
	}
}

// webDBNeedOf is what err asks the user for (nil: nothing the page can
// clear) and the line that says so; a host key is remembered on net.
func (w *webDBConn) needOf(err error) (*webproto.DBNeed, string) {
	var unknown *dbclient.UnknownHostKeyError
	if errors.As(err, &unknown) && w.net != nil {
		w.net.askHostKey(w.c.ID, unknown)
		host, what := webDBHostName(unknown.Host), "SSH host"
		if unknown.Jump != "" {
			what = "SSH jump host"
		}
		return &webproto.DBNeed{Need: "host_key", Host: host, KeyType: sanitizeDBText(dbKeyTypeName(unknown.KeyType)), Fingerprint: sanitizeDBText(unknown.Fingerprint), Jump: unknown.Jump != ""},
			"Unknown " + what + " " + host + ". Check its key before you trust it."
	}
	var secret *dbclient.SSHSecretError
	if errors.As(err, &secret) {
		if secret.Kind == "passphrase" {
			key := sanitizeDBText(filepath.Base(secret.KeyFile))
			msg := "The SSH key " + key + " needs its passphrase."
			if secret.Wrong {
				msg = "Wrong passphrase for the SSH key " + key + "."
			}
			return &webproto.DBNeed{Need: "ssh_passphrase", Key: key, Wrong: secret.Wrong}, msg
		}
		msg := "The SSH server needs a password."
		if secret.Wrong {
			msg = "The SSH server did not accept the password."
		}
		return &webproto.DBNeed{Need: "ssh_password", Wrong: secret.Wrong}, msg
	}
	if webDBAuthFailed(err) && webDBPasswordName(w.c) != "" {
		if w.snap.password == "" {
			return &webproto.DBNeed{Need: "password"}, "The database needs a password."
		}
		return &webproto.DBNeed{Need: "password", Wrong: true}, "The database did not accept the password."
	}
	return nil, ""
}

// webDBAuthFailed reports a server that refused the login's password
// (PostgreSQL 28P01, MySQL 1045, Redis WRONGPASS / NOAUTH).
func webDBAuthFailed(err error) bool {
	s := err.Error()
	for _, mark := range []string{"SQLSTATE 28P01", "password authentication failed", "Error 1045", "WRONGPASS", "NOAUTH"} {
		if strings.Contains(s, mark) {
			return true
		}
	}
	return false
}

// webDBPasswordName is the secret store name c reads its password from
// when the page may set it: db.<id> (no password in its settings) or the
// NAME of ${secret:NAME}. "": c takes it from elsewhere (a .env value, a
// reference, a literal in its file) or has none.
func webDBPasswordName(c dbclient.Conn) string {
	if c.Driver == "sqlite" && !dbclient.IsSQLCipher(c) || c.Driver == "duckdb" || dbProjectCmdRef(c) {
		return ""
	}
	if _, literal := databaseLiteralPassword(c); literal {
		return ""
	}
	switch {
	case c.Password == "":
		return "db." + c.ID
	case strings.HasPrefix(c.Password, "${secret:") && strings.HasSuffix(c.Password, "}"):
		return strings.TrimSuffix(strings.TrimPrefix(c.Password, "${secret:"), "}")
	}
	return ""
}

// webDBSecretName is where db.secret of kind keeps c's secret ("" and why
// not when c takes no such secret).
func webDBSecretName(c dbclient.Conn, kind string) (string, string) {
	auth := dbSSHAuth(c)
	switch kind {
	case "password":
		if n := webDBPasswordName(c); n != "" {
			return n, ""
		}
		return "", "This connection takes its password from its settings. Change it there."
	case "ssh_password":
		if c.SSH != nil && auth == dbclient.SSHAuthPassword {
			return dbSSHSecretName(c.ID, "password"), ""
		}
		return "", "This connection does not log in to SSH with a password."
	case "ssh_passphrase":
		if c.SSH != nil && auth != dbclient.SSHAuthPassword && auth != dbclient.SSHAuthAgent {
			return dbSSHSecretName(c.ID, "passphrase"), ""
		}
		return "", "This connection does not use an SSH key file."
	}
	return "", "Unknown kind of secret."
}

// webDBSecret keeps a secret a read asked for in the laptop's secret
// store (the laptop's own prompts keep theirs there too). The value goes
// nowhere else: no answer, no audit line, no log; it is registered so
// every text the page gets has it hidden.
func (m *Model) webDBSecret(call *webCall) tea.Cmd {
	a := call.args.(*webproto.DBSecretArgs)
	_, c, ok := m.webDBAny(call, a.Conn)
	if !ok {
		return nil
	}
	name, why := webDBSecretName(c, a.Kind)
	if name == "" {
		call.fail(webproto.CodeBadRequest, why)
		return nil
	}
	ai.RegisterSecret(a.Value)
	kind, value := a.Kind, a.Value
	call.later()
	return func() tea.Msg {
		store := databaseSecretStore()
		if store == nil || store.Locked() {
			call.fail(webproto.CodeConflict, "The secret store is locked. Unlock it on the laptop first.")
			return nil
		}
		if err := store.Set(name, value); err != nil {
			call.fail(webproto.CodeConflict, "Could not save it in the laptop's secret store.")
			return nil
		}
		return applyMsg(func(m *Model) tea.Cmd {
			m.webAuditOp(call, c.ID+" "+kind, "saved")
			call.reply(webproto.DBDone{Text: "Saved in the laptop's secret store."})
			return nil
		})
	}
}

func (m *Model) webDBTrustHost(call *webCall) tea.Cmd {
	a := call.args.(*webproto.DBTrustArgs)
	if _, _, ok := m.webDBAny(call, a.Conn); !ok {
		return nil
	}
	ask := m.web.dbNet.takeHostKey(a.Conn, a.Fingerprint)
	if ask == nil {
		call.fail(webproto.CodeConflict, "That is not the key the page asked about. Open the connection again.")
		return nil
	}
	host := webDBHostName(ask.Host)
	if err := dbclient.TrustHostKey(ask.KnownHosts, ask.Host, ask.Key); err != nil {
		recordError("[web] db.trust_host: " + strings.TrimSpace(err.Error()))
		call.fail(webproto.CodeConflict, "Could not save the host key to known_hosts.")
		return nil
	}
	m.webAuditOp(call, a.Conn+" "+host+" "+ask.Fingerprint, "trusted")
	call.reply(webproto.DBDone{Text: "Host key of " + host + " saved."})
	return nil
}
