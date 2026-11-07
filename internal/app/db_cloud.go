package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Cloud and containers in the Databases view (DBX-49).
//
//   - A connection with a "kube" block connects through `kubectl
//     port-forward` to its pod; the forward lives as long as the session.
//   - Databases: Run New Database Container… runs PostgreSQL, MySQL,
//     MariaDB or Redis in Docker (port on 127.0.0.1 only, a generated
//     password in the job's env) after a Cancel-first card, and adds the
//     connection with the password in the secret store.
//   - Databases: Discover Cloud Databases… lists the instances the
//     installed aws / gcloud / az CLI can see; the one picked is added
//     like a new connection (password prompt, test, save).
//   - Databases: Use Cloud IAM Token Login… makes a saved connection of
//     yours ask the cloud CLI for a fresh token at each connect (a
//     ${cmd:} password), after a card that shows the command.

const (
	pickerKindDBRunEngine  pickerKindEnum  = 3914
	pickerKindDBCloud      pickerKindEnum  = 3916
	pickerKindDBCloudPick  pickerKindEnum  = 3917
	pickerKindDBIAM        pickerKindEnum  = 3918
	confirmKindDBRunEngine confirmKindEnum = 3915
	confirmKindDBIAM       confirmKindEnum = 3919
	dbCloudListTimeout                     = 60 * time.Second
)

// dbKubeForward starts the port-forward of c.Kube: the local port and the
// func that ends it (tests swap it for a fake).
var dbKubeForward = func(ctx context.Context, c dbclient.Conn) (int, func(), error) {
	cl := kube.Client{Binary: kube.Find(nil), Context: c.Kube.Context, Namespace: c.Kube.Namespace, X: servicesExec}
	fw, err := cl.PortForward(ctx, c.Kube.Pod, c.Kube.Port)
	if err != nil {
		return 0, nil, err
	}
	return fw.Local, fw.Stop, nil
}

// dbStopRedis is a Redis session that ends its port-forward on Close
// (it keeps the Redis methods).
type dbStopRedis struct {
	dbRedis
	stop func()
}

func (r dbStopRedis) Close() error {
	err := r.dbRedis.Close()
	r.stop()
	return err
}

// dbKubeOpen opens a session of c with open: through a new port-forward
// when c has a kube block.
func dbKubeOpen(ctx context.Context, c dbclient.Conn, open func(dbclient.Conn) (dbclient.Session, error)) (dbclient.Session, error) {
	if c.Kube == nil || c.Driver == "sqlite" {
		return open(c)
	}
	local, stop, err := dbKubeForward(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("kubectl port-forward to pod %s: %w", c.Kube.Pod, err)
	}
	sess, err := open(c.Forwarded(local))
	if err != nil {
		stop()
		return nil, err
	}
	if r, ok := sess.(dbRedis); ok {
		return dbStopRedis{dbRedis: r, stop: stop}, nil
	}
	return dbclient.OnClose(sess, stop), nil
}

// dbCloudState is the pending cloud / container step.
type dbCloudState struct {
	run       *dbRunContainer
	found     []dbclient.CloudInstance
	iamConn   string
	iamRef    string
	iamSSLSet bool
}

type dbRunContainer struct {
	plan     dbclient.DBContainer
	cmd      services.Command
	password string
}

type dbCloudFoundMsg struct {
	provider  string
	instances []dbclient.CloudInstance
	err       error
}

// dbCloudLookPath finds a CLI; dbCloudCapture runs a list command (tests
// swap both).
var (
	dbCloudLookPath = exec.LookPath
	dbCloudCapture  = func(ctx context.Context, argv []string) ([]byte, error) {
		out, stderr, code, err := servicesExec.Capture(ctx, "", argv...)
		if err == nil && code != 0 {
			err = fmt.Errorf("%s", errLine(stderr))
		}
		return []byte(out), err
	}
)

func dbRandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())[:2*n]
	}
	return hex.EncodeToString(b)
}

// dbFreePort is a free TCP port on 127.0.0.1.
func dbFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func (m *Model) dbCloud() *dbCloudState {
	m.ensureDatabasesLoaded()
	if m.databases.cloud == nil {
		m.databases.cloud = &dbCloudState{}
	}
	return m.databases.cloud
}

func (m *Model) dbOpenPicker(kind pickerKindEnum, title string, items []picker.Item) {
	m.picker = picker.NewItems(title, items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = kind
}

// databaseRunContainerStart asks for the engine.
func (m *Model) databaseRunContainerStart() tea.Cmd {
	items := make([]picker.Item, 0, len(dbclient.DBContainerEngines))
	for _, e := range dbclient.DBContainerEngines {
		items = append(items, picker.Item{ID: e.ID, Title: e.Title, Hint: e.Image})
	}
	m.dbOpenPicker(pickerKindDBRunEngine, " Run a new database container ", items)
	return nil
}

// dbContainerBinary is docker (or podman) for a new container.
func (m *Model) dbContainerBinary() string {
	if m.services != nil && m.services.containerEngine.Binary != "" {
		return m.services.containerEngine.Binary
	}
	for _, b := range []string{"docker", "podman"} {
		if p, err := dbCloudLookPath(b); err == nil {
			return p
		}
	}
	return ""
}

// databaseRunContainerCard shows the command before it runs.
func (m *Model) databaseRunContainerCard(engine string) tea.Cmd {
	bin := m.dbContainerBinary()
	if bin == "" {
		return m.pushToastDetail(toast.Info, "Docker is not installed", "Install Docker (or Podman) to run a database container.")
	}
	port, err := dbFreePort()
	if err != nil {
		return m.pushToastDetail(toast.Errr, "No free port", sanitizeDBText(err.Error()))
	}
	suffix := dbRandHex(2)
	for m.databases.duplicateConnectionID("container-" + engine + "-" + suffix) {
		suffix = dbRandHex(2)
	}
	password := dbRandHex(12)
	plan, err := dbclient.NewDBContainer(engine, port, password, suffix)
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot plan the container", sanitizeDBText(err.Error()))
	}
	e := containers.Engine{Binary: bin}
	cmd, err := e.RunOptionsCommand(containers.RunOptions{Image: plan.Image, Name: plan.Name, Detach: true, Ports: plan.Ports, Env: plan.Env, Command: plan.Command})
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot plan the container", sanitizeDBText(err.Error()))
	}
	cmd.Label = "run: " + plan.Name
	ai.RegisterSecret(password)
	m.dbCloud().run = &dbRunContainer{plan: plan, cmd: cmd, password: password}
	body := "Command: " + svcShellLine(cmd.Argv) + "\n\n" +
		"The port is open on 127.0.0.1 only. The password is generated, passed in the environment (not on the command line) and saved in the secret store.\n\n" +
		"Then the connection " + plan.Conn.ID + " is added to your connections. The first start can take a minute (the image is downloaded)."
	m.confirm = confirm.New("Run a new "+plan.Engine.Title+" container?", body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"}, {ID: "run", Title: "Run", Style: confirm.StylePrimary},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindDBRunEngine
	return nil
}

// databaseRunContainer runs the container job and saves the connection.
func (m *Model) databaseRunContainer() tea.Cmd {
	cs := m.dbCloud()
	r := cs.run
	cs.run = nil
	if r == nil {
		return nil
	}
	env := map[string]any{}
	if m.services != nil {
		for k, v := range m.services.contextEnv() {
			env[k] = v
		}
	}
	for k, v := range r.cmd.Env {
		env[k] = v
	}
	job := terminalJob{Label: r.cmd.Label, Cmdline: svcShellLine(r.cmd.Argv), Env: env, Service: containerTabPrefix + "run:" + r.plan.Name}
	if _, err := m.spawnTerminalJob(job); err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot run the container", sanitizeDBText(err.Error()))
	}
	s := m.databases
	s.pendingConn, s.pendingPassword, s.pendingPasswordRef = r.plan.Conn, r.password, dbTypedRef{}
	if cmd := m.databaseSavePending(false); cmd != nil {
		return cmd
	}
	s.databaseSelectPath(r.plan.Conn.ID)
	return m.pushToastDetail(toast.Info, "Container starting: "+r.plan.Name, "Connect with Enter on "+r.plan.Conn.ID+" when it is ready.")
}

// databaseCloudStart asks which cloud to look in (installed CLIs only).
func (m *Model) databaseCloudStart() tea.Cmd {
	var items []picker.Item
	for _, p := range dbclient.CloudProviders {
		if _, err := dbCloudLookPath(p.CLI); err == nil {
			items = append(items, picker.Item{ID: p.ID, Title: p.Title, Hint: "with " + p.CLI})
		}
	}
	if len(items) == 0 {
		return m.pushToastDetail(toast.Info, "No cloud CLI found", "Install and sign in to aws, gcloud or az; termocode lists the databases they can see.")
	}
	m.dbOpenPicker(pickerKindDBCloud, " Discover cloud databases ", items)
	return nil
}

// databaseCloudList runs the provider's list commands.
func databaseCloudList(provider string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbCloudListTimeout)
		defer cancel()
		var all []dbclient.CloudInstance
		for _, argv := range dbclient.CloudListCommands(provider) {
			out, err := dbCloudCapture(ctx, argv)
			if err != nil {
				return dbCloudFoundMsg{provider: provider, err: fmt.Errorf("%s: %w", strings.Join(argv[:min(3, len(argv))], " "), err)}
			}
			found, err := dbclient.ParseCloudInstances(provider, out)
			if err != nil {
				return dbCloudFoundMsg{provider: provider, err: err}
			}
			all = append(all, found...)
		}
		return dbCloudFoundMsg{provider: provider, instances: all}
	}
}

func (m *Model) onDBCloudFound(msg dbCloudFoundMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Cloud discovery failed", dbclient.Redact(sanitizeDBText(msg.err.Error())))
	}
	if len(msg.instances) == 0 {
		return m.pushToastDetail(toast.Info, "No PostgreSQL or MySQL instances found", "The CLI is signed in to an account or project with none.")
	}
	cs := m.dbCloud()
	cs.found = msg.instances
	items := make([]picker.Item, 0, len(msg.instances))
	for i, inst := range msg.instances {
		hint := inst.Conn.Host + " · " + inst.Detail
		if m.databases.duplicateConnectionID(inst.Conn.ID) {
			hint = "already added · " + hint
		}
		items = append(items, picker.Item{ID: fmt.Sprint(i), Title: inst.Conn.Name, Hint: sanitizeDBText(hint)})
	}
	m.dbOpenPicker(pickerKindDBCloudPick, " Add a cloud database ", items)
	return nil
}

// databaseCloudAdd starts the add flow for a found instance: password
// (or a ${cmd:} token), test, save.
func (m *Model) databaseCloudAdd(id string) tea.Cmd {
	cs := m.dbCloud()
	var inst dbclient.CloudInstance
	ok := false
	for i, x := range cs.found {
		if fmt.Sprint(i) == id {
			inst, ok = x, true
		}
	}
	cs.found = nil
	if !ok {
		return nil
	}
	s := m.databases
	c := inst.Conn
	for base, n := c.ID, 2; s.duplicateConnectionID(c.ID); n++ {
		c.ID = fmt.Sprintf("%s-%d", base, n)
	}
	s.pendingConn, s.pendingPassword, s.addFromURL = c, "", false
	target := c.Host
	if c.User != "" {
		target = c.User + "@" + target
	}
	return m.databaseAddPrompt(promptKindDBPassword, "Add "+sanitizeDBText(c.Name)+" to Databases",
		"Password for "+sanitizeDBText(target)+" (blank if none; IAM: Use Cloud IAM Token Login after saving):", "", true)
}

// databaseIAMStart asks which cloud signs the token of the connection
// under the cursor.
func (m *Model) databaseIAMStart() tea.Cmd {
	s := m.databases
	r, ok := m.databaseCurrentRow()
	if s == nil || !ok {
		return tea.Batch(m.revealDatabasesView(false), m.pushToastDetail(toast.Info, "Select a connection first", ""))
	}
	c, ok := s.connection(r.conn)
	if !ok {
		return m.pushToastDetail(toast.Info, "Select a connection first", "")
	}
	if c.Source != dbclient.SourceUser {
		return m.pushToastDetail(toast.Info, "Only your own connections can use IAM login", "Project files cannot run commands. Copy the connection to your own list first.")
	}
	if c.Driver != "postgres" && c.Driver != "mysql" {
		return m.pushToastDetail(toast.Info, "IAM login is for PostgreSQL and MySQL", "")
	}
	m.dbCloud().iamConn = c.ID
	var items []picker.Item
	for _, p := range dbclient.CloudProviders {
		items = append(items, picker.Item{ID: p.ID, Title: p.Title, Hint: "token from " + p.CLI})
	}
	m.dbOpenPicker(pickerKindDBIAM, " IAM token login for "+sanitizeDBText(dbConnName(c))+" ", items)
	return nil
}

// databaseIAMCard shows the command that will run at each connect.
func (m *Model) databaseIAMCard(provider string) tea.Cmd {
	cs := m.dbCloud()
	c, ok := m.databases.connection(cs.iamConn)
	if !ok {
		return nil
	}
	ref, err := dbclient.IAMTokenRef(provider, c)
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot use IAM login", sanitizeDBText(err.Error()))
	}
	cs.iamRef = ref
	cs.iamSSLSet = c.SSL == nil
	line := strings.TrimSuffix(strings.TrimPrefix(ref, "${cmd:"), "}")
	body := "At each connect termocode runs:\n\n  " + line + "\n\nand logs in with the token it prints. The CLI must be signed in."
	if cs.iamSSLSet {
		body += " SSL is set to require (IAM login needs it)."
	}
	m.confirm = confirm.New("Use IAM token login for "+sanitizeDBText(dbConnName(c))+"?", body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"}, {ID: "use", Title: "Use IAM Login", Style: confirm.StylePrimary},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindDBIAM
	return nil
}

// databaseIAMApply saves the ${cmd:} password in the user config.
func (m *Model) databaseIAMApply() tea.Cmd {
	cs := m.dbCloud()
	id, ref, setSSL := cs.iamConn, cs.iamRef, cs.iamSSLSet
	cs.iamConn, cs.iamRef = "", ""
	s := m.databases
	if id == "" || ref == "" {
		return nil
	}
	change := func(c *dbclient.Conn) {
		c.Password = ref
		if setSSL && c.SSL == nil {
			c.SSL = &dbclient.SSLConfig{Mode: dbclient.SSLRequire}
		}
	}
	users := append([]dbclient.Conn(nil), s.userConns...)
	found := false
	for i := range users {
		if users[i].ID == id {
			change(&users[i])
			found = true
		}
	}
	if !found {
		return nil
	}
	dir, err := configDir()
	if err == nil {
		err = writeDBConfig(filepath.Join(dir, "db", "connections.json"), users)
	}
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot save the connection", sanitizeDBText(err.Error()))
	}
	s.userConns = users
	for i := range s.conns {
		if s.conns[i].ID == id {
			change(&s.conns[i])
			n := s.nodes[id]
			n.detail = dbConnDetail(s.conns[i])
			s.nodes[id] = n
		}
	}
	s.touch()
	return m.pushToastDetail(toast.Info, "IAM token login on", "Connect again to log in with a fresh token.")
}

// databaseCloudPicker routes the pickers of this file.
func (m *Model) databaseCloudPicker(id string) (tea.Cmd, bool) {
	switch m.pickerKind {
	case pickerKindDBRunEngine:
		return m.databaseRunContainerCard(id), true
	case pickerKindDBCloud:
		return tea.Batch(databaseCloudList(id), m.pushToast(toast.Info, "Asking the cloud CLI…")), true
	case pickerKindDBCloudPick:
		return m.databaseCloudAdd(id), true
	case pickerKindDBIAM:
		return m.databaseIAMCard(id), true
	}
	return nil, false
}

// databaseCloudConfirm routes the cards of this file.
func (m *Model) databaseCloudConfirm(id string) (tea.Cmd, bool) {
	switch m.confirmKind {
	case confirmKindDBRunEngine:
		if id != "run" {
			m.dbCloud().run = nil
			return nil, true
		}
		return m.databaseRunContainer(), true
	case confirmKindDBIAM:
		if id != "use" {
			m.dbCloud().iamConn, m.dbCloud().iamRef = "", ""
			return nil, true
		}
		return m.databaseIAMApply(), true
	}
	return nil, false
}
