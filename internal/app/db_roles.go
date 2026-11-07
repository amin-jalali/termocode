package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Users and roles (DBX-18). Show Users and Roles and Show Grants of a
// Role… are reads that run in the connection's console (a normal grid).
// Create / Alter Role… and Grant / Revoke… write the DDL into the console
// and send it through the run card, which shows the statement with Cancel
// first: nothing changes on the server before the user says Run.

const (
	promptKindDBRoleGrants  promptKindEnum = 5210
	promptKindDBRoleName    promptKindEnum = 5211
	promptKindDBGrantRole   promptKindEnum = 5212
	promptKindDBGrantTarget promptKindEnum = 5213
)

const pickerKindDBRolePreset pickerKindEnum = 5210

// dbRolesRun sends one statement through the console (a test seam).
var dbRolesRun = (*Model).databaseRunInConsole

// dbRolesState is the open roles flow.
type dbRolesState struct {
	conn   string
	alter  bool
	revoke bool
	name   string
	host   string
}

// databaseRolesConn is the current SQL connection when it has roles.
func (m *Model) databaseRolesConn() (dbclient.Conn, tea.Cmd, bool) {
	c, cmd, ok := m.databaseSessionConn()
	if !ok {
		return c, cmd, false
	}
	if _, ok := dbclient.RoleQueriesFor(c.Driver); !ok {
		return c, m.pushToastDetail(toast.Info, "Not available for "+sanitizeDBText(c.Driver), "Users and roles need a PostgreSQL or MySQL server."), false
	}
	return c, nil, true
}

func (m *Model) databaseRolesShow() tea.Cmd {
	c, cmd, ok := m.databaseRolesConn()
	if !ok {
		return cmd
	}
	q, _ := dbclient.RoleQueriesFor(c.Driver)
	if err := dbReadOnlyCheck(c.Driver, q.Roles); err != nil {
		return m.dbConsoleError(err)
	}
	return dbRolesRun(m, c.ID, q.Roles)
}

func dbAccountLabel(driver string) string {
	if driver == "mysql" || driver == "mariadb" {
		return "Account (user@host, host defaults to %):"
	}
	return "Role name:"
}

// databaseRolesStart opens the first prompt of a flow.
func (m *Model) databaseRolesStart(kind promptKindEnum, title string, alter, revoke bool) tea.Cmd {
	c, cmd, ok := m.databaseRolesConn()
	if !ok {
		return cmd
	}
	m.dbConsole().roles = dbRolesState{conn: c.ID, alter: alter, revoke: revoke}
	return m.openHTTPPrompt(kind, title, dbAccountLabel(c.Driver), "")
}

// databaseRolesPrompt routes the roles prompts. ok=false: not ours.
func (m *Model) databaseRolesPrompt(kind promptKindEnum, value string) (tea.Cmd, bool) {
	switch kind {
	case promptKindDBRoleGrants, promptKindDBRoleName, promptKindDBGrantRole, promptKindDBGrantTarget:
	default:
		return nil, false
	}
	st := &m.dbConsole().roles
	c, ok := m.databases.connection(st.conn)
	if !ok {
		return m.pushToastDetail(toast.Warn, "Connection not found", sanitizeDBText(st.conn)), true
	}
	if kind == promptKindDBGrantTarget {
		privs, schema, table, err := dbclient.ParseGrantTarget(value)
		if err != nil {
			return m.pushToastDetail(toast.Warn, "Privileges: "+err.Error(), ""), true
		}
		sql, err := dbclient.GrantDDL(c.Driver, dbclient.GrantSpec{Role: st.name, Host: st.host, Privileges: privs, Schema: schema, Table: table, Revoke: st.revoke})
		if err != nil {
			return m.pushToastDetail(toast.Warn, err.Error(), ""), true
		}
		return dbRolesRun(m, c.ID, sql), true
	}
	st.name, st.host = dbclient.ParseAccount(c.Driver, value)
	switch kind {
	case promptKindDBRoleGrants:
		sql, err := dbclient.GrantsSQL(c.Driver, st.name, st.host)
		if err != nil {
			return m.pushToastDetail(toast.Warn, err.Error(), ""), true
		}
		if err := dbReadOnlyCheck(c.Driver, sql); err != nil {
			return m.dbConsoleError(err), true
		}
		return dbRolesRun(m, c.ID, sql), true
	case promptKindDBGrantRole:
		if st.name == "" {
			return m.pushToastDetail(toast.Warn, "Role name is empty", ""), true
		}
		title := "Grant privileges to " + sanitizeDBText(st.name)
		if st.revoke {
			title = "Revoke privileges from " + sanitizeDBText(st.name)
		}
		return m.openHTTPPrompt(promptKindDBGrantTarget, title, `Privileges on schema[.table], e.g. "SELECT, INSERT on public.users":`, ""), true
	}
	// promptKindDBRoleName: pick the kind of role next.
	if st.name == "" {
		return m.pushToastDetail(toast.Warn, "Role name is empty", ""), true
	}
	presets := dbclient.RolePresets(c.Driver)
	items := make([]picker.Item, 0, len(presets))
	for _, p := range presets {
		items = append(items, picker.Item{ID: p.ID, Title: p.Title, Hint: p.Hint})
	}
	verb := "Create "
	if st.alter {
		verb = "Alter "
	}
	m.picker = picker.NewItems(verb+sanitizeDBText(st.name)+" as · Enter shows the statement", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBRolePreset
	m.pickerOpen = true
	return nil, true
}

// databaseRolesPick writes the role DDL of the picked preset and sends it
// through the run card.
func (m *Model) databaseRolesPick(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBRolePreset {
		return nil, false
	}
	st := m.dbConsole().roles
	c, ok := m.databases.connection(st.conn)
	if !ok {
		return nil, true
	}
	for _, p := range dbclient.RolePresets(c.Driver) {
		if p.ID != id {
			continue
		}
		spec := p.Spec
		spec.Name, spec.Host = st.name, st.host
		sql, err := dbclient.RoleDDL(c.Driver, spec, st.alter)
		if err != nil {
			return m.pushToastDetail(toast.Warn, err.Error(), ""), true
		}
		return dbRolesRun(m, c.ID, sql), true
	}
	return nil, true
}
