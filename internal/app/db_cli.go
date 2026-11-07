package app

import (
	"errors"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

// DBX-45: $ on a connection opens its command line client (psql, mysql,
// sqlite3 or redis-cli) in a terminal tab. The password and SSL settings
// go in the tab's environment, never on the command line.

// dbCLIJob is the terminal tab for c.
func dbCLIJob(c dbclient.Conn, password, root string) (terminalJob, error) {
	if c.SSH != nil {
		return terminalJob{}, errors.New("this connection goes through SSH, and the command line client cannot use that tunnel")
	}
	cli, err := dbclient.CLICommand(c, dbclient.CLIOptions{Password: password, Root: root})
	if err != nil {
		return terminalJob{}, err
	}
	parts := []string{shellQuote(cli.Tool)}
	for _, a := range cli.Args {
		parts = append(parts, shellQuote(a))
	}
	env := map[string]any{}
	for _, kv := range cli.Env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return terminalJob{Label: cli.Tool + " · " + dbConnName(c), Cmdline: strings.Join(parts, " "), Dir: root, Env: env}, nil
}

// databaseOpenCLI is $ on a connection row.
func (m *Model) databaseOpenCLI(id string) tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	c, ok := s.connection(id)
	if !ok {
		return nil
	}
	tool := dbclient.CLITool(c.Driver)
	if dbclient.IsSQLCipher(c) {
		tool = "sqlcipher"
	}
	if tool == "" {
		return m.pushToastDetail(toast.Info, "No command line client", "termocode knows psql, mysql, sqlite3 and redis-cli.")
	}
	if _, err := exec.LookPath(tool); err != nil {
		return m.pushToastDetail(toast.Warn, tool+" is not installed", "Install it and make sure it is on your PATH.")
	}
	if dbProjectCmdRef(c) {
		return m.pushToastDetail(toast.Errr, "Not run: ${cmd:} in a project file", secrets.ProjectCmdWarning)
	}
	if databaseNeedsUnlock(databaseSecretStore(), c) {
		return m.withSecretsUnlocked(func(m *Model) tea.Cmd { return m.databaseOpenCLI(id) })
	}
	password, errCmd := m.databaseResolvePassword(id, c)
	if errCmd != nil {
		return errCmd
	}
	j, err := dbCLIJob(c, password, s.root)
	if err != nil {
		return m.pushToastDetail(toast.Info, "Cannot open "+tool, sanitizeDBText(err.Error()))
	}
	if _, err := m.spawnTerminalJob(j); err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot open "+tool, sanitizeDBText(err.Error()))
	}
	if dbclient.IsSQLCipher(c) {
		return tea.Batch(m.fetchStateCmd(), m.pushToastDetail(toast.Info, "Opened sqlcipher for "+dbConnName(c), "In a terminal tab. Type PRAGMA key = '…'; first; the key is never put on the command line."))
	}
	return tea.Batch(m.fetchStateCmd(), m.pushToastDetail(toast.Info, "Opened "+tool+" for "+dbConnName(c), "In a terminal tab; the password is in its environment only."))
}
