package app

import (
	"errors"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

// DBX-45: saved SQL runs (.termocode/sql-runs.json). "Databases: Saved
// SQL Runs…" lists them; a pick opens a card that shows the script, the
// connections, the before-launch steps and the log file (Cancel first).
// Run plays the script on each connection with its command line client
// in one terminal tab; the output is also added to the log file.

const (
	pickerKindDBSQLRuns  pickerKindEnum  = 4571
	confirmKindDBSQLRun  confirmKindEnum = 4572
	dbSQLRunEditID                       = "\x00edit"
	dbSQLRunPaletteID                    = "db-saved-runs"
	dbSQLRunPaletteTitle                 = "Databases: Saved SQL Runs…"
)

// databaseSavedRuns opens the picker of saved runs.
func (m *Model) databaseSavedRuns() tea.Cmd {
	m.ensureDatabasesLoaded()
	s := m.databases
	if s == nil {
		return nil
	}
	runs, err := dbclient.LoadSQLRuns(s.root)
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot read sql-runs.json", sanitizeDBText(err.Error()))
	}
	if len(runs) == 0 {
		return m.pushToastDetail(toast.Info, "No saved SQL runs", "Add them to .termocode/sql-runs.json. The Databases guide shows how.")
	}
	items := make([]picker.Item, 0, len(runs)+1)
	for _, r := range runs {
		items = append(items, picker.Item{ID: r.Name, Title: sanitizeDBText(r.Name), Hint: sanitizeDBText(r.Script) + " · " + dbPlural(len(r.Connections), "connection")})
	}
	items = append(items, picker.Item{ID: dbSQLRunEditID, Title: "Edit sql-runs.json", Hint: ".termocode/sql-runs.json"})
	m.picker = picker.NewItems("Saved SQL runs · Enter shows what runs", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBSQLRuns
	m.pickerOpen = true
	return nil
}

// databaseSQLRunPicked opens the card of the picked run.
func (m *Model) databaseSQLRunPicked(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBSQLRuns {
		return nil, false
	}
	s := m.databases
	if s == nil {
		return nil, true
	}
	if id == dbSQLRunEditID {
		m.openFileAtLine(dbclient.SQLRunsPath(s.root), 1)
		return nil, true
	}
	r, targets, errCmd := m.databaseSQLRunResolve(id)
	if errCmd != nil {
		return errCmd, true
	}
	names := make([]string, 0, len(targets))
	for _, c := range targets {
		names = append(names, sanitizeDBText(dbConnName(c)))
	}
	s.sqlRunPending = r.Name
	m.confirm = confirm.New("Run "+sanitizeDBText(r.Name)+"?", sanitizeDBLines(dbclient.SQLRunSummary(r, names)), []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "run", Title: "Run", Style: confirm.StylePrimary},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBSQLRun
	return nil, true
}

// databaseSQLRunConfirm answers the run card.
func (m *Model) databaseSQLRunConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindDBSQLRun {
		return nil, false
	}
	s := m.databases
	if s == nil {
		return nil, true
	}
	name := s.sqlRunPending
	s.sqlRunPending = ""
	if id != "run" || name == "" {
		return nil, true
	}
	return m.databaseSQLRunStart(name), true
}

// databaseSQLRunResolve finds run name and its connections; a non-nil
// cmd is the error toast.
func (m *Model) databaseSQLRunResolve(name string) (dbclient.SQLRun, []dbclient.Conn, tea.Cmd) {
	s := m.databases
	runs, err := dbclient.LoadSQLRuns(s.root)
	if err != nil {
		return dbclient.SQLRun{}, nil, m.pushToastDetail(toast.Errr, "Cannot read sql-runs.json", sanitizeDBText(err.Error()))
	}
	var r dbclient.SQLRun
	found := false
	for _, x := range runs {
		if x.Name == name {
			r, found = x, true
			break
		}
	}
	if !found {
		return r, nil, m.pushToastDetail(toast.Warn, "Saved run not found", "sql-runs.json changed. Open the list again.")
	}
	conns := make([]dbclient.Conn, 0, len(r.Connections))
	for _, ref := range r.Connections {
		c, ok := s.sqlRunConnection(ref)
		if !ok {
			return r, nil, m.pushToastDetail(toast.Errr, "Unknown connection "+sanitizeDBText(ref), "Use a connection id from the Databases view in sql-runs.json.")
		}
		conns = append(conns, c)
	}
	return r, conns, nil
}

// sqlRunConnection finds a connection by id, then by name.
func (s *dbState) sqlRunConnection(ref string) (dbclient.Conn, bool) {
	if c, ok := s.connection(ref); ok {
		return c, true
	}
	for _, c := range s.conns {
		if c.Name != "" && c.Name == ref {
			return c, true
		}
	}
	return dbclient.Conn{}, false
}

// databaseSQLRunStart checks every connection, reads the passwords and
// opens the terminal tab of the run.
func (m *Model) databaseSQLRunStart(name string) tea.Cmd {
	s := m.databases
	r, conns, errCmd := m.databaseSQLRunResolve(name)
	if errCmd != nil {
		return errCmd
	}
	if _, err := os.Stat(dbclient.SQLRunPath(s.root, r.Script)); err != nil {
		return m.pushToastDetail(toast.Errr, "Script not found", sanitizeDBText(r.Script))
	}
	store := databaseSecretStore()
	for _, c := range conns {
		if databaseNeedsUnlock(store, c) {
			return m.withSecretsUnlocked(func(m *Model) tea.Cmd { return m.databaseSQLRunStart(name) })
		}
	}
	targets := make([]dbclient.SQLRunTarget, 0, len(conns))
	for _, c := range conns {
		label := dbConnName(c)
		tool := dbclient.CLITool(c.Driver)
		switch {
		case tool == "":
			return m.pushToastDetail(toast.Info, "No command line client for "+sanitizeDBText(label), "Saved runs use psql, mysql, sqlite3 or redis-cli.")
		case c.SSH != nil:
			return m.pushToastDetail(toast.Info, sanitizeDBText(label)+" goes through SSH", "The command line client cannot use that tunnel.")
		case dbProjectCmdRef(c):
			return m.pushToastDetail(toast.Errr, "Not run: ${cmd:} in a project file", secrets.ProjectCmdWarning)
		}
		if _, err := exec.LookPath(tool); err != nil {
			return m.pushToastDetail(toast.Warn, tool+" is not installed", "Install it and make sure it is on your PATH.")
		}
		password, errCmd := m.databaseResolvePassword(c.ID, c)
		if errCmd != nil {
			return errCmd
		}
		cli, err := dbclient.CLICommand(c, dbclient.CLIOptions{Password: password, Root: s.root})
		if err != nil {
			return m.pushToastDetail(toast.Errr, "Cannot run on "+sanitizeDBText(label), sanitizeDBText(err.Error()))
		}
		targets = append(targets, dbclient.SQLRunTarget{Name: label, CLI: cli})
	}
	j, err := dbSQLRunJob(r, targets, s.root)
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot start the run", sanitizeDBText(err.Error()))
	}
	if _, err := m.spawnTerminalJob(j); err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot start the run", sanitizeDBText(err.Error()))
	}
	detail := "In a terminal tab."
	if r.Log != "" {
		detail = "In a terminal tab; the output is also added to " + sanitizeDBText(r.Log) + "."
	}
	return tea.Batch(m.fetchStateCmd(), m.pushToastDetail(toast.Info, "Started "+sanitizeDBText(r.Name), detail))
}

// dbSQLRunJob is the terminal tab of a run.
func dbSQLRunJob(r dbclient.SQLRun, targets []dbclient.SQLRunTarget, root string) (terminalJob, error) {
	if len(targets) == 0 {
		return terminalJob{}, errors.New("no connection to run on")
	}
	line, env, err := dbclient.SQLRunShell(r, targets, root)
	if err != nil {
		return terminalJob{}, err
	}
	e := make(map[string]any, len(env))
	for k, v := range env {
		e[k] = v
	}
	return terminalJob{Label: "SQL run · " + r.Name, Cmdline: line, Dir: root, Env: e}, nil
}
