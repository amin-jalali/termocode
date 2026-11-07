package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Plan step details (DBX-39): Enter on a step of the Plan view opens a card
// with everything about that one step (its share, its table, the step it
// sits in, its details and every field the server sent). i shows the quick
// documentation of the step's table from the loaded Databases tree (no
// query runs); on a step without a table, the doc of every table the plan
// reads. Both are read-only cards: Close is the first button.

const confirmKindDBPlanStep confirmKindEnum = 1430

// dbPlanStepAt is the index in pl.parsed.Lines of the step on page row
// row (0 = the first plan row); a detail row belongs to its step. -1 when
// the row is not a plan row.
func dbPlanStepAt(pl *dbPlanRun, row int) int {
	if pl == nil || row < 0 || row >= len(pl.rows) {
		return -1
	}
	i := pl.rows[row]
	for i >= 0 && i < len(pl.parsed.Lines) && pl.parsed.Lines[i].Detail {
		i--
	}
	if i < 0 || i >= len(pl.parsed.Lines) {
		return -1
	}
	return i
}

// dbPlanOneLine is a plan text on one safe line.
func dbPlanOneLine(s string) string {
	return dbSafeMultiline(strings.ReplaceAll(strings.TrimSpace(s), "\n", " "))
}

// dbPlanStepCard is the title and body of the details card of step i.
func dbPlanStepCard(pl *dbPlanRun, i int) (string, []string) {
	lines := pl.parsed.Lines
	l := lines[i]
	var body []string
	if l.Share > 0 {
		share := fmt.Sprintf("Share of the whole plan: %.1f%%", l.Share*100)
		if pl.analyze {
			share = fmt.Sprintf("Share of the real time: %.1f%%", l.Share*100)
		}
		if l.Costly {
			share += " · the costly step"
		}
		body = append(body, share)
	} else if l.Costly {
		body = append(body, "The costly step")
	}
	if l.Table != "" {
		body = append(body, "Table: "+l.Table+" (i shows its doc)")
	}
	for j := i - 1; j >= 0; j-- {
		if !lines[j].Detail && lines[j].Depth < l.Depth {
			body = append(body, "Inside: "+dbPlanOneLine(lines[j].Text))
			break
		}
	}
	under := 0
	for j := i + 1; j < len(lines) && (lines[j].Depth > l.Depth); j++ {
		if !lines[j].Detail {
			under++
		}
	}
	if under > 0 {
		body = append(body, "Steps under it: "+dbItoa(under))
	}
	var details []string
	for j := i + 1; j < len(lines) && lines[j].Detail && lines[j].Depth == l.Depth+1; j++ {
		details = append(details, "  "+dbPlanOneLine(lines[j].Text))
	}
	if len(details) > 0 {
		body = append(append(body, "", "Details:"), details...)
	}
	if len(l.Fields) > 0 {
		body = append(body, "", "From the server:")
		for _, f := range l.Fields {
			body = append(body, "  "+dbPlanOneLine(f))
		}
	}
	if len(body) == 0 {
		body = []string{"The server sent nothing more about this step."}
	}
	return "Step: " + dbPlanOneLine(l.Text), body
}

// dbPlanTables is every table the plan reads, in plan order, once each.
func dbPlanTables(pl *dbPlanRun) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range pl.parsed.Lines {
		if l.Detail || l.Table == "" || seen[strings.ToLower(l.Table)] {
			continue
		}
		seen[strings.ToLower(l.Table)] = true
		out = append(out, l.Table)
	}
	return out
}

// dbSplitTable splits "schema.table" (a table name alone has no schema).
func dbSplitTable(name string) (schema, table string) {
	if i := strings.LastIndex(name, "."); i > 0 {
		return name[:i], name[i+1:]
	}
	return "", name
}

// dbPlanTableDoc is the quick documentation of tables on conn: the doc of
// the one table, or a section per table.
func dbPlanTableDoc(s *dbState, conn string, tables []string) (string, []string) {
	doc := func(name string) (string, []string) {
		schema, table := dbSplitTable(name)
		title, lines := dbQuickDoc(s, conn, dbclient.SQLRef{Kind: "table", Schema: schema, Table: table})
		out := lines[:0:0]
		for _, l := range lines {
			if !strings.HasPrefix(l, "o opens the data") { // that hint is for the tree
				out = append(out, l)
			}
		}
		return title, strings.Split(strings.TrimRight(strings.Join(out, "\n"), "\n"), "\n")
	}
	if len(tables) == 1 {
		return doc(tables[0])
	}
	var body []string
	for i, t := range tables {
		title, lines := doc(t)
		if i > 0 {
			body = append(body, "")
		}
		body = append(body, title)
		for _, l := range lines {
			body = append(body, "  "+l)
		}
	}
	return "Tables of the plan (" + dbPlural(len(tables), "table") + ")", body
}

// databasePlanStep is Enter on a Plan view row: the step's details card.
func (m *Model) databasePlanStep(row int) tea.Cmd {
	s := m.dbConsole()
	pl := s.shown.plan
	i := dbPlanStepAt(pl, row)
	if i < 0 {
		return m.pushToastDetail(toast.Info, "Put the cursor on a step", "Enter shows the details of the plan step under the cursor.")
	}
	pl.step = i
	title, body := dbPlanStepCard(pl, i)
	for j, l := range body {
		body[j] = sanitizeDBText(l)
	}
	buttons := []confirm.Button{{ID: "close", Title: "Close"}}
	if pl.parsed.Lines[i].Table != "" {
		buttons = append(buttons, confirm.Button{ID: "doc", Title: "Table doc", Style: confirm.StylePrimary})
	}
	buttons = append(buttons, confirm.Button{ID: "copy", Title: "Copy"})
	m.confirm = confirm.New(sanitizeDBText(title), strings.Join(body, "\n"), buttons).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBPlanStep
	return nil
}

// databasePlanStepConfirm answers the step card.
func (m *Model) databasePlanStepConfirm(id string) tea.Cmd {
	s := m.dbConsole()
	if s.shown == nil || s.shown.plan == nil {
		return nil
	}
	pl := s.shown.plan
	if pl.step < 0 || pl.step >= len(pl.parsed.Lines) {
		return nil
	}
	switch id {
	case "doc":
		return m.databasePlanDoc(pl, pl.step)
	case "copy":
		title, body := dbPlanStepCard(pl, pl.step)
		return m.copyWithToast(title+"\n"+strings.Join(body, "\n")+"\n", "Copied the step")
	}
	return nil
}

// databasePlanTableDocKey is i on a Plan view row.
func (m *Model) databasePlanTableDocKey(row int) tea.Cmd {
	s := m.dbConsole()
	pl := s.shown.plan
	return m.databasePlanDoc(pl, dbPlanStepAt(pl, row))
}

// databasePlanDoc shows the doc of the table of step i, or of every table
// of the plan when step i reads none (or i is -1).
func (m *Model) databasePlanDoc(pl *dbPlanRun, i int) tea.Cmd {
	tables := dbPlanTables(pl)
	if i >= 0 && pl.parsed.Lines[i].Table != "" {
		tables = []string{pl.parsed.Lines[i].Table}
	}
	if len(tables) == 0 {
		return m.pushToastDetail(toast.Info, "This plan reads no named table", "")
	}
	m.ensureDatabasesLoaded()
	title, body := dbPlanTableDoc(m.databases, m.dbConsole().shown.conn, tables)
	return m.databaseQuickDocCard(title, body)
}
