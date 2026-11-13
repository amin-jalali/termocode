package app

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Object actions (DBX-33): Databases: Object Actions… (A in the tree)
// lists what can be done to the row: drop (with a cascade preview that
// lists the foreign keys and views it also removes or breaks), truncate,
// create a schema or database, copy a table as a quick backup, make a
// primary key from a column. Names are asked in a prompt; every action
// ends on a Cancel-first card with the exact SQL.

const (
	pickerKindDBObjAction  pickerKindEnum  = 3973
	promptKindDBObjName    promptKindEnum  = 3974
	confirmKindDBObjAction confirmKindEnum = 3975
)

// dbObjTarget is the tree row an action is for.
type dbObjTarget struct {
	kind                         dbNodeKind
	conn, driver, sessKey, path  string
	schema, name, column, parent string // parent: the path to reload after
}

// dbObjPending is the action between the picker, the prompt and the card.
type dbObjPending struct {
	t     dbObjTarget
	id    string
	stmts []string
}

type dbObjDepsMsg struct {
	p    dbObjPending
	deps []string
	err  error
}

type dbObjDoneMsg struct {
	p   dbObjPending
	err error
}

// dbObjectActionItems are the actions of a node kind on a driver family.
func dbObjectActionItems(kind dbNodeKind, family string) []picker.Item {
	pg, lite := family == "postgres", family == "sqlite"
	var out []picker.Item
	add := func(id, title, hint string) { out = append(out, picker.Item{ID: id, Title: title, Hint: hint}) }
	switch kind {
	case dbNodeTable:
		add("copy", "Copy table (quick backup)…", "CREATE TABLE … + INSERT … SELECT")
		if lite {
			add("truncate", "Empty table…", "DELETE FROM")
		} else {
			add("truncate", "Truncate table…", "TRUNCATE TABLE")
		}
		add("drop", "Drop table…", "shows what depends on it")
		if pg {
			add("drop-cascade", "Drop table with cascade…", "also drops its dependents")
		}
	case dbNodeView, dbNodeRoutine, dbNodeSequence:
		add("drop", "Drop "+dbObjWord(kind)+"…", "")
	case dbNodeColumn:
		if !lite {
			add("make-pk", "Make primary key from this column…", "ALTER TABLE … ADD PRIMARY KEY")
		}
	case dbNodeSchema:
		if !lite {
			add("create-schema", "Create schema…", "")
			add("drop", "Drop schema…", "")
		}
	case dbNodeDatabase:
		switch family {
		case "mysql":
			add("create-schema", "Create database…", "")
			add("drop", "Drop database…", "")
		case "postgres":
			add("create-schema", "Create schema…", "in this database")
		}
	case dbNodeConnection:
		if pg || family == "mysql" {
			add("create-database", "Create database…", "")
		}
	}
	return out
}

func dbObjWord(kind dbNodeKind) string {
	switch kind {
	case dbNodeTable:
		return "table"
	case dbNodeView:
		return "view"
	case dbNodeRoutine:
		return "function"
	case dbNodeSequence:
		return "sequence"
	case dbNodeSchema, dbNodeDatabase:
		return "schema"
	}
	return "object"
}

// dbObjectActionFor is the engine action of picker id on t; name is what
// the prompt asked (a new schema, database or copy name).
func dbObjectActionFor(id string, t dbObjTarget, name string) dbclient.ObjectAction {
	a := dbclient.ObjectAction{Schema: t.schema, Name: t.name}
	switch id {
	case "drop", "drop-cascade":
		a.Kind, a.Object, a.Cascade = dbclient.ActDrop, dbObjWord(t.kind), id == "drop-cascade"
		if t.kind == dbNodeSchema || t.kind == dbNodeDatabase {
			a.Schema = ""
		}
	case "truncate":
		a.Kind = dbclient.ActTruncate
	case "copy":
		a.Kind, a.NewName = dbclient.ActCopyTable, name
	case "make-pk":
		a.Kind, a.Columns = dbclient.ActMakePK, []string{t.column}
	case "create-schema":
		a = dbclient.ObjectAction{Kind: dbclient.ActCreateSchema, Name: name}
	case "create-database":
		a = dbclient.ObjectAction{Kind: dbclient.ActCreateDatabase, Name: name}
	}
	return a
}

// dbObjectActionCardText is the message of the card.
func dbObjectActionCardText(stmts, deps []string, destructive bool) string {
	var b strings.Builder
	b.WriteString("Runs:\n")
	for _, s := range stmts {
		b.WriteString(s + ";\n")
	}
	if len(deps) > 0 {
		b.WriteString("\nAlso removed or broken (" + dbItoa(len(deps)) + "):\n")
		for _, d := range deps {
			b.WriteString("  " + d + "\n")
		}
	}
	if destructive {
		b.WriteString("\nThis cannot be undone.")
	}
	return strings.TrimRight(b.String(), "\n")
}

// dbObjTargetOf is the action target of a tree row.
func (s *dbState) dbObjTargetOf(r dbTreeRow) (dbObjTarget, bool) {
	c, ok := s.connection(r.conn)
	if !ok || dbclient.DialectFor(c.Driver) == nil {
		return dbObjTarget{}, false
	}
	t := dbObjTarget{kind: r.kind, conn: r.conn, driver: c.Driver, path: r.path, sessKey: s.sessionKeyFor(r.path, r.conn), name: r.name}
	fam := dbDriverFamily(c.Driver)
	if fam == "postgres" {
		t.schema = dbAncestorSchema(r.path)
	} else if fam == "mysql" {
		t.schema = dbAncestorDatabase(r.path)
	}
	t.parent = s.dbReloadAncestor(r.path)
	if r.kind == dbNodeColumn {
		i := strings.LastIndex(r.path, "/col/")
		if i < 0 {
			return dbObjTarget{}, false
		}
		table := s.nodes[r.path[:i]]
		t.column, _ = dbColumnParts(s.nodes[r.path])
		t.name, t.parent = table.name, r.path[:i]
	}
	if r.kind == dbNodeSchema || r.kind == dbNodeDatabase {
		t.schema = ""
	}
	if r.kind == dbNodeConnection || r.kind == dbNodeDatabase && fam == "postgres" {
		t.parent = r.path
	}
	return t, true
}

// dbReloadAncestor is the nearest schema, database or connection above
// path: reloading it shows what an action changed.
func (s *dbState) dbReloadAncestor(path string) string {
	for p := path; ; {
		i := strings.LastIndex(p, "/")
		if i < 0 {
			return p
		}
		p = p[:i]
		switch s.nodes[p].kind {
		case dbNodeSchema, dbNodeDatabase, dbNodeConnection:
			if _, ok := s.nodes[p]; ok {
				return p
			}
		}
	}
}

// databaseObjectActions opens the action picker of the tree row.
func (m *Model) databaseObjectActions() tea.Cmd {
	s := m.databases
	r, ok := m.databaseCurrentRow()
	if s == nil || !ok || r.conn == "" {
		return m.pushToastDetail(toast.Info, "Select an object first", "Put the cursor on a table, view, column, schema or connection in the Databases view.")
	}
	t, ok := s.dbObjTargetOf(r)
	if !ok {
		return m.pushToastDetail(toast.Info, "No object actions here", "Object actions need a SQL database.")
	}
	items := dbObjectActionItems(r.kind, dbDriverFamily(t.driver))
	if len(items) == 0 {
		return m.pushToastDetail(toast.Info, "No object actions here", "Put the cursor on a table, view, column, schema or connection.")
	}
	if s.sessions[t.sessKey] == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Press Enter on the connection in the Databases view first.")
	}
	if c, _ := s.connection(t.conn); c.ReadOnly {
		return m.pushToastDetail(toast.Warn, "Read-only connection", "Object actions change the database.")
	}
	s.objAction = &dbObjPending{t: t}
	m.picker = picker.NewItems(" "+sanitizeDBText(dbJoinName(t.schema, t.name, t.column))+" ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindDBObjAction
	return nil
}

// databaseDropKey is D on a tree row (KB-1: D means remove): the drop of
// the object under the cursor, through the same card as Object Actions
// (it reads what depends on a table first, Cancel is first). Rows with
// nothing to drop say so.
func (m *Model) databaseDropKey() tea.Cmd {
	s := m.databases
	r, ok := m.databaseCurrentRow()
	if s == nil || !ok || r.conn == "" {
		return m.pushToastDetail(toast.Info, "Select an object first", "Put the cursor on a table, view, schema or database.")
	}
	t, ok := s.dbObjTargetOf(r)
	if !ok {
		return m.pushToastDetail(toast.Info, "Nothing to remove here", "Drop needs a SQL database object.")
	}
	canDrop := false
	for _, it := range dbObjectActionItems(r.kind, dbDriverFamily(t.driver)) {
		if it.ID == "drop" {
			canDrop = true
		}
	}
	if !canDrop {
		return m.pushToastDetail(toast.Info, "Nothing to remove here", "D drops a table, view, schema or database. Use A for other actions.")
	}
	if s.sessions[t.sessKey] == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Press Enter on the connection in the Databases view first.")
	}
	if c, _ := s.connection(t.conn); c.ReadOnly {
		return m.pushToastDetail(toast.Warn, "Read-only connection", "Dropping changes the database.")
	}
	s.objAction = &dbObjPending{t: t, id: "drop"}
	return m.databaseObjActionPrepare("")
}

// databaseObjActionPicker takes the chosen action: a name prompt or the
// card.
func (m *Model) databaseObjActionPicker(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBObjAction {
		return nil, false
	}
	s := m.databases
	if s == nil || s.objAction == nil || id == "" {
		return nil, true
	}
	p := s.objAction
	p.id = id
	switch id {
	case "copy":
		return m.openHTTPPrompt(promptKindDBObjName, "Copy table "+sanitizeDBText(p.t.name), "Name of the copy:", dbclient.BackupTableName(p.t.name, time.Now().Format("20060102_1504"))), true
	case "create-schema":
		word := "schema"
		if dbDriverFamily(p.t.driver) == "mysql" {
			word = "database"
		}
		return m.openHTTPPrompt(promptKindDBObjName, "Create "+word, "Name of the new "+word+":", ""), true
	case "create-database":
		return m.openHTTPPrompt(promptKindDBObjName, "Create database", "Name of the new database:", ""), true
	}
	return m.databaseObjActionPrepare(""), true
}

// databaseObjActionPrompt takes the typed name.
func (m *Model) databaseObjActionPrompt(kind promptKindEnum, value string) (tea.Cmd, bool) {
	if kind != promptKindDBObjName {
		return nil, false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return m.pushToast(toast.Info, "No name: nothing done"), true
	}
	return m.databaseObjActionPrepare(value), true
}

// databaseObjActionPrepare builds the SQL; a drop first reads what
// depends on the object.
func (m *Model) databaseObjActionPrepare(name string) tea.Cmd {
	s := m.databases
	if s == nil || s.objAction == nil {
		return nil
	}
	p := *s.objAction
	d := dbclient.DialectFor(p.t.driver)
	stmts, err := dbclient.ObjectActionSQL(d, dbObjectActionFor(p.id, p.t, name))
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Cannot do that", sanitizeDBText(err.Error()))
	}
	p.stmts = stmts
	if (p.id != "drop" && p.id != "drop-cascade") || p.t.kind != dbNodeTable {
		return m.databaseObjActionCard(p, nil, nil)
	}
	sess := s.sessions[p.t.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "")
	}
	return func() tea.Msg {
		q, args, err := dbclient.DropDependentsQuery(d, p.t.schema, p.t.name)
		if err != nil {
			return dbObjDepsMsg{p: p, err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), dbCompareTimeout)
		defer cancel()
		ro, err := sess.ReadOnlySession()
		if err != nil {
			return dbObjDepsMsg{p: p, err: err}
		}
		defer ro.Close()
		r := ro.Run(ctx, q, dbclient.RunOptions{MaxRows: 200, Args: args})
		if r.Err != nil {
			return dbObjDepsMsg{p: p, err: r.Err}
		}
		var deps []string
		for _, row := range r.Rows {
			if len(row) < 3 {
				continue
			}
			deps = append(deps, row[0].String()+" "+dbJoinName(row[1].String(), row[2].String()))
		}
		return dbObjDepsMsg{p: p, deps: deps}
	}
}

func (m *Model) onDBObjDeps(msg dbObjDepsMsg) tea.Cmd {
	return m.databaseObjActionCard(msg.p, msg.deps, msg.err)
}

// databaseObjActionCard shows the Cancel-first card.
func (m *Model) databaseObjActionCard(p dbObjPending, deps []string, depErr error) tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	s.objAction = &p
	destructive := p.id == "drop" || p.id == "drop-cascade" || p.id == "truncate"
	for i := range deps {
		deps[i] = sanitizeDBText(deps[i])
	}
	text := dbObjectActionCardText(p.stmts, deps, destructive)
	if depErr != nil {
		text = "Could not list what depends on it (" + dbclient.Redact(sanitizeDBText(depErr.Error())) + ").\n\n" + text
	} else if p.id == "drop" && len(deps) > 0 {
		text += "\nWithout cascade the drop fails while these exist."
	}
	c, _ := s.connection(p.t.conn)
	title := "Run on " + sanitizeDBText(dbConnName(c)) + "?"
	if c.Environment == "production" {
		title = "Write to production (" + sanitizeDBText(dbConnName(c)) + ")?"
	}
	if m.databases.manualMode(p.t.conn) {
		text += "\n\nRuns inside the open transaction where the database allows it."
	}
	runStyle := confirm.StylePrimary
	if destructive {
		runStyle = confirm.StyleDestructive
	}
	m.confirm = confirm.New(title, sanitizeDBText(text), []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "copy", Title: "Copy SQL"},
		{ID: "run", Title: "Run", Style: runStyle},
	}).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBObjAction
	return nil
}

// databaseObjActionConfirm answers the card.
func (m *Model) databaseObjActionConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindDBObjAction {
		return nil, false
	}
	s := m.databases
	if s == nil || s.objAction == nil {
		return nil, true
	}
	p := *s.objAction
	s.objAction = nil
	switch id {
	case "copy":
		return m.copyWithToast(strings.Join(p.stmts, ";\n")+";", "Copied the SQL"), true
	case "run":
	default:
		return nil, true
	}
	sess := s.sessions[p.t.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", ""), true
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbCompareTimeout)
		defer cancel()
		for _, stmt := range p.stmts {
			if r := sess.Run(ctx, stmt, dbclient.RunOptions{}); r.Err != nil {
				return dbObjDoneMsg{p: p, err: r.Err}
			}
		}
		return dbObjDoneMsg{p: p}
	}, true
}

func (m *Model) onDBObjDone(msg dbObjDoneMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Object action failed", dbclient.Redact(sanitizeDBText(msg.err.Error())))
	}
	done := m.pushToastDetail(toast.Info, "Done", sanitizeDBText(strings.Join(msg.p.stmts, ";\n")))
	s := m.databases
	if s == nil {
		return done
	}
	if n, ok := s.nodes[msg.p.t.parent]; ok {
		n.loaded = false
		s.nodes[msg.p.t.parent] = n
		return tea.Batch(done, m.databaseExpand(msg.p.t.parent))
	}
	return done
}
