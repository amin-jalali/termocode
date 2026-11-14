package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Edit object source (DBX-32): Databases: Edit Object Source (E on a view,
// routine or trigger) reads its stored CREATE statement on a read-only
// session and opens it in a buffer. Save, then Databases: Submit Object
// Source… reads the stored source again (a warning when someone else
// changed it meanwhile) and shows the diff and the exact script on a
// Cancel-first card. Databases: Pending Source Changes… lists the saved,
// not yet submitted sources.

const (
	confirmKindDBSourceSubmit confirmKindEnum = 3971
	pickerKindDBSourcePending pickerKindEnum  = 3972
	// pickerKindDBSourcePackagePart asks for an Oracle package's spec or
	// body (DBX-63).
	pickerKindDBSourcePackagePart pickerKindEnum = 3976
)

// dbSourceState tracks the opened sources by buffer file.
type dbSourceState struct {
	edits   map[string]*dbSourceOpen
	pending dbclient.PendingSources
	submit  *dbSourceSubmit // the open submit card
	// pkgRow is the Oracle package row whose spec or body is being picked.
	pkgRow *dbTreeRow
}

type dbSourceOpen struct {
	edit            *dbclient.SourceEdit
	driver, sessKey string
}

type dbSourceSubmit struct {
	file string
	plan dbclient.SourcePlan
}

type dbSourceLoadedMsg struct {
	conn, driver, sessKey, name string
	kind                        dbclient.ObjectKind
	text                        string
	err                         error
}

type dbSourceCheckedMsg struct {
	file, storedNow string
	plan            dbclient.SourcePlan
	err             error
}

type dbSourceStoredMsg struct {
	file string
	err  error
	// compile are the PL/SQL compile errors after an Oracle submit.
	compile []dbclient.CompileError
}

func (s *dbState) sourceState() *dbSourceState {
	if s.sources == nil {
		s.sources = &dbSourceState{edits: map[string]*dbSourceOpen{}}
	}
	return s.sources
}

// dbSourceKindOf is the editable object kind of a tree node kind.
func dbSourceKindOf(k dbNodeKind) (dbclient.ObjectKind, bool) {
	switch k {
	case dbNodeView:
		return dbclient.ObjectView, true
	case dbNodeRoutine:
		return dbclient.ObjectFunction, true
	case dbNodeTrigger:
		return dbclient.ObjectTrigger, true
	}
	return "", false
}

// dbSourceFileName is a safe, readable, unique file name for an object.
func dbSourceFileName(kind dbclient.ObjectKind, name string) string {
	clean := strings.Map(func(r rune) rune {
		if r == '_' || r == '-' || r == '.' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, name)
	clean = strings.ReplaceAll(clean, "..", "_")
	if len(clean) > 60 {
		clean = clean[:60]
	}
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(string(kind)+"\x00"+name)))[:8]
	return string(kind) + "-" + clean + "-" + sum + ".sql"
}

// dbSourceCardText is the message of the submit card.
func dbSourceCardText(e *dbclient.SourceEdit, plan dbclient.SourcePlan, outdated bool) string {
	var b strings.Builder
	if outdated {
		b.WriteString("Warning: this " + string(e.Kind) + " changed on the server since you opened it. Submitting overwrites that change.\n\n")
	}
	if !plan.Transactional && len(plan.Statements) > 1 {
		b.WriteString("These statements are not in one transaction: if the CREATE fails, the old " + string(e.Kind) + " is already gone.\n\n")
	}
	b.WriteString("Runs:\n")
	for _, st := range plan.Statements {
		b.WriteString(strings.TrimRight(st, "; \n") + ";\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// dbReadSource runs the source query of an object on a read-only session.
func dbReadSource(ctx context.Context, sess dbclient.Session, driver string, kind dbclient.ObjectKind, name string) (string, error) {
	q, err := dbclient.ObjectSourceQuery(driver, kind, name)
	if err != nil {
		return "", err
	}
	ro, err := sess.ReadOnlySession()
	if err != nil {
		return "", err
	}
	defer ro.Close()
	max := 1
	if q.Lines {
		max = dbSourceMaxLines
	}
	r := ro.Run(ctx, q.SQL, dbclient.RunOptions{MaxRows: max, Args: q.Args})
	if r.Err != nil {
		return "", r.Err
	}
	if q.Lines && r.Truncated {
		return "", fmt.Errorf("the source of %s %s has more than %d lines", kind, name, dbSourceMaxLines)
	}
	text, ok := q.Text(r)
	if !ok {
		return "", fmt.Errorf("no stored source for %s %s", kind, name)
	}
	return text, nil
}

// dbSourceMaxLines bounds the all_source lines read for one PL/SQL unit.
const dbSourceMaxLines = 200000

// databaseEditSourceStart reads the source of the tree row's object.
func (m *Model) databaseEditSourceStart() tea.Cmd {
	s := m.databases
	r, ok := m.databaseCurrentRow()
	if s == nil || !ok {
		return m.pushToastDetail(toast.Info, "Select a view, routine or trigger", "Put the cursor on one in the Databases view.")
	}
	kind, ok := dbSourceKindOf(r.kind)
	if !ok {
		return m.pushToastDetail(toast.Info, "Select a view, routine or trigger", "Only these have a source to edit.")
	}
	c, ok := s.connection(r.conn)
	if !ok {
		return nil
	}
	key := s.sessionKeyFor(r.path, r.conn)
	if s.sessions[key] == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Press Enter on the connection in the Databases view first.")
	}
	if c.Driver == "oracle" && strings.Contains(r.path, "/Packages/") {
		row := r
		s.sourceState().pkgRow = &row
		m.picker = picker.NewItems(" Edit package "+sanitizeDBText(r.name)+" ", []picker.Item{
			{ID: "spec", Title: "Package spec", Hint: "the declarations"},
			{ID: "body", Title: "Package body", Hint: "the code"},
		})
		m.picker.SetSize(m.w, m.h)
		m.pickerOpen = true
		m.pickerKind = pickerKindDBSourcePackagePart
		return nil
	}
	return m.databaseEditSourceLoad(r, kind)
}

// databaseEditSourceLoad reads the source of kind for the tree row r.
func (m *Model) databaseEditSourceLoad(r dbTreeRow, kind dbclient.ObjectKind) tea.Cmd {
	s := m.databases
	c, ok := s.connection(r.conn)
	if !ok {
		return nil
	}
	key := s.sessionKeyFor(r.path, r.conn)
	sess := s.sessions[key]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Press Enter on the connection in the Databases view first.")
	}
	name := r.name
	if sch := dbAncestorSchema(r.path); sch != "" && kind != dbclient.ObjectTrigger && dbDriverFamily(c.Driver) == "postgres" {
		d := dbclient.DialectFor(c.Driver)
		name = d.QuoteIdent(sch) + "." + d.QuoteIdent(r.name)
	}
	if sch := dbAncestorSchema(r.path); sch != "" && c.Driver == "oracle" {
		name = sch + "." + r.name
	}
	driver := c.Driver
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbCompareTimeout)
		defer cancel()
		text, err := dbReadSource(ctx, sess, driver, kind, name)
		if err != nil && kind == dbclient.ObjectFunction && dbDriverFamily(driver) == "mysql" {
			if t2, err2 := dbReadSource(ctx, sess, driver, dbclient.ObjectProcedure, name); err2 == nil {
				kind, text, err = dbclient.ObjectProcedure, t2, nil
			}
		}
		if err == nil && driver == "oracle" && (kind == dbclient.ObjectFunction || kind == dbclient.ObjectProcedure) {
			if k, ok := dbclient.SourceHeaderKind(text); ok {
				kind = k // a routine row is the procedure or function it is
			}
		}
		return dbSourceLoadedMsg{conn: r.conn, driver: driver, sessKey: key, name: name, kind: kind, text: text, err: err}
	}
}

func (m *Model) onDBSourceLoaded(msg dbSourceLoadedMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Could not read the source", dbclient.Redact(sanitizeDBText(msg.err.Error())))
	}
	if m.databases == nil {
		return nil
	}
	rootHash := fmt.Sprintf("%x", sha256.Sum256([]byte(m.databaseWorkspaceRoot())))[:16]
	connHash := fmt.Sprintf("%x", sha256.Sum256([]byte(msg.conn)))[:12]
	file := dbConfigPath("sources", rootHash, connHash, dbSourceFileName(msg.kind, msg.name))
	if file == "" {
		return m.pushToastDetail(toast.Errr, "Could not open the source", "No config folder.")
	}
	st := m.databases.sourceState()
	text := strings.TrimRight(msg.text, "\n") + "\n"
	note := "Save, then run Databases: Submit Object Source… to store it."
	if old, ok := st.edits[file]; ok {
		if disk, err := os.ReadFile(file); err == nil && string(disk) != old.edit.Stored {
			// The user's saved edit stays; Submit warns if the server changed.
			m.openFileAtLine(file, 1)
			return tea.Batch(m.fetchStateCmd(), m.pushToastDetail(toast.Info, "Your unsubmitted edit is kept", note))
		}
	}
	if err := os.MkdirAll(dirOf(file), 0o700); err != nil {
		return m.pushToastDetail(toast.Errr, "Could not open the source", err.Error())
	}
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		return m.pushToastDetail(toast.Errr, "Could not open the source", err.Error())
	}
	st.edits[file] = &dbSourceOpen{edit: dbclient.NewSourceEdit(msg.conn, msg.kind, msg.name, text), driver: msg.driver, sessKey: msg.sessKey}
	if m.nvim == nil {
		return m.copyWithToast(text, "No editor: source copied instead")
	}
	m.ensureEditorWindowCurrent()
	m.openFileAtLine(file, 1)
	m.focus = FocusEditor
	return tea.Batch(m.fetchStateCmd(), m.pushToastDetail(toast.Info, "Editing "+string(msg.kind)+" "+sanitizeDBText(msg.name), note))
}

// databaseSubmitSourceStart checks the current buffer's edit and reads
// the stored source again for the outdated warning.
func (m *Model) databaseSubmitSourceStart() tea.Cmd {
	if m.databases == nil || m.nvim == nil {
		return m.pushToastDetail(toast.Info, "Open an object source first", "Databases: Edit Object Source (E on a view, routine or trigger).")
	}
	file, lines, ok := m.httpBufText(0)
	st := m.databases.sourceState()
	o := st.edits[file]
	if !ok || o == nil {
		return m.pushToastDetail(toast.Info, "Not an object source buffer", "Open one with Databases: Edit Object Source (E in the tree).")
	}
	o.edit.Text = strings.Join(lines, "\n")
	if !o.edit.Dirty() {
		st.pending.Drop(o.edit.Conn, o.edit.Kind, o.edit.Name)
		return m.pushToastDetail(toast.Info, "No changes", "The buffer is the same as the stored source.")
	}
	st.pending.Put(o.edit)
	c, _ := m.databases.connection(o.edit.Conn)
	if c.ReadOnly {
		return m.pushToastDetail(toast.Warn, "Cannot submit", "The connection is read-only.")
	}
	plan, err := dbclient.SubmitObjectSource(o.driver, o.edit.Kind, o.edit.Name, o.edit.Text)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Cannot submit", sanitizeDBText(err.Error()))
	}
	sess := m.databases.sessions[o.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect again in the Databases view, then submit.")
	}
	e := *o.edit
	driver := o.driver
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbCompareTimeout)
		defer cancel()
		now, err := dbReadSource(ctx, sess, driver, e.Kind, e.Name)
		return dbSourceCheckedMsg{file: file, storedNow: now, plan: plan, err: err}
	}
}

func (m *Model) onDBSourceChecked(msg dbSourceCheckedMsg) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	st := m.databases.sourceState()
	o := st.edits[msg.file]
	if o == nil {
		return nil
	}
	outdated := msg.err == nil && o.edit.Outdated(msg.storedNow)
	e := o.edit
	c, _ := m.databases.connection(e.Conn)
	title := "Store the edited " + string(e.Kind) + " " + sanitizeDBText(e.Name) + "?"
	if c.Environment == "production" {
		title = "Write to production (" + sanitizeDBText(dbConnName(c)) + ")? " + string(e.Kind) + " " + sanitizeDBText(e.Name)
	}
	text := dbSourceCardText(e, msg.plan, outdated)
	if msg.err != nil {
		text = "Could not read the stored source again (" + dbclient.Redact(sanitizeDBText(msg.err.Error())) + "): the outdated check did not run.\n\n" + text
	}
	if m.databases.manualMode(e.Conn) {
		text += "\n\nRuns inside the open transaction: Commit saves it."
	}
	st.submit = &dbSourceSubmit{file: msg.file, plan: msg.plan}
	diff := strings.Split(strings.TrimRight(e.Preview(), "\n"), "\n")
	if len(diff) > 2 {
		diff = diff[2:] // the --- / +++ header is in the title
	}
	for i, l := range diff {
		diff[i] = sanitizeDBText(l)
	}
	m.confirm = confirm.New(title, sanitizeDBText(text), []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "copy", Title: "Copy SQL"},
		{ID: "submit", Title: "Submit", Style: confirm.StyleDestructive},
	}).WithDiff(diff).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBSourceSubmit
	return nil
}

// databaseSourceConfirm answers the submit card.
func (m *Model) databaseSourceConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindDBSourceSubmit {
		return nil, false
	}
	if m.databases == nil {
		return nil, true
	}
	st := m.databases.sourceState()
	sub := st.submit
	st.submit = nil
	if sub == nil {
		return nil, true
	}
	switch id {
	case "copy":
		return m.copyWithToast(strings.Join(sub.plan.Statements, ";\n")+";", "Copied the SQL"), true
	case "submit":
	default:
		return nil, true
	}
	o := st.edits[sub.file]
	if o == nil {
		return nil, true
	}
	sess := m.databases.sessions[o.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect again in the Databases view, then submit."), true
	}
	manual := m.databases.manualMode(o.edit.Conn)
	plan, file := sub.plan, sub.file
	plsql := o.driver == "oracle" && dbclient.IsPLSQLKind(o.edit.Kind)
	kind, name, text := o.edit.Kind, o.edit.Name, o.edit.Text
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbCompareTimeout)
		defer cancel()
		tx := plan.Transactional && len(plan.Statements) > 1 && !manual
		if tx {
			if err := sess.Begin(ctx); err != nil {
				return dbSourceStoredMsg{file: file, err: err}
			}
		}
		for _, stmt := range plan.Statements {
			if r := sess.Run(ctx, stmt, dbclient.RunOptions{}); r.Err != nil && !(plsql && strings.Contains(r.Err.Error(), "ORA-24344")) {
				if tx {
					_ = sess.Rollback(context.Background())
				}
				return dbSourceStoredMsg{file: file, err: r.Err}
			}
		}
		if tx {
			if err := sess.Commit(ctx); err != nil {
				return dbSourceStoredMsg{file: file, err: err}
			}
		}
		if !plsql {
			return dbSourceStoredMsg{file: file}
		}
		// Oracle stores a unit that does not compile: read why (DBX-63).
		q, err := dbclient.OracleCompileErrorsQuery(kind, name)
		if err != nil {
			return dbSourceStoredMsg{file: file}
		}
		r := sess.Run(ctx, q.SQL, dbclient.RunOptions{MaxRows: 500, Args: q.Args})
		if r.Err != nil {
			return dbSourceStoredMsg{file: file, err: fmt.Errorf("stored, but the compile errors could not be read: %w", r.Err)}
		}
		return dbSourceStoredMsg{file: file, compile: dbclient.OracleCompileErrors(r, text)}
	}, true
}

func (m *Model) onDBSourceStored(msg dbSourceStoredMsg) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	st := m.databases.sourceState()
	o := st.edits[msg.file]
	if o == nil {
		return nil
	}
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Could not store the "+string(o.edit.Kind), dbclient.Redact(sanitizeDBText(msg.err.Error())))
	}
	o.edit.Stored = o.edit.Text
	st.pending.Drop(o.edit.Conn, o.edit.Kind, o.edit.Name)
	m.databaseSetCompileProblems(msg.file, o.edit.Name, msg.compile)
	if errs := dbCompileErrorCount(msg.compile); len(msg.compile) > 0 {
		var lines []string
		for i, e := range msg.compile {
			if i == 5 {
				lines = append(lines, fmt.Sprintf("… %d more in the buffer's problems", len(msg.compile)-5))
				break
			}
			lines = append(lines, fmt.Sprintf("line %d: %s", e.Line, sanitizeDBText(e.Text)))
		}
		level, title := toast.Warn, fmt.Sprintf("Stored %s %s: compiled with %s", o.edit.Kind, sanitizeDBText(o.edit.Name), dbPlural(len(msg.compile)-errs, "warning"))
		if errs > 0 {
			level, title = toast.Errr, fmt.Sprintf("Stored %s %s, but it compiled with %s", o.edit.Kind, sanitizeDBText(o.edit.Name), dbPlural(errs, "error"))
		}
		return m.pushToastDetail(level, title, strings.Join(lines, "\n"))
	}
	return m.pushToastDetail(toast.Info, "Stored "+string(o.edit.Kind)+" "+sanitizeDBText(o.edit.Name), "")
}

func dbCompileErrorCount(list []dbclient.CompileError) int {
	n := 0
	for _, e := range list {
		if !e.Warning {
			n++
		}
	}
	return n
}

// dbCompileNamespace is the vim.diagnostic namespace of PL/SQL compile
// errors.
const dbCompileNamespace = "termocode-plsql"

// databaseSetCompileProblems puts the compile errors of a submit on the
// source buffer as diagnostics (an empty list clears them), so they show
// in the gutter and in Problems.
func (m *Model) databaseSetCompileProblems(file, name string, list []dbclient.CompileError) {
	if m.nvim == nil {
		return
	}
	diags := make([]any, 0, len(list))
	for _, e := range list {
		sev := 1
		if e.Warning {
			sev = 2
		}
		diags = append(diags, []any{e.Line, e.Col, sev, e.Text})
	}
	_ = m.nvim.ExecLuaArgs(setTaskDiagnosticsLua, []any{[]any{file, diags}}, "oracle: "+name, dbCompileNamespace)
}

// databasePendingSources lists the saved, not yet submitted sources.
func (m *Model) databasePendingSources() tea.Cmd {
	if m.databases == nil {
		return m.pushToast(toast.Info, "No pending source changes")
	}
	st := m.databases.sourceState()
	files := map[string]string{}
	for file, o := range st.edits {
		if disk, err := os.ReadFile(file); err == nil {
			o.edit.Text = string(disk)
		}
		st.pending.Put(o.edit)
		files[o.edit.Conn+"\x00"+string(o.edit.Kind)+"\x00"+o.edit.Name] = file
	}
	list := st.pending.List()
	if len(list) == 0 {
		return m.pushToastDetail(toast.Info, "No pending source changes", "Edit one with E on a view, routine or trigger.")
	}
	items := make([]picker.Item, 0, len(list))
	for _, e := range list {
		added, removed := 0, 0
		for _, l := range strings.Split(e.Preview(), "\n")[2:] {
			switch {
			case strings.HasPrefix(l, "+"):
				added++
			case strings.HasPrefix(l, "-"):
				removed++
			}
		}
		file := files[e.Conn+"\x00"+string(e.Kind)+"\x00"+e.Name]
		items = append(items, picker.Item{ID: file, Title: sanitizeDBText(string(e.Kind) + " " + e.Name), Hint: sanitizeDBText(fmt.Sprintf("%s · +%d −%d", e.Conn, added, removed))})
	}
	m.picker = picker.NewItems(" Pending source changes (Enter opens; submit from the buffer) ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindDBSourcePending
	return nil
}

// databaseSourcePicker opens the chosen pending source.
func (m *Model) databaseSourcePicker(id string) (tea.Cmd, bool) {
	if m.pickerKind == pickerKindDBSourcePackagePart {
		if m.databases == nil {
			return nil, true
		}
		st := m.databases.sourceState()
		row := st.pkgRow
		st.pkgRow = nil
		if row == nil || (id != "spec" && id != "body") {
			return nil, true
		}
		kind := dbclient.ObjectPackage
		if id == "body" {
			kind = dbclient.ObjectPackageBody
		}
		return m.databaseEditSourceLoad(*row, kind), true
	}
	if m.pickerKind != pickerKindDBSourcePending {
		return nil, false
	}
	if id == "" {
		return nil, true
	}
	m.ensureEditorWindowCurrent()
	m.openFileAtLine(id, 1)
	m.focus = FocusEditor
	return m.fetchStateCmd(), true
}
