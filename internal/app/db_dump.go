package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Dump and restore (DBX-19): Databases: Dump Database… and Restore
// Database… run the server's own tools (pg_dump / pg_restore / psql,
// mysqldump / mysql) found on PATH. Both ask for the file, then show a
// card with the command (the password is never on it, nor in the process
// arguments) and Cancel first. The tool runs off the UI goroutine; Esc on
// its card stops it.

const (
	promptKindDBDumpPath    promptKindEnum = 5220
	promptKindDBRestorePath promptKindEnum = 5221
)

const (
	confirmKindDBDump    confirmKindEnum = 5220
	confirmKindDBRestore confirmKindEnum = 5221
)

// Test seams: the PATH lookup, the process run and the connected check.
var (
	dbToolLookPath = exec.LookPath
	dbToolRun      = dbRunToolProcess
	// dbDumpConnected: the password of a connection is known once it is
	// connected (it may live in the keyring).
	dbDumpConnected = func(m *Model, id string) bool { return m.databases.sessions[id] != nil }
)

// dbDumpState is the dump or restore being prepared.
type dbDumpState struct {
	conn    string
	restore bool
	cmd     dbclient.CLICmd
	path    string // the tool's absolute path
	file    string // display name of the file
}

// dbRunToolProcess runs the tool and returns the end of its error output.
func dbRunToolProcess(ctx context.Context, path string, c dbclient.CLICmd) (string, error) {
	cmd := exec.CommandContext(ctx, path, c.Args...)
	cmd.Env = append(os.Environ(), c.Env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if c.Stdin != "" {
		f, err := os.Open(c.Stdin)
		if err != nil {
			return "", err
		}
		defer f.Close()
		cmd.Stdin = f
	}
	err := cmd.Run()
	out := strings.TrimSpace(stderr.String())
	if len(out) > 600 {
		out = "…" + out[len(out)-600:]
	}
	return out, err
}

type dbToolDoneMsg struct {
	job   *dbJob
	title string
	out   string
	err   error
	took  time.Duration
}

// databaseDumpStart is the palette entry of Dump (restore=false) or
// Restore.
func (m *Model) databaseDumpStart(restore bool) tea.Cmd {
	c, cmd, ok := m.databaseSessionConn()
	if !ok {
		return cmd
	}
	tool := dbclient.DumpTool(c.Driver)
	if tool == "" {
		return m.pushToastDetail(toast.Info, "Not available for "+sanitizeDBText(c.Driver), "Dump and restore need a PostgreSQL or MySQL server.")
	}
	switch {
	case restore && c.ReadOnly:
		return m.pushToastDetail(toast.Warn, "Cannot restore", "The connection is read-only.")
	case c.SSH != nil || c.Kube != nil:
		return m.pushToastDetail(toast.Info, "Not available over a tunnel", "Dump and restore reach the server directly; run the tool on a host that can.")
	case !dbDumpConnected(m, c.ID):
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	if restore {
		tool = "pg_restore / psql"
		if c.Driver == "mysql" || c.Driver == "mariadb" {
			tool = "mysql"
		}
	}
	for _, t := range strings.Split(tool, " / ") {
		if _, err := dbToolLookPath(t); err != nil {
			return m.pushToastDetail(toast.Warn, t+" was not found", "Install the "+sanitizeDBText(c.Driver)+" client tools so "+t+" is on PATH.")
		}
	}
	m.dbConsole().dump = &dbDumpState{conn: c.ID, restore: restore}
	name := sanitizeDBText(dbConnName(c))
	if restore {
		return m.openHTTPPrompt(promptKindDBRestorePath, "Restore into "+name, "Dump file (.sql, or a pg_dump archive):", "")
	}
	ext := ".sql"
	if dbclient.DumpTool(c.Driver) == "pg_dump" {
		ext = ".dump"
	}
	def := c.Database + "-" + clock.Now().Format("2006-01-02") + ext
	return m.openHTTPPrompt(promptKindDBDumpPath, "Dump "+name, "File (.dump = pg_dump archive, .sql = plain SQL):", def)
}

// databaseDumpPrompt routes the file prompts. ok=false: not ours.
func (m *Model) databaseDumpPrompt(kind promptKindEnum, value string) (tea.Cmd, bool) {
	if kind != promptKindDBDumpPath && kind != promptKindDBRestorePath {
		return nil, false
	}
	s := m.dbConsole()
	d := s.dump
	if d == nil {
		return nil, true
	}
	c, ok := m.databases.connection(d.conn)
	if !ok {
		s.dump = nil
		return m.pushToastDetail(toast.Warn, "Connection not found", sanitizeDBText(d.conn)), true
	}
	abs, display, err := m.dbResolvePath(value)
	if err != nil {
		s.dump = nil
		return m.pushToastDetail(toast.Warn, "Cannot use that file", err.Error()), true
	}
	o := dbclient.CLIOptions{Password: m.databases.passwords[c.ID], Root: m.databaseWorkspaceRoot()}
	var cmd dbclient.CLICmd
	if d.restore {
		if st, serr := os.Stat(abs); serr != nil || st.IsDir() {
			s.dump = nil
			return m.pushToastDetail(toast.Warn, "Cannot restore", sanitizeDBText(display)+" is not a file."), true
		}
		cmd, err = dbclient.RestoreCommand(c, o, abs)
	} else {
		cmd, err = dbclient.DumpCommand(c, o, abs)
	}
	if err != nil {
		s.dump = nil
		return m.pushToastDetail(toast.Warn, "Cannot start", err.Error()), true
	}
	path, err := dbToolLookPath(cmd.Tool)
	if err != nil {
		s.dump = nil
		return m.pushToastDetail(toast.Warn, cmd.Tool+" was not found", "Install it so it is on PATH."), true
	}
	d.cmd, d.path, d.file = cmd, path, display
	line := cmd.Tool + " " + strings.Join(cmd.Args, " ")
	if cmd.Stdin != "" {
		line += " < " + cmd.Stdin
	}
	name := sanitizeDBText(dbConnName(c))
	title, body, btn, ck := "Dump "+name+" to "+sanitizeDBText(display)+"?", "", confirm.Button{ID: "dump", Title: "Dump"}, confirmKindDBDump
	if _, serr := os.Stat(abs); serr == nil && !d.restore {
		body = "The file exists and is replaced.\n\n"
	}
	if d.restore {
		title = "Restore " + sanitizeDBText(display) + " into " + name + "?"
		body = "Every statement of the file runs on the server: tables and rows can be replaced or dropped. It cannot be undone.\n\n"
		if c.Environment == "production" {
			body = "This is a PRODUCTION connection. " + body
		}
		btn = confirm.Button{ID: "restore", Title: "Restore", Style: confirm.StyleDestructive}
		ck = confirmKindDBRestore
	}
	body += dbSafeMultiline(sanitizeDBText(line))
	m.confirm = confirm.New(title, body, []confirm.Button{{ID: "cancel", Title: "Cancel"}, btn}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = ck
	return nil, true
}

// databaseDumpConfirm runs the tool on "dump" / "restore".
func (m *Model) databaseDumpConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindDBDump && m.confirmKind != confirmKindDBRestore {
		return nil, false
	}
	s := m.dbConsole()
	d := s.dump
	s.dump = nil
	if d == nil || (id != "dump" && id != "restore") {
		return nil, true
	}
	if s.job != nil {
		return m.pushToastDetail(toast.Warn, "Another transfer is running", "Esc cancels it."), true
	}
	ctx, cancel := context.WithCancel(context.Background())
	title := "Dumping to " + sanitizeDBText(d.file)
	if d.restore {
		title = "Restoring " + sanitizeDBText(d.file)
	}
	job := &dbJob{kind: "tool", title: title, conn: d.conn, cancel: cancel}
	s.job = job
	m.databaseJobCard(job)
	cmd, path := d.cmd, d.path
	return func() tea.Msg {
		start := time.Now()
		out, err := dbToolRun(ctx, path, cmd)
		if ctx.Err() != nil {
			err = context.Canceled
		}
		cancel()
		return dbToolDoneMsg{job: job, title: title, out: out, err: err, took: time.Since(start)}
	}, true
}

func (m *Model) onDBToolDone(msg dbToolDoneMsg) tea.Cmd {
	s := m.dbConsole()
	if s.job != msg.job {
		return nil
	}
	s.job = nil
	m.closeJobCard()
	switch {
	case errors.Is(msg.err, context.Canceled):
		return m.pushToastDetail(toast.Info, "Stopped", msg.title+" was stopped; the result may be incomplete.")
	case msg.err != nil:
		detail := sanitizeDBText(msg.out)
		if detail == "" {
			detail = msg.err.Error()
		}
		return m.pushToastDetail(toast.Errr, msg.title+" failed", detail)
	}
	return m.pushToastDetail(toast.Info, "Done: "+msg.title, fmt.Sprintf("%.1f s", msg.took.Seconds()))
}
