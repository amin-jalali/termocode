package app

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Import CSV (plan F10): `i` on a table in the tree, or Databases: Import
// CSV into Table… → a picker of the workspace's .csv files (or a typed
// path) → a preview card (encoding, delimiter, header, row count, the
// column mapping, the first rows) → Import. The file is read once, one row
// at a time, and goes in as bound INSERTs inside one transaction: the first
// bad row rolls everything back and is named. A read-only connection never
// imports; production gets the strong card; an open manual transaction
// takes the rows into it (Commit saves them). The AI cannot import.

// dbImport is the import being prepared or run.
type dbImport struct {
	t       dbTable
	conn    dbclient.Conn
	path    string
	display string
	sniff   dbclient.CSVSniff
	records int // records in the file, header included
	cols    []string
	mapping []int // per table column: the file column, or -1
	mapCol  int   // the table column the mapping picker is changing
	files   []string
	sessKey string
	skipBad bool // Bad rows: skip (left out and logged) instead of stop
	// more are the other files of a several-files import (absolute; the
	// first is path); records then counts the records of all of them.
	more []string
	// opts are the reading options (Options… on the card) and formats the
	// per-column formats, one per table column.
	opts    dbclient.CSVOptions
	formats []dbclient.ColumnFormat
	fmtCol  int // the table column the format picker is changing
}

// files is how many files the import reads.
func (imp *dbImport) fileCount() int { return 1 + len(imp.more) }

const (
	dbImportTypePath = "\x00path"
	dbImportTypeGlob = "\x00glob"
)

var (
	dbGlyphFrom = theme.Icon{NerdFont: "←", Unicode: "←", ASCII: "<-"}
	dbGlyphInto = theme.Icon{NerdFont: "→", Unicode: "→", ASCII: "->"}
)

// databaseImportStart is `i`: it checks the target, then looks for csv files.
func (m *Model) databaseImportStart() tea.Cmd {
	t, cmd, ok := m.databaseTableTarget()
	if !ok {
		return cmd
	}
	s := m.dbConsole()
	c, _ := m.databases.connection(t.conn)
	switch {
	case t.view:
		return m.pushToastDetail(toast.Info, "Cannot import into a view", "Select a table.")
	case c.ReadOnly:
		return m.pushToastDetail(toast.Warn, "Cannot import", "The connection is read-only.")
	case s.job != nil || s.running != nil || s.submitting != nil:
		return m.pushToastDetail(toast.Warn, "A query or transfer is running", "Esc cancels it.")
	}
	s.imp = &dbImport{t: t, conn: c, sessKey: t.sessKey}
	root := m.databaseWorkspaceRoot()
	imp := s.imp
	return func() tea.Msg { return dbImportFilesMsg{imp: imp, files: dbFindCSV(root)} }
}

type dbImportFilesMsg struct {
	imp   *dbImport
	files []string
}

// dbFindCSV lists the .csv files of the workspace (a few folders deep, no
// hidden or dependency folders), at most 200.
func dbFindCSV(root string) []string {
	var out []string
	visited := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		visited++
		if visited > 5000 || len(out) >= 200 {
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			name := d.Name()
			if p != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") || strings.Count(rel, string(filepath.Separator)) >= 4 {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".csv") {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func (m *Model) onDBImportFiles(msg dbImportFilesMsg) tea.Cmd {
	s := m.dbConsole()
	if s.imp != msg.imp {
		return nil
	}
	imp := msg.imp
	imp.files = msg.files
	if len(msg.files) == 0 {
		return m.openHTTPPrompt(promptKindDBImportPath, "Import CSV into "+imp.t.label(), "CSV file (in the workspace, or a full path):", "")
	}
	var items []picker.Item
	for _, f := range msg.files {
		items = append(items, picker.Item{ID: f, Title: sanitizeDBText(f)})
	}
	items = append(items, picker.Item{ID: dbImportTypePath, Title: "Type a path…"},
		picker.Item{ID: dbImportTypeGlob, Title: "Several files (a pattern)…", Hint: "for example data/*.csv, one transaction"})
	m.picker = picker.NewItems("Import CSV into "+imp.t.label(), items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBImportFile
	m.pickerOpen = true
	return nil
}

type dbImportPreviewMsg struct {
	imp     *dbImport
	sniff   dbclient.CSVSniff
	records int
	cols    []string
	err     error
	reload  bool // Options… changed: keep the columns and the mapping
}

// databaseImportLoad reads the start of the file, counts its rows and reads
// the table's columns, off the UI goroutine.
func (m *Model) databaseImportLoad(value string) tea.Cmd {
	s := m.dbConsole()
	imp := s.imp
	if imp == nil {
		return nil
	}
	abs, display, err := m.dbResolvePath(value)
	if err != nil {
		s.imp = nil
		return m.pushToastDetail(toast.Warn, "Cannot import", err.Error())
	}
	sess := m.databases.sessions[imp.sessKey]
	if sess == nil {
		s.imp = nil
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	imp.path, imp.display, imp.more = abs, display, nil
	return m.databaseImportRead(imp, sess, nil)
}

// databaseImportRead sniffs the first file with the import's options,
// counts the records of every file and (the first time) reads the table's
// columns, off the UI goroutine. header != nil keeps the user's header
// choice (a reload after Options…).
func (m *Model) databaseImportRead(imp *dbImport, sess dbclient.Session, header *bool) tea.Cmd {
	req := imp.t.request()
	paths := append([]string{imp.path}, imp.more...)
	opts := imp.opts
	needCols := len(imp.cols) == 0
	return tea.Batch(m.pushToast(toast.Info, "Reading "+sanitizeDBText(imp.display)+"…"), func() tea.Msg {
		msg := dbImportPreviewMsg{imp: imp, reload: !needCols}
		sn, err := dbclient.SniffCSVWith(paths[0], opts)
		if err != nil {
			msg.err = err
			return msg
		}
		if header != nil {
			sn = sn.WithHeader(*header)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		for _, p := range paths {
			n, err := dbclient.CountCSVRows(ctx, p, sn)
			if err != nil {
				if len(paths) > 1 {
					err = fmt.Errorf("%s: %w", filepath.Base(p), err)
				}
				msg.err = err
				return msg
			}
			msg.records += n
			if sn.Header {
				msg.records++
			}
		}
		msg.sniff = sn
		if !needCols {
			return msg
		}
		mctx, mcancel := context.WithTimeout(context.Background(), dbMetaTimeout)
		defer mcancel()
		ir, err := sess.Introspect(mctx, req)
		if err != nil {
			msg.err = err
			return msg
		}
		for _, c := range ir.Columns {
			msg.cols = append(msg.cols, c.Name)
		}
		return msg
	})
}

func (m *Model) onDBImportPreview(msg dbImportPreviewMsg) tea.Cmd {
	s := m.dbConsole()
	if s.imp != msg.imp {
		return nil
	}
	imp := msg.imp
	if msg.err != nil && msg.reload {
		// The options do not fit the file: say so and keep the old ones.
		imp.opts = imp.sniff.Options
		return tea.Batch(m.pushToastDetail(toast.Warn, "Cannot read "+sanitizeDBText(imp.display)+" that way", dbErrorText(msg.err)), m.databaseImportCard())
	}
	if msg.err != nil {
		s.imp = nil
		return m.pushToastDetail(toast.Warn, "Cannot import "+sanitizeDBText(imp.display), dbErrorText(msg.err))
	}
	if msg.reload {
		sameCols := msg.sniff.Fields == imp.sniff.Fields && strings.Join(msg.sniff.Columns, "\x00") == strings.Join(imp.sniff.Columns, "\x00")
		imp.sniff, imp.records = msg.sniff, msg.records
		if !sameCols {
			imp.mapping = dbclient.AutoMap(imp.cols, imp.sniff)
		}
		return m.databaseImportCard()
	}
	if len(msg.cols) == 0 {
		s.imp = nil
		return m.pushToastDetail(toast.Warn, "Cannot import", "The columns of "+imp.t.label()+" could not be read.")
	}
	imp.sniff, imp.records, imp.cols = msg.sniff, msg.records, msg.cols
	imp.mapping = dbclient.AutoMap(imp.cols, imp.sniff)
	imp.formats = make([]dbclient.ColumnFormat, len(imp.cols))
	return m.databaseImportCard()
}

func (imp *dbImport) rows() int {
	n := imp.records
	if imp.sniff.Header {
		n -= imp.fileCount()
	}
	return max(n, 0)
}

func (imp *dbImport) mapped() int {
	n := 0
	for _, v := range imp.mapping {
		if v >= 0 {
			n++
		}
	}
	return n
}

func dbCSVDelimiter(r rune) string {
	if r == '\t' {
		return "tab"
	}
	return `"` + string(r) + `"`
}

// dbImportCardText is the preview: file facts, what the import does, the
// mapping, then the first rows (the card scrolls; the facts come first).
func (m *Model) dbImportCardText(imp *dbImport) string {
	var b strings.Builder
	header := "no header"
	if imp.sniff.Header {
		header = "header row"
	}
	fmt.Fprintf(&b, "%s · %s · delimiter %s · %s · %s\n", sanitizeDBText(imp.display), imp.sniff.Encoding, dbCSVDelimiter(imp.sniff.Delimiter), header, dbPlural(imp.rows(), "row"))
	if imp.fileCount() > 1 {
		fmt.Fprintf(&b, "%s, read one after another; the delimiter, header and options of the first apply to all\n", dbPlural(imp.fileCount(), "file"))
	}
	if o := dbImportOptionsText(imp.sniff.Options); o != "" {
		b.WriteString("Options: " + o + "\n")
	}
	fmt.Fprintf(&b, "Into %s on %s\n", imp.t.label(), sanitizeDBText(dbConnName(imp.conn)))
	if imp.skipBad {
		b.WriteString("Rows are inserted in groups, in one transaction.\nRows that fail are skipped and listed in an error log. Empty cells become NULL.\n")
	} else {
		b.WriteString("Rows are inserted in groups, in one transaction;\nnothing is written if any row fails. Empty cells become NULL.\n")
	}
	if m.databases.manualMode(imp.t.conn) {
		b.WriteString("The rows join your open transaction: Commit (K) saves them.\n")
	}
	if imp.mapped() == 0 {
		b.WriteString("Map at least one column to import.\n")
	}
	// The first rows come before the mapping: the card shows only a few
	// lines (it scrolls), and the rows are what people check first.
	if sample := imp.sniff.Sample; len(sample) > 0 {
		sample = sample[:min(len(sample), 3)]
		b.WriteString("First " + dbPlural(len(sample), "row") + "\n")
		for _, row := range sample {
			cells := make([]string, len(row))
			for j, c := range row {
				cells[j] = ansi.Truncate(dbOneLine(c), 16, "…")
			}
			b.WriteString("  " + strings.Join(cells, " | ") + "\n")
		}
	}
	b.WriteString("Mapping (file column " + dbGlyphInto.String() + " table column)\n")
	width := 0
	for _, c := range imp.cols {
		width = max(width, ansi.StringWidth(dbOneLine(c)))
	}
	width = min(width, 24)
	for i, c := range imp.cols {
		from := "(skipped)"
		if imp.mapping[i] >= 0 && imp.mapping[i] < len(imp.sniff.Columns) {
			from = dbOneLine(imp.sniff.Columns[imp.mapping[i]])
		}
		name := ansi.Truncate(dbOneLine(c), width, "…")
		format := ""
		if i < len(imp.formats) && !imp.formats[i].IsZero() && imp.mapping[i] >= 0 {
			format = "  [" + dbOneLine(imp.formats[i].Describe()) + "]"
		}
		fmt.Fprintf(&b, "  %s%s %s %s%s\n", name, strings.Repeat(" ", width-ansi.StringWidth(name)), dbGlyphFrom.String(), from, format)
	}
	return strings.TrimRight(b.String(), "\n")
}

// databaseImportCard shows (or refreshes) the preview card.
func (m *Model) databaseImportCard() tea.Cmd {
	s := m.dbConsole()
	imp := s.imp
	if imp == nil {
		return nil
	}
	name := sanitizeDBText(dbConnName(imp.conn))
	title := fmt.Sprintf("Import %s into %s", dbPlural(imp.rows(), "row"), imp.t.label())
	if imp.fileCount() > 1 {
		title = fmt.Sprintf("Import %s from %s into %s", dbPlural(imp.rows(), "row"), dbPlural(imp.fileCount(), "file"), imp.t.label())
	}
	opts := confirm.Button{ID: "options", Title: "Options…"}
	hdr := "Header: off"
	if imp.sniff.Header {
		hdr = "Header: on"
	}
	bad := confirm.Button{ID: "bad-rows", Title: "Bad rows: stop"}
	if imp.skipBad {
		bad.Title = "Bad rows: skip"
	}
	importBtn := confirm.Button{ID: "import", Title: "Import", Style: confirm.StylePrimary}
	buttons := []confirm.Button{importBtn, {ID: "map", Title: "Map columns…"}, {ID: "header", Title: hdr}, bad, opts, {ID: "cancel", Title: "Cancel"}}
	if imp.mapped() == 0 {
		buttons = buttons[1:]
	}
	if imp.conn.Environment == "production" {
		title = "Write to production (" + name + ")? " + title
		importBtn.Style = confirm.StyleDestructive
		buttons = []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "map", Title: "Map columns…"}, {ID: "header", Title: hdr}, bad, opts}
		if imp.mapped() > 0 {
			buttons = append(buttons, importBtn)
		}
	}
	m.confirm = confirm.New(title, m.dbImportCardText(imp), buttons).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBImport
	return nil
}

func (m *Model) databaseImportConfirm(id string) tea.Cmd {
	s := m.dbConsole()
	imp := s.imp
	if imp == nil {
		return nil
	}
	switch id {
	case "import":
		return m.databaseImportRun()
	case "header":
		imp.sniff = imp.sniff.WithHeader(!imp.sniff.Header)
		imp.mapping = dbclient.AutoMap(imp.cols, imp.sniff)
		return m.databaseImportCard()
	case "map":
		return m.databaseImportMapPicker()
	case "bad-rows":
		imp.skipBad = !imp.skipBad
		return m.databaseImportCard()
	case "options":
		return m.databaseImportOptionsPicker()
	}
	s.imp = nil
	return nil
}

func (m *Model) databaseImportMapPicker() tea.Cmd {
	imp := m.dbConsole().imp
	var items []picker.Item
	for i, c := range imp.cols {
		from := "(skipped)"
		if imp.mapping[i] >= 0 && imp.mapping[i] < len(imp.sniff.Columns) {
			from = dbOneLine(imp.sniff.Columns[imp.mapping[i]])
		}
		items = append(items, picker.Item{ID: strconv.Itoa(i), Title: dbOneLine(c), Hint: dbGlyphFrom.String() + " " + from})
	}
	m.picker = picker.NewItems("Map columns (Enter changes one, Esc is done)", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBImportMapCol
	m.pickerOpen = true
	return nil
}

func (m *Model) databaseImportPicker(id string) tea.Cmd {
	s := m.dbConsole()
	imp := s.imp
	if imp == nil {
		return nil
	}
	switch m.pickerKind {
	case pickerKindDBImportFile:
		if id == dbImportTypePath {
			return m.openHTTPPrompt(promptKindDBImportPath, "Import CSV into "+imp.t.label(), "CSV file (in the workspace, or a full path):", "")
		}
		if id == dbImportTypeGlob {
			return m.openHTTPPrompt(promptKindDBImportGlob, "Import CSV files into "+imp.t.label(), "Files (a pattern like data/*.csv; * and ? match):", "")
		}
		return m.databaseImportLoad(id)
	case pickerKindDBImportMapCol:
		col, err := strconv.Atoi(id)
		if err != nil || col < 0 || col >= len(imp.cols) {
			return m.databaseImportCard()
		}
		imp.mapCol = col
		items := []picker.Item{{ID: "-1", Title: "(skip this column)", Hint: "gets its default or NULL"}}
		for j, name := range imp.sniff.Columns {
			hint := ""
			if len(imp.sniff.Sample) > 0 && j < len(imp.sniff.Sample[0]) {
				hint = ansi.Truncate(dbOneLine(imp.sniff.Sample[0][j]), 24, "…")
			}
			items = append(items, picker.Item{ID: strconv.Itoa(j), Title: dbOneLine(name), Hint: hint})
		}
		m.picker = picker.NewItems("File column for "+dbOneLine(imp.cols[col]), items)
		m.picker.SetSize(m.w, m.h)
		m.pickerKind = pickerKindDBImportMapSrc
		m.pickerOpen = true
		return nil
	default: // pickerKindDBImportMapSrc
		src, err := strconv.Atoi(id)
		if err != nil {
			return m.databaseImportMapPicker()
		}
		imp.mapping[imp.mapCol] = src
		return m.databaseImportMapPicker()
	}
}

type dbImportDoneMsg struct {
	job   *dbJob
	imp   *dbImport
	res   dbclient.ImportResult
	began bool
	key   string
	sess  dbclient.Session
}

// databaseImportRun starts the import on the table's session.
func (m *Model) databaseImportRun() tea.Cmd {
	s := m.dbConsole()
	imp := s.imp
	if imp == nil || imp.mapped() == 0 {
		return nil
	}
	if s.job != nil || s.running != nil || s.submitting != nil {
		s.imp = nil
		return m.pushToastDetail(toast.Warn, "A query or transfer is running", "Esc cancels it.")
	}
	sess := m.databases.sessions[imp.sessKey]
	if sess == nil {
		s.imp = nil
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	d := dbclient.DialectFor(imp.t.driver)
	if d == nil {
		s.imp = nil
		return m.pushToastDetail(toast.Warn, "Cannot import", "This connection does not use SQL.")
	}
	var targets []string
	var sources []int
	var formats []dbclient.ColumnFormat
	for i, c := range imp.cols {
		if imp.mapping[i] >= 0 {
			targets, sources = append(targets, c), append(sources, imp.mapping[i])
			var f dbclient.ColumnFormat
			if i < len(imp.formats) {
				f = imp.formats[i]
			}
			formats = append(formats, f)
		}
	}
	spec := dbclient.ImportSpec{Path: imp.path, MorePaths: imp.more, Sniff: imp.sniff, Dialect: d, Schema: imp.t.schema, Table: imp.t.name, Targets: targets, Sources: sources, SkipBad: imp.skipBad, Formats: formats}
	manual := m.databases.manualMode(imp.t.conn)
	open := m.databases.txOpen(imp.sessKey)
	spec.InTx = manual
	ctx, cancel := context.WithCancel(context.Background())
	job := &dbJob{kind: "import", title: "Importing into " + imp.t.label(), conn: imp.t.conn, total: int64(imp.rows()), cancel: cancel, blocks: true}
	s.job = job
	m.databaseJobCard(job)
	key := imp.sessKey
	run := func() tea.Msg {
		msg := dbImportDoneMsg{job: job, imp: imp, key: key, sess: sess}
		if manual && !open {
			// The transaction outlives this command: never a context that ends.
			if err := sess.Begin(context.Background()); err != nil {
				msg.res = dbclient.ImportResult{Err: err}
				cancel()
				return msg
			}
			msg.began = true
		}
		msg.res = dbclient.ImportCSV(ctx, sess, spec, func(n int64) { job.done.Store(n) })
		cancel()
		return msg
	}
	return tea.Batch(run, dbJobTickCmd(job))
}

func (m *Model) onDBImportDone(msg dbImportDoneMsg) tea.Cmd {
	s := m.dbConsole()
	if s.job != msg.job {
		return nil
	}
	s.job, s.imp = nil, nil
	m.closeJobCard()
	if msg.began {
		m.databases.tx(msg.key).open = true
		m.databases.touch()
	}
	imp, res := msg.imp, msg.res
	switch {
	case res.Cancelled:
		return m.pushToastDetail(toast.Info, "Import cancelled", "Nothing was imported.")
	case res.Err != nil:
		title := "Import failed"
		if res.FailedRow > 0 {
			title = fmt.Sprintf("Import failed: row %d (line %d)", res.FailedRow, res.FailedLine)
			if res.FailedFile != "" {
				title = fmt.Sprintf("Import failed: %s row %d (line %d)", sanitizeDBText(filepath.Base(res.FailedFile)), res.FailedRow, res.FailedLine)
			}
		}
		m.confirm = confirm.New(title, dbSafeMultiline(dbErrorText(res.Err))+"\n\nNothing was imported.", []confirm.Button{{ID: "close", Title: "Close", Style: confirm.StylePrimary}}).WithScrollText()
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindDBInfo
		return nil
	}
	if m.databases.manualMode(imp.t.conn) {
		m.databases.tx(msg.key).writes += int(res.Rows)
		m.databases.touch()
	}
	title := fmt.Sprintf("Imported %s into %s", dbPlural(int(res.Rows), "row"), imp.t.label())
	detail := fmt.Sprintf("%d ms", res.Elapsed.Milliseconds())
	if m.databases.manualMode(imp.t.conn) {
		detail += " · in the open transaction: K commits, Z rolls back"
	}
	level := toast.Info
	if res.SkippedRows > 0 {
		level = toast.Warn
		title += fmt.Sprintf(", %d skipped", res.SkippedRows)
		if p, err := dbWriteImportLog(imp, res); err != nil {
			detail = "The error log could not be written: " + dbErrorText(err)
		} else {
			detail = "Error log: " + p
		}
	}
	cmds := []tea.Cmd{m.pushToastDetail(level, title, detail)}
	if t := s.curTab(); t != nil && t.run != nil && t.run.tv != nil && t.run.tv.conn == imp.t.conn && t.run.tv.schema == imp.t.schema && t.run.tv.name == imp.t.name && !t.dirty() {
		cmds = append(cmds, m.databaseRefreshTab(t))
	}
	return tea.Batch(cmds...)
}

// dbWriteImportLog writes the skipped rows of an import to a new file
// (0600) in the config folder and returns its path.
func dbWriteImportLog(imp *dbImport, res dbclient.ImportResult) (string, error) {
	dir := dbConfigPath("import-errors")
	if dir == "" {
		return "", os.ErrNotExist
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	base := strings.TrimSuffix(filepath.Base(imp.path), filepath.Ext(imp.path))
	p := filepath.Join(dir, base+"-"+time.Now().Format("20060102-150405")+".log")
	text := dbclient.ImportErrorLog(imp.display, imp.t.label(), res)
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		return "", err
	}
	return p, nil
}
