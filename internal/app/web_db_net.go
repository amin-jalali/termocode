package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// webDBIdleFor is how long the page keeps a connection's read-only
// session unused before it closes it.
var webDBIdleFor = 2 * time.Minute

// webDBNet is the hub's tunnels and the host keys it asked about (own
// lock: the reads run off Update).
type webDBNet struct {
	mu       sync.Mutex
	tunnels  map[string]*dbclient.Tunnel              // by connection id and SSH settings
	hostKeys map[string]*dbclient.UnknownHostKeyError // the key last asked about, by connection
	kept     map[string][]*webDBKept                  // idle read-only sessions, by webDBConn.keptKey
	stopped  bool                                     // the hub stopped: nothing is kept any more
	// resolving locks the container lookups of a tunnel (by tunnel key).
	resolving map[string]*sync.Mutex
}

// webDBKeepMax is how many idle read-only sessions the page keeps per
// connection (two reads at once each have one).
const webDBKeepMax = 2

// webDBKept is a read-only session the page keeps for the next reads of
// its connection, with the password it logged in with (every answer
// masks it) and the tunnel it rides.
type webDBKept struct {
	sess  dbclient.Session
	pw    string
	hasPW bool
	via   *dbclient.Tunnel // the laptop's tunnel it rides (nil: the page's, or none)
	tun   *dbclient.Tunnel // the tunnel it rides (nil: none)
	timer *time.Timer      // closes it after webDBIdleFor unused
}

// take is an idle session kept under key that rides via (it is the
// caller's until its lease ends); nil: none. Sessions whose tunnel
// closed are closed.
func (n *webDBNet) take(key string, via *dbclient.Tunnel) *webDBKept {
	n.mu.Lock()
	var got *webDBKept
	var stale, rest []*webDBKept
	for _, k := range n.kept[key] {
		switch {
		case k.tun != nil && !k.tun.Alive():
			k.timer.Stop()
			stale = append(stale, k)
		case got == nil && k.via == via:
			k.timer.Stop()
			got = k
		default:
			rest = append(rest, k)
		}
	}
	if len(rest) == 0 {
		delete(n.kept, key)
	} else {
		n.kept[key] = rest
	}
	n.mu.Unlock()
	for _, k := range stale {
		_ = k.sess.Close()
	}
	return got
}

// put keeps k under key for webDBIdleFor (closed instead when the hub
// stopped or enough are kept).
func (n *webDBNet) put(key string, k *webDBKept) {
	n.mu.Lock()
	if n.stopped || len(n.kept[key]) >= webDBKeepMax || k.tun != nil && !k.tun.Alive() {
		n.mu.Unlock()
		_ = k.sess.Close()
		return
	}
	if n.kept == nil {
		n.kept = map[string][]*webDBKept{}
	}
	n.kept[key] = append(n.kept[key], k)
	k.timer = time.AfterFunc(webDBIdleFor, func() { n.expire(key, k) })
	n.mu.Unlock()
}

// expire closes k when it is still kept (idle for webDBIdleFor).
func (n *webDBNet) expire(key string, k *webDBKept) {
	n.mu.Lock()
	list, found := n.kept[key], false
	for i, x := range list {
		if x == k {
			n.kept[key] = append(list[:i:i], list[i+1:]...)
			found = true
			break
		}
	}
	if len(n.kept[key]) == 0 {
		delete(n.kept, key)
	}
	n.mu.Unlock()
	if found {
		_ = k.sess.Close()
	}
}

// dropConn closes the kept sessions of connection id (its password or
// settings changed).
func (n *webDBNet) dropConn(id string) {
	n.mu.Lock()
	var drop []*webDBKept
	for key, list := range n.kept {
		if strings.HasPrefix(key, id+"\x00") {
			drop = append(drop, list...)
			delete(n.kept, key)
		}
	}
	for _, k := range drop {
		k.timer.Stop()
	}
	n.mu.Unlock()
	for _, k := range drop {
		_ = k.sess.Close()
	}
}

// webDBLease is a kept session lent to one read: Close gives it back for
// the next read, unless a call on it failed (it may be broken: it is
// closed) or a transaction was asked for (never on a kept session).
type webDBLease struct {
	n   *webDBNet
	key string
	k   *webDBKept

	mu   sync.Mutex
	bad  bool
	done bool
}

func (l *webDBLease) fail(err error) {
	if err != nil {
		l.mu.Lock()
		l.bad = true
		l.mu.Unlock()
	}
}

// webDBAgain reports a read that failed on a dropped connection (a kept one
// the server closed: a restart, a recreated container): it runs once
// more, on a new connection of the pool. It only reads.
func webDBAgain(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() == nil && dbclient.ConnLost(err)
}

func (l *webDBLease) Ping(ctx context.Context) error {
	err := l.k.sess.Ping(ctx)
	if webDBAgain(ctx, err) {
		err = l.k.sess.Ping(ctx)
	}
	l.fail(err)
	return err
}

func (l *webDBLease) Introspect(ctx context.Context, req dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	r, err := l.k.sess.Introspect(ctx, req)
	if webDBAgain(ctx, err) {
		r, err = l.k.sess.Introspect(ctx, req)
	}
	l.fail(err)
	return r, err
}

func (l *webDBLease) Run(ctx context.Context, sql string, opts dbclient.RunOptions) dbclient.Result {
	r := l.k.sess.Run(ctx, sql, opts)
	if webDBAgain(ctx, r.Err) {
		r = l.k.sess.Run(ctx, sql, opts)
	}
	l.fail(r.Err)
	return r
}

func (l *webDBLease) Begin(ctx context.Context) error {
	l.fail(errors.New("a transaction"))
	return l.k.sess.Begin(ctx)
}

func (l *webDBLease) Commit(ctx context.Context) error {
	l.fail(errors.New("a transaction"))
	return l.k.sess.Commit(ctx)
}

func (l *webDBLease) Rollback(ctx context.Context) error {
	l.fail(errors.New("a transaction"))
	return l.k.sess.Rollback(ctx)
}

func (l *webDBLease) ReadOnlySession() (dbclient.Session, error) { return l.k.sess.ReadOnlySession() }

func (l *webDBLease) Cancel() { l.k.sess.Cancel() }

func (l *webDBLease) Close() error {
	l.mu.Lock()
	done, bad := l.done, l.bad
	l.done = true
	l.mu.Unlock()
	switch {
	case done:
	case bad:
		_ = l.k.sess.Close()
	default:
		l.n.put(l.key, l.k)
	}
	return nil
}

// webDBKeeps reports a driver whose read-only session the page keeps (a
// network login; sqlite opens a local file).
func webDBKeeps(driver string) bool { return driver == "postgres" || driver == "mysql" }

// keptKey names w's kept sessions: the connection's settings and the
// password it logs in with (the laptop's, or the secret store's).
func (w *webDBConn) keptKey() string {
	b, _ := json.Marshal(w.c)
	pw := "store"
	if w.snap.hasPW {
		sum := sha256.Sum256([]byte(w.snap.password))
		pw = hex.EncodeToString(sum[:])
	}
	return w.c.ID + "\x00" + string(b) + "\x00" + pw
}

// open is a read-only session of w's connection (the caller closes it).
// A network connection's session is kept for the next reads (one login,
// not one per read); w.fresh asks for a new login.
func (w *webDBConn) open(ctx context.Context) (dbclient.Session, error) {
	if w.net == nil || w.fresh || !webDBKeeps(w.c.Driver) {
		return w.session(ctx, true)
	}
	key := w.keptKey()
	if k := w.net.take(key, w.via); k != nil {
		w.snap.password, w.snap.hasPW = k.pw, k.hasPW
		return &webDBLease{n: w.net, key: key, k: k}, nil
	}
	w.keep = true
	s, err := w.session(ctx, true)
	w.keep = false
	if err != nil {
		return nil, err
	}
	k := &webDBKept{sess: s, pw: w.snap.password, hasPW: w.snap.hasPW, via: w.via, tun: w.tun}
	return &webDBLease{n: w.net, key: key, k: k}, nil
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
func (n *webDBNet) dial(ctx context.Context, c dbclient.Conn, root string, sec dbSSHSecrets) (dbclient.DialFunc, *dbclient.Tunnel, error) {
	t, err := n.tunnel(ctx, c, root, sec)
	if err != nil {
		return nil, nil, err
	}
	if c.Container == nil {
		return t.DialContext, t, nil
	}
	host, mu := webDBContainerHost(c), n.resolver(webDBTunnelKey(c))
	// resolve asks for the IP unless the tunnel points host somewhere
	// other than was: one ask for reads that fail at the same time.
	resolve := func(ctx context.Context, was string) error {
		mu.Lock()
		defer mu.Unlock()
		if t.HostMapped(host) != was {
			return nil
		}
		return dbResolveContainer(ctx, c, t)
	}
	if err := resolve(ctx, ""); err != nil {
		return nil, nil, err
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		was := t.HostMapped(host)
		nc, err := t.DialContext(ctx, network, addr)
		if err == nil || ctx.Err() != nil || !t.Alive() {
			return nc, err
		}
		if rerr := resolve(ctx, was); rerr != nil {
			return nil, rerr // the container is gone or stopped: say so
		}
		return t.DialContext(ctx, network, addr)
	}, t, nil
}

// resolver is the lock of the container lookups through the tunnel key
// names.
func (n *webDBNet) resolver(key string) *sync.Mutex {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.resolving == nil {
		n.resolving = map[string]*sync.Mutex{}
	}
	if n.resolving[key] == nil {
		n.resolving[key] = &sync.Mutex{}
	}
	return n.resolving[key]
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

// closeAll closes every kept session, then every tunnel (the hub stops).
func (n *webDBNet) closeAll() {
	n.mu.Lock()
	n.stopped = true
	var drop []*webDBKept
	for key, list := range n.kept {
		drop = append(drop, list...)
		delete(n.kept, key)
	}
	n.mu.Unlock()
	for _, k := range drop {
		k.timer.Stop()
		_ = k.sess.Close()
	}
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
	dial, t, err := w.net.dial(ctx, w.c, w.root, w.sec)
	if err != nil {
		return err
	}
	w.snap.dial, w.tun = dial, t
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
		tun  *dbclient.Tunnel
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
			sess, err = dbAIOpenAs(octx, cp.root, cp.c, &cp.snap, readOnly, cp.keep)
		}
		done <- opened{snap: cp.snap, tun: cp.tun, sess: sess, err: err, ok: true}
	}, func() {
		done <- opened{err: errors.New("the read stopped on an internal error (a crash report was saved)")}
	})
	give := time.NewTimer(dbTestTimeout + dbConnectGrace)
	defer give.Stop()
	select {
	case r := <-done:
		cancel()
		if r.ok {
			w.snap, w.tun = r.snap, r.tun
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
	kind, value, net := a.Kind, a.Value, &m.web.dbNet
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
		net.dropConn(c.ID) // the next read logs in with it
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
