package app

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/lazyre"
)

// Owner rule (2026-11-09): termocode uses only the connection settings
// the owner configured and never tries another route. When a configured
// connection fails, the reason is one sentence that names the step that
// failed and the configured values (never a secret), so the owner can fix
// the config: the same line in the Databases view, on the web page and in
// the AI's db_* tool results.

// dbConnectProblem is that sentence for a connect of c that failed with
// err after the database's address was reached (or not reached): the
// address that did not answer, the login, the database, SSL. "": err is
// none of those (the caller shows the driver's own line).
func dbConnectProblem(c dbclient.Conn, err error) string {
	if err == nil {
		return ""
	}
	if t := dbclient.ReachText(err); t != "" {
		return sanitizeDBText(t)
	}
	s := err.Error()
	// Only a connect's own errors: PostgreSQL ends a login with FATAL, pgx
	// says "failed to connect", MySQL's login errors are 1045 and 1049. A
	// statement's error (ERROR: role "x" does not exist) is not one.
	if !strings.Contains(s, "failed to connect") && !strings.Contains(s, "FATAL:") && !strings.Contains(s, "Error 1045") && !strings.Contains(s, "Error 1049") &&
		!strings.Contains(s, "tls error: ") && !strings.Contains(s, "server refused TLS connection") {
		return ""
	}
	user, database := dbConnUserDB(c)
	// A blank setting: the name the server was given (the driver's
	// default), as the server says it.
	if user == "" {
		user = dbFirstGroup(dbServerUser.FindStringSubmatch(s))
	}
	if database == "" {
		database = dbFirstGroup(dbServerDatabase.FindStringSubmatch(s))
	}
	if user == "" {
		user = "(none set)"
	}
	if database == "" {
		database = "(none set)"
	}
	what := ""
	switch {
	case strings.Contains(s, "SQLSTATE 28P01"), strings.Contains(s, "password authentication failed"), strings.Contains(s, "Error 1045"):
		what = dbDriverTitle(c.Driver) + " did not accept the password of user " + user
	case dbPGRoleMissing.MatchString(s):
		what = "user " + user + " does not exist there"
	case strings.Contains(s, "SQLSTATE 3D000"), dbPGDatabaseMissing.MatchString(s), strings.Contains(s, "Error 1049"):
		what = "database " + database + " does not exist there"
	case strings.Contains(s, "no pg_hba.conf entry"):
		what = "PostgreSQL does not let user " + user + " in from there (no pg_hba.conf entry)"
	case strings.Contains(s, "server refused TLS connection"), strings.Contains(s, "SSL is not enabled on the server"), strings.Contains(s, "server does not support SSL"):
		what = "the server does not accept SSL"
		if mode := dbConnSSLMode(c); mode != "" {
			what += " (sslmode " + mode + ")"
		}
	case strings.Contains(s, "x509: "):
		what = "the SSL check failed (" + dbReasonTail(s[strings.Index(s, "x509: "):]) + ")"
	case strings.Contains(s, "tls error: "):
		what = "the SSL handshake failed (" + dbReasonTail(s[strings.Index(s, "tls error: ")+len("tls error: "):]) + ")"
	}
	if what == "" {
		return ""
	}
	head := dbConnAddr(c) + " answers, but "
	if c.SSH != nil {
		head = "SSH to " + dbSSHLabel(*c.SSH) + " works and " + head
	}
	return sanitizeDBText(head + what + ".")
}

var (
	dbPGRoleMissing     = lazyre.New(`role "[^"]*" does not exist`)
	dbPGDatabaseMissing = lazyre.New(`database "[^"]*" does not exist|Unknown database '`)
	// The names in the server's own words (pgx's "user=… database=…",
	// PostgreSQL's quotes, MySQL's 'user'@).
	dbServerUser     = lazyre.New("`user=([^\\s`]+)|for user \"([^\"]+)\"|role \"([^\"]+)\"|for user '([^']+)'")
	dbServerDatabase = lazyre.New("database=([^\\s`]+)`|database \"([^\"]+)\"|Unknown database '([^']+)'")
)

// dbReasonTail is the first line of a reason, without a trailing dot.
func dbReasonTail(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return strings.TrimSuffix(strings.TrimSpace(s), ".")
}

// dbConnUserDB is the configured user and database of c (from its URL
// when the fields are blank).
func dbConnUserDB(c dbclient.Conn) (string, string) {
	user, database := c.User, c.Database
	if (user == "" || database == "") && c.URL != "" {
		if p, err := dbclient.ParseURL(c.URL); err == nil {
			if user == "" {
				user = p.User
			}
			if database == "" {
				database = p.Database
			}
		}
	}
	return user, database
}

// dbFirstGroup is the first group of mt that matched ("": none).
func dbFirstGroup(mt []string) string {
	for i := 1; i < len(mt); i++ {
		if mt[i] != "" {
			return mt[i]
		}
	}
	return ""
}

// dbConnSSLMode is the configured sslmode of c ("": none set).
func dbConnSSLMode(c dbclient.Conn) string {
	if m := c.Params["sslmode"]; m != "" {
		return m
	}
	if m, err := dbclient.EffectiveSSLMode(c.SSL); err == nil {
		return m
	}
	return ""
}

// dbConnErrorText is the line shown for a failed connect of c: the
// precise sentence, else the driver's own line.
func dbConnErrorText(c dbclient.Conn, err error) string {
	if p := dbConnectProblem(c, err); p != "" {
		return p
	}
	return dbErrorText(err)
}

// dbConnectFail is a connect error of the AI's db_* tools: its text is
// the precise sentence (the driver's error stays underneath).
type dbConnectFail struct {
	text string
	err  error
}

func (e *dbConnectFail) Error() string { return e.text }
func (e *dbConnectFail) Unwrap() error { return e.err }

// ── the Docker hint ────────────────────────────────────────────────────

// dbDockerPSCmd lists the server's running containers: name, image and
// ports (read-only; one SSH session on the open tunnel).
const dbDockerPSCmd = `docker ps --format '{{.Names}}{{"\t"}}{{.Image}}{{"\t"}}{{.Ports}}'`

// dbDockerHintWait bounds the docker ps of the hint.
var dbDockerHintWait = 3 * time.Second

// dbDockerHint is one sentence of information when `docker ps` (psOut)
// shows a container of driver (by image or name) that has port only
// inside Docker (not published on the server); "" otherwise.
func dbDockerHint(psOut, driver string, port int) string {
	if port <= 0 {
		return ""
	}
	inside := strconv.Itoa(port) + "/tcp"
	for _, line := range strings.Split(psOut, "\n") {
		f := strings.Split(strings.TrimSpace(line), "\t")
		if len(f) != 3 || !containerNameOK(f[0]) {
			continue
		}
		name, image, ports := f[0], f[1], f[2]
		if dbDriverForImage(image) != driver && dbDriverForName(name) != driver {
			continue
		}
		only, published := false, false
		for _, p := range strings.Split(ports, ",") {
			p = strings.TrimSpace(p)
			switch {
			case p == inside:
				only = true
			case strings.HasSuffix(p, "->"+inside):
				published = true
			}
		}
		if only && !published {
			n := strconv.Itoa(port)
			return "A container " + sanitizeDBText(name) + " on that server has " + n + " only inside Docker; publish it (ports: \"127.0.0.1:" + n + ":" + n + "\") or point this connection at it."
		}
	}
	return ""
}

var dbContainerNameRe = lazyre.New(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func containerNameOK(s string) bool { return dbContainerNameRe.MatchString(s) }

// dbDriverForName is the driver a container's name says (pg-main,
// shop-mysql, …); "" when it says none.
func dbDriverForName(name string) string {
	for _, w := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == '-' || r == '_' || r == '.' }) {
		switch w {
		case "pg", "postgres", "postgresql", "pgsql":
			return "postgres"
		case "mysql", "mariadb":
			return "mysql"
		case "redis", "valkey":
			return "redis"
		}
	}
	return ""
}

// dbHintDial is dial (through tun) that adds the Docker hint to a
// refused dial of c's configured address: one read-only docker ps on the
// tunnel's own SSH connection, asked once. It only informs; nothing else
// is dialled. A connection to a configured container needs no hint.
func dbHintDial(c dbclient.Conn, tun *dbclient.Tunnel, dial dbclient.DialFunc) dbclient.DialFunc {
	if tun == nil || dial == nil || c.Container != nil {
		return dial
	}
	var once sync.Once
	var hint string
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		nc, err := dial(ctx, network, addr)
		var reach *dbclient.ReachError
		if err == nil || !errors.As(err, &reach) || reach.Via == "" || reach.Timeout || reach.Prohibited || reach.Reason != "" {
			return nc, err
		}
		once.Do(func() {
			_, p, _ := net.SplitHostPort(addr)
			port, _ := strconv.Atoi(p)
			hctx, cancel := context.WithTimeout(ctx, dbDockerHintWait)
			defer cancel()
			if out, code, oerr := tun.Output(hctx, dbDockerPSCmd); oerr == nil && code == 0 {
				hint = dbDockerHint(out, c.Driver, port)
			}
		})
		reach.Hint = hint
		return nil, err
	}
}
