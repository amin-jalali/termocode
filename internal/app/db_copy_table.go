package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Databases: Copy Table Data to Another Connection… (DBX-19). On a table
// (or view) in the tree: a picker of the connected SQL databases that can
// be written to (the same one too), a prompt for the target table, then
// the rows are read on a read-only session into a private CSV file in the
// config folder (0600, exact text, NULL as an empty cell, the export mask
// applied) and that file opens the normal import card for the target: the
// column mapping, the production colours and Cancel first, one
// transaction. The file is removed when the import ends or is cancelled.
// The AI cannot start a copy (it is a write the user confirms).

const pickerKindDBCopyTarget pickerKindEnum = 9461

const promptKindDBCopyTable promptKindEnum = 9461

// dbCopyTarget is one connected database the rows can go to.
type dbCopyTarget struct {
	conn, name, sessKey, driver string
}

// dbCopyTable is the copy being prepared.
type dbCopyTable struct {
	src     dbTable
	targets []dbCopyTarget
	tgt     dbCopyTarget
}

// dbCopyTempDir is the private folder of the copy files.
func dbCopyTempDir() string { return dbConfigPath("copy-tmp") }

// dbCopySweep removes copy files older than a day (a crash left them).
func dbCopySweep(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "copy-") {
			continue
		}
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > 24*time.Hour {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// dbCopyDefaultName is what the target prompt starts with: the source's
// name on another connection, nothing on the same session (it would be the
// source itself). SQLite has no schemas to carry over.
func dbCopyDefaultName(src dbTable, tgt dbCopyTarget) string {
	if tgt.sessKey == src.sessKey {
		return ""
	}
	if src.schema != "" && dbDriverFamily(tgt.driver) == dbDriverFamily(src.driver) && dbDriverFamily(tgt.driver) != "sqlite" {
		return src.schema + "." + src.name
	}
	return src.name
}

// dbCopyParseName splits "schema.table" (or "table") as typed.
func dbCopyParseName(s string) (schema, name string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", errors.New("Type the target table, like public.users or users.")
	}
	if i := strings.Index(s, "."); i >= 0 {
		schema, name = strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
		if schema == "" || name == "" {
			return "", "", errors.New("Type schema.table, or only the table name.")
		}
		return schema, name, nil
	}
	return "", s, nil
}

// databaseCopyTableStart checks the source and lists the targets.
func (m *Model) databaseCopyTableStart() tea.Cmd {
	t, cmd, ok := m.databaseTableTarget()
	if !ok {
		return cmd
	}
	s := m.dbConsole()
	if s.job != nil || s.running != nil || s.submitting != nil {
		return m.pushToastDetail(toast.Warn, "A query or transfer is running", "Esc cancels it.")
	}
	if dbclient.DialectFor(t.driver) == nil {
		return m.pushToastDetail(toast.Info, "Cannot copy", "This connection does not use SQL.")
	}
	var targets []dbCopyTarget
	keys := make([]string, 0, len(m.databases.sessions))
	for k := range m.databases.sessions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		conn := key
		if n, ok := m.databases.nodes[key]; ok && n.conn != "" {
			conn = n.conn
		} else if i := strings.Index(key, "/"); i > 0 {
			conn = key[:i]
		}
		c, ok := m.databases.connection(conn)
		if !ok || c.ReadOnly || dbclient.DialectFor(c.Driver) == nil || m.databases.sessions[key] == nil {
			continue
		}
		name := dbConnName(c)
		if key != conn {
			name += " · " + strings.TrimPrefix(key, conn+"/")
		}
		targets = append(targets, dbCopyTarget{conn: conn, name: name, sessKey: key, driver: c.Driver})
	}
	if len(targets) == 0 {
		return m.pushToastDetail(toast.Info, "No database to copy into",
			"Connect the target database (Enter on its connection); read-only connections cannot be written to.")
	}
	s.copyTbl = &dbCopyTable{src: t, targets: targets}
	items := make([]picker.Item, 0, len(targets))
	for i, tg := range targets {
		hint := dbDriverTitle(tg.driver)
		if tg.sessKey == t.sessKey {
			hint += " · same database"
		}
		items = append(items, picker.Item{ID: strconv.Itoa(i), Title: sanitizeDBText(tg.name), Hint: hint})
	}
	m.picker = picker.NewItems("Copy rows of "+t.label()+" into…", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBCopyTarget
	m.pickerOpen = true
	return nil
}

// databaseCopyTargetPicked asks for the target table.
func (m *Model) databaseCopyTargetPicked(id string) tea.Cmd {
	s := m.dbConsole()
	ct := s.copyTbl
	if ct == nil {
		return nil
	}
	i, err := strconv.Atoi(id)
	if err != nil || i < 0 || i >= len(ct.targets) {
		s.copyTbl = nil
		return nil
	}
	ct.tgt = ct.targets[i]
	return m.openHTTPPrompt(promptKindDBCopyTable, "Copy rows of "+ct.src.label()+" into "+sanitizeDBText(ct.tgt.name),
		"Target table (schema.table or table; it must exist):", dbCopyDefaultName(ct.src, ct.tgt))
}

type dbCopyReadMsg struct {
	job  *dbJob
	ct   *dbCopyTable
	tgt  dbTable
	path string
	rows int64
	err  error
}

// databaseCopyTableTyped reads the source rows into the copy file.
func (m *Model) databaseCopyTableTyped(value string) tea.Cmd {
	s := m.dbConsole()
	ct := s.copyTbl
	if ct == nil {
		return nil
	}
	schema, name, err := dbCopyParseName(value)
	if err != nil {
		return m.pushToastDetail(toast.Info, "Cannot copy", err.Error())
	}
	tgt := dbTable{conn: ct.tgt.conn, driver: ct.tgt.driver, sessKey: ct.tgt.sessKey, schema: schema, name: name, path: ct.tgt.sessKey}
	src := ct.src
	if tgt.sessKey == src.sessKey && tgt.schema == src.schema && tgt.name == src.name {
		return m.pushToastDetail(toast.Info, "That is the source table", "Type another table to copy the rows into.")
	}
	if s.job != nil || s.running != nil || s.submitting != nil {
		s.copyTbl = nil
		return m.pushToastDetail(toast.Warn, "A query or transfer is running", "Esc cancels it.")
	}
	sess := m.databases.sessions[src.sessKey]
	if sess == nil || m.databases.sessions[tgt.sessKey] == nil {
		s.copyTbl = nil
		return m.pushToastDetail(toast.Info, "Not connected any more", "Connect both databases again, then copy.")
	}
	dir := dbCopyTempDir()
	if dir == "" {
		s.copyTbl = nil
		return m.pushToastDetail(toast.Warn, "Cannot copy", "The config folder is unknown.")
	}
	mask := loadDBConsoleSettings().exportMask()
	ctx, cancel := context.WithCancel(context.Background())
	job := &dbJob{kind: "export", title: "Reading " + src.label() + " to copy", conn: src.conn, cancel: cancel}
	s.job = job
	m.databaseJobCard(job)
	query := "SELECT * FROM " + src.qualified()
	driver := src.driver
	run := func() tea.Msg {
		defer cancel()
		msg := dbCopyReadMsg{job: job, ct: ct, tgt: tgt}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			msg.err = err
			return msg
		}
		_ = os.Chmod(dir, 0o700)
		dbCopySweep(dir, time.Now())
		f, err := os.CreateTemp(dir, "copy-*.csv")
		if err != nil {
			msg.err = err
			return msg
		}
		path := f.Name()
		_ = f.Close()
		_ = os.Remove(path) // WriteCopyCSV makes it again, 0600 and exclusive
		ro, err := sess.ReadOnlySession()
		if err != nil {
			msg.err = err
			return msg
		}
		defer ro.Close()
		msg.rows, msg.err = dbclient.WriteCopyCSV(ctx, path, func(ctx context.Context, sink dbclient.RowSink) error {
			return dbclient.StreamRows(ctx, ro, driver, query, dbMasked(sink, mask))
		}, func(n int64) { job.done.Store(n) })
		if msg.err == nil {
			msg.path = path
		}
		return msg
	}
	return tea.Batch(run, dbJobTickCmd(job))
}

// onDBCopyRead opens the import card for the copy file.
func (m *Model) onDBCopyRead(msg dbCopyReadMsg) tea.Cmd {
	s := m.dbConsole()
	if s.job != msg.job {
		if msg.path != "" {
			_ = os.Remove(msg.path)
		}
		return nil
	}
	s.job = nil
	m.closeJobCard()
	if s.copyTbl == msg.ct {
		s.copyTbl = nil
	}
	switch {
	case errors.Is(msg.err, context.Canceled):
		return m.pushToastDetail(toast.Info, "Copy cancelled", "Nothing was copied.")
	case msg.err != nil:
		return m.pushToastDetail(toast.Warn, "Cannot read "+msg.ct.src.label(), dbErrorText(msg.err)+" · Nothing was copied.")
	}
	c, ok := m.databases.connection(msg.tgt.conn)
	if !ok || m.databases.sessions[msg.tgt.sessKey] == nil {
		_ = os.Remove(msg.path)
		return m.pushToastDetail(toast.Info, "Not connected any more", "Connect the target again, then copy.")
	}
	from := msg.ct.src.label()
	if sc, ok := m.databases.connection(msg.ct.src.conn); ok {
		from = sanitizeDBText(dbConnName(sc)) + " · " + from
	}
	s.imp = &dbImport{t: msg.tgt, conn: c, sessKey: msg.tgt.sessKey, tmp: msg.path, copyFrom: from, masked: len(loadDBConsoleSettings().exportMask()) > 0}
	cmd := m.databaseImportLoad(msg.path)
	if s.imp != nil {
		s.imp.display = "rows of " + from
	}
	return cmd
}

// dropTemp removes the copy file of a copy import (nothing for a CSV the
// user picked).
func (imp *dbImport) dropTemp() {
	if imp != nil && imp.tmp != "" {
		_ = os.Remove(imp.tmp)
		imp.tmp = ""
	}
}

// dbImportEnd forgets the import being prepared and removes its copy file.
func (s *dbConsoleState) dbImportEnd() {
	s.imp.dropTemp()
	s.imp = nil
}
