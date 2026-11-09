package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// db_connection: the AI manages the connections of the Databases view as
// the GUI does (add, edit, delete, test, connect, disconnect, SSH tunnel,
// SSL, Open in Databases of a database container).
//
// It asks exactly where the GUI asks: the web's cards (db.add, db.edit's
// connection form, delete, db.disconnect, db.ssh, db.ssl) ask every time,
// with Cancel first and no "Always allow"; test, connect and Open in
// Databases run at once, as db.test, db.connect and the Containers row do.
//
// A password never comes from the model: a password or a URL (which can
// hold one) is refused before the card. connect and Open in Databases
// start the view's own connect, whose password prompt asks the user; the
// typed value goes to the secret store (internal/secrets), never to the AI.
// The arguments are checked with the web's own rules (webproto).

// aiDBConnOps are the ops; true: the op asks (a card every time).
var aiDBConnOps = map[string]bool{
	"add": true, "edit": true, "delete": true, "disconnect": true, "ssh": true, "ssl": true,
	"test": false, "connect": false, "open_container": false,
}

// aiDBConnFields are the fields edit may change (never the id, the
// password or a URL).
var aiDBConnFields = []string{"name", "host", "port", "user", "database", "path", "environment", "read_only"}

// aiDBAnyConn is a saved connection of any driver by id.
func aiDBAnyConn(root, id string) (dbclient.Conn, error) {
	_, _, conns := dbLoadConfigs(root)
	var ids []string
	for _, c := range conns {
		if c.ID == id {
			return c, nil
		}
		ids = append(ids, c.ID)
	}
	if id == "" {
		return dbclient.Conn{}, fmt.Errorf("give conn (one of: %s; see db_connections)", dbAIList(ids))
	}
	return dbclient.Conn{}, fmt.Errorf("no connection %q; connections: %s (see db_connections)", sanitizeDBText(id), dbAIList(ids))
}

// aiDBCheckWeb checks v with the web's rules for op.
func aiDBCheckWeb(op string, v any) error {
	raw, err := json.Marshal(v)
	if err == nil {
		_, err = webproto.DecodeArgs(op, raw)
	}
	if err != nil {
		return errors.New(strings.TrimPrefix(err.Error(), "webproto: "))
	}
	return nil
}

// aiDBInside checks that rel is a workspace file (or a new file in a
// workspace folder), links followed.
func aiDBInside(root, rel string) (string, error) {
	if strings.TrimSpace(rel) == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "~") {
		return "", errors.New("give a file path relative to the workspace")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("the file must be inside the workspace")
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootReal = root
	}
	abs := filepath.Join(root, clean)
	for d := abs; ; d = filepath.Dir(d) {
		if real, err := filepath.EvalSymlinks(d); err == nil {
			if r, err := filepath.Rel(rootReal, real); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
				return "", errors.New("the file must be inside the workspace")
			}
			break
		}
		if d == root || d == filepath.Dir(d) {
			break
		}
	}
	return abs, nil
}

// aiDBFileLabel names the file that holds c.
func aiDBFileLabel(source string) string {
	if source == dbclient.SourceUser {
		return "your connections file (~/.config/termocode/db/connections.json)"
	}
	return ".termocode/databases.json"
}

// aiDBConnReq is one parsed db_connection call.
type aiDBConnReq struct {
	op      string
	conn    dbclient.Conn // the saved connection (not for add)
	add     dbclient.Conn // add: the new connection
	project bool          // add: save to the project file
	edited  dbclient.Conn // edit: c after the change
	changes []string      // edit: "field: old → new"
	ssh     *dbclient.SSHConfig
	ssl     *dbclient.SSLConfig
	remove  bool
	err     error
}

func aiDBArgSet(a map[string]any, k string) bool {
	v, ok := a[k]
	if !ok || v == nil {
		return false
	}
	if s, isStr := v.(string); isStr {
		return strings.TrimSpace(s) != ""
	}
	return true
}

func aiDBConnParse(root string, a map[string]any) *aiDBConnReq {
	r := &aiDBConnReq{op: ai.ArgString(a, "op")}
	fail := func(err error) *aiDBConnReq { r.err = err; return r }
	if _, ok := aiDBConnOps[r.op]; !ok {
		return fail(errors.New("give op: add, edit, delete, test, connect, disconnect, ssh, ssl or open_container"))
	}
	for _, k := range []string{"password", "ssh_password", "passphrase", "secret"} {
		if _, ok := a[k]; ok {
			return fail(errors.New("a password is never given to the AI: the Databases view asks the user for it on connect (op connect) and keeps it in the secret store"))
		}
	}
	if _, ok := a["url"]; ok {
		return fail(errors.New("a url is not taken (it can hold a password): give host, port, user and database"))
	}
	if r.op == "open_container" {
		if strings.TrimSpace(ai.ArgString(a, "container")) == "" {
			return fail(errors.New("give container: the name of a database container (containers_list)"))
		}
		return r
	}
	if r.op == "add" {
		return aiDBConnParseAdd(root, a, r)
	}
	c, err := aiDBAnyConn(root, ai.ArgString(a, "conn"))
	if err != nil {
		return fail(err)
	}
	r.conn = c
	switch r.op {
	case "edit":
		return aiDBConnParseEdit(root, a, r)
	case "ssh", "ssl":
		if c.Driver == "sqlite" {
			return fail(errors.New("a SQLite database is a local file: it has no tunnel or SSL"))
		}
		if r.op == "ssl" {
			ssl := dbclient.SSLConfig{Mode: ai.ArgString(a, "ssl_mode"), CA: ai.ArgString(a, "ssl_ca"), Cert: ai.ArgString(a, "ssl_cert"), Key: ai.ArgString(a, "ssl_key")}
			if err := aiDBCheckWeb(webproto.OpDBSSL, webproto.DBSSLArgs{Conn: c.ID, Mode: ssl.Mode, CA: ssl.CA, Cert: ssl.Cert, Key: ssl.Key}); err != nil {
				return fail(fmt.Errorf("%s (ssl_mode: disable, require, verify-ca or verify-full)", err))
			}
			r.ssl = &ssl
			return r
		}
		if ai.ArgBool(a, "remove") {
			if c.SSH == nil {
				return fail(errors.New("this connection has no SSH tunnel"))
			}
			r.remove = true
			return r
		}
		if strings.TrimSpace(ai.ArgString(a, "ssh_host")) == "" {
			return fail(errors.New("give ssh_host (or remove: true)"))
		}
		var jump []string
		for _, j := range strings.Split(ai.ArgString(a, "ssh_jump"), ",") {
			if j = strings.TrimSpace(j); j != "" {
				jump = append(jump, j)
			}
		}
		ssh := dbclient.SSHConfig{Host: ai.ArgString(a, "ssh_host"), Port: ai.ArgInt(a, "ssh_port"), User: ai.ArgString(a, "ssh_user"),
			Auth: ai.ArgString(a, "ssh_auth"), KeyFile: ai.ArgString(a, "ssh_key_file"), Jump: jump}
		if err := aiDBCheckWeb(webproto.OpDBSSH, webproto.DBSSHArgs{Conn: c.ID, Host: ssh.Host, Port: ssh.Port, User: ssh.User, Auth: ssh.Auth, KeyFile: ssh.KeyFile, Jump: ssh.Jump}); err != nil {
			return fail(fmt.Errorf("%s (ssh_auth: agent, key or password)", err))
		}
		if c.SSH != nil {
			ssh.KnownHosts = c.SSH.KnownHosts
		}
		r.ssh = &ssh
	}
	return r
}

func aiDBConnParseAdd(root string, a map[string]any, r *aiDBConnReq) *aiDBConnReq {
	switch f := ai.ArgString(a, "file"); f {
	case "", "project":
		r.project = true
	case "user":
	default:
		r.err = errors.New("file is project (.termocode/databases.json, the default) or user (your connections file)")
		return r
	}
	c := dbclient.Conn{ID: ai.ArgString(a, "id"), Name: ai.ArgString(a, "name"), Driver: ai.ArgString(a, "driver"), Host: ai.ArgString(a, "host"),
		Port: ai.ArgInt(a, "port"), User: ai.ArgString(a, "user"), Database: ai.ArgString(a, "database"), Path: ai.ArgString(a, "path"),
		DB: ai.ArgInt(a, "db"), Environment: ai.ArgString(a, "environment"), ReadOnly: ai.ArgBool(a, "read_only"), Source: dbclient.SourceUser}
	if r.project {
		c.Source = dbclient.SourceProject
	}
	if err := aiDBCheckWeb(webproto.OpDBAdd, webproto.DBAddArgs{ID: c.ID, Name: c.Name, Driver: c.Driver, Host: c.Host, Port: c.Port, User: c.User,
		Database: c.Database, Path: c.Path, DB: c.DB, Env: c.Environment, ReadOnly: c.ReadOnly}); err != nil {
		r.err = fmt.Errorf("%s (id: a-z 0-9 _ -; driver: postgres, mysql, sqlite, redis or cassandra)", err)
		return r
	}
	if _, err := aiDBAnyConn(root, c.ID); err == nil {
		r.err = fmt.Errorf("a connection with id %s exists", c.ID)
		return r
	}
	if c.Driver != "sqlite" && c.Port == 0 {
		c.Port = webDBDefaultPort(c.Driver)
	}
	if err := dbclient.Validate(c); err != nil {
		r.err = errors.New(sanitizeDBText(err.Error()))
		return r
	}
	if c.Driver == "sqlite" {
		if _, err := aiDBInside(root, c.Path); err != nil {
			r.err = err
			return r
		}
	}
	r.add = c
	return r
}

func aiDBConnParseEdit(root string, a map[string]any, r *aiDBConnReq) *aiDBConnReq {
	c := r.conn
	n := c
	show := func(v string) string {
		if v == "" {
			return "(none)"
		}
		return sanitizeDBText(v)
	}
	for _, k := range aiDBConnFields {
		if !aiDBArgSet(a, k) {
			continue
		}
		var old, now string
		switch k {
		case "name":
			old, n.Name = c.Name, ai.ArgString(a, k)
			now = n.Name
		case "host":
			old, n.Host = c.Host, ai.ArgString(a, k)
			now = n.Host
		case "port":
			n.Port = ai.ArgInt(a, k)
			old, now = strconv.Itoa(c.Port), strconv.Itoa(n.Port)
		case "user":
			old, n.User = c.User, ai.ArgString(a, k)
			now = n.User
		case "database":
			old, n.Database = c.Database, ai.ArgString(a, k)
			now = n.Database
		case "path":
			old, n.Path = c.Path, ai.ArgString(a, k)
			now = n.Path
		case "environment":
			old, n.Environment = c.Environment, ai.ArgString(a, k)
			now = n.Environment
		case "read_only":
			n.ReadOnly = ai.ArgBool(a, k)
			old, now = dbOnOff(c.ReadOnly), dbOnOff(n.ReadOnly)
		}
		if old != now {
			r.changes = append(r.changes, k+": "+show(old)+" → "+show(now))
		}
	}
	if len(r.changes) == 0 {
		r.err = errors.New("nothing to change: give one of " + strings.Join(aiDBConnFields, ", "))
		return r
	}
	if c.URL != "" && (n.Host != c.Host || n.Port != c.Port || n.User != c.User || n.Database != c.Database) {
		r.err = errors.New("this connection is a URL: the user changes its server in the Databases view")
		return r
	}
	switch n.Driver { // the web's checks know these drivers
	case "postgres", "mysql", "sqlite", "redis", "cassandra":
		if err := aiDBCheckWeb(webproto.OpDBAdd, webproto.DBAddArgs{ID: n.ID, Name: n.Name, Driver: n.Driver, Host: n.Host, Port: n.Port, User: n.User,
			Database: n.Database, Path: n.Path, DB: n.DB, Env: n.Environment, ReadOnly: n.ReadOnly}); err != nil {
			r.err = err
			return r
		}
	}
	if err := dbclient.Validate(n); err != nil {
		r.err = errors.New(sanitizeDBText(err.Error()))
		return r
	}
	if n.Driver == "sqlite" && n.Path != c.Path {
		if _, err := aiDBInside(root, n.Path); err != nil {
			r.err = err
			return r
		}
	}
	r.edited = n
	return r
}

func aiDBConnDescribe(r *aiDBConnReq, a map[string]any) string {
	if r.err != nil {
		return "db_connection (invalid: " + dbOneLine(r.err.Error()) + ")"
	}
	name := sanitizeDBText(dbConnName(r.conn))
	switch r.op {
	case "add":
		c := r.add
		var b strings.Builder
		file := aiDBFileLabel(c.Source)
		b.WriteString("Save this connection to " + file + "?\n\n")
		line := func(k, v string) {
			if v != "" {
				b.WriteString(k + ": " + sanitizeDBText(v) + "\n")
			}
		}
		line("Name", dbConnName(c))
		line("Id", c.ID)
		line("Type", c.Driver)
		if c.Driver == "sqlite" {
			line("File", c.Path)
		} else {
			line("Server", c.Host+":"+strconv.Itoa(c.Port))
		}
		line("User", c.User)
		line("Database", c.Database)
		if c.Driver == "redis" {
			line("Database number", strconv.Itoa(c.DB))
		}
		line("Environment", c.Environment)
		if c.ReadOnly {
			line("Read-only", "yes")
		}
		if c.Driver != "sqlite" {
			b.WriteString("\nNo password is saved. The Databases view asks you for it when you connect.")
		}
		return strings.TrimRight(b.String(), "\n")
	case "edit":
		return "Change connection " + name + " in " + aiDBFileLabel(r.conn.Source) + "?\n\n" + strings.Join(r.changes, "\n")
	case "delete":
		return "Delete connection " + name + "?\n\nThis removes " + sanitizeDBText(r.conn.ID) + " from " + aiDBFileLabel(r.conn.Source) +
			", closes its sessions and deletes its saved password. The database itself is not touched."
	case "disconnect":
		return "Disconnect " + name + "?\n\nUncommitted changes of an open transaction are rolled back."
	case "ssh":
		if r.remove {
			return "Remove the SSH tunnel of " + name + "? It connects to " + sanitizeDBText(r.conn.Host) + " directly after that."
		}
		target := r.ssh.Host
		if r.ssh.User != "" {
			target = r.ssh.User + "@" + target
		}
		if r.ssh.Port != 0 {
			target += ":" + strconv.Itoa(r.ssh.Port)
		}
		body := "Connect to " + name + " through the SSH server " + sanitizeDBText(target) + "?"
		if len(r.ssh.Jump) > 0 {
			body += "\nJump hosts: " + sanitizeDBText(strings.Join(r.ssh.Jump, ", "))
		}
		if r.ssh.Auth != "" {
			body += "\nSign-in: " + r.ssh.Auth
		}
		if r.ssh.KeyFile != "" {
			body += "\nKey file: " + sanitizeDBText(r.ssh.KeyFile)
		}
		return body + "\n\nA password or key passphrase, if one is needed, is asked from you when you connect."
	case "ssl":
		body := "Set SSL of " + name + " to " + r.ssl.Mode + "?"
		for _, f := range [][2]string{{"CA", r.ssl.CA}, {"Client certificate", r.ssl.Cert}, {"Client key", r.ssl.Key}} {
			if f[1] != "" {
				body += "\n" + f[0] + ": " + sanitizeDBText(f[1])
			}
		}
		if r.ssl.Mode == "disable" {
			body += "\n\nThe connection is not encrypted."
		}
		return body
	case "test":
		return "Test connection " + name
	case "connect":
		return "Connect " + name + " in the Databases view"
	case "open_container":
		return "Open container " + sanitizeDBText(ai.ArgString(a, "container")) + " in Databases"
	}
	return "db_connection " + r.op
}

// databaseRemoveConn deletes connection id from the file that holds it,
// closes its sessions and deletes its saved secrets.
func (m *Model) databaseRemoveConn(id string) error {
	s := m.databases
	c, ok := s.connection(id)
	if !ok {
		return errors.New("connection not found")
	}
	var gone []string
	for p := range s.nodes {
		if p == id || strings.HasPrefix(p, id+"/") {
			gone = append(gone, p)
		}
	}
	switch c.Source {
	case dbclient.SourceUser:
		users, err := dbclient.DeleteConn(s.userConns, id)
		if err != nil {
			return err
		}
		_ = m.databaseDisconnectNow(id)
		s.dbCloseConnSessions(id)
		if err := m.dbSaveUserConns(users, gone...); err != nil {
			return err
		}
	case dbclient.SourceProject:
		list, err := dbclient.DeleteConn(s.projectConns, id)
		if err != nil {
			return err
		}
		if err := writeDBConfig(filepath.Join(s.root, ".termocode", "databases.json"), list); err != nil {
			return err
		}
		_ = m.databaseDisconnectNow(id)
		s.dbCloseConnSessions(id)
		for _, p := range gone {
			delete(s.nodes, p)
		}
		s.projectConns = list
		s.load(s.root)
	default:
		return errors.New("this connection is not in a connections file")
	}
	if store := databaseSecretStore(); store != nil && !store.Locked() {
		for _, name := range []string{"db." + c.ID, dbSSHSecretName(c.ID, "password"), dbSSHSecretName(c.ID, "passphrase")} {
			_ = store.Delete(name)
		}
	}
	return nil
}

func aiDBConnectionTool(root string, call aiIDECaller) ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	num := func(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
	boo := func(d string) map[string]any { return map[string]any{"type": "boolean", "description": d} }
	ops := make([]string, 0, len(aiDBConnOps))
	for op := range aiDBConnOps {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	ui := func(ctx context.Context, fn func(m *Model) (string, tea.Cmd, error)) (string, error) {
		if call == nil {
			return "", errors.New("the Databases view is not available")
		}
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			m.ensureDatabasesLoaded()
			return fn(m)
		})
	}
	return ai.Tool{
		Name: "db_connection",
		Description: "Manage a connection of the Databases view, in the file that holds it (.termocode/databases.json or your connections file). " +
			"op add (id, driver, host, port, user, database, path for sqlite, db for redis, environment, read_only, file project|user), " +
			"edit (conn + the fields to change), delete, ssh (ssh_host, ssh_port, ssh_user, ssh_auth, ssh_key_file, ssh_jump; or remove: true), " +
			"ssl (ssl_mode, ssl_ca, ssl_cert, ssl_key) and disconnect ask the user every time. test (a fresh login and a ping), connect " +
			"(the view connects; it asks the user for a missing password) and open_container (Open in Databases of a database container) run at once. " +
			"Never pass a password or a URL: the user types passwords in the Databases view.",
		Params: map[string]any{
			"op":   map[string]any{"type": "string", "enum": ops, "description": "what to do"},
			"conn": str("connection id (db_connections); not for add or open_container"),
			"id":   str("add: the new connection's id (a-z 0-9 _ -)"), "name": str("add / edit: the display name"),
			"driver": map[string]any{"type": "string", "enum": []string{"postgres", "mysql", "sqlite", "redis", "cassandra"}, "description": "add: the database type"},
			"host":   str("add / edit: the server"), "port": num("add / edit: the port (default: the driver's)"),
			"user": str("add / edit: the login name"), "database": str("add / edit: the database (cassandra: keyspace)"),
			"path": str("add / edit (sqlite): a file inside the workspace"), "db": num("add (redis): the database number"),
			"environment": str("add / edit: dev, staging, production …"), "read_only": boo("add / edit: refuse writes"),
			"file":     map[string]any{"type": "string", "enum": []string{"project", "user"}, "description": "add: project (.termocode/databases.json, default) or user (your connections file)"},
			"ssh_host": str("ssh: the SSH server or ~/.ssh/config alias"), "ssh_port": num("ssh: its port"), "ssh_user": str("ssh: the SSH login"),
			"ssh_auth":     map[string]any{"type": "string", "enum": []string{"agent", "key", "password"}, "description": "ssh: how to sign in"},
			"ssh_key_file": str("ssh: the key file"), "ssh_jump": str("ssh: jump hosts, comma separated"), "remove": boo("ssh: remove the tunnel"),
			"ssl_mode": map[string]any{"type": "string", "enum": []string{"disable", "require", "verify-ca", "verify-full"}, "description": "ssl: the mode"},
			"ssl_ca":   str("ssl: CA file"), "ssl_cert": str("ssl: client certificate file"), "ssl_key": str("ssl: client key file"),
			"container": str("open_container: the container's name (containers_list)"),
		},
		Required: []string{"op"},
		Describe: func(a map[string]any) string { return aiDBConnDescribe(aiDBConnParse(root, a), a) },
		Validate: func(a map[string]any) error { return aiDBConnParse(root, a).err },
		Gate: func(a map[string]any) (bool, string) {
			ask, ok := aiDBConnOps[ai.ArgString(a, "op")]
			return ask || !ok, ""
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			r := aiDBConnParse(root, a)
			if r.err != nil {
				return "", r.err
			}
			id := r.conn.ID
			switch r.op {
			case "add":
				c := r.add
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					s := m.databases
					if s.duplicateConnectionID(c.ID) {
						return "", nil, fmt.Errorf("a connection with id %s exists", c.ID)
					}
					s.pendingConn, s.pendingPassword, s.pendingPasswordRef, s.pendingSSH = c, "", dbTypedRef{}, dbSSHSecrets{}
					if cmd := m.databaseSavePending(r.project); cmd != nil {
						s.pendingConn = dbclient.Conn{}
						return "", cmd, errors.New("could not save the connection")
					}
					return "added " + c.ID + " to " + aiDBFileLabel(c.Source) + "; db_connection op connect connects it (the user types a password there if one is needed)", nil, nil
				})
			case "edit":
				n := r.edited
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					if err := m.databaseUpdateConn(id, func(c *dbclient.Conn) {
						c.Name, c.Host, c.Port, c.User, c.Database, c.Path, c.Environment, c.ReadOnly = n.Name, n.Host, n.Port, n.User, n.Database, n.Path, n.Environment, n.ReadOnly
					}); err != nil {
						return "", nil, errors.New(sanitizeDBText(err.Error()))
					}
					out := "saved " + id + " (" + strings.Join(r.changes, "; ") + ")"
					if m.databases.sessions[id] != nil {
						out += "; it is connected: disconnect and connect again to use it"
					}
					return out, nil, nil
				})
			case "ssh", "ssl":
				ssh, ssl, remove := r.ssh, r.ssl, r.remove
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					if err := m.databaseUpdateConn(id, func(c *dbclient.Conn) {
						switch {
						case remove:
							c.SSH = nil
						case ssh != nil:
							s := *ssh
							c.SSH = &s
						default:
							s := *ssl
							c.SSL = &s
						}
					}); err != nil {
						return "", nil, errors.New(sanitizeDBText(err.Error()))
					}
					out := "saved the " + strings.ToUpper(r.op) + " settings of " + id
					if m.databases.sessions[id] != nil {
						out += "; connect again to use them"
					}
					return out, nil, nil
				})
			case "delete":
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					if err := m.databaseRemoveConn(id); err != nil {
						return "", nil, errors.New(sanitizeDBText(err.Error()))
					}
					return "deleted connection " + id + " from " + aiDBFileLabel(r.conn.Source), nil, nil
				})
			case "disconnect":
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					if m.databases.sessions[id] == nil {
						return "", nil, fmt.Errorf("%s is not connected", id)
					}
					return "disconnected " + id, m.databaseDisconnectNow(id), nil
				})
			case "connect":
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					if m.databases.sessions[id] != nil {
						return id + " is already connected", nil, nil
					}
					return "Connecting " + id + " in the Databases view. If a password is needed, the view asks the user for it (you never see it); db_connections shows the state.", m.databaseConnect(id), nil
				})
			case "open_container":
				want := strings.TrimSpace(ai.ArgString(a, "container"))
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					targets := dbRuntimeTargets(m.services)
					var names []string
					for _, t := range targets {
						if t.name == want || (t.isCont && t.cont.ID == want) || (!t.isCont && t.compose == want) {
							if saved, ok := m.databases.databaseSavedMatch(t); ok {
								return t.name + " is already in Databases as " + saved.ID, nil, nil
							}
							return "Opening " + t.name + " in Databases: the user types its password there (blank if none); then db_connections lists it.", m.databaseOpenTarget(t), nil
						}
						names = append(names, t.name)
					}
					if len(names) == 0 {
						return "", nil, errors.New("no database container is known; open the Containers view first (containers_list)")
					}
					return "", nil, fmt.Errorf("%s is not a database container; database containers: %s", sanitizeDBText(want), strings.Join(names, ", "))
				})
			case "test":
				return aiDBTestConn(ctx, root, call, r.conn)
			}
			return "", errors.New("unknown op")
		},
	}
}

// aiDBTestConn logs in fresh (never a clone of the user's session) and
// pings, as db.test does.
func aiDBTestConn(ctx context.Context, root string, call aiIDECaller, c dbclient.Conn) (string, error) {
	snap := dbAITake(ctx, call, c.ID)
	snap.user = nil
	ctx, cancel := context.WithTimeout(ctx, loadDBConsoleSettings().timeout())
	defer cancel()
	start := time.Now()
	var sess dbclient.Session
	var err error
	switch {
	case c.Driver == "redis":
		sess, err = redisAIOpen(ctx, root, c, &snap, true)
	default:
		if _, cerr := dbAIConn(root, c.ID); cerr != nil {
			return "", errors.New("test works on SQL and Redis connections; use op connect for " + c.EngineLabel())
		}
		sess, err = dbAIOpen(ctx, root, c, &snap, true)
	}
	if err == nil {
		err = sess.Ping(ctx)
		_ = sess.Close()
	}
	if err != nil {
		return "", fmt.Errorf("the connection failed: %s (if a password is missing, db_connection op connect lets the user type it)", dbAIError(err, snap.password).Error())
	}
	return fmt.Sprintf("the connection works (%d ms)", time.Since(start).Milliseconds()), nil
}
