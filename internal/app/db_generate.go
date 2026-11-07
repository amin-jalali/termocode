package app

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// DBX-43: c on a table or view in the tree opens "Generate code…": SQL
// templates, column lists and a Go / TypeScript / Python type made from
// the table's columns. The text is copied; nothing is run.

const pickerKindDBGenerate pickerKindEnum = 1290

// dbGenerateItems are the picker rows; a view gets no INSERT, UPDATE or
// DELETE.
func dbGenerateItems(view bool) []picker.Item {
	rows := [][3]string{
		{"select", "SELECT", "every column"},
		{"insert", "INSERT", "a row with placeholders"},
		{"update", "UPDATE", "by the primary key"},
		{"delete", "DELETE", "by the primary key"},
		{"create", "CREATE", "the DDL"},
		{"drop", "DROP", "the statement only"},
		{"names", "Column names", "id, name, …"},
		{"qnames", "Qualified column names", "schema.table.column"},
		{"go", "Go struct", "with db and json tags"},
		{"ts", "TypeScript interface", ""},
		{"python", "Python dataclass", ""},
	}
	var out []picker.Item
	for _, r := range rows {
		if view && (r[0] == "insert" || r[0] == "update" || r[0] == "delete") {
			continue
		}
		out = append(out, picker.Item{ID: r[0], Title: r[1], Hint: r[2]})
	}
	return out
}

// dbGenerate makes the text of kind for t; ok is false for an unknown kind
// or a table without columns.
func dbGenerate(kind string, t dbTable, meta dbTableMeta) (string, bool) {
	d := dbclient.DialectFor(t.driver)
	if d == nil || len(meta.cols) == 0 && kind != "drop" && kind != "create" {
		return "", false
	}
	q := t.qualified()
	quoted := func(cols []dbColInfo, prefix string) []string {
		out := make([]string, len(cols))
		for i, c := range cols {
			out[i] = prefix + d.QuoteIdent(c.name)
		}
		return out
	}
	var keys, rest []dbColInfo
	for _, c := range meta.cols {
		if c.pk {
			keys = append(keys, c)
		} else {
			rest = append(rest, c)
		}
	}
	n := 0
	ph := func() string { n++; return d.Placeholder(n) }
	assign := func(cols []dbColInfo, sep string) string {
		parts := make([]string, len(cols))
		for i, c := range cols {
			parts[i] = d.QuoteIdent(c.name) + " = " + ph()
		}
		return strings.Join(parts, sep)
	}
	where := func() string {
		if len(keys) == 0 {
			return "WHERE <condition>;"
		}
		return "WHERE " + assign(keys, " AND ") + ";"
	}
	switch kind {
	case "select":
		return "SELECT " + strings.Join(quoted(meta.cols, ""), ", ") + "\nFROM " + q + ";", true
	case "insert":
		phs := make([]string, len(meta.cols))
		for i := range phs {
			phs[i] = ph()
		}
		return "INSERT INTO " + q + " (" + strings.Join(quoted(meta.cols, ""), ", ") + ")\nVALUES (" + strings.Join(phs, ", ") + ");", true
	case "update":
		set := rest
		if len(set) == 0 {
			set = meta.cols
		}
		s := "UPDATE " + q + "\nSET " + assign(set, ", ")
		return s + "\n" + where(), true
	case "delete":
		return "DELETE FROM " + q + "\n" + where(), true
	case "create":
		return dbBuildDDL(t, meta), true
	case "drop":
		if t.view {
			return "DROP VIEW " + q + ";", true
		}
		return "DROP TABLE " + q + ";", true
	case "names":
		names := make([]string, len(meta.cols))
		for i, c := range meta.cols {
			names[i] = sanitizeDBText(c.name)
		}
		return strings.Join(names, ", "), true
	case "qnames":
		return strings.Join(quoted(meta.cols, q+"."), ", "), true
	case "go":
		return dbGenGo(t, meta.cols), true
	case "ts":
		return dbGenTS(t, meta.cols), true
	case "python":
		return dbGenPython(t, meta.cols), true
	}
	return "", false
}

// dbTypeClass sorts a column type into int, float, bool, time, bytes or
// text.
func dbTypeClass(typ string) string {
	t := strings.ToLower(typ)
	switch {
	case strings.Contains(t, "bool"), t == "bit", t == "tinyint(1)":
		return "bool"
	case strings.Contains(t, "int") || t == "serial" || t == "bigserial" || t == "smallserial":
		return "int"
	case strings.Contains(t, "numeric"), strings.Contains(t, "decimal"), strings.Contains(t, "real"),
		strings.Contains(t, "double"), strings.Contains(t, "float"), strings.Contains(t, "money"):
		return "float"
	case strings.Contains(t, "timestamp"), strings.Contains(t, "date"), strings.HasPrefix(t, "time"):
		return "time"
	case strings.Contains(t, "bytea"), strings.Contains(t, "blob"), strings.Contains(t, "binary"):
		return "bytes"
	}
	return "text"
}

// dbWords splits a name into its letter/digit runs.
func dbWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

var dbGoInitialisms = map[string]string{"id": "ID", "url": "URL", "uri": "URI", "api": "API", "http": "HTTP", "json": "JSON", "uuid": "UUID", "sql": "SQL", "ip": "IP", "html": "HTML"}

// dbGoName is name in Go's exported CamelCase ("user_id" → "UserID").
func dbGoName(name, fallback string) string {
	var b strings.Builder
	for _, w := range dbWords(name) {
		if up, ok := dbGoInitialisms[strings.ToLower(w)]; ok {
			b.WriteString(up)
			continue
		}
		r := []rune(w)
		b.WriteString(strings.ToUpper(string(r[0])) + string(r[1:]))
	}
	s := b.String()
	if s == "" {
		return fallback
	}
	if unicode.IsDigit([]rune(s)[0]) {
		s = "T" + s
	}
	return s
}

func dbGoTag(s string) string {
	return strings.NewReplacer("`", "", `"`, "", "\n", "", "\r", "").Replace(sanitizeDBText(s))
}

func dbGenGo(t dbTable, cols []dbColInfo) string {
	types := map[string]string{"int": "int64", "float": "float64", "bool": "bool", "time": "time.Time", "bytes": "[]byte", "text": "string"}
	names := make([]string, len(cols))
	kinds := make([]string, len(cols))
	wn, wt := 0, 0
	for i, c := range cols {
		names[i] = dbGoName(c.name, fmt.Sprintf("Col%d", i+1))
		k := types[dbTypeClass(c.typ)]
		if !c.notNull && k != "[]byte" {
			k = "*" + k
		}
		kinds[i] = k
		wn, wt = max(wn, len(names[i])), max(wt, len(k))
	}
	var b strings.Builder
	b.WriteString("type " + dbGoName(t.name, "Row") + " struct {\n")
	for i, c := range cols {
		tag := dbGoTag(c.name)
		fmt.Fprintf(&b, "\t%-*s %-*s `db:\"%s\" json:\"%s\"`\n", wn, names[i], wt, kinds[i], tag, tag)
	}
	b.WriteString("}")
	return b.String()
}

func dbGenTS(t dbTable, cols []dbColInfo) string {
	types := map[string]string{"int": "number", "float": "number", "bool": "boolean", "time": "string", "bytes": "string", "text": "string"}
	var b strings.Builder
	b.WriteString("export interface " + dbGoName(t.name, "Row") + " {\n")
	for _, c := range cols {
		name := sanitizeDBText(c.name)
		if !dbPlainIdent(name) {
			name = fmt.Sprintf("%q", name)
		}
		k := types[dbTypeClass(c.typ)]
		if !c.notNull {
			k += " | null"
		}
		b.WriteString("  " + name + ": " + k + ";\n")
	}
	b.WriteString("}")
	return b.String()
}

// dbPlainIdent is true for a name that needs no quotes (letters, digits,
// _ and $, not starting with a digit).
func dbPlainIdent(s string) bool {
	for i, r := range s {
		if !(unicode.IsLetter(r) || r == '_' || r == '$' || i > 0 && unicode.IsDigit(r)) {
			return false
		}
	}
	return s != ""
}

var dbPyKeywords = map[string]bool{"False": true, "None": true, "True": true, "and": true, "as": true, "assert": true, "async": true, "await": true, "break": true, "class": true, "continue": true, "def": true, "del": true, "elif": true, "else": true, "except": true, "finally": true, "for": true, "from": true, "global": true, "if": true, "import": true, "in": true, "is": true, "lambda": true, "nonlocal": true, "not": true, "or": true, "pass": true, "raise": true, "return": true, "try": true, "while": true, "with": true, "yield": true}

func dbPyName(name string, i int) string {
	s := strings.Join(dbWords(name), "_")
	if s == "" {
		return fmt.Sprintf("col%d", i+1)
	}
	if unicode.IsDigit([]rune(s)[0]) {
		s = "c" + s
	}
	if dbPyKeywords[s] {
		s += "_"
	}
	return s
}

func dbGenPython(t dbTable, cols []dbColInfo) string {
	types := map[string]string{"int": "int", "float": "float", "bool": "bool", "time": "datetime", "bytes": "bytes", "text": "str"}
	var req, opt []string
	for i, c := range cols {
		k := types[dbTypeClass(c.typ)]
		if c.notNull {
			req = append(req, "    "+dbPyName(c.name, i)+": "+k)
		} else {
			opt = append(opt, "    "+dbPyName(c.name, i)+": Optional["+k+"] = None")
		}
	}
	lines := append([]string{"@dataclass", "class " + dbGoName(t.name, "Row") + ":"}, append(req, opt...)...)
	return strings.Join(lines, "\n")
}

// databaseCodeGenStart is c in the tree: the Generate code picker for the
// table under the cursor.
func (m *Model) databaseCodeGenStart() tea.Cmd {
	if cmd, ok := m.databaseRoutineGenStart(); ok { // db_generate_routine.go
		return cmd
	}
	t, cmd, ok := m.databaseTableTarget()
	if !ok {
		return cmd
	}
	m.databases.genTable = t
	m.databases.genRoutine = dbRoutineTarget{}
	m.picker = picker.NewItems("Generate code for "+t.label()+" (Enter copies · → inserts)", dbGenerateItems(t.view))
	m.picker.SetRowAction("→", "insert at the cursor")
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBGenerate
	m.pickerOpen = true
	return nil
}

// databaseGeneratePicker handles the pick: the columns are read, then
// onDBMeta copies the text.
func (m *Model) databaseGeneratePicker(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBGenerate {
		return nil, false
	}
	return m.databaseGeneratePick(id, false), true
}

// databaseGeneratePick runs the picked row: insert puts the text at the
// editor cursor (→) instead of copying it (Enter).
func (m *Model) databaseGeneratePick(id string, insert bool) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	if r := m.databases.genRoutine; r.name != "" {
		return m.databaseRoutineLoad(r, id, insert)
	}
	t := m.databases.genTable
	if t.name == "" {
		return nil
	}
	return m.databaseLoadMetaMsg(t, dbMetaMsg{code: id, insert: insert})
}

// onDBGenerated copies the generated text.
func (m *Model) onDBGenerated(msg dbMetaMsg) tea.Cmd {
	text, ok := dbGenerate(msg.code, msg.tbl, msg.meta)
	if !ok {
		return m.pushToastDetail(toast.Info, "Nothing to generate", "The table has no columns.")
	}
	title := msg.code
	for _, it := range dbGenerateItems(msg.tbl.view) {
		if it.ID == msg.code {
			title = it.Title
		}
	}
	return m.databaseDeliverCode(text, title+" for "+msg.tbl.label(), msg.insert)
}
