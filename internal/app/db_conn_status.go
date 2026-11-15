package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// The true state of a database connection (owner 2026-11-15: the AI's
// list said "not connected" for all three, while BaaytonaDB could not
// connect at all and rentcar-db-1 had no password). It comes from a
// cheap check with no side effects and no network dial: the open
// sessions, the config (host, port, user, the SSH and container
// target), whether the password is there (the secret store's index, an
// env or .env variable; never its value) and the last connect error of
// this run. Only the user's Test (a button) dials.

// Kinds of a connection status.
const (
	dbStatusConnected   = "connected"
	dbStatusConnecting  = "connecting"
	dbStatusReady       = "ready"       // complete; nothing failed yet
	dbStatusMissing     = "missing"     // Field is not set
	dbStatusUnreachable = "unreachable" // the last connect failed
	dbStatusTunnelDown  = "tunnel_down" // the last SSH tunnel open failed
	dbStatusProblem     = "problem"     // the file entry is broken
)

// dbConnStatus is one connection's state.
type dbConnStatus struct {
	Kind   string
	Field  string    // missing: password, user, host, port, ssh host, container
	Detail string    // one line, no password
	At     time.Time // unreachable, tunnel down: when it failed
}

// dbStatusLive is what the Databases view holds for the connection now.
type dbStatusLive struct {
	connected, connecting bool
	problem               string // the row's own error, when it shows one
}

// dbStatusEnv is what the password check reads: the workspace (its
// .env), the secret store's names (its index: no unlock) and whether a
// store opened at all.
type dbStatusEnv struct {
	root  string
	names map[string]bool
	store bool
	typed map[string]bool // a password typed this session (the view holds it)
}

// dbAuthDrivers log in with a user and a password.
var dbAuthDrivers = map[string]bool{"postgres": true, "mysql": true, "sqlserver": true, "oracle": true}

// dbUserDrivers have no default user (PostgreSQL and MySQL use the
// login's or root).
var dbUserDrivers = map[string]bool{"sqlserver": true, "oracle": true}

// dbConnStatusOf checks c.
func dbConnStatusOf(c dbclient.Conn, env dbStatusEnv, live dbStatusLive, last *dbConnErr) dbConnStatus {
	switch {
	case live.connected:
		return dbConnStatus{Kind: dbStatusConnected, Detail: dbStatusRoute(c)}
	case live.connecting:
		return dbConnStatus{Kind: dbStatusConnecting, Detail: dbStatusRoute(c)}
	}
	// A failed try is fresher than a guess about the password (a server
	// may take none); any other missing field wins: it is the fix.
	if st, ok := dbStatusMissingInfo(c, env); ok && (st.Field != "password" || last == nil || c.Problem != "") {
		return st
	}
	if c.Problem != "" {
		return dbConnStatus{Kind: dbStatusProblem, Detail: dbStatusText(dbFriendlyProblem(c.Problem))}
	}
	if last != nil {
		k := dbStatusUnreachable
		if last.Tunnel {
			k = dbStatusTunnelDown
		}
		return dbConnStatus{Kind: k, Detail: dbStatusText(last.Text), At: last.At}
	}
	return dbConnStatus{Kind: dbStatusReady, Detail: dbStatusRoute(c)}
}

// dbStatusMissingInfo is the first field c lacks.
func dbStatusMissingInfo(c dbclient.Conn, env dbStatusEnv) (dbConnStatus, bool) {
	miss := func(field, detail string) (dbConnStatus, bool) {
		return dbConnStatus{Kind: dbStatusMissing, Field: field, Detail: detail}, true
	}
	p := strings.ToLower(c.Problem)
	switch {
	case strings.Contains(p, "password must be a reference"), strings.Contains(p, "literal password"):
		return miss("password", "the project file holds the password itself; it must be a reference")
	case strings.Contains(p, "host and valid port"), strings.Contains(p, "server address is required"):
		if strings.TrimSpace(c.Host) == "" && c.URL == "" {
			return miss("host", "no host set")
		}
		return miss("port", "no valid port set")
	case strings.Contains(p, "path is required"):
		return miss("path", "no file path set")
	}
	file := c.Driver == "sqlite" || c.Driver == "duckdb"
	if file {
		return dbConnStatus{}, false
	}
	if c.SSH != nil && strings.TrimSpace(c.SSH.Host) == "" {
		return miss("ssh host", "the SSH tunnel names no server")
	}
	if c.Container != nil && strings.TrimSpace(c.Container.Name) == "" {
		return miss("container", "no container name set")
	}
	var fromURL dbclient.Conn
	if c.URL != "" {
		fromURL, _ = dbclient.ParseURL(c.URL)
	}
	if c.URL == "" && c.Kube == nil && strings.TrimSpace(c.Host) == "" && c.Container == nil && (c.Options == nil || c.Options.Socket == "") {
		return miss("host", "no host set")
	}
	if !dbAuthDrivers[c.Driver] {
		return dbConnStatus{}, false
	}
	if dbUserDrivers[c.Driver] && strings.TrimSpace(c.User) == "" && fromURL.User == "" {
		return miss("user", "no user set")
	}
	if ok, why := dbStatusHasPassword(c, fromURL, env); !ok {
		return miss("password", why)
	}
	return dbConnStatus{}, false
}

// dbStatusHasPassword says whether c's password is there, without
// reading a value from the secret store (its index only).
func dbStatusHasPassword(c, fromURL dbclient.Conn, env dbStatusEnv) (bool, string) {
	if env.typed[c.ID] || fromURL.Password != "" {
		return true, ""
	}
	pw := strings.TrimSpace(c.Password)
	if pw == "" {
		if env.names["db."+c.ID] {
			return true, ""
		}
		return false, "nothing saved"
	}
	ref, err := secrets.ParseRef(pw)
	if err != nil {
		if strings.HasPrefix(pw, "${secret:") && strings.HasSuffix(pw, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(pw, "${secret:"), "}")
			if !env.store {
				return false, "the secret store is not open (secret " + name + ")"
			}
			if env.names[name] {
				return true, ""
			}
			return false, "secret " + name + " is not saved"
		}
		return true, "" // a password in your own file (moved to the store on connect)
	}
	switch ref.Kind {
	case secrets.RefEnv:
		if v, ok := os.LookupEnv(ref.Arg); ok && v != "" {
			return true, ""
		}
		return false, "the variable " + ref.Arg + " is not set"
	case secrets.RefDotenv:
		if v, ok := databaseEnvValue(filepath.Join(env.root, ".env"), ref.Arg); ok && v != "" {
			return true, ""
		}
		return false, ref.Arg + " is not in the project's .env"
	}
	return true, "" // ${cmd:}: runs on connect
}

// dbStatusRoute is how c is reached, for the ready and connected rows.
func dbStatusRoute(c dbclient.Conn) string {
	switch {
	case c.Container != nil && c.SSH != nil:
		return "container " + sanitizeDBText(c.Container.Name) + " on " + sanitizeDBText(c.Container.Context) + " through ssh " + dbSSHLabel(*c.SSH)
	case c.Container != nil:
		return "container " + sanitizeDBText(c.Container.Name) + " on this computer"
	case c.SSH != nil:
		return "SSH tunnel to " + dbSSHLabel(*c.SSH) + " opens on connect"
	}
	return ""
}

// dbStatusText is s on one line without the " · " the list splits on.
func dbStatusText(s string) string {
	return strings.ReplaceAll(dbOneLine(sanitizeDBText(s)), " · ", ", ")
}

// Label is the status as the list and the card show it.
func (st dbConnStatus) Label(now time.Time) string {
	when := ""
	if !st.At.IsZero() {
		when = " (last try " + dbStatusAgo(now.Sub(st.At)) + ")"
	}
	switch st.Kind {
	case dbStatusConnected:
		return "connected"
	case dbStatusConnecting:
		return "connecting"
	case dbStatusMissing:
		// "password, nothing saved": never "password <word>", which the
		// redaction reads as a password.
		if d := dbStatusText(st.Detail); d != "" {
			return "missing info: " + st.Field + ", " + d
		}
		return "missing info: " + st.Field
	case dbStatusUnreachable:
		return "unreachable: " + dbStatusText(st.Detail) + when
	case dbStatusTunnelDown:
		return "SSH tunnel down: " + dbStatusText(st.Detail) + when
	case dbStatusProblem:
		return "problem: " + dbStatusText(st.Detail)
	}
	return "ready to connect"
}

// dbStatusFromLabel reads a label back (the card reads the list's text).
func dbStatusFromLabel(l string) (kind, field string) {
	switch {
	case l == "connected":
		return dbStatusConnected, ""
	case l == "connecting":
		return dbStatusConnecting, ""
	case strings.HasPrefix(l, "missing info: "):
		f := strings.TrimPrefix(l, "missing info: ")
		if i := strings.Index(f, ", "); i >= 0 {
			f = f[:i]
		}
		return dbStatusMissing, f
	case strings.HasPrefix(l, "unreachable: "):
		return dbStatusUnreachable, ""
	case strings.HasPrefix(l, "SSH tunnel down: "):
		return dbStatusTunnelDown, ""
	case strings.HasPrefix(l, "problem: "):
		return dbStatusProblem, ""
	}
	return dbStatusReady, ""
}

// dbStatusNext is the next step the model offers for a status.
func dbStatusNext(st dbConnStatus) string {
	switch st.Kind {
	case dbStatusConnected:
		return "list tables or run a query"
	case dbStatusConnecting:
		return "wait for the connect"
	case dbStatusMissing:
		return "Fix… on the card sets the " + st.Field + "; then Connect"
	case dbStatusUnreachable:
		return "Test it, then fix the route or the server (the error says which step failed)"
	case dbStatusTunnelDown:
		return "Test it; check the SSH server and its key"
	case dbStatusProblem:
		return "fix the connection file (Fix… on the card)"
	}
	return "Connect"
}

func dbStatusAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + " min ago"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " h ago"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + " days ago"
}

// ── the last connect error of each connection (this run only) ─────────

// dbConnErr is the last failed connect of a connection: the Databases
// view's connect or Test, or a db_* tool's login.
type dbConnErr struct {
	Text   string
	At     time.Time
	Tunnel bool // the SSH tunnel did not open
}

var dbConnErrs struct {
	sync.Mutex
	m map[string]dbConnErr
}

// dbNoteConnErr keeps text as id's last error.
func dbNoteConnErr(id, text string, tunnel bool) {
	if id == "" || strings.TrimSpace(text) == "" {
		return
	}
	dbConnErrs.Lock()
	defer dbConnErrs.Unlock()
	if dbConnErrs.m == nil {
		dbConnErrs.m = map[string]dbConnErr{}
	}
	dbConnErrs.m[id] = dbConnErr{Text: dbStatusText(text), At: time.Now(), Tunnel: tunnel}
}

// dbClearConnErr drops id's last error (a connect worked).
func dbClearConnErr(id string) {
	dbConnErrs.Lock()
	defer dbConnErrs.Unlock()
	delete(dbConnErrs.m, id)
}

// dbLastConnErr is id's last error (nil: none).
func dbLastConnErr(id string) *dbConnErr {
	dbConnErrs.Lock()
	defer dbConnErrs.Unlock()
	if e, ok := dbConnErrs.m[id]; ok {
		return &e
	}
	return nil
}

// dbStatusEnvNow is the env of a check now: the store's names, no unlock.
func dbStatusEnvNow(root string) dbStatusEnv {
	env := dbStatusEnv{root: root, names: map[string]bool{}}
	if store := databaseSecretStore(); store != nil {
		env.store = store.Backend() != "none"
		for _, n := range store.Names() {
			env.names[n] = true
		}
	}
	return env
}

// dbIsTunnelErr: the SSH tunnel itself failed (not the database behind a
// tunnel that worked).
func dbIsTunnelErr(err error) bool {
	if err == nil {
		return false
	}
	var reach *dbclient.ReachError
	if errors.As(err, &reach) {
		return reach.Via != "" && reach.Prohibited
	}
	var unknown *dbclient.UnknownHostKeyError
	var changed *dbclient.HostKeyChangedError
	var secret *dbclient.SSHSecretError
	if errors.As(err, &unknown) || errors.As(err, &changed) || errors.As(err, &secret) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "ssh: ") || strings.Contains(s, "ssh tunnel")
}

// dbAINoteOpen keeps a db_* tool's failed login as c's last error, and
// drops it when one worked. A stop, a cancel or an unanswered question
// is not the connection's fault.
func dbAINoteOpen(ctx context.Context, c dbclient.Conn, snap *dbAISnapshot, err error) {
	if err == nil {
		dbClearConnErr(c.ID)
		return
	}
	var none dbAINoAnswer
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.As(err, &none) || errors.Is(err, errDBAIAskBusy) {
		return
	}
	text := dbConnectProblem(c, err)
	if text == "" {
		text = dbErrorText(err)
	}
	dbNoteConnErr(c.ID, text, snap != nil && snap.routeErr != nil)
}
