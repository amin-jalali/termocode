package dbclient

import (
	"fmt"
	"sort"
	"strings"
)

// CanonFile is one object kind of a schema as canonical DDL.
type CanonFile struct{ Name, Text string }

// CompareFile is one object kind of a compare: Old is the target (the
// schema the script changes), New the source (the model it copies), so
// the + lines of the diff are what the script adds.
type CompareFile struct{ Name, Old, New string }

// SchemaComparison is the outcome of CompareSchemas.
type SchemaComparison struct {
	Files       []CompareFile
	Script      string // the migration script: text only, never run
	Changes     int    // objects that differ
	Destructive int    // DROP statements in the script
	Truncated   bool   // one side was only read in part
}

// sortCanon returns s with every list in canonical order (a copy: the
// caller's slices are not reordered).
func sortCanon(s CanonSchema) CanonSchema {
	s.Tables = append([]CanonTable(nil), s.Tables...)
	for i := range s.Tables {
		t := &s.Tables[i]
		t.Constraints = append([]CanonConstraint(nil), t.Constraints...)
		sort.SliceStable(t.Constraints, func(i, j int) bool {
			a, b := t.Constraints[i], t.Constraints[j]
			if ka, kb := constraintRank(a.Kind), constraintRank(b.Kind); ka != kb {
				return ka < kb
			}
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			return strings.Join(a.Columns, "\x1f") < strings.Join(b.Columns, "\x1f")
		})
		t.Indexes = append([]CanonIndex(nil), t.Indexes...)
		for k := range t.Indexes {
			t.Indexes[k].Table = t.Name
		}
		sort.SliceStable(t.Indexes, func(i, j int) bool {
			if t.Indexes[i].Name != t.Indexes[j].Name {
				return t.Indexes[i].Name < t.Indexes[j].Name
			}
			return strings.Join(t.Indexes[i].Exprs, "\x1f") < strings.Join(t.Indexes[j].Exprs, "\x1f")
		})
	}
	sort.SliceStable(s.Tables, func(i, j int) bool { return s.Tables[i].Name < s.Tables[j].Name })
	s.Views = append([]CanonView(nil), s.Views...)
	sort.SliceStable(s.Views, func(i, j int) bool { return s.Views[i].Name < s.Views[j].Name })
	s.Routines = append([]CanonRoutine(nil), s.Routines...)
	sort.SliceStable(s.Routines, func(i, j int) bool { return routineKey(s.Routines[i]) < routineKey(s.Routines[j]) })
	return s
}

func routineKey(r CanonRoutine) string { return r.Name + "\x00" + r.Kind + "\x00" + r.Args }

// canonWriter renders one dialect; qual qualifies a name with the
// target schema (the script) or not at all (the canonical files).
type canonWriter struct {
	driver string
	q      func(string) string
	schema string // "" in the canonical files
}

func newCanonWriter(driver, schema string) canonWriter {
	d := DialectFor(driver)
	if d == nil {
		d = PostgresDialect{}
	}
	if normDriver(driver) == "sqlite" && (schema == "main" || schema == "") {
		schema = ""
	}
	return canonWriter{driver: normDriver(driver), q: d.QuoteIdent, schema: schema}
}

func (w canonWriter) sqlite() bool { return w.driver == "sqlite" }

// qual is a table or view name, with the schema in the script.
func (w canonWriter) qual(name string) string {
	if w.schema == "" {
		return w.q(name)
	}
	return w.q(w.schema) + "." + w.q(name)
}

func (w canonWriter) quoteAll(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = w.q(n)
	}
	return strings.Join(out, ", ")
}

func (w canonWriter) column(c CanonColumn) string {
	s := w.q(c.Name)
	if c.Type != "" {
		s += " " + c.Type
	}
	if c.NotNull {
		s += " NOT NULL"
	}
	if c.Default != "" {
		s += " DEFAULT " + c.Default
	}
	if c.Extra != "" {
		s += " " + c.Extra
	}
	return s
}

// constraint renders a table constraint; ref names the referenced table
// (qualified in the script).
func (w canonWriter) constraint(c CanonConstraint, ref func(string) string) string {
	s := ""
	if c.Name != "" {
		s = "CONSTRAINT " + w.q(c.Name) + " "
	}
	switch c.Kind {
	case "CHECK":
		return s + c.Check
	case "FOREIGN KEY":
		s += "FOREIGN KEY (" + w.quoteAll(c.Columns) + ") REFERENCES " + ref(c.RefTable)
		if len(c.RefColumns) > 0 {
			s += " (" + w.quoteAll(c.RefColumns) + ")"
		}
		if c.OnUpdate != "" && c.OnUpdate != "NO ACTION" {
			s += " ON UPDATE " + c.OnUpdate
		}
		if c.OnDelete != "" && c.OnDelete != "NO ACTION" {
			s += " ON DELETE " + c.OnDelete
		}
		return s
	}
	return s + c.Kind + " (" + w.quoteAll(c.Columns) + ")"
}

// table is CREATE TABLE with its columns and constraints; withFK false
// leaves the foreign keys out (the script adds them after every table).
func (w canonWriter) table(t CanonTable, withFK bool) string {
	var parts []string
	for _, c := range t.Columns {
		parts = append(parts, "  "+w.column(c))
	}
	for _, c := range t.Constraints {
		if c.Kind == "FOREIGN KEY" && !withFK {
			continue
		}
		parts = append(parts, "  "+w.constraint(c, w.refName))
	}
	return "CREATE TABLE " + w.qual(t.Name) + " (\n" + strings.Join(parts, ",\n") + "\n);"
}

// refName is the referenced table of a foreign key: SQLite cannot
// qualify it, the others reference the table in the same schema.
func (w canonWriter) refName(name string) string {
	if w.sqlite() {
		return w.q(name)
	}
	return w.qual(name)
}

func (w canonWriter) index(ix CanonIndex) string {
	if ix.Raw != "" {
		return strings.TrimRight(ix.Raw, "; ") + ";"
	}
	head := "CREATE "
	if ix.Unique {
		head += "UNIQUE "
	}
	method, using := ix.Method, ""
	if w.driver == "mysql" && (method == "FULLTEXT" || method == "SPATIAL") {
		head += method + " "
		method = ""
	}
	name, table := w.q(ix.Name), w.qual(ix.Table)
	if w.sqlite() && w.schema != "" {
		name, table = w.qual(ix.Name), w.q(ix.Table)
	}
	s := head + "INDEX " + name + " ON " + table
	if method != "" {
		if w.driver == "mysql" {
			using = " USING " + method
		} else {
			s += " USING " + method
		}
	}
	s += " (" + strings.Join(ix.Exprs, ", ") + ")" + using
	if ix.Where != "" {
		s += " WHERE " + ix.Where
	}
	return s + ";"
}

func (w canonWriter) dropIndex(ix CanonIndex) string {
	if w.driver == "mysql" {
		return "DROP INDEX " + w.q(ix.Name) + " ON " + w.qual(ix.Table) + ";"
	}
	return "DROP INDEX " + w.qual(ix.Name) + ";"
}

func (w canonWriter) view(v CanonView) string {
	if v.Full {
		return v.Definition + ";"
	}
	kind := "VIEW"
	if v.Materialized {
		kind = "MATERIALIZED VIEW"
	}
	return "CREATE " + kind + " " + w.qual(v.Name) + " AS\n" + v.Definition + ";"
}

func (w canonWriter) dropView(v CanonView) string {
	kind := "VIEW"
	if v.Materialized {
		kind = "MATERIALIZED VIEW"
	}
	return "DROP " + kind + " " + w.qual(v.Name) + ";"
}

func (w canonWriter) routine(r CanonRoutine) string {
	s := "CREATE " + r.Kind + " " + w.qual(r.Name) + "(" + r.Args + ")"
	if r.Kind == "FUNCTION" && r.Returns != "" {
		s += " RETURNS " + r.Returns
	}
	if w.driver == "postgres" {
		tag := "$$"
		if strings.Contains(r.Body, "$$") {
			tag = "$body$"
		}
		return s + " LANGUAGE " + r.Language + " AS " + tag + "\n" + r.Body + "\n" + tag + ";"
	}
	return s + "\n" + r.Body + ";"
}

func (w canonWriter) routineLabel(r CanonRoutine) string {
	return strings.ToLower(r.Kind) + " " + w.q(r.Name) + "(" + r.Args + ")"
}

func joinBlocks(blocks []string, empty string) string {
	if len(blocks) == 0 {
		return empty + "\n"
	}
	return strings.Join(blocks, "\n\n") + "\n"
}

// CanonFiles is s as canonical DDL, one text per object kind: tables
// (with columns, keys and foreign keys), views, routines (not SQLite) and
// indexes.
func CanonFiles(s CanonSchema) []CanonFile {
	s = sortCanon(s)
	w := newCanonWriter(s.Driver, "")
	var tables, views, routines, indexes []string
	for _, t := range s.Tables {
		tables = append(tables, w.table(t, true))
		for _, ix := range t.Indexes {
			indexes = append(indexes, w.index(ix))
		}
	}
	for _, v := range s.Views {
		views = append(views, w.view(v))
	}
	for _, r := range s.Routines {
		routines = append(routines, w.routine(r))
	}
	out := []CanonFile{
		{Name: "tables.sql", Text: joinBlocks(tables, "-- no tables")},
		{Name: "views.sql", Text: joinBlocks(views, "-- no views")},
	}
	if !w.sqlite() {
		out = append(out, CanonFile{Name: "routines.sql", Text: joinBlocks(routines, "-- no routines")})
	}
	return append(out, CanonFile{Name: "indexes.sql", Text: joinBlocks(indexes, "-- no indexes")})
}

// scriptB collects the migration script.
type scriptB struct {
	body  []string
	drops []string
	todo  int
}

func (b *scriptB) section(title string) { b.body = append(b.body, "", "-- "+title) }
func (b *scriptB) add(stmt string)      { b.body = append(b.body, stmt) }
func (b *scriptB) review(note string)   { b.body = append(b.body, "-- REVIEW: "+note); b.todo++ }
func (b *scriptB) drop(stmt string) {
	b.body = append(b.body, "-- DESTRUCTIVE: review before running", stmt)
	b.drops = append(b.drops, strings.TrimSuffix(stmt, ";"))
}

// CompareSchemas compares source (the model) with target (the schema to
// change) and writes a basic migration script that makes the target
// match the source. The script is text: nothing here runs it.
func CompareSchemas(source, target CanonSchema) SchemaComparison {
	source, target = sortCanon(source), alignColumns(sortCanon(target), source)
	out := SchemaComparison{Truncated: source.Truncated || target.Truncated}
	srcFiles, tgtFiles := CanonFiles(source), CanonFiles(target)
	for i := range srcFiles {
		out.Files = append(out.Files, CompareFile{Name: srcFiles[i].Name, Old: tgtFiles[i].Text, New: srcFiles[i].Text})
	}
	canon := newCanonWriter(source.Driver, "")
	w := newCanonWriter(source.Driver, target.Schema)
	var b scriptB
	// View bodies name tables without a schema: they resolve in the target.
	switch {
	case w.driver == "postgres" && target.Schema != "":
		b.section("Names without a schema (view bodies) resolve in the target schema")
		b.add("SET search_path TO " + w.q(target.Schema) + ";")
	case w.driver == "mysql" && target.Schema != "":
		b.section("Names without a schema (view bodies) resolve in the target database")
		b.add("USE " + w.q(target.Schema) + ";")
	}

	srcT, tgtT := map[string]CanonTable{}, map[string]CanonTable{}
	for _, t := range source.Tables {
		srcT[t.Name] = t
	}
	for _, t := range target.Tables {
		tgtT[t.Name] = t
	}
	srcV, tgtV := map[string]CanonView{}, map[string]CanonView{}
	for _, v := range source.Views {
		srcV[v.Name] = v
	}
	for _, v := range target.Views {
		tgtV[v.Name] = v
	}
	indexKey := func(ix CanonIndex) string { return ix.Table + "\x00" + ix.Name }
	srcI, tgtI := map[string]CanonIndex{}, map[string]CanonIndex{}
	var srcIOrder, tgtIOrder []string
	for _, t := range source.Tables {
		for _, ix := range t.Indexes {
			srcI[indexKey(ix)] = ix
			srcIOrder = append(srcIOrder, indexKey(ix))
		}
	}
	for _, t := range target.Tables {
		for _, ix := range t.Indexes {
			tgtI[indexKey(ix)] = ix
			tgtIOrder = append(tgtIOrder, indexKey(ix))
		}
	}

	// 1. Views that go or change are dropped first: they depend on tables.
	var viewDrops, viewCreates []CanonView
	for _, v := range target.Views {
		s, ok := srcV[v.Name]
		if !ok || canon.view(s) != canon.view(v) {
			viewDrops = append(viewDrops, v)
		}
	}
	for _, v := range source.Views {
		t, ok := tgtV[v.Name]
		if !ok || canon.view(t) != canon.view(v) {
			viewCreates = append(viewCreates, v)
		}
	}
	out.Changes += len(viewCreates) + countMissing(viewDrops, srcV)
	if len(viewDrops) > 0 {
		b.section("Views: drop the ones that go or change (they depend on tables)")
		for _, v := range viewDrops {
			b.drop(w.dropView(v))
		}
	}

	// 2. Indexes that go or change (on tables that stay).
	var ixDrops, ixCreates []CanonIndex
	for _, k := range tgtIOrder {
		ix := tgtI[k]
		s, ok := srcI[k]
		if _, keep := srcT[ix.Table]; keep && (!ok || canon.index(s) != canon.index(ix)) {
			ixDrops = append(ixDrops, ix)
		}
	}
	for _, k := range srcIOrder {
		ix := srcI[k]
		t, ok := tgtI[k]
		if !ok || canon.index(t) != canon.index(ix) {
			ixCreates = append(ixCreates, ix)
		}
	}
	for _, k := range tgtIOrder {
		if _, ok := srcI[k]; !ok {
			out.Changes++
		}
	}
	out.Changes += len(ixCreates)
	if len(ixDrops) > 0 {
		b.section("Indexes: drop the ones that go or change")
		for _, ix := range ixDrops {
			b.drop(w.dropIndex(ix))
		}
	}

	// 3. New tables; their foreign keys come after every table exists.
	var newTables []CanonTable
	for _, t := range source.Tables {
		if _, ok := tgtT[t.Name]; !ok {
			newTables = append(newTables, t)
		}
	}
	if len(newTables) > 0 {
		b.section("Tables only in the source: create them")
		for _, t := range newTables {
			b.add(w.table(t, w.sqlite()))
		}
	}
	out.Changes += len(newTables)

	// 4. Tables in both: add and drop columns; the rest is for a person.
	var adds, drops, notes []string
	for _, t := range source.Tables {
		old, ok := tgtT[t.Name]
		if !ok {
			continue
		}
		if canon.table(old, true) != canon.table(t, true) {
			out.Changes++
		}
		oldCols := map[string]CanonColumn{}
		for _, c := range old.Columns {
			oldCols[c.Name] = c
		}
		newCols := map[string]bool{}
		for _, c := range t.Columns {
			newCols[c.Name] = true
			o, ok := oldCols[c.Name]
			switch {
			case !ok:
				adds = append(adds, "ALTER TABLE "+w.qual(t.Name)+" ADD COLUMN "+w.column(c)+";")
			case canon.column(o) != canon.column(c):
				notes = append(notes, fmt.Sprintf("column %s.%s differs: target has %s, source has %s. Not changed by this script.",
					w.q(t.Name), w.q(c.Name), columnSpec(canon, o), columnSpec(canon, c)))
			}
		}
		for _, c := range old.Columns {
			if !newCols[c.Name] {
				drops = append(drops, "ALTER TABLE "+w.qual(t.Name)+" DROP COLUMN "+w.q(c.Name)+";")
			}
		}
		oldCons, newCons := map[string]bool{}, map[string]bool{}
		for _, c := range old.Constraints {
			oldCons[canon.constraint(c, canon.q)] = true
		}
		for _, c := range t.Constraints {
			text := canon.constraint(c, canon.q)
			newCons[text] = true
			if !oldCons[text] {
				notes = append(notes, fmt.Sprintf("table %s: only the source has %s", w.q(t.Name), text))
			}
		}
		for _, c := range old.Constraints {
			if text := canon.constraint(c, canon.q); !newCons[text] {
				notes = append(notes, fmt.Sprintf("table %s: only the target has %s", w.q(t.Name), text))
			}
		}
	}
	if len(adds) > 0 {
		b.section("Columns only in the source: add them")
		for _, s := range adds {
			b.add(s)
		}
	}
	if len(drops) > 0 {
		b.section("Columns only in the target: drop them (their data is lost)")
		for _, s := range drops {
			b.drop(s)
		}
	}
	if len(notes) > 0 {
		b.section("Differences this script does not change")
		for _, n := range notes {
			b.review(n)
		}
	}

	// 5. Tables only in the target.
	var gone []CanonTable
	for _, t := range target.Tables {
		if _, ok := srcT[t.Name]; !ok {
			gone = append(gone, t)
		}
	}
	out.Changes += len(gone)
	if len(gone) > 0 {
		b.section("Tables only in the target: drop them (their data is lost)")
		for _, t := range gone {
			b.drop("DROP TABLE " + w.qual(t.Name) + ";")
		}
	}

	// 6. Foreign keys of the new tables.
	if !w.sqlite() {
		var fks []string
		for _, t := range newTables {
			for _, c := range t.Constraints {
				if c.Kind == "FOREIGN KEY" {
					fks = append(fks, "ALTER TABLE "+w.qual(t.Name)+" ADD "+w.constraint(c, w.refName)+";")
				}
			}
		}
		if len(fks) > 0 {
			b.section("Foreign keys of the new tables")
			for _, s := range fks {
				b.add(s)
			}
		}
	}

	// 7. Indexes and views of the source.
	if len(ixCreates) > 0 {
		b.section("Indexes only in the source, or changed: create them")
		for _, ix := range ixCreates {
			b.add(w.index(ix))
		}
	}
	if len(viewCreates) > 0 {
		b.section("Views only in the source, or changed: create them")
		for _, v := range viewCreates {
			b.add(w.view(v))
		}
	}

	// 8. Routines: shown, never written.
	srcR, tgtR := map[string]CanonRoutine{}, map[string]CanonRoutine{}
	for _, r := range source.Routines {
		srcR[routineKey(r)] = r
	}
	for _, r := range target.Routines {
		tgtR[routineKey(r)] = r
	}
	var rnotes []string
	for _, r := range source.Routines {
		t, ok := tgtR[routineKey(r)]
		switch {
		case !ok:
			rnotes = append(rnotes, w.routineLabel(r)+" is only in the source: create it by hand (routines.sql has its text).")
		case canon.routine(t) != canon.routine(r):
			rnotes = append(rnotes, w.routineLabel(r)+" differs: see routines.sql.")
		default:
			continue
		}
		out.Changes++
	}
	for _, r := range target.Routines {
		if _, ok := srcR[routineKey(r)]; !ok {
			rnotes = append(rnotes, w.routineLabel(r)+" is only in the target: drop it by hand if nothing needs it.")
			out.Changes++
		}
	}
	if len(rnotes) > 0 {
		b.section("Routines (never written by this script)")
		for _, n := range rnotes {
			b.review(n)
		}
	}

	out.Destructive = len(b.drops)
	out.Script = scriptHeader(source, target, b) + strings.Join(b.body, "\n") + "\n"
	if out.Changes == 0 {
		out.Script = scriptHeader(source, target, b) + "\n-- The schemas are the same. There is nothing to change.\n"
	}
	return out
}

// alignColumns puts the columns of target's tables in the order the
// source has them (the others after, in their own order): a column order
// that differs only because of the migration history is not a change.
func alignColumns(target, source CanonSchema) CanonSchema {
	order := map[string]map[string]int{}
	for _, t := range source.Tables {
		pos := map[string]int{}
		for i, c := range t.Columns {
			pos[c.Name] = i
		}
		order[t.Name] = pos
	}
	for i := range target.Tables {
		pos, ok := order[target.Tables[i].Name]
		if !ok {
			continue
		}
		cols := append([]CanonColumn(nil), target.Tables[i].Columns...)
		sort.SliceStable(cols, func(a, b int) bool {
			pa, oka := pos[cols[a].Name]
			pb, okb := pos[cols[b].Name]
			switch {
			case oka && okb:
				return pa < pb
			case oka != okb:
				return oka
			}
			return false
		})
		target.Tables[i].Columns = cols
	}
	return target
}

func countMissing(views []CanonView, in map[string]CanonView) int {
	n := 0
	for _, v := range views {
		if _, ok := in[v.Name]; !ok {
			n++
		}
	}
	return n
}

func columnSpec(w canonWriter, c CanonColumn) string {
	spec := strings.TrimSpace(strings.TrimPrefix(w.column(c), w.q(c.Name)))
	if spec == "" {
		return "(no type)"
	}
	return spec
}

func scriptLabel(s CanonSchema) string {
	if s.Label != "" {
		return canonClean(s.Label, false)
	}
	return s.Schema
}

func scriptHeader(source, target CanonSchema, b scriptB) string {
	lines := []string{
		"-- Schema migration script (Databases: Compare Schemas)",
		"-- Target, the schema this script changes: " + scriptLabel(target),
		"-- Source, the schema it copies:           " + scriptLabel(source),
		"-- review before running. Nothing in this script has been run: termocode never runs it.",
		"-- Only basic changes are written: CREATE / DROP TABLE, ADD / DROP COLUMN, indexes, views.",
		`-- Lines that start with "-- REVIEW:" need a change by hand.`,
	}
	if source.Truncated || target.Truncated {
		lines = append(lines, fmt.Sprintf("-- NOTE: a schema is large: only part of it was compared (at most %d tables).", CompareMaxTables))
	}
	if len(b.drops) > 0 {
		word := "statements"
		if len(b.drops) == 1 {
			word = "statement"
		}
		lines = append(lines, fmt.Sprintf("-- WARNING: %d destructive %s. They delete objects or data:", len(b.drops), word))
		for _, d := range b.drops {
			lines = append(lines, "--   "+strings.ReplaceAll(d, "\n", " "))
		}
	} else {
		lines = append(lines, "-- No destructive statements.")
	}
	return strings.Join(lines, "\n") + "\n"
}
