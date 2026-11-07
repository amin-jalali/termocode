package dbclient

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// SQLServerDialect contains Microsoft SQL Server / Azure SQL (T-SQL)
// syntax and catalog queries. The driver itself is not wired yet.
type SQLServerDialect struct{}

func (SQLServerDialect) QuoteIdent(s string) string {
	return "[" + strings.ReplaceAll(s, "]", "]]") + "]"
}
func (SQLServerDialect) Placeholder(n int) string { return fmt.Sprintf("@p%d", n) }

// Limit pages with OFFSET / FETCH, which needs an ORDER BY in the query
// (callers add one, ORDER BY (SELECT NULL) when there is no order).
func (SQLServerDialect) Limit(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", offset, limit)
}

// ReadOnlyBegin: T-SQL has no read-only transaction; the read-only rules
// (statement classifier, rollback at the end) keep data safe.
func (SQLServerDialect) ReadOnlyBegin() string { return "BEGIN TRANSACTION" }
func (SQLServerDialect) CatalogQueries() map[string]string {
	return map[string]string{
		"databases":    `SELECT name FROM sys.databases ORDER BY name`,
		"schemas":      `SELECT name FROM sys.schemas WHERE name NOT IN ('sys', 'INFORMATION_SCHEMA', 'guest') AND name NOT LIKE 'db[_]%' ORDER BY name`,
		"objects":      `SELECT TABLE_NAME, TABLE_TYPE FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = @p1 ORDER BY TABLE_NAME`,
		"columns":      `SELECT COLUMN_NAME, DATA_TYPE FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = @p1 AND TABLE_NAME = @p2 ORDER BY ORDINAL_POSITION`,
		"keys":         `SELECT kcu.COLUMN_NAME FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu ON tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME AND tc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY' AND tc.TABLE_SCHEMA = @p1 AND tc.TABLE_NAME = @p2 ORDER BY kcu.ORDINAL_POSITION`,
		"indexes":      `SELECT i.name, STRING_AGG(c.name, ',') WITHIN GROUP (ORDER BY ic.key_ordinal), i.is_unique FROM sys.indexes i JOIN sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id AND ic.is_included_column = 0 JOIN sys.columns c ON c.object_id = ic.object_id AND c.column_id = ic.column_id JOIN sys.tables t ON t.object_id = i.object_id JOIN sys.schemas s ON s.schema_id = t.schema_id WHERE s.name = @p1 AND t.name = @p2 AND i.name IS NOT NULL GROUP BY i.name, i.is_unique ORDER BY i.name`,
		"foreign_keys": `SELECT fk.name, pc.name, rt.name, rc.name FROM sys.foreign_keys fk JOIN sys.foreign_key_columns fkc ON fkc.constraint_object_id = fk.object_id JOIN sys.tables t ON t.object_id = fk.parent_object_id JOIN sys.schemas s ON s.schema_id = t.schema_id JOIN sys.columns pc ON pc.object_id = fkc.parent_object_id AND pc.column_id = fkc.parent_column_id JOIN sys.tables rt ON rt.object_id = fkc.referenced_object_id JOIN sys.columns rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id WHERE s.name = @p1 AND t.name = @p2 ORDER BY fk.name, fkc.constraint_column_id`,
		"routines":     `SELECT ROUTINE_NAME FROM INFORMATION_SCHEMA.ROUTINES WHERE ROUTINE_SCHEMA = @p1 ORDER BY ROUTINE_NAME`,
		"sequences":    `SELECT name FROM sys.sequences WHERE SCHEMA_NAME(schema_id) = @p1 ORDER BY name`,
	}
}

// DDL builds a CREATE TABLE text from sys.columns (T-SQL has no SHOW
// CREATE TABLE); the table name is bound, never pasted in.
func (SQLServerDialect) DDL(table string) (string, []any) {
	return `SELECT 'CREATE TABLE ' + QUOTENAME(s.name) + '.' + QUOTENAME(t.name) + ' (' + CHAR(10) + STRING_AGG(CAST('  ' + QUOTENAME(c.name) + ' ' + TYPE_NAME(c.user_type_id) + CASE WHEN c.is_nullable = 0 THEN ' NOT NULL' ELSE '' END AS nvarchar(max)), ',' + CHAR(10)) WITHIN GROUP (ORDER BY c.column_id) + CHAR(10) + ');' FROM sys.tables t JOIN sys.schemas s ON s.schema_id = t.schema_id JOIN sys.columns c ON c.object_id = t.object_id WHERE t.name = @p1 AND s.name = SCHEMA_NAME() GROUP BY s.name, t.name`, []any{table}
}

// Explain is empty: T-SQL has no EXPLAIN prefix. The plan needs SET
// SHOWPLAN_XML in its own batch (SQLServerShowplanBatches); returning
// the bare statement here would run it.
func (SQLServerDialect) Explain(string, bool) string { return "" }

// SQLServerShowplanBatches is the three batches that read the plan of
// stmt on one session: SHOWPLAN_XML (the statement does not run) or,
// with analyze, STATISTICS XML (it runs and the actual counts come back).
func SQLServerShowplanBatches(stmt string, analyze bool) []string {
	stmt = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(stmt), ";"))
	opt := "SHOWPLAN_XML"
	if analyze {
		opt = "STATISTICS XML"
	}
	return []string{"SET " + opt + " ON", stmt, "SET " + opt + " OFF"}
}

// maxPlanDepth stops a hostile plan from nesting without end.
const maxPlanDepth = 512

// parseSQLServerShowplan reads SHOWPLAN XML / STATISTICS XML: nested
// RelOp elements, each with its cost, rows and (when it ran) actual
// counters; the object it reads comes from its Object element.
func parseSQLServerShowplan(p *Plan, text string) bool {
	if !strings.Contains(text, "<ShowPlanXML") {
		return false
	}
	d := xml.NewDecoder(strings.NewReader(text))
	// The server labels the text utf-16, but the driver hands over a Go
	// (UTF-8) string.
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	type open struct {
		n       *planNode
		op      string
		obj     string
		actRows float64
		actMs   float64
		hasAct  bool
		cost    string
		rows    string
	}
	var roots []*planNode
	var stack []*open
	depth := 0
	finish := func(o *open) {
		t := o.op
		if o.obj != "" {
			t += " on " + o.obj
		}
		t += "  cost=" + o.cost + " rows=" + o.rows
		if o.hasAct {
			t += " | actual " + ms(o.actMs) + " ms rows=" + ms(o.actRows)
		}
		o.n.text = t
	}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}
		switch e := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > maxPlanDepth {
				return false
			}
			attr := func(name string) string {
				for _, a := range e.Attr {
					if a.Name.Local == name {
						return a.Value
					}
				}
				return ""
			}
			switch e.Name.Local {
			case "StmtSimple":
				if s := strings.TrimSpace(attr("StatementText")); s != "" {
					p.Summary = append(p.Summary, "Statement: "+s)
				}
			case "RelOp":
				o := &open{n: &planNode{}, op: attr("PhysicalOp"), cost: trimNum(attr("EstimatedTotalSubtreeCost")), rows: trimNum(attr("EstimateRows"))}
				if l := attr("LogicalOp"); l != "" && l != o.op {
					o.op += " (" + l + ")"
				}
				if f, err := strconv.ParseFloat(attr("EstimatedTotalSubtreeCost"), 64); err == nil {
					o.n.total = f
				}
				if len(stack) > 0 {
					parent := stack[len(stack)-1].n
					parent.children = append(parent.children, o.n)
				} else {
					roots = append(roots, o.n)
				}
				stack = append(stack, o)
			case "Object":
				if len(stack) > 0 && stack[len(stack)-1].obj == "" {
					o := stack[len(stack)-1]
					var parts []string
					for _, k := range []string{"Schema", "Table"} {
						if v := attr(k); v != "" {
							parts = append(parts, v)
						}
					}
					o.obj = strings.Join(parts, ".")
					if ix := attr("Index"); ix != "" {
						o.obj += " using " + ix
					}
				}
			case "RunTimeCountersPerThread":
				if len(stack) > 0 {
					o := stack[len(stack)-1]
					r, _ := strconv.ParseFloat(attr("ActualRows"), 64)
					t, _ := strconv.ParseFloat(attr("ActualElapsedms"), 64)
					o.actRows += r
					o.actMs = max(o.actMs, t)
					o.hasAct = true
				}
			}
		case xml.EndElement:
			depth--
			if e.Name.Local == "RelOp" && len(stack) > 0 {
				finish(stack[len(stack)-1])
				stack = stack[:len(stack)-1]
			}
		}
	}
	if len(roots) == 0 {
		return false
	}
	var all []*planNode
	for _, r := range roots {
		r.emit(p, 0, &all)
	}
	p.costRoots(roots, all)
	return true
}

// trimNum shows a plan number short: "7.5" for "7.5000", "120" for
// "120.0"; text that is not a number stays as it is.
func trimNum(s string) string {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
