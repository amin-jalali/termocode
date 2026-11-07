package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The table designer (DBX-15): Databases: Create Table… (on a schema) and
// Databases: Alter Table… (on a table) open a form in a multi-line
// prompt, one part per line. Submit builds the DDL; a preview card shows
// the exact script with Cancel first (Edit goes back to the form) and
// Apply runs it on the connection's session.

const (
	promptKindDBDesign  promptKindEnum  = 4372
	confirmKindDBDesign confirmKindEnum = 4373
)

// dbDesignState is the open design: where it goes and the last text.
type dbDesignState struct {
	create                  bool
	conn, sessKey, driver   string
	schema, table, label    string
	text, script            string
	production, readOnlyCon bool
}

// dbDesignTemplate is the form text a design starts from.
func dbDesignTemplate(create bool, table string) string {
	if create {
		return strings.Join([]string{
			"-- New table: one part per line. Lines with -- are notes.",
			"--   column <name> <type> [not null] [default <expr>] [pk]",
			"--   index <name> (<cols>) [unique]   unique <name> (<cols>)",
			"--   fk <name> (<cols>) references <table> (<cols>) [on delete cascade]",
			"--   check <name> <expression>",
			"table new_table",
			"column id bigint not null pk",
			"column name text not null",
			"",
		}, "\n")
	}
	return strings.Join([]string{
		"-- Changes to " + table + ": one per line. Lines with -- are notes.",
		"--   add column <name> <type> [not null] [default <expr>]",
		"--   drop column <name>       rename column <old> to <new>",
		"--   add index <name> (<cols>) [unique]   drop index <name>",
		"--   add unique <name> (<cols>)   add check <name> <expression>",
		"--   add fk <name> (<cols>) references <table> (<cols>) [on delete cascade]",
		"--   drop constraint <name> [primary key|unique|foreign key|check]",
		"add column new_column text",
		"",
	}, "\n")
}

// dbDesignCardText is the preview card's title and body.
func dbDesignCardText(conn, label, script string, destructive int, production bool) (string, string) {
	title := "Apply this design to " + label + "?"
	if production {
		title = "Change production (" + conn + "): " + label + "?"
	}
	body := dbSafeMultiline(strings.TrimRight(script, "\n"))
	if destructive > 0 {
		body += "\n\n" + dbPlural(destructive, "statement") + " drops something. This cannot be undone."
	}
	return title, body
}

// databaseDesignStart opens the form for a new table (create) or for the
// table under the tree cursor.
func (m *Model) databaseDesignStart(create bool) tea.Cmd {
	s := m.databases
	r, ok := m.databaseCurrentRow()
	if s == nil || !ok {
		return m.pushToastDetail(toast.Info, "Select a schema or a table first", "Put the cursor on it in the Databases view.")
	}
	c, ok := s.connection(r.conn)
	if !ok || dbclient.DialectFor(c.Driver) == nil {
		return m.pushToastDetail(toast.Info, "The table designer needs a SQL database", "")
	}
	if c.ReadOnly {
		return m.pushToastDetail(toast.Warn, sanitizeDBText(dbConnName(c))+" is read-only", "The designer changes the schema; turn read-only off in Databases: Configure first.")
	}
	d := &dbDesignState{create: create, conn: r.conn, driver: dbDriverFamily(c.Driver), production: c.Environment == "production"}
	if create {
		d.schema = dbERSchemaOf(s.nodes, r.path, c.Driver)
		if d.schema == "" {
			return m.pushToastDetail(toast.Info, "Select a schema first", "Put the cursor on a schema (or a table in it) in the Databases view.")
		}
		d.sessKey = s.sessionKeyFor(r.path, r.conn)
		d.label = sanitizeDBText(d.schema)
	} else {
		t, cmd, ok := m.databaseTableTarget()
		if !ok {
			return cmd
		}
		if t.view {
			return m.pushToastDetail(toast.Info, "A view has no columns to change", "Edit its source instead.")
		}
		d.sessKey, d.schema, d.table, d.label = t.sessKey, t.schema, t.name, t.label()
	}
	if d.driver == "sqlite" {
		d.schema = ""
	}
	if s.sessions[d.sessKey] == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	d.text = dbDesignTemplate(create, d.label)
	s.design = d
	return m.databaseDesignForm("")
}

func (m *Model) databaseDesignForm(errText string) tea.Cmd {
	d := m.databases.design
	title := "Create Table in " + d.label
	if !d.create {
		title = "Alter Table " + d.label
	}
	m.prompt = prompt.New(title, "", d.text)
	m.prompt.SetMultiline()
	if errText != "" {
		m.prompt.SetError(errText)
	}
	m.prompt.SetSize(m.w, m.h)
	m.promptKind = promptKindDBDesign
	m.promptOpen = true
	return nil
}

// databaseDesignSubmit builds the DDL from the form and shows the card;
// a mistake goes back to the form with the reason.
func (m *Model) databaseDesignSubmit(value string) tea.Cmd {
	s := m.databases
	if s == nil || s.design == nil {
		return nil
	}
	d := s.design
	d.text = value
	td, ops, create, err := dbclient.ParseDesignSpec(value)
	if err == nil && create != d.create {
		if d.create {
			err = errNotCreate
		} else {
			err = errNotAlter
		}
	}
	var plan dbclient.AlterPlan
	if err == nil {
		if d.create {
			plan.Script, err = dbclient.DesignCreateDDL(d.driver, d.schema, td)
		} else {
			plan, err = dbclient.DesignAlterDDL(d.driver, d.schema, d.table, ops)
		}
	}
	if err != nil {
		return m.databaseDesignForm(sanitizeDBText(err.Error()))
	}
	d.script = plan.Script
	label := d.label
	if d.create {
		label = d.label + "." + sanitizeDBText(td.Name)
	}
	title, body := dbDesignCardText(sanitizeDBText(d.conn), label, plan.Script, plan.Destructive, d.production)
	m.confirm = confirm.New(title, body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "edit", Title: "Edit"},
		{ID: "apply", Title: "Apply", Style: confirm.StyleDestructive},
	}).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBDesign
	return nil
}

const (
	errNotCreate = dbErr("a new table starts with: table <name>")
	errNotAlter  = dbErr("changes start with add, drop or rename (Create Table… makes a new table)")
)

// databaseDesignConfirm answers the preview card.
func (m *Model) databaseDesignConfirm(id string) tea.Cmd {
	s := m.databases
	if s == nil || s.design == nil {
		return nil
	}
	d := s.design
	switch id {
	case "edit":
		return m.databaseDesignForm("")
	case "apply":
	default:
		s.design = nil
		return nil
	}
	s.design = nil
	c, ok := s.connection(d.conn)
	if !ok {
		return nil
	}
	stmts, err := dbclient.Split(d.script, c.Driver)
	if err != nil || len(stmts) == 0 {
		return m.pushToastDetail(toast.Errr, "Cannot run the design", "The script did not split into statements.")
	}
	r := &dbRun{conn: d.conn, sql: stmts[0], statements: stmts, line: 1, by: "user", reviewed: true}
	if d.sessKey != d.conn {
		r.sessKey = d.sessKey
	}
	cs := m.dbConsole()
	cs.batchSeq++
	r.batch = cs.batchSeq
	return tea.Batch(m.databaseStartRun(r), m.pushToastDetail(toast.Info, "Applying the design", "Refresh the tree (r) to see the change."))
}
