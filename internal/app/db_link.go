package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The Services / Containers link (DB-12): a database container or compose
// service opens the Databases view ("Open in Databases"), and a saved
// connection whose container is stopped says so and can start it ("S").
// Everything reads the snapshot the Services view already holds; nothing
// polls, and no password is read from the environment.

// dbRuntimeTarget is a database container or compose service.
type dbRuntimeTarget struct {
	driver   string // postgres, mysql or redis
	name     string // display name (sanitized)
	port     int    // the published port on this machine
	user     string
	database string
	compose  string // compose service name ("" for a plain container)
	cont     containers.Container
	isCont   bool
	running  bool
	starting bool
}

// dbLocalHost reports a host name that means this machine.
func dbLocalHost(h string) bool {
	switch strings.ToLower(strings.TrimSpace(h)) {
	case "localhost", "127.0.0.1", "::1", "[::1]", "0.0.0.0", "":
		return true
	}
	return false
}

func dbPublishedComposePort(svc services.Service, st services.Status) int {
	for _, p := range st.Ports {
		if p.Published > 0 {
			return p.Published
		}
	}
	for _, p := range svc.Ports {
		if p.Published > 0 {
			return p.Published
		}
	}
	return 0
}

func dbPublishedContainerPort(c containers.Container) int {
	for _, p := range c.Ports {
		if p.PublicPort > 0 {
			return p.PublicPort
		}
	}
	return 0
}

// dbContainerStopped reports a container that is not running and not
// merely paused or restarting.
func dbContainerStopped(c containers.Container) bool {
	switch strings.ToLower(c.State) {
	case "exited", "created", "dead":
		return true
	}
	return false
}

// dbPrefill picks the user and database for a driver from the public
// environment values; the image defaults fill the rest.
func dbPrefill(driver string, env map[string]string) (user, database string) {
	clean := func(v string) string { return sanitizeDBText(strings.TrimSpace(v)) }
	switch driver {
	case "postgres":
		user, database = clean(env["POSTGRES_USER"]), clean(env["POSTGRES_DB"])
		if user == "" {
			user = "postgres"
		}
		if database == "" {
			database = user
		}
	case "mysql":
		user, database = clean(env["MYSQL_USER"]), clean(env["MYSQL_DATABASE"])
		if user == "" {
			user = clean(env["MARIADB_USER"])
		}
		if database == "" {
			database = clean(env["MARIADB_DATABASE"])
		}
		if user == "" {
			user = "root"
		}
		if database == "" {
			database = "mysql"
		}
	}
	return user, database
}

func dbTargetFromCompose(st *servicesState, svc services.Service) (dbRuntimeTarget, bool) {
	driver := dbDriverForImage(svc.Image)
	if driver == "" {
		return dbRuntimeTarget{}, false
	}
	status := st.statusOf(svcSecCompose, svc.Name)
	port := dbPublishedComposePort(svc, status)
	if port == 0 {
		port = defaultDBPort(driver)
	}
	starting := status.State == services.Starting || status.State == services.Pulling
	stopped := status.State == services.NotCreated || status.State == services.Stopped || status.State == services.Exited
	t := dbRuntimeTarget{driver: driver, name: sanitizeDBText(svc.Name), port: port, compose: svc.Name,
		running: !stopped && !starting, starting: starting}
	t.user, t.database = dbPrefill(driver, svc.PublicEnv)
	return t, true
}

func dbTargetFromContainer(st *servicesState, c containers.Container) (dbRuntimeTarget, bool) {
	driver := dbDriverForImage(c.Image)
	if driver == "" {
		return dbRuntimeTarget{}, false
	}
	port := dbPublishedContainerPort(c)
	if port == 0 {
		port = defaultDBPort(driver)
	}
	name := c.Image
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}
	t := dbRuntimeTarget{driver: driver, name: sanitizeDBText(name), port: port, cont: c, isCont: true,
		running: !dbContainerStopped(c), starting: st.busy[containerBusyPrefix+c.ID] == services.Starting}
	// A container of a compose project: read the user and database names
	// from its compose service when this workspace declares it.
	if svcName := c.Labels["com.docker.compose.service"]; svcName != "" && (st.project == "" || c.Project == "" || c.Project == st.project) {
		if svc, ok := st.findService(svcSecCompose, svcName); ok {
			t.user, t.database = dbPrefill(driver, svc.PublicEnv)
			return t, true
		}
	}
	t.user, t.database = dbPrefill(driver, nil)
	return t, true
}

// dbTargetFromRow is the database container or service of a Services /
// Containers row.
func (m *Model) dbTargetFromRow(r svcRow) (dbRuntimeTarget, bool) {
	st := m.services
	if st == nil {
		return dbRuntimeTarget{}, false
	}
	switch {
	case r.kind == svcRowService && r.section == svcSecCompose:
		if svc, ok := st.findService(svcSecCompose, r.name); ok {
			return dbTargetFromCompose(st, svc)
		}
	case r.kind == svcRowHostItem && r.host == hostContainer && r.idx >= 0 && r.idx < len(st.containerRows):
		return dbTargetFromContainer(st, st.containerRows[r.idx])
	}
	return dbRuntimeTarget{}, false
}

// dbRuntimeTargets lists every database container and compose service of
// the snapshot.
func dbRuntimeTargets(st *servicesState) []dbRuntimeTarget {
	if st == nil {
		return nil
	}
	var out []dbRuntimeTarget
	for _, svc := range st.compose {
		if t, ok := dbTargetFromCompose(st, svc); ok {
			out = append(out, t)
		}
	}
	for _, c := range st.containerRows {
		if t, ok := dbTargetFromContainer(st, c); ok {
			out = append(out, t)
		}
	}
	return out
}

// dbTargetForConn is the container or service behind a saved connection:
// a local host and the same published port and driver. A running match
// wins; none is returned when the snapshot has not loaded.
func dbTargetForConn(st *servicesState, c dbclient.Conn) (dbRuntimeTarget, bool) {
	if st == nil || c.Driver == "sqlite" || !dbLocalHost(c.Host) || c.Port == 0 || !(st.loaded || st.containerDetected) {
		return dbRuntimeTarget{}, false
	}
	driver := c.Driver
	if driver == "mariadb" {
		driver = "mysql"
	}
	var found dbRuntimeTarget
	have := false
	for _, t := range dbRuntimeTargets(st) {
		if t.driver != driver || t.port != c.Port {
			continue
		}
		if t.running {
			return dbRuntimeTarget{}, false
		}
		if !have {
			found, have = t, true
		}
	}
	return found, have
}

// dbStoppedHint is the footer text for a saved connection whose container
// or service is stopped (or starting). w is the room for the text.
func (m Model) dbStoppedHint(r dbTreeRow, w int) (string, bool) {
	s := m.databases
	if s == nil || r.kind != dbNodeConnection {
		return "", false
	}
	c, ok := s.connection(r.conn)
	if !ok {
		return "", false
	}
	t, ok := dbTargetForConn(m.services, c)
	if !ok {
		return "", false
	}
	var options []string
	if t.starting {
		options = []string{"service " + t.name + " is starting", t.name + " is starting", "starting"}
	} else {
		options = []string{"service " + t.name + " is stopped · S start", t.name + " stopped · S start", "stopped · S start"}
	}
	for _, o := range options {
		if len([]rune(o)) <= w {
			return o, true
		}
	}
	return options[len(options)-1], true
}

// databaseStartService starts the stopped container or service behind a
// connection through the existing start actions.
func (m *Model) databaseStartService(id string) tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	c, ok := s.connection(id)
	if !ok {
		return nil
	}
	t, ok := dbTargetForConn(m.services, c)
	if !ok {
		return m.pushToastDetail(toast.Info, "No stopped service for this connection", "Open the Containers view once so its services are known.")
	}
	if t.starting {
		return m.pushToastDetail(toast.Info, "Service is already starting", t.name)
	}
	if t.isCont {
		return m.containerAction("start", t.cont)
	}
	note, cmd := m.servicesStart(svcSecCompose, t.compose)
	return m.servicesNoteCmd(note, cmd)
}

// databaseSavedMatch is a saved connection to the same local server.
func (s *dbState) databaseSavedMatch(t dbRuntimeTarget) (dbclient.Conn, bool) {
	for _, c := range s.conns {
		driver := c.Driver
		if driver == "mariadb" {
			driver = "mysql"
		}
		if driver == t.driver && c.Port == t.port && dbLocalHost(c.Host) {
			return c, true
		}
	}
	return dbclient.Conn{}, false
}

// databaseSelectPath moves the Databases cursor to the row of a node.
func (s *dbState) databaseSelectPath(path string) bool {
	for i, r := range s.rows() {
		if r.path == path {
			s.cursor = i
			s.touch()
			return true
		}
	}
	return false
}

// databaseOpenFromRow is "Open in Databases" for the selected Services /
// Containers row.
func (m *Model) databaseOpenFromRow(r svcRow) tea.Cmd {
	t, ok := m.dbTargetFromRow(r)
	if !ok {
		return m.pushToastDetail(toast.Info, "Not a database container", "Open in Databases works on PostgreSQL, MySQL, MariaDB, Redis and Valkey.")
	}
	return m.databaseOpenTarget(t)
}

func (m *Model) databaseOpenTarget(t dbRuntimeTarget) tea.Cmd {
	open := m.revealDatabasesView(true)
	return tea.Batch(open, m.databaseOpenTargetRevealed(t))
}

func (m *Model) databaseOpenTargetRevealed(t dbRuntimeTarget) tea.Cmd {
	s := m.databases
	if saved, ok := s.databaseSavedMatch(t); ok {
		s.databaseSelectPath(saved.ID)
		m.databasesSaveSession()
		return m.pushToastDetail(toast.Info, "Already in Databases", sanitizeDBText(dbConnName(saved)))
	}
	c := dbclient.Conn{Driver: t.driver, Host: "localhost", Port: t.port, User: t.user, Database: t.database, Source: dbclient.SourceUser}
	if t.driver == "redis" {
		c.ID = dbRedisConnectionID(c)
	} else {
		c.ID = dbConnectionID(c)
	}
	if base := c.ID; s.duplicateConnectionID(base) {
		c.ID = base + "-" + strconv.Itoa(t.port)
	}
	c.Name = c.ID
	s.pendingConn, s.pendingPassword, s.addFromURL = c, "", false
	if t.isCont {
		s.databaseSelectPath("suggest-container-" + t.cont.ID)
	} else {
		s.databaseSelectPath("suggest-service-" + t.compose)
	}
	target := c.Host + ":" + strconv.Itoa(c.Port)
	if c.User != "" {
		target = c.User + "@" + target
	}
	if c.Database != "" {
		target += "/" + c.Database
	}
	cmd := m.databaseAddPrompt(promptKindDBPassword, "Add "+t.name+" to Databases", "Password for "+target+" (blank if none):", "", true)
	m.prompt.SetAllowEmpty(true)
	return cmd
}

const dbOpenInDatabasesTitle = "Open in Databases"

// openContainerMenu is the right-click menu of a container row.
func (m *Model) openContainerMenu(r svcRow, x, y int) {
	st := m.services
	st.menuRow = r
	items := []menu.Item{
		{ID: "start", Title: "Start", Hint: "s", Icon: "▸"},
		{ID: "stop", Title: "Stop", Hint: "x", Icon: "▪"},
		{ID: "restart", Title: "Restart", Hint: "r", Icon: "↻"},
		{ID: "logs", Title: "Logs", Hint: "enter", Icon: "≡"},
		{Sep: true},
		{ID: "copy-id", Title: "Copy ID", Hint: "y", Icon: "⎘"},
	}
	if _, ok := m.dbTargetFromRow(r); ok {
		items = append(items, menu.Item{ID: "databases", Title: dbOpenInDatabasesTitle, Icon: theme.IconDatabases.String()})
	}
	m.menu = menu.New(items, x, y)
	m.menu.SetScreenSize(m.w, m.h)
	m.menuOpen = true
	m.menuKind = menuKindServices
}

// handleContainerRowMenuSelect runs a container-row menu choice.
func (m *Model) handleContainerRowMenuSelect(id string, r svcRow) tea.Cmd {
	st := m.services
	if r.idx < 0 || r.idx >= len(st.containerRows) {
		return nil
	}
	c := st.containerRows[r.idx]
	switch id {
	case "start", "stop", "restart":
		return m.containerAction(id, c)
	case "logs":
		return m.openContainerTerminal(c, "logs")
	case "copy-id":
		m.copyToClipboard(c.ID)
		return m.servicesToast(toast.Info, "Copied container id", shortImageID(c.ID))
	case "databases":
		return m.databaseOpenFromRow(r)
	}
	return nil
}
