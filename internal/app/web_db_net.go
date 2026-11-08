package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The page's own way to a database behind an SSH tunnel. When the laptop
// has no tunnel open for the connection, a read from the page opens one
// (the saved SSH settings and secrets, the same known_hosts check as the
// laptop's) and keeps it for the next reads until the hub stops. An
// unknown host key is never trusted on its own: the read fails with the
// key (a DBNeed), and db.trust_host saves exactly that key once the user
// said yes on the page.

func init() {
	webOps[webproto.OpDBTrustHost] = (*Model).webDBTrustHost
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
func (n *webDBNet) dial(ctx context.Context, c dbclient.Conn, root string, sec dbSSHSecrets) (dbclient.DialFunc, error) {
	key := webDBTunnelKey(c)
	n.mu.Lock()
	if t := n.tunnels[key]; t.Alive() {
		n.mu.Unlock()
		return t.DialContext, nil
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
		return old.DialContext, nil
	} else if old != nil {
		_ = old.Close()
	}
	if n.tunnels == nil {
		n.tunnels = map[string]*dbclient.Tunnel{}
	}
	n.tunnels[key] = t
	return t.DialContext, nil
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
func (w *webDBConn) session(ctx context.Context, readOnly bool) (dbclient.Session, error) {
	if err := w.route(ctx); err != nil {
		return nil, err
	}
	return dbAIOpen(ctx, w.root, w.c, &w.snap, readOnly)
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
	return nil, ""
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
