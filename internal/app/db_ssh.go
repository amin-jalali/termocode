package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// SSH tunnels and SSL (DB-11). The tunnel is opened with the connection
// and closed with it (disconnect, hide, refresh, quit); per-database
// sessions and the AI's sessions dial through the open tunnel, never a
// new one, and the AI never sees an SSH secret or trusts a host key. SSH
// passwords and key passphrases live in the secret store as
// db.<id>.ssh / db.<id>.sshkey; a connection being added keeps them in
// memory until it is saved.

const (
	pickerKindDBSSHAuth pickerKindEnum = 1500 + iota
	pickerKindDBSSLMode
)

const (
	promptKindDBSSHHost promptKindEnum = 1500 + iota
	promptKindDBSSHPort
	promptKindDBSSHUser
	promptKindDBSSHKeyFile
	promptKindDBSSHSecret
	promptKindDBSSLCA
	promptKindDBSSLCert
	promptKindDBSSLKey
)

const confirmKindDBTrustHost confirmKindEnum = 1500

// dbSSHSecrets are the tunnel's secrets for one connect.
type dbSSHSecrets struct{ password, passphrase string }

// dbSSHHome is the home the tunnel reads ~/.ssh from (tests: a temp dir).
var dbSSHHome = func() string { h, _ := os.UserHomeDir(); return h }

// dbTunnelDial dials the SSH server (nil: TCP; tests: an in-process
// server).
var dbTunnelDial dbclient.DialFunc

// After a host key is trusted or a secret is typed, the action is run
// again: connect a row, test a row, or test the connection being added.
const (
	dbThenConnect = "connect"
	dbThenTest    = "test"
	dbThenPending = "pending"
)

type dbHostAsk struct {
	err  *dbclient.UnknownHostKeyError
	conn string
	then string
}

type dbSecretAsk struct {
	conn, kind, then string // kind: "password" | "passphrase"
}

// dbSSHEdit is the Add SSH Tunnel / SSL Settings prompt chain.
type dbSSHEdit struct {
	conn    string // "" = the connection being added
	ssh     dbclient.SSHConfig
	ssl     dbclient.SSLConfig
	secret  string // typed password / passphrase (kept until saved)
	defPort int
	defUser string
}

func (s *dbState) tunnel(id string) *dbclient.Tunnel {
	if s == nil || s.tunnels == nil {
		return nil
	}
	return s.tunnels[id]
}

func (s *dbState) setTunnel(id string, t *dbclient.Tunnel) {
	if s.tunnels == nil {
		s.tunnels = map[string]*dbclient.Tunnel{}
	}
	if old := s.tunnels[id]; old != nil && old != t {
		_ = old.Close()
	}
	if t == nil {
		delete(s.tunnels, id)
		return
	}
	s.tunnels[id] = t
}

// closeTunnel closes the tunnel of id (disconnect, reconnect).
func (s *dbState) closeTunnel(id string) { s.setTunnel(id, nil) }

// closeTunnels closes every tunnel except keep.
func (s *dbState) closeTunnels(keep map[string]bool) {
	for id := range s.tunnels {
		if !keep[id] {
			s.closeTunnel(id)
		}
	}
}

// dbDialFor is the dialer of a new session of c: the open tunnel, or an
// error when c needs one and it is down.
func (s *dbState) dbDialFor(c dbclient.Conn) (dbclient.DialFunc, error) {
	if c.SSH == nil {
		return nil, nil
	}
	t := s.tunnel(c.ID)
	if !t.Alive() {
		return nil, dbclient.ErrTunnelClosed
	}
	return t.DialContext, nil
}

// dbResolveConnPaths gives c absolute key, known_hosts and certificate
// paths (~ is the home, a relative path is in the workspace). The SSH and
// SSL objects are copied: the caller's connection is not changed.
func dbResolveConnPaths(c dbclient.Conn, root, home string) dbclient.Conn {
	abs := func(p string) string {
		switch {
		case p == "":
			return ""
		case p == "~":
			return home
		case strings.HasPrefix(p, "~/"):
			return filepath.Join(home, p[2:])
		case filepath.IsAbs(p):
			return p
		}
		return filepath.Join(root, p)
	}
	if c.SSH != nil {
		ssh := *c.SSH
		ssh.KeyFile, ssh.KnownHosts = abs(ssh.KeyFile), abs(ssh.KnownHosts)
		c.SSH = &ssh
	}
	if c.SSL != nil {
		ssl := *c.SSL
		ssl.CA, ssl.Cert, ssl.Key = abs(ssl.CA), abs(ssl.Cert), abs(ssl.Key)
		c.SSL = &ssl
	}
	return c
}

// dbOpenTunnel opens c's tunnel (c.SSH must be set).
func dbOpenTunnel(ctx context.Context, c dbclient.Conn, sec dbSSHSecrets) (*dbclient.Tunnel, error) {
	return dbclient.OpenTunnel(ctx, *c.SSH, dbclient.TunnelOptions{Home: dbSSHHome(), Password: sec.password, Passphrase: sec.passphrase, Dial: dbTunnelDial})
}

func dbSSHSecretName(id, kind string) string {
	if kind == "password" {
		return "db." + id + ".ssh"
	}
	return "db." + id + ".sshkey"
}

func dbSSHAuth(c dbclient.Conn) string {
	if c.SSH == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(c.SSH.Auth))
}

// databaseSSHSecrets reads the tunnel secrets of c (pending: the add
// flow's in-memory ones). Missing secrets are not an error: the tunnel
// asks for them.
func (m *Model) databaseSSHSecrets(c dbclient.Conn, pending bool) dbSSHSecrets {
	var sec dbSSHSecrets
	if c.SSH == nil || m.databases == nil {
		return sec
	}
	if pending {
		sec = m.databases.pendingSSH
	} else if store := databaseSecretStore(); store != nil {
		switch dbSSHAuth(c) {
		case dbclient.SSHAuthPassword:
			sec.password, _, _ = store.Get(dbSSHSecretName(c.ID, "password"))
		case dbclient.SSHAuthAgent:
		default:
			sec.passphrase, _, _ = store.Get(dbSSHSecretName(c.ID, "passphrase"))
		}
	}
	if sec.password != "" {
		ai.RegisterSecret(sec.password)
	}
	if sec.passphrase != "" {
		ai.RegisterSecret(sec.passphrase)
	}
	return sec
}

// databaseTunnelError handles the errors that need the user: an unknown
// host key (the trust card) and a missing or wrong SSH secret (a masked
// prompt). ok=false: an ordinary error.
func (m *Model) databaseTunnelError(err error, conn, then string) (tea.Cmd, bool) {
	var unknown *dbclient.UnknownHostKeyError
	if errors.As(err, &unknown) {
		m.databases.hostAsk = &dbHostAsk{err: unknown, conn: conn, then: then}
		host := sanitizeDBText(unknown.Host)
		if h, p, e := netSplit(unknown.Host); e == nil && p == "22" {
			host = sanitizeDBText(h)
		}
		title, body := "Unknown host "+host, "This SSH server is not in your known_hosts file yet. Trust it only if this fingerprint matches the one your admin gave you."
		if unknown.Jump != "" {
			title = "Unknown jump host " + host
			body = "This jump host is on the way to the SSH server and is not in your known_hosts file yet. Trust it only if this fingerprint matches the one your admin gave you."
		}
		m.confirm = confirm.New(title, body,
			[]confirm.Button{
				{ID: "cancel", Title: "Cancel"},
				{ID: "trust", Title: "Trust and save to known_hosts", Style: confirm.StylePrimary},
			}).WithCode(sanitizeDBText(unknown.Fingerprint), sanitizeDBText(dbKeyTypeName(unknown.KeyType))+" key · saved to "+sanitizeDBText(dbHomeShort(unknown.KnownHosts)))
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindDBTrustHost
		return nil, true
	}
	var need *dbclient.SSHSecretError
	if errors.As(err, &need) {
		m.databases.secretAsk = &dbSecretAsk{conn: conn, kind: need.Kind, then: then}
		title, label := "SSH password", "Password for the SSH server:"
		if need.Kind == "passphrase" {
			title, label = "SSH key passphrase", "Passphrase for "+sanitizeDBText(filepath.Base(need.KeyFile))+":"
		}
		if need.Wrong {
			title += " · not accepted, try again"
		}
		return m.databaseAddPrompt(promptKindDBSSHSecret, title, label, "", true), true
	}
	return nil, false
}

func dbKeyTypeName(t string) string {
	switch t {
	case "ssh-ed25519":
		return "ED25519"
	case "ssh-rsa":
		return "RSA"
	case "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521":
		return "ECDSA"
	}
	return t
}

func dbHomeShort(p string) string {
	if h := dbSSHHome(); h != "" {
		if rel, err := filepath.Rel(h, p); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return p
}

func netSplit(hostport string) (string, string, error) {
	i := strings.LastIndex(hostport, ":")
	if i < 0 {
		return hostport, "", errors.New("no port")
	}
	return strings.Trim(hostport[:i], "[]"), hostport[i+1:], nil
}

// databaseSSHConfirm is the trust card's answer.
func (m *Model) databaseSSHConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindDBTrustHost {
		return nil, false
	}
	s := m.databases
	if s == nil || s.hostAsk == nil {
		return nil, true
	}
	ask := s.hostAsk
	s.hostAsk = nil
	if id != "trust" {
		if n, ok := s.nodes[ask.conn]; ok && ask.then == dbThenConnect {
			n.problem = "SSH host not trusted · Enter to check its key again"
			s.nodes[ask.conn] = n
			s.touch()
		}
		return nil, true
	}
	if err := dbclient.TrustHostKey(ask.err.KnownHosts, ask.err.Host, ask.err.Key); err != nil {
		return m.pushToastDetail(toast.Errr, "Could not save the host key", sanitizeDBText(err.Error())), true
	}
	return tea.Batch(m.pushToastDetail(toast.Info, "Host key saved", sanitizeDBText(ask.err.Fingerprint)), m.databaseRetry(ask.conn, ask.then)), true
}

func (m *Model) databaseRetry(conn, then string) tea.Cmd {
	switch then {
	case dbThenConnect:
		return m.databaseConnect(conn)
	case dbThenTest:
		return m.databaseTestConnection(conn)
	case dbThenPending:
		return m.databaseRetestPending()
	}
	return nil
}

// databaseSSHSecretTyped stores the typed SSH password / passphrase and
// runs the action again.
func (m *Model) databaseSSHSecretTyped(value string) tea.Cmd {
	s := m.databases
	if s == nil || s.secretAsk == nil {
		return nil
	}
	ask := s.secretAsk
	s.secretAsk = nil
	if value == "" {
		return m.pushToastDetail(toast.Warn, "Nothing typed", "The SSH tunnel was not opened.")
	}
	ai.RegisterSecret(value)
	if ask.then == dbThenPending {
		if ask.kind == "password" {
			s.pendingSSH.password = value
		} else {
			s.pendingSSH.passphrase = value
		}
		return m.databaseRetestPending()
	}
	store := databaseSecretStore()
	if store == nil {
		return m.pushToastDetail(toast.Errr, "Secret store unavailable", "Unlock or configure the secret store first.")
	}
	if err := store.Set(dbSSHSecretName(ask.conn, ask.kind), value); err != nil {
		return m.pushToastDetail(toast.Errr, "Could not save the SSH secret", sanitizeDBText(err.Error()))
	}
	return m.databaseRetry(ask.conn, ask.then)
}

// dbTunnelProblem is the row text of a tunnel error.
func dbTunnelProblem(err error) string {
	var changed *dbclient.HostKeyChangedError
	if errors.As(err, &changed) {
		return "✘ " + sanitizeDBText(changed.Error())
	}
	return ""
}

// ---- Add SSH Tunnel… / SSL Settings… ----

// databaseSSHTarget is the connection the SSH / SSL flow edits: a saved
// row (id) or the connection being added ("").
func (m *Model) databaseSSHTarget(id string) (dbclient.Conn, bool) {
	s := m.databases
	if s == nil {
		return dbclient.Conn{}, false
	}
	if id == "" {
		return s.pendingConn, s.pendingConn.ID != ""
	}
	return s.connection(id)
}

func (m *Model) databaseSSHStart(id string) tea.Cmd {
	c, ok := m.databaseSSHTarget(id)
	if !ok {
		return m.pushToastDetail(toast.Info, "Select a connection", "Pick a connection row first.")
	}
	if c.Driver == "sqlite" {
		return m.pushToastDetail(toast.Info, "No tunnel for SQLite", "A SQLite database is a local file.")
	}
	e := &dbSSHEdit{conn: id}
	if c.SSH != nil {
		e.ssh = *c.SSH
	}
	m.databases.sshEdit = e
	m.databases.secretAsk = nil
	return m.databaseAddPrompt(promptKindDBSSHHost, "SSH tunnel · "+sanitizeDBText(dbConnName(c))+" · 1/4", "SSH server (host or ~/.ssh/config alias):", e.ssh.Host, false)
}

func (m *Model) databaseSSLStart(id string) tea.Cmd {
	c, ok := m.databaseSSHTarget(id)
	if !ok {
		return m.pushToastDetail(toast.Info, "Select a connection", "Pick a connection row first.")
	}
	if c.Driver == "sqlite" {
		return m.pushToastDetail(toast.Info, "No SSL for SQLite", "A SQLite database is a local file.")
	}
	e := &dbSSHEdit{conn: id}
	if c.SSL != nil {
		e.ssl = *c.SSL
	}
	m.databases.sshEdit = e
	m.databases.secretAsk = nil
	cur, _ := dbclient.EffectiveSSLMode(c.SSL)
	items := []picker.Item{
		{ID: dbclient.SSLDisable, Title: "Off", Hint: "no encryption"},
		{ID: dbclient.SSLRequire, Title: "Require", Hint: "encrypted, server not checked"},
		{ID: dbclient.SSLVerifyCA, Title: "Verify CA", Hint: "the certificate must come from the CA"},
		{ID: dbclient.SSLVerifyFull, Title: "Verify CA and host name", Hint: "safest"},
	}
	m.picker = picker.NewItems(" SSL · "+sanitizeDBText(dbConnName(c))+" ", items)
	if cur != "" {
		m.picker.SetCursorID(cur)
	}
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindDBSSLMode
	return nil
}

// databaseSSHPicker routes the auth and SSL-mode pickers.
func (m *Model) databaseSSHPicker(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBSSHAuth && m.pickerKind != pickerKindDBSSLMode {
		return nil, false
	}
	s := m.databases
	if s == nil || s.sshEdit == nil {
		return nil, true
	}
	e := s.sshEdit
	if m.pickerKind == pickerKindDBSSLMode {
		e.ssl.Mode = id
		if id == dbclient.SSLDisable {
			e.ssl = dbclient.SSLConfig{Mode: id}
			return m.databaseSSHFinish(), true
		}
		cmd := m.databaseAddPrompt(promptKindDBSSLCA, "SSL · CA certificate (optional)", "CA file (empty: the system's CAs):", e.ssl.CA, false)
		m.prompt.SetAllowEmpty(true)
		return cmd, true
	}
	e.ssh.Auth = id
	switch id {
	case dbclient.SSHAuthKey:
		def := e.ssh.KeyFile
		if def == "" {
			if t, err := dbclient.ResolveSSH(dbclient.SSHConfig{Host: e.ssh.Host, Auth: "key"}, dbSSHHome()); err == nil {
				for _, f := range t.KeyFiles {
					if _, err := os.Stat(f); err == nil {
						def = dbHomeShort(f)
						break
					}
				}
			}
		}
		if def == "" {
			def = "~/.ssh/id_ed25519"
		}
		return m.databaseAddPrompt(promptKindDBSSHKeyFile, "SSH tunnel · key file", "Private key file:", def, false), true
	case dbclient.SSHAuthPassword:
		cmd := m.databaseAddPrompt(promptKindDBSSHSecret, "SSH tunnel · password", "SSH password (kept in the secret store):", "", true)
		s.secretAsk = nil
		return cmd, true
	default:
		e.ssh.KeyFile = ""
		return m.databaseSSHFinish(), true
	}
}

// databaseSSHPrompt routes the SSH / SSL prompts. ok=false: not ours.
func (m *Model) databaseSSHPrompt(kind promptKindEnum, value string) (tea.Cmd, bool) {
	if kind < promptKindDBSSHHost || kind > promptKindDBSSLKey {
		return nil, false
	}
	s := m.databases
	if s == nil {
		return nil, true
	}
	if kind == promptKindDBSSHSecret && s.secretAsk != nil {
		return m.databaseSSHSecretTyped(value), true
	}
	e := s.sshEdit
	if e == nil {
		return nil, true
	}
	value = strings.TrimSpace(value)
	switch kind {
	case promptKindDBSSHHost:
		if value == "" {
			return m.pushToastDetail(toast.Warn, "SSH server is needed", "Enter a host name, an address or a ~/.ssh/config alias."), true
		}
		e.ssh.Host = value
		t, _ := dbclient.ResolveSSH(dbclient.SSHConfig{Host: value}, dbSSHHome())
		e.defPort, e.defUser = t.Port, t.User
		port := e.ssh.Port
		if port == 0 {
			port = e.defPort
		}
		if port == 0 {
			port = 22
		}
		return m.databaseAddPrompt(promptKindDBSSHPort, "SSH tunnel · 2/4", "SSH port:", strconv.Itoa(port), false), true
	case promptKindDBSSHPort:
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return m.pushToastDetail(toast.Warn, "That port is not valid", "Enter a number from 1 to 65535."), true
		}
		e.ssh.Port = port
		if port == e.defPort || (e.defPort == 0 && port == 22) {
			e.ssh.Port = 0 // the default (or ~/.ssh/config) applies
		}
		user := e.ssh.User
		if user == "" {
			user = e.defUser
		}
		return m.databaseAddPrompt(promptKindDBSSHUser, "SSH tunnel · 3/4", "SSH user:", user, false), true
	case promptKindDBSSHUser:
		if value == "" {
			return m.pushToastDetail(toast.Warn, "SSH user is needed", "Enter the user to log in as."), true
		}
		e.ssh.User = value
		if value == e.defUser {
			e.ssh.User = ""
		}
		items := []picker.Item{
			{ID: dbclient.SSHAuthAgent, Title: "ssh-agent", Hint: "keys loaded in your agent"},
			{ID: dbclient.SSHAuthKey, Title: "Key file", Hint: "passphrase in the secret store"},
			{ID: dbclient.SSHAuthPassword, Title: "Password", Hint: "kept in the secret store"},
		}
		m.picker = picker.NewItems(" SSH tunnel · 4/4 · log in with ", items)
		if e.ssh.Auth != "" {
			m.picker.SetCursorID(e.ssh.Auth)
		}
		m.picker.SetSize(m.w, m.h)
		m.pickerOpen = true
		m.pickerKind = pickerKindDBSSHAuth
		return nil, true
	case promptKindDBSSHKeyFile:
		if value == "" {
			return m.pushToastDetail(toast.Warn, "Key file is needed", "Enter the path of your private key."), true
		}
		e.ssh.KeyFile = value
		cmd := m.databaseAddPrompt(promptKindDBSSHSecret, "SSH tunnel · key passphrase", "Passphrase (leave blank if the key has none):", "", true)
		m.prompt.SetAllowEmpty(true)
		return cmd, true
	case promptKindDBSSHSecret:
		e.secret = value
		return m.databaseSSHFinish(), true
	case promptKindDBSSLCA:
		e.ssl.CA = value
		cmd := m.databaseAddPrompt(promptKindDBSSLCert, "SSL · client certificate (optional)", "Client certificate file (empty: none):", e.ssl.Cert, false)
		m.prompt.SetAllowEmpty(true)
		return cmd, true
	case promptKindDBSSLCert:
		e.ssl.Cert = value
		if value == "" {
			e.ssl.Key = ""
			return m.databaseSSHFinish(), true
		}
		return m.databaseAddPrompt(promptKindDBSSLKey, "SSL · client key", "Client key file:", e.ssl.Key, false), true
	case promptKindDBSSLKey:
		if value == "" {
			return m.pushToastDetail(toast.Warn, "Client key is needed", "A client certificate needs its key file."), true
		}
		e.ssl.Key = value
		return m.databaseSSHFinish(), true
	}
	return nil, true
}

// databaseSSHFinish saves the edited ssh / ssl object and tests the
// connection through it.
func (m *Model) databaseSSHFinish() tea.Cmd {
	s := m.databases
	e := s.sshEdit
	s.sshEdit = nil
	if e == nil {
		return nil
	}
	isSSH := e.ssh.Host != ""
	apply := func(c *dbclient.Conn) {
		if isSSH {
			ssh := e.ssh
			c.SSH = &ssh
		} else {
			ssl := e.ssl
			c.SSL = &ssl
		}
	}
	if e.conn == "" {
		apply(&s.pendingConn)
		if isSSH {
			s.pendingSSH = dbSSHSecrets{}
			if e.ssh.Auth == dbclient.SSHAuthPassword {
				s.pendingSSH.password = e.secret
			} else if e.ssh.Auth == dbclient.SSHAuthKey {
				s.pendingSSH.passphrase = e.secret
			}
		}
		return m.databaseRetestPending()
	}
	if isSSH && e.secret != "" {
		ai.RegisterSecret(e.secret)
		store := databaseSecretStore()
		if store == nil {
			return m.pushToastDetail(toast.Errr, "Secret store unavailable", "Unlock or configure the secret store first.")
		}
		kind := "passphrase"
		if e.ssh.Auth == dbclient.SSHAuthPassword {
			kind = "password"
		}
		if err := store.Set(dbSSHSecretName(e.conn, kind), e.secret); err != nil {
			return m.pushToastDetail(toast.Errr, "Could not save the SSH secret", sanitizeDBText(err.Error()))
		}
	}
	if err := m.databaseUpdateConn(e.conn, apply); err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot save connection", sanitizeDBText(err.Error()))
	}
	// The next connect uses the new settings. A connection holding an
	// open transaction stays as it is until the user ends it.
	if len(s.txKeys(e.conn)) > 0 {
		return tea.Batch(m.pushToastDetail(toast.Info, "Saved · reconnect to use it", "Commit or roll back first; the open transaction keeps the old connection."), m.databaseTestConnection(e.conn))
	}
	_ = m.databaseDisconnectNow(e.conn)
	return m.databaseTestConnection(e.conn)
}

// databaseUpdateConn changes a saved connection and writes its file.
func (m *Model) databaseUpdateConn(id string, edit func(*dbclient.Conn)) error {
	s := m.databases
	c, ok := s.connection(id)
	if !ok {
		return errors.New("connection not found")
	}
	list := &s.userConns
	path := ""
	if c.Source == dbclient.SourceProject {
		list = &s.projectConns
		path = filepath.Join(s.root, ".termocode", "databases.json")
	} else {
		dir, err := configDir()
		if err != nil {
			return err
		}
		path = filepath.Join(dir, "db", "connections.json")
	}
	all := append([]dbclient.Conn(nil), (*list)...)
	found := false
	for i := range all {
		if all[i].ID == id {
			edit(&all[i])
			found = true
		}
	}
	if !found {
		return errors.New("connection not found in its file")
	}
	if err := writeDBConfig(path, all); err != nil {
		return err
	}
	*list = all
	for i := range s.conns {
		if s.conns[i].ID == id {
			edit(&s.conns[i])
			n := s.nodes[id]
			n.detail = dbConnDetail(s.conns[i])
			s.nodes[id] = n
		}
	}
	s.touch()
	return nil
}

// databaseTestConnection tests a saved connection (through its tunnel and
// TLS) without touching its open session.
func (m *Model) databaseTestConnection(id string) tea.Cmd { return m.databaseOpen(id, true) }

// dbTestTimeout bounds one test (SSH handshake + database login).
const dbTestTimeout = 15 * time.Second
