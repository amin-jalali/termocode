package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	tea "github.com/charmbracelet/bubbletea"
)

// Export (plan F10): `S` on a result page, or Databases: Export Result…
// → a picker of the formats → a file prompt → (a card when the result is
// only part of the rows) → (a card when the file exists) → the file.
// "Export all" re-runs the query without the row cap in a read-only
// session and streams the rows into a temporary file, so no more than a
// page is ever in memory; the file replaces the chosen path only when it is
// complete. CSV and TSV cells that start with = + - @ get a leading quote
// (spreadsheet formula protection).

const (
	pickerKindDBExportFormat pickerKindEnum = 1350 + iota
	pickerKindDBImportFile
	pickerKindDBImportMapCol
	pickerKindDBImportMapSrc
)

const (
	confirmKindDBExportScope confirmKindEnum = 1350 + iota
	confirmKindDBExportOverwrite
	confirmKindDBImport
	confirmKindDBJob
	confirmKindDBInfo
)

const (
	promptKindDBExportPath promptKindEnum = 1350 + iota
	promptKindDBImportPath
)

// dbExport is one export in progress: what to write, where, and how.
type dbExport struct {
	format    dbclient.ExportFormat
	opts      dbclient.ExportOptions
	path      string // absolute
	display   string // how the path is shown (relative to the workspace when it is inside)
	res       dbclient.Result
	all       bool
	overwrite bool
	// what Export all re-runs (sql == "": not possible)
	sql, reason, driver, conn, sessKey string
	partial                            string // "": the rows are everything; else why they are not
	loaded                             int
}

// dbJob is a long export or import that runs off the UI goroutine.
type dbJob struct {
	kind   string // "export" or "import"
	title  string
	conn   string
	total  int64 // 0: unknown
	done   atomic.Int64
	cancel context.CancelFunc
	// blocks: the session is in use, so no statement may start on it
	blocks bool
	gen    uint64
}

type dbJobTickMsg struct{ job *dbJob }

const dbJobTick = 400 * time.Millisecond

func dbJobTickCmd(j *dbJob) tea.Cmd {
	return tea.Tick(dbJobTick, func(time.Time) tea.Msg { return dbJobTickMsg{job: j} })
}

// databaseJobCard shows the progress card of the running job.
func (m *Model) databaseJobCard(j *dbJob) {
	text := dbPlural(int(j.done.Load()), "row") + " done"
	if j.kind == "export" {
		text = dbPlural(int(j.done.Load()), "row") + " written"
	}
	if j.total > 0 {
		text = fmt.Sprintf("%s / %s rows", dbThousands(int(j.done.Load())), dbThousands(int(j.total)))
	}
	text += " · Esc cancels"
	if j.kind == "import" {
		text += " (nothing is kept)"
	}
	m.confirm = confirm.New(j.title, text, []confirm.Button{{ID: "cancel", Title: "Cancel"}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBJob
}

func (m *Model) onDBJobTick(msg dbJobTickMsg) tea.Cmd {
	s := m.dbConsole()
	if s.job != msg.job {
		return nil // finished: no further tick, so an idle view costs nothing
	}
	if m.confirmOpen && m.confirmKind == confirmKindDBJob {
		m.databaseJobCard(msg.job)
	}
	return dbJobTickCmd(msg.job)
}

// databaseJobCancel stops the running job (Esc on its card, the cancel
// command); false when none runs.
func (m *Model) databaseJobCancel() bool {
	if m.databases == nil || m.databases.console == nil || m.databases.console.job == nil {
		return false
	}
	m.databases.console.job.cancel()
	return true
}

// closeJobCard removes the progress card when it is still the one shown.
func (m *Model) closeJobCard() {
	if m.confirmOpen && m.confirmKind == confirmKindDBJob {
		m.confirmOpen = false
	}
}

// dbThousands writes 12000 as "12 000".
func dbThousands(n int) string {
	s := fmt.Sprint(n)
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ' ')
		}
		out = append(out, s[i])
	}
	return string(out)
}

var dbFileSafe = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// dbExportTable is the table the rows came from: the data view's, the one a
// plain SELECT names, or the name of the first column's table.
func (m *Model) dbExportTable(r *dbRun) (schema, table string) {
	if r.tv != nil {
		return r.tv.schema, r.tv.name
	}
	c, ok := m.databases.connection(r.conn)
	if ok {
		if sel, why := dbclient.SimpleSelect(dbStripComments(r.sql), c.Driver); why == "" {
			return sel.Schema, sel.Table
		}
	}
	if len(r.result.Columns) > 0 && r.result.Columns[0].Table != "" {
		return "", r.result.Columns[0].Table
	}
	return "", ""
}

// databaseExportStart is `s`: the format picker for the result on the page.
func (m *Model) databaseExportStart() tea.Cmd {
	s := m.dbConsole()
	r := s.shown
	if r == nil || r.result.Err != nil || len(r.result.Columns) == 0 {
		return m.pushToastDetail(toast.Info, "Nothing to export", "Run a query that returns rows first.")
	}
	if s.job != nil {
		return m.pushToastDetail(toast.Warn, "Another transfer is running", "Esc cancels it.")
	}
	driver := ""
	if c, ok := m.databases.connection(r.conn); ok {
		driver = c.Driver
	}
	schema, table := m.dbExportTable(r)
	e := &dbExport{res: s.displayedResult(), driver: driver, conn: r.conn, sessKey: r.sessKey, opts: dbclient.ExportOptions{Driver: driver, Schema: schema, Table: table}}
	if e.sessKey == "" {
		e.sessKey = r.conn
	}
	e.loaded = len(e.res.Rows)
	switch {
	case r.tv != nil:
		sql, err := m.dbExportTableSQL(r.tv)
		if err == nil {
			e.sql = sql
		} else {
			e.reason = err.Error()
		}
		if r.tv.more || r.tv.offset > 0 {
			e.partial = "page"
		}
	default:
		e.sql, e.reason = dbExportConsoleSQL(driver, r)
		if r.result.Truncated {
			e.partial = "limit"
		}
	}
	s.exp = e
	var items []picker.Item
	for _, f := range dbclient.ExportFormats {
		items = append(items, picker.Item{ID: string(f.Format), Title: f.Title, Hint: f.Ext})
	}
	m.picker = picker.NewItems("Export rows as…", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBExportFormat
	m.pickerOpen = true
	return nil
}

// dbExportTableSQL is the data view's query without LIMIT / OFFSET (its
// WHERE and ORDER BY stay).
func (m *Model) dbExportTableSQL(tv *dbTableView) (string, error) {
	c := *tv
	c.offset = 0
	return c.selectSQL(0)
}

// dbExportConsoleSQL is the statement an Export all re-runs: only a single
// plain read, never a write that would run twice.
func dbExportConsoleSQL(driver string, r *dbRun) (sql, reason string) {
	text := strings.TrimSpace(dbStripComments(r.sql))
	if dbclient.DialectFor(driver) == nil {
		return "", "This connection does not use SQL."
	}
	kind, stmts := dbclient.Classify(text, driver)
	if kind != dbclient.KindRead || len(stmts) != 1 {
		return "", "Only a plain SELECT can run again without the limit."
	}
	return text, ""
}

func (m *Model) databaseExportFormatPicked(id string) tea.Cmd {
	s := m.dbConsole()
	e := s.exp
	if e == nil {
		return nil
	}
	var info *dbclient.ExportFormatInfo
	for i := range dbclient.ExportFormats {
		if string(dbclient.ExportFormats[i].Format) == id {
			info = &dbclient.ExportFormats[i]
		}
	}
	if info == nil {
		s.exp = nil
		return nil
	}
	e.format = info.Format
	if e.format == dbclient.FormatSQL && e.opts.Table == "" {
		e.opts.Table = "result"
	}
	base := dbFileSafe.ReplaceAllString(e.opts.Table, "_")
	if base == "" {
		base = "result"
	}
	name := base + "-" + clock.Now().Format("2006-01-02") + info.Ext
	return m.openHTTPPrompt(promptKindDBExportPath, "Export "+info.Title, "File (in the workspace, or a full path):", name)
}

// dbResolvePath makes the typed path absolute (relative to the workspace)
// and says how to show it.
func (m *Model) dbResolvePath(p string) (abs, display string, err error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", "", errors.New("Type a file name.")
	}
	if strings.HasPrefix(p, "~/") {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", "", errors.New("The home folder is unknown.")
		}
		p = filepath.Join(home, p[2:])
	}
	root := m.databaseWorkspaceRoot()
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	abs = filepath.Clean(p)
	display = abs
	if rel, rerr := filepath.Rel(root, abs); rerr == nil && !strings.HasPrefix(rel, "..") {
		display = rel
	}
	return abs, display, nil
}

func (m *Model) databaseExportPathTyped(value string) tea.Cmd {
	s := m.dbConsole()
	e := s.exp
	if e == nil {
		return nil
	}
	abs, display, err := m.dbResolvePath(value)
	if err != nil {
		s.exp = nil
		return m.pushToastDetail(toast.Warn, "Cannot export", err.Error())
	}
	if st, serr := os.Stat(abs); serr == nil && st.IsDir() {
		s.exp = nil
		return m.pushToastDetail(toast.Warn, "Cannot export", sanitizeDBText(display)+" is a folder.")
	}
	e.path, e.display = abs, display
	if e.partial == "" {
		return m.databaseExportCheckPath()
	}
	title := "Export rows"
	var msg string
	loadedLabel := "Export " + dbPlural(e.loaded, "row")
	if e.partial == "page" {
		msg = "The data view shows one page (" + dbPlural(e.loaded, "row") + "). The table has more."
		loadedLabel = "Export this page"
	} else {
		msg = fmt.Sprintf("Only the first %s rows are loaded; the query has more.", dbThousands(e.loaded))
		loadedLabel = "Export " + dbThousands(e.loaded) + " loaded rows"
	}
	buttons := []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "loaded", Title: loadedLabel}}
	if e.sql != "" {
		msg += "\n\nExport all runs the query again without the limit and writes the rows as they arrive."
		if s.filter != "" && e.partial == "limit" {
			msg += " The text filter does not apply to it."
		}
		buttons = append(buttons, confirm.Button{ID: "all", Title: "Export all", Style: confirm.StylePrimary})
	} else {
		msg += "\n\nExport all is not possible here: " + e.reason
	}
	m.confirm = confirm.New(title, msg, buttons)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBExportScope
	return nil
}

// databaseExportCheckPath asks before an existing file is replaced.
func (m *Model) databaseExportCheckPath() tea.Cmd {
	s := m.dbConsole()
	e := s.exp
	if st, err := os.Stat(e.path); err == nil && !st.IsDir() {
		m.confirm = confirm.New("Replace the existing file?", sanitizeDBText(e.display)+" already exists.\nIt is replaced only when the export is complete.",
			[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "replace", Title: "Replace", Style: confirm.StyleDestructive}})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindDBExportOverwrite
		return nil
	}
	return m.databaseExportGo()
}

// databaseExportConfirm handles the two export cards.
func (m *Model) databaseExportConfirm(id string) tea.Cmd {
	s := m.dbConsole()
	e := s.exp
	if e == nil {
		return nil
	}
	switch m.confirmKind {
	case confirmKindDBExportScope:
		switch id {
		case "loaded":
			return m.databaseExportCheckPath()
		case "all":
			e.all = true
			return m.databaseExportCheckPath()
		}
	case confirmKindDBExportOverwrite:
		if id == "replace" {
			e.overwrite = true
			return m.databaseExportGo()
		}
	}
	s.exp = nil
	return nil
}

type dbExportDoneMsg struct {
	job  *dbJob
	e    *dbExport
	n    int64
	err  error
	took time.Duration
}

// databaseExportGo writes the file off the UI goroutine.
func (m *Model) databaseExportGo() tea.Cmd {
	s := m.dbConsole()
	e := s.exp
	if e == nil {
		return nil
	}
	if s.job != nil {
		s.exp = nil
		return m.pushToastDetail(toast.Warn, "Another transfer is running", "Esc cancels it.")
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &dbJob{kind: "export", title: "Exporting to " + sanitizeDBText(e.display), conn: e.conn, cancel: cancel}
	s.job = job
	opts := e.opts
	src := func(ctx context.Context, sink dbclient.RowSink) error {
		return dbclient.WriteRows(sink, e.res)
	}
	var tick tea.Cmd
	if e.all {
		sess := m.databases.sessions[e.sessKey]
		if sess == nil {
			s.job = nil
			s.exp = nil
			cancel()
			return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
		}
		query, driver := e.sql, e.driver
		src = func(ctx context.Context, sink dbclient.RowSink) error {
			ro, err := sess.ReadOnlySession()
			if err != nil {
				return err
			}
			defer ro.Close()
			return dbclient.StreamRows(ctx, ro, driver, query, sink)
		}
		m.databaseJobCard(job)
		tick = dbJobTickCmd(job)
	}
	overwrite, path, format := e.overwrite, e.path, e.format
	run := func() tea.Msg {
		start := time.Now()
		n, err := dbclient.ExportToFile(ctx, path, format, opts, overwrite, src, func(n int64) { job.done.Store(n) })
		cancel()
		return dbExportDoneMsg{job: job, e: e, n: n, err: err, took: time.Since(start)}
	}
	return tea.Batch(run, tick)
}

func (m *Model) onDBExportDone(msg dbExportDoneMsg) tea.Cmd {
	s := m.dbConsole()
	if s.job != msg.job {
		return nil
	}
	s.job, s.exp = nil, nil
	m.closeJobCard()
	where := sanitizeDBText(msg.e.display)
	switch {
	case errors.Is(msg.err, context.Canceled):
		return m.pushToastDetail(toast.Info, "Export cancelled", "No file was written.")
	case errors.Is(msg.err, dbclient.ErrExportExists):
		return m.pushToastDetail(toast.Warn, "Export stopped", where+" appeared while the export ran. Nothing was replaced.")
	case msg.err != nil:
		return m.pushToastDetail(toast.Errr, "Export failed", dbErrorText(msg.err)+" · No file was written.")
	}
	return m.pushToastDetail(toast.Info, "Exported "+dbPlural(int(msg.n), "row"), fmt.Sprintf("%s · %d ms", where, msg.took.Milliseconds()))
}

// dbExportPicked and the other hooks below are called from the shared
// confirm / picker / prompt routers.
func (m *Model) databaseTransferPicker(id string) (tea.Cmd, bool) {
	switch m.pickerKind {
	case pickerKindDBExportFormat:
		return m.databaseExportFormatPicked(id), true
	case pickerKindDBImportFile, pickerKindDBImportMapCol, pickerKindDBImportMapSrc:
		return m.databaseImportPicker(id), true
	}
	return nil, false
}

func (m *Model) databaseTransferConfirm(id string) (tea.Cmd, bool) {
	switch m.confirmKind {
	case confirmKindDBExportScope, confirmKindDBExportOverwrite:
		return m.databaseExportConfirm(id), true
	case confirmKindDBImport:
		return m.databaseImportConfirm(id), true
	case confirmKindDBJob:
		m.databaseJobCancel()
		return nil, true
	case confirmKindDBInfo:
		return nil, true
	}
	return nil, false
}

func (m *Model) databaseTransferPrompt(kind promptKindEnum, value string) (tea.Cmd, bool) {
	switch kind {
	case promptKindDBExportPath:
		return m.databaseExportPathTyped(value), true
	case promptKindDBImportPath:
		return m.databaseImportLoad(value), true
	}
	return nil, false
}

// databaseTransferClosed runs when Esc closes a card: it drops what the card
// was for, or cancels the running job.
func (m *Model) databaseTransferClosed() {
	if m.databases == nil || m.databases.console == nil {
		return
	}
	s := m.databases.console
	switch m.confirmKind {
	case confirmKindDBJob:
		m.databaseJobCancel()
	case confirmKindDBExportScope, confirmKindDBExportOverwrite:
		s.exp = nil
	case confirmKindDBImport:
		s.imp = nil
	}
}

// databaseTransferPickerClosed handles Esc in the transfer pickers: the
// mapping pickers go back to the card, the others end the transfer.
func (m *Model) databaseTransferPickerClosed() (tea.Cmd, bool) {
	if m.databases == nil || m.databases.console == nil {
		return nil, false
	}
	s := m.databases.console
	switch m.pickerKind {
	case pickerKindDBImportMapCol, pickerKindDBImportMapSrc:
		return m.databaseImportCard(), true
	case pickerKindDBImportFile:
		s.imp = nil
		return nil, true
	case pickerKindDBExportFormat:
		s.exp = nil
		return nil, true
	}
	return nil, false
}

// dbSessionBusy: an import holds its session, so nothing else may start a
// statement (or end the transaction) on it until it ends.
func (m *Model) dbSessionBusy() bool {
	return m.databases != nil && m.databases.console != nil && m.databases.console.job != nil && m.databases.console.job.blocks
}
