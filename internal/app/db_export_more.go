package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Export settings and extractors (DBX-36): the export picker lists every
// format (JSON lines, YAML, XML and SQL UPDATEs too); the CSV settings
// (db_csv_separator, db_csv_header, db_csv_null) shape delimited files;
// db_export_mask hides the values of named columns in exports and copies;
// columns hidden in the grid are left out; A on a result page copies the
// rows in a picked format; Databases: Export Tables to Folder… writes every
// table of a schema as CSV plus schema.sql (the DDL), never over a file.

const (
	pickerKindDBCopyAs        pickerKindEnum  = 5342
	promptKindDBExportFolder  promptKindEnum  = 5342
	confirmKindDBExportFolder confirmKindEnum = 5342
)

// dbMaskText replaces a masked value.
const dbMaskText = "••••"

// dbExportFormatList is the picker's list: the main formats, then the rest.
func dbExportFormatList() []dbclient.ExportFormatInfo {
	return append(append([]dbclient.ExportFormatInfo(nil), dbclient.ExportFormats...), dbclient.MoreExportFormats...)
}

func dbExportFormatInfo(id string) (dbclient.ExportFormatInfo, bool) {
	for _, f := range dbExportFormatList() {
		if string(f.Format) == id {
			return f, true
		}
	}
	return dbclient.ExportFormatInfo{}, false
}

// exportOptions adds the CSV settings to o.
func (s dbConsoleSettings) exportOptions(o dbclient.ExportOptions) dbclient.ExportOptions {
	if s.CSVSeparator != nil {
		if r := []rune(*s.CSVSeparator); len(r) == 1 {
			o.Separator = r[0]
		} else if *s.CSVSeparator == `\t` {
			o.Separator = '\t'
		}
	}
	if s.CSVHeader != nil && !*s.CSVHeader {
		o.NoHeader = true
	}
	if s.CSVNull != nil {
		o.NullText = *s.CSVNull
	}
	return o
}

// dbMaskMatch says whether a column name matches one of the patterns (any
// case; * and ? as in a file glob).
func dbMaskMatch(patterns []string, name string) bool {
	n := strings.ToLower(name)
	for _, p := range patterns {
		if ok, err := path.Match(strings.ToLower(strings.TrimSpace(p)), n); err == nil && ok {
			return true
		}
	}
	return false
}

// dbMaskResult is a copy of r with the matching columns' values masked.
func dbMaskResult(r dbclient.Result, patterns []string) dbclient.Result {
	idx := dbMaskIndexes(r.Columns, patterns)
	if len(idx) == 0 {
		return r
	}
	out := r
	out.Rows = make([][]dbclient.Cell, len(r.Rows))
	for i, row := range r.Rows {
		out.Rows[i] = dbMaskRow(row, idx)
	}
	return out
}

func dbMaskIndexes(cols []dbclient.Column, patterns []string) []int {
	var idx []int
	for i, c := range cols {
		if dbMaskMatch(patterns, c.Name) {
			idx = append(idx, i)
		}
	}
	return idx
}

func dbMaskRow(row []dbclient.Cell, idx []int) []dbclient.Cell {
	out := append([]dbclient.Cell(nil), row...)
	for _, i := range idx {
		if i < len(out) && !out[i].Null {
			out[i] = dbclient.Cell{Text: dbMaskText}
		}
	}
	return out
}

// dbMaskSink masks the rows a streamed export writes.
type dbMaskSink struct {
	dbclient.RowSink
	patterns []string
	idx      []int
}

func (s *dbMaskSink) Columns(cols []dbclient.Column) error {
	s.idx = dbMaskIndexes(cols, s.patterns)
	return s.RowSink.Columns(cols)
}

func (s *dbMaskSink) Row(row []dbclient.Cell) error {
	if len(s.idx) > 0 {
		row = dbMaskRow(row, s.idx)
	}
	return s.RowSink.Row(row)
}

// dbMasked wraps sink when masking is set.
func dbMasked(sink dbclient.RowSink, patterns []string) dbclient.RowSink {
	if len(patterns) == 0 {
		return sink
	}
	return &dbMaskSink{RowSink: sink, patterns: patterns}
}

var errDBUpdateNoKey = errors.New("SQL UPDATEs need a key: open the table's data, or select its primary key columns")

// dbCopyAsText writes r in format f.
func dbCopyAsText(r dbclient.Result, f dbclient.ExportFormat, o dbclient.ExportOptions) (string, error) {
	if f == dbclient.FormatSQLUpdate && len(o.KeyColumns) == 0 {
		return "", errDBUpdateNoKey
	}
	if (f == dbclient.FormatSQL || f == dbclient.FormatSQLUpdate) && o.Table == "" {
		o.Table = "result"
	}
	var b bytes.Buffer
	w, err := dbclient.NewRowWriter(f, &b, o)
	if err != nil {
		return "", err
	}
	if err := dbclient.WriteRows(w, r); err != nil {
		return "", err
	}
	if err := w.End(); err != nil {
		return "", err
	}
	return b.String(), nil
}

// dbResultKeys are the key columns of the result on the page: the data
// view's keys, else the result's own key.
func dbResultKeys(r *dbRun) []string {
	if r.tv != nil && len(r.tv.keys) > 0 {
		return append([]string(nil), r.tv.keys...)
	}
	var out []string
	for _, i := range r.result.Key {
		if i >= 0 && i < len(r.result.Columns) {
			out = append(out, r.result.Columns[i].Name)
		}
	}
	return out
}

// dbShownColumns are the names of the columns the grid shows when some are
// hidden (nil: all are shown, so all are exported).
func (s *dbConsoleState) dbShownColumns(cols []dbclient.Column) []string {
	vis := s.visibleCols(cols)
	if len(vis) == len(cols) {
		return nil
	}
	var out []string
	for _, i := range vis {
		out = append(out, cols[i].Name)
	}
	return out
}

// dbExportPrepare fills the settings, the key columns and the shown columns
// into an export of the result on the page.
func (m *Model) dbExportPrepare(o dbclient.ExportOptions) dbclient.ExportOptions {
	s := m.dbConsole()
	o = loadDBConsoleSettings().exportOptions(o)
	if s.shown != nil {
		o.KeyColumns = dbResultKeys(s.shown)
		o.Columns = s.dbShownColumns(s.shown.result.Columns)
	}
	return o
}

// ── copy as ─────────────────────────────────────────────────────────────

// databaseCopyAsStart is A: the format picker for a copy of the rows.
func (m *Model) databaseCopyAsStart() tea.Cmd {
	s := m.dbConsole()
	if s.shown == nil || s.shown.result.Err != nil || len(s.shown.result.Columns) == 0 {
		return m.pushToastDetail(toast.Info, "Nothing to copy", "Run a query that returns rows first.")
	}
	var items []picker.Item
	for _, f := range dbExportFormatList() {
		items = append(items, picker.Item{ID: string(f.Format), Title: f.Title, Hint: "copy " + dbPlural(len(s.displayedResult().Rows), "row")})
	}
	m.picker = picker.NewItems("Copy rows as…", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBCopyAs
	m.pickerOpen = true
	return nil
}

func (m *Model) databaseCopyAsPicked(id string) tea.Cmd {
	s := m.dbConsole()
	info, ok := dbExportFormatInfo(id)
	if !ok || s.shown == nil {
		return nil
	}
	driver := ""
	if c, ok := m.databases.connection(s.shown.conn); ok {
		driver = c.Driver
	}
	schema, table := m.dbExportTable(s.shown)
	o := m.dbExportPrepare(dbclient.ExportOptions{Driver: driver, Schema: schema, Table: table})
	res := dbMaskResult(s.displayedResult(), loadDBConsoleSettings().exportMask())
	text, err := dbCopyAsText(res, info.Format, o)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Cannot copy as "+info.Title, sanitizeDBText(err.Error()))
	}
	return m.copyWithToast(dbRedact(text), "Copied "+dbPlural(len(res.Rows), "row")+" as "+info.Title)
}

// ── export tables to a folder ───────────────────────────────────────────

// dbFolderExport is an Export Tables to Folder in progress.
type dbFolderExport struct {
	tgt    dbSearchTarget
	folder string // absolute
	show   string
}

type dbFolderReport struct {
	written int
	rows    int64
	skipped []string // a file was there
	failed  []string
}

type dbFolderDoneMsg struct {
	job *dbJob
	x   dbFolderExport
	rep dbFolderReport
	err error
}

// dbExportTables writes every table of tgt to folder as <table>.csv and
// the DDL to schema.sql, in one read-only session; an existing file is
// left alone and counted as skipped.
func dbExportTables(ctx context.Context, sess dbclient.Session, tgt dbSearchTarget, folder string, o dbclient.ExportOptions, progress func(done, total int)) (dbFolderReport, error) {
	var rep dbFolderReport
	ro, err := sess.ReadOnlySession()
	if err != nil {
		return rep, err
	}
	defer ro.Close()
	d := dbclient.DialectFor(tgt.driver)
	if d == nil {
		return rep, errDBNotSQL
	}
	db := dbAncestorDatabase(tgt.path)
	if tgt.driver == "mysql" {
		db = ""
	}
	cat, err := ro.Introspect(ctx, dbclient.IntrospectionRequest{Database: db, Schema: tgt.schema})
	if err != nil {
		return rep, err
	}
	var tables []string
	for _, ob := range cat.Objects {
		if !strings.Contains(strings.ToLower(ob.Kind), "view") {
			tables = append(tables, ob.Name)
		}
	}
	mask := loadDBConsoleSettings().exportMask()
	var ddl strings.Builder
	for i, name := range tables {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		if progress != nil {
			progress(i, len(tables))
		}
		if ir, err := ro.Introspect(ctx, dbclient.IntrospectionRequest{Database: db, Schema: tgt.schema, Object: name, Kind: "table"}); err == nil && strings.TrimSpace(ir.DDL) != "" {
			ddl.WriteString(strings.TrimSpace(ir.DDL) + "\n\n")
		}
		qualified := d.QuoteIdent(name)
		if tgt.schema != "" && tgt.driver != "sqlite" {
			qualified = d.QuoteIdent(tgt.schema) + "." + qualified
		}
		file := dbFileSafe.ReplaceAllString(name, "_")
		if file == "" || file == "." || file == ".." {
			file = "table"
		}
		opts := o
		opts.Driver, opts.Schema, opts.Table = tgt.driver, tgt.schema, name
		n, err := dbclient.ExportToFile(ctx, filepath.Join(folder, file+".csv"), dbclient.FormatCSV, opts, false, func(ctx context.Context, sink dbclient.RowSink) error {
			return dbclient.StreamRows(ctx, ro, tgt.driver, "SELECT * FROM "+qualified, dbMasked(sink, mask))
		}, nil)
		switch {
		case errors.Is(err, dbclient.ErrExportExists):
			rep.skipped = append(rep.skipped, name)
		case ctx.Err() != nil:
			return rep, ctx.Err()
		case err != nil:
			rep.failed = append(rep.failed, name)
		default:
			rep.written++
			rep.rows += n
		}
	}
	if progress != nil {
		progress(len(tables), len(tables))
	}
	if ddl.Len() > 0 {
		p := filepath.Join(folder, "schema.sql")
		if _, err := os.Stat(p); err == nil {
			rep.skipped = append(rep.skipped, "schema.sql")
		} else if err := os.WriteFile(p, []byte(ddl.String()), 0o644); err != nil {
			rep.failed = append(rep.failed, "schema.sql")
		}
	}
	return rep, nil
}

// databaseExportTablesStart is Databases: Export Tables to Folder…: the
// schema from the tree, then the folder.
func (m *Model) databaseExportTablesStart() tea.Cmd {
	m.ensureDatabasesLoaded()
	row, ok := m.databaseCurrentRow()
	if !ok {
		return m.pushToastDetail(toast.Info, "Select a schema", "Move to a schema, a database or a table in the tree first.")
	}
	tgt, err := dbSearchTargetAt(m.databases, row)
	if err != nil {
		return m.pushToastDetail(toast.Info, "Cannot export here", sanitizeDBText(err.Error()))
	}
	if m.databases.sessions[tgt.sessKey] == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	s := m.dbConsole()
	if s.job != nil {
		return m.pushToastDetail(toast.Warn, "Another job is running", "Esc cancels it.")
	}
	s.folder = &dbFolderExport{tgt: tgt}
	name := dbFileSafe.ReplaceAllString(tgt.conn+"-"+tgt.schema, "_") + "-export"
	return m.openHTTPPrompt(promptKindDBExportFolder, "Export Tables of "+tgt.label(), "Folder (made if missing; files there are kept):", name)
}

func (m *Model) databaseExportFolderTyped(value string) tea.Cmd {
	s := m.dbConsole()
	x := s.folder
	if x == nil {
		return nil
	}
	abs, display, err := m.dbResolvePath(value)
	if err != nil {
		s.folder = nil
		return m.pushToastDetail(toast.Warn, "Cannot export", err.Error())
	}
	if st, err := os.Stat(abs); err == nil && !st.IsDir() {
		s.folder = nil
		return m.pushToastDetail(toast.Warn, "Cannot export", sanitizeDBText(display)+" is a file.")
	}
	x.folder, x.show = abs, display
	msg := "Every table of " + x.tgt.label() + " is written as <table>.csv, and the tables' DDL as schema.sql, into " + sanitizeDBText(display) + ".\n\nThe rows are read in a read-only session. A file that is already there is kept and skipped."
	if len(loadDBConsoleSettings().exportMask()) > 0 {
		msg += " Masked columns (db_export_mask) are written as " + dbMaskText + "."
	}
	m.confirm = confirm.New("Export tables to a folder?", msg, []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "export", Title: "Export", Style: confirm.StylePrimary}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBExportFolder
	return nil
}

func (m *Model) databaseExportFolderConfirm(id string) tea.Cmd {
	s := m.dbConsole()
	x := s.folder
	s.folder = nil
	if x == nil || id != "export" {
		return nil
	}
	if s.job != nil {
		return m.pushToastDetail(toast.Warn, "Another job is running", "Esc cancels it.")
	}
	sess := m.databases.sessions[x.tgt.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	if err := os.MkdirAll(x.folder, 0o755); err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot make the folder", sanitizeDBText(err.Error()))
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &dbJob{kind: "tables", title: "Exporting " + x.tgt.label() + " to " + sanitizeDBText(x.show), conn: x.tgt.conn, cancel: cancel}
	s.job = job
	m.databaseJobCard(job)
	opts := loadDBConsoleSettings().exportOptions(dbclient.ExportOptions{})
	run := func() tea.Msg {
		defer cancel()
		rep, err := dbExportTables(ctx, sess, x.tgt, x.folder, opts, func(done, total int) {
			job.of.Store(int64(total))
			job.done.Store(int64(done))
		})
		return dbFolderDoneMsg{job: job, x: *x, rep: rep, err: err}
	}
	return tea.Batch(run, dbJobTickCmd(job))
}

func (m *Model) onDBFolderDone(msg dbFolderDoneMsg) tea.Cmd {
	s := m.dbConsole()
	if s.job != msg.job {
		return nil
	}
	s.job = nil
	m.closeJobCard()
	rep := msg.rep
	switch {
	case errors.Is(msg.err, context.Canceled):
		return m.pushToastDetail(toast.Info, "Export cancelled", fmt.Sprintf("%s written before it stopped.", dbPlural(rep.written, "table")))
	case msg.err != nil:
		return m.pushToastDetail(toast.Errr, "Export failed", dbErrorText(msg.err))
	}
	note := fmt.Sprintf("%s · %s rows · %s", sanitizeDBText(msg.x.show), dbThousands(int(rep.rows)), time.Now().Format("15:04"))
	if len(rep.skipped) > 0 {
		note += " · kept (already there): " + sanitizeDBText(strings.Join(rep.skipped, ", "))
	}
	level := toast.Info
	if len(rep.failed) > 0 {
		level = toast.Warn
		note += " · failed: " + sanitizeDBText(strings.Join(rep.failed, ", "))
	}
	return m.pushToastDetail(level, "Exported "+dbPlural(rep.written, "table"), note)
}
