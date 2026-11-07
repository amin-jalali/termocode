package app

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// DBX-43 (rest): c on a routine in the tree opens Generate code with
// "Call with arguments…" (a small form: one value per argument, then the
// call) and DROP. In the picker Enter copies the text and → inserts it at
// the editor cursor; tables get the same → (insert) as routines.

const promptKindDBRoutineArg promptKindEnum = 1291

// dbRoutineTarget is the routine under the tree cursor.
type dbRoutineTarget struct {
	conn, driver, sessKey, schema, name string
}

func (r dbRoutineTarget) qualified() string {
	d := dbclient.DialectFor(r.driver)
	if d == nil {
		return r.name
	}
	if r.schema != "" {
		return d.QuoteIdent(r.schema) + "." + d.QuoteIdent(r.name)
	}
	return d.QuoteIdent(r.name)
}

// dbRoutineArg is one argument of a routine signature.
type dbRoutineArg struct {
	mode, name, typ string // mode: "", IN, OUT, INOUT or VARIADIC
}

// dbRoutineAsk is the open argument form.
type dbRoutineAsk struct {
	target  dbRoutineTarget
	routine dbclient.CanonRoutine
	inputs  []dbRoutineArg
	vals    []string
	insert  bool
}

func dbRoutineGenerateItems() []picker.Item {
	return []picker.Item{
		{ID: "call", Title: "Call with arguments…", Hint: "a value for each argument"},
		{ID: "rdrop", Title: "DROP", Hint: "the statement only"},
	}
}

// dbSplitTop splits s at commas outside brackets and quotes.
func dbSplitTop(s string) []string {
	var out []string
	depth, start := 0, 0
	var quote rune
	for i, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '`':
			quote = r
		case r == '(' || r == '[':
			depth++
		case r == ')' || r == ']':
			depth--
		case r == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// dbMultiWordTypes start a type of more than one word, so an argument
// that starts with one has no name.
var dbMultiWordTypes = []string{"character varying", "double precision", "bit varying", "timestamp with", "timestamp without", "time with", "time without"}

// dbParseRoutineArgs reads a signature as PostgreSQL
// (pg_get_function_identity_arguments) or MySQL (canon.go) writes it.
func dbParseRoutineArgs(s string) []dbRoutineArg {
	var out []dbRoutineArg
	if strings.TrimSpace(s) == "" {
		return nil
	}
	for _, part := range dbSplitTop(s) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var a dbRoutineArg
		if i := strings.IndexByte(part, ' '); i > 0 {
			switch w := strings.ToUpper(part[:i]); w {
			case "IN", "OUT", "INOUT", "VARIADIC":
				a.mode, part = w, strings.TrimSpace(part[i+1:])
			}
		}
		if q := part[0]; q == '"' || q == '`' {
			if j := strings.IndexByte(part[1:], q); j >= 0 {
				a.name, a.typ = part[1:j+1], strings.TrimSpace(part[j+2:])
				out = append(out, a)
				continue
			}
		}
		lower := strings.ToLower(part)
		multi := false
		for _, t := range dbMultiWordTypes {
			if strings.HasPrefix(lower, t) {
				multi = true
			}
		}
		if i := strings.IndexByte(part, ' '); i > 0 && !multi {
			a.name, a.typ = part[:i], strings.TrimSpace(part[i+1:])
		} else {
			a.typ = part
		}
		out = append(out, a)
	}
	return out
}

// dbRoutineInputs are the arguments the form asks for (not OUT).
func dbRoutineInputs(args []dbRoutineArg) []dbRoutineArg {
	var out []dbRoutineArg
	for _, a := range args {
		if a.mode != "OUT" {
			out = append(out, a)
		}
	}
	return out
}

// dbArgLiteral is a typed value as SQL: blank or NULL is NULL, a number
// for a number type stays bare, anything else is a quoted string.
func dbArgLiteral(d dbclient.Dialect, typ, v string) string {
	if strings.TrimSpace(v) == "" || strings.EqualFold(strings.TrimSpace(v), "null") {
		return "NULL"
	}
	kind := ""
	switch dbTypeClass(typ) {
	case "int", "float":
		kind = "number"
	case "bool":
		kind = "bool"
	}
	return dbclient.SQLLiteral(d, dbclient.Cell{Text: v, Kind: kind})
}

func dbCommentText(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(sanitizeDBText(s), "*/", ""), "/*", "")
}

// dbRoutineCall is the call of r with vals (one per input argument, in
// order): SELECT * FROM f(…) for a PostgreSQL function, SELECT f(…) for
// a MySQL one, CALL p(…) for a procedure. An OUT argument is NULL
// (PostgreSQL) or a session variable @name (MySQL).
func dbRoutineCall(t dbRoutineTarget, r dbclient.CanonRoutine, vals []string) string {
	d := dbclient.DialectFor(t.driver)
	_, mysql := d.(dbclient.MySQLDialect)
	var parts []string
	vi := 0
	for i, a := range dbParseRoutineArgs(r.Args) {
		if a.mode == "OUT" {
			if mysql {
				name := strings.Join(dbWords(a.name), "_")
				if name == "" {
					name = fmt.Sprintf("out%d", i+1)
				}
				parts = append(parts, "@"+name)
			} else {
				parts = append(parts, "NULL /* "+dbCommentText(a.name)+" */")
			}
			continue
		}
		v := ""
		if vi < len(vals) {
			v = vals[vi]
		}
		vi++
		p := dbArgLiteral(d, a.typ, v)
		if a.name != "" {
			p += " /* " + dbCommentText(a.name) + " */"
		}
		parts = append(parts, p)
	}
	call := t.qualified() + "(" + strings.Join(parts, ", ") + ")"
	switch {
	case r.Kind == "PROCEDURE":
		return "CALL " + call + ";"
	case mysql:
		return "SELECT " + call + ";"
	}
	return "SELECT * FROM " + call + ";"
}

// dbRoutineDrop is DROP FUNCTION / PROCEDURE; PostgreSQL needs the
// argument types to pick the overload.
func dbRoutineDrop(t dbRoutineTarget, r dbclient.CanonRoutine) string {
	kind := "FUNCTION"
	if r.Kind == "PROCEDURE" {
		kind = "PROCEDURE"
	}
	if _, pg := dbclient.DialectFor(t.driver).(dbclient.PostgresDialect); pg {
		return "DROP " + kind + " " + t.qualified() + "(" + sanitizeDBText(r.Args) + ");"
	}
	return "DROP " + kind + " " + t.qualified() + ";"
}

// databaseRoutineAt is the routine at path, if the row is one.
func (m *Model) databaseRoutineAt(path string) (dbRoutineTarget, bool) {
	s := m.databases
	if s == nil {
		return dbRoutineTarget{}, false
	}
	n, ok := s.nodes[path]
	if !ok || n.kind != dbNodeRoutine {
		return dbRoutineTarget{}, false
	}
	c, ok := s.connection(n.conn)
	if !ok || dbclient.DialectFor(c.Driver) == nil {
		return dbRoutineTarget{}, false
	}
	r := dbRoutineTarget{conn: n.conn, driver: c.Driver, name: n.name, sessKey: s.sessionKeyFor(path, n.conn)}
	if _, pg := dbclient.DialectFor(c.Driver).(dbclient.PostgresDialect); pg {
		r.schema = dbAncestorSchema(path)
	} else {
		r.schema = dbAncestorDatabase(path)
	}
	return r, true
}

// databaseRoutineGenStart is c on a routine row; ok is false for any
// other row.
func (m *Model) databaseRoutineGenStart() (tea.Cmd, bool) {
	row, ok := m.databaseCurrentRow()
	if !ok {
		return nil, false
	}
	r, ok := m.databaseRoutineAt(row.path)
	if !ok {
		return nil, false
	}
	if m.databases.sessions[r.sessKey] == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection."), true
	}
	m.databases.genTable = dbTable{}
	m.databases.genRoutine = r
	m.picker = picker.NewItems("Generate code for "+sanitizeDBText(r.name)+" (Enter copies · → inserts)", dbRoutineGenerateItems())
	m.picker.SetRowAction("→", "insert at the cursor")
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBGenerate
	m.pickerOpen = true
	return nil, true
}

// dbRoutineMsg brings the routine's signature read off the UI goroutine.
type dbRoutineMsg struct {
	gen     uint64
	target  dbRoutineTarget
	routine dbclient.CanonRoutine
	found   bool
	code    string
	insert  bool
	err     error
}

// databaseRoutineLoad reads the signature (read-only) for code.
func (m *Model) databaseRoutineLoad(r dbRoutineTarget, code string, insert bool) tea.Cmd {
	sess := m.databases.sessions[r.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	gen := m.nextTableGen()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbMetaTimeout)
		defer cancel()
		msg := dbRoutineMsg{gen: gen, target: r, code: code, insert: insert}
		ro, err := sess.ReadOnlySession()
		if err != nil {
			msg.err = err
			return msg
		}
		defer ro.Close()
		schema, err := dbclient.ReadCanonSchema(ctx, ro, r.driver, r.schema)
		if err != nil {
			msg.err = err
			return msg
		}
		for _, rt := range schema.Routines {
			if rt.Name == r.name {
				msg.routine, msg.found = rt, true
				break
			}
		}
		return msg
	}
}

func (m *Model) onDBRoutine(msg dbRoutineMsg) tea.Cmd {
	if m.databases == nil || m.dbConsole().tableGen != msg.gen {
		return nil
	}
	if msg.err != nil {
		return m.dbConsoleError(msg.err)
	}
	if !msg.found {
		return m.pushToastDetail(toast.Info, "Routine not found", "Refresh the tree (r) and try again.")
	}
	if msg.code == "rdrop" {
		return m.databaseDeliverCode(dbRoutineDrop(msg.target, msg.routine), "DROP for "+sanitizeDBText(msg.target.name), msg.insert)
	}
	a := &dbRoutineAsk{target: msg.target, routine: msg.routine, inputs: dbRoutineInputs(dbParseRoutineArgs(msg.routine.Args)), insert: msg.insert}
	if len(a.inputs) == 0 {
		return m.databaseRoutineFinish(a)
	}
	m.databases.routineAsk = a
	return m.databaseRoutineAskNext()
}

func (m *Model) databaseRoutineAskNext() tea.Cmd {
	a := m.databases.routineAsk
	arg := a.inputs[len(a.vals)]
	name := sanitizeDBText(arg.name)
	if name == "" {
		name = fmt.Sprintf("argument %d", len(a.vals)+1)
	}
	title := fmt.Sprintf("Call %s: argument %d of %d", sanitizeDBText(a.target.name), len(a.vals)+1, len(a.inputs))
	cmd := m.openHTTPPrompt(promptKindDBRoutineArg, title, name+" ("+sanitizeDBText(arg.typ)+"), blank for NULL:", "")
	m.prompt.SetAllowEmpty(true)
	return cmd
}

// databaseRoutineArgEntered is the promptKindDBRoutineArg submit.
func (m *Model) databaseRoutineArgEntered(value string) tea.Cmd {
	if m.databases == nil || m.databases.routineAsk == nil {
		return nil
	}
	a := m.databases.routineAsk
	a.vals = append(a.vals, value)
	if len(a.vals) < len(a.inputs) {
		return m.databaseRoutineAskNext()
	}
	m.databases.routineAsk = nil
	return m.databaseRoutineFinish(a)
}

func (m *Model) databaseRoutineFinish(a *dbRoutineAsk) tea.Cmd {
	return m.databaseDeliverCode(dbRoutineCall(a.target, a.routine, a.vals), "the call of "+sanitizeDBText(a.target.name), a.insert)
}

// databaseDeliverCode copies text, or with insert puts it at the editor
// cursor (copied instead when no file is open).
func (m *Model) databaseDeliverCode(text, what string, insert bool) tea.Cmd {
	if !insert {
		return m.copyWithToast(text, "Copied "+what)
	}
	if m.nvim == nil || m.editor.Path() == "" {
		return tea.Batch(m.copyWithToast(text, "No file is open: copied "+what+" instead"))
	}
	m.ensureEditorWindowCurrent()
	if err := m.nvim.ExecLuaArgs(`local text = ...
vim.api.nvim_put(vim.split(text, '\n', { plain = true }), 'c', false, true)`, text); err != nil {
		return m.copyWithToast(text, "Could not insert: copied "+what+" instead")
	}
	m.focus = FocusEditor
	return tea.Batch(m.fetchStateCmd(), m.pushToastDetail(toast.Info, "Inserted "+what, "At the cursor in "+sanitizeDBText(m.editor.Path())+"."))
}
