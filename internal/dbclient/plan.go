package dbclient

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Plan is a query plan as indented lines: what the Plan view shows. The
// parsers read what the server returned; they never run SQL.
type Plan struct {
	Lines   []PlanLine
	Summary []string // lines under the tree ("Planning time: 0.2 ms")
	Raw     string   // the server's own text (JSON or the tree)
	Analyze bool
}

// PlanLine is one node (or one detail of a node) of a plan.
type PlanLine struct {
	Depth  int
	Text   string
	Detail bool // a condition or sort key of the node above, not a node
	Costly bool // this node takes the biggest share of the cost or time
}

// Nodes is the number of plan nodes (the details do not count).
func (p Plan) Nodes() int {
	n := 0
	for _, l := range p.Lines {
		if !l.Detail {
			n++
		}
	}
	return n
}

// costlyShare is the share of the whole plan one node must take (of its
// own cost, without its children) to be marked costly.
const costlyShare = 0.4

// ExplainForms is the SQL to ask the server for the plan of stmt, best
// form first; the next form is tried only when the server refuses the one
// before (an old MySQL without FORMAT=JSON). analyze runs the statement.
// SQLite has no ANALYZE: it always gives the plan.
func ExplainForms(driver, stmt string, analyze, mariadb bool) []string {
	stmt = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(stmt), ";"))
	switch normDriver(driver) {
	case "postgres", "postgresql":
		if analyze {
			return []string{"EXPLAIN (ANALYZE, FORMAT JSON) " + stmt}
		}
		return []string{"EXPLAIN (FORMAT JSON) " + stmt}
	case "mysql":
		switch {
		case analyze && mariadb:
			return []string{"ANALYZE FORMAT=JSON " + stmt, "ANALYZE " + stmt}
		case analyze:
			return []string{"EXPLAIN ANALYZE " + stmt}
		}
		return []string{"EXPLAIN FORMAT=JSON " + stmt, "EXPLAIN " + stmt}
	case "sqlite":
		return []string{"EXPLAIN QUERY PLAN " + stmt}
	}
	return nil
}

// ParsePlan turns the rows of an explain result into a Plan. Anything it
// does not know is shown as the text the server sent.
func ParsePlan(driver string, res Result, analyze bool) Plan {
	p := Plan{Analyze: analyze}
	var texts []string
	for _, row := range res.Rows {
		for _, c := range row {
			if !c.Null {
				texts = append(texts, c.Text)
			}
		}
	}
	p.Raw = strings.Join(texts, "\n")
	whole := strings.TrimSpace(p.Raw)
	switch normDriver(driver) {
	case "postgres", "postgresql":
		if parsePGJSON(&p, whole) {
			return p
		}
	case "mysql":
		if strings.HasPrefix(whole, "{") && parseMySQLJSON(&p, whole) {
			return p
		}
		if hasColumn(res, "select_type") {
			parseMySQLTable(&p, res)
			return p
		}
	case "sqlite":
		if parseSQLitePlan(&p, res) {
			return p
		}
	}
	parseIndentedText(&p, texts)
	return p
}

func hasColumn(res Result, name string) bool {
	for _, c := range res.Columns {
		if strings.EqualFold(c.Name, name) {
			return true
		}
	}
	return false
}

// planNode is the tree the JSON forms are read into before they become
// lines; own is the cost or time the node takes without its children.
type planNode struct {
	text     string
	details  []string
	children []*planNode
	total    float64 // cost or time including the children (0: unknown)
	line     int     // index in Plan.Lines
}

func (n *planNode) emit(p *Plan, depth int, nodes *[]*planNode) {
	n.line = len(p.Lines)
	p.Lines = append(p.Lines, PlanLine{Depth: depth, Text: n.text})
	*nodes = append(*nodes, n)
	for _, d := range n.details {
		p.Lines = append(p.Lines, PlanLine{Depth: depth + 1, Text: d, Detail: true})
	}
	for _, c := range n.children {
		c.emit(p, depth+1, nodes)
	}
}

// costRoots flags the costly nodes of every tree in roots.
func (p *Plan) costRoots(roots []*planNode, all []*planNode) {
	if len(all) < 2 {
		return
	}
	var rootTotal float64
	for _, r := range roots {
		rootTotal += r.total
	}
	if rootTotal <= 0 {
		return
	}
	for _, n := range all {
		if n.total <= 0 {
			continue
		}
		own := n.total
		for _, c := range n.children {
			own -= c.total
		}
		if own/rootTotal >= costlyShare {
			p.Lines[n.line].Costly = true
		}
	}
}

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return ""
	}
	return ""
}

func ms(f float64) string {
	return strconv.FormatFloat(math.Round(f*1000)/1000, 'f', -1, 64)
}

// parsePGJSON reads EXPLAIN (FORMAT JSON): [{"Plan": {...}, ...}].
func parsePGJSON(p *Plan, text string) bool {
	var top []map[string]any
	if json.Unmarshal([]byte(text), &top) != nil || len(top) == 0 {
		return false
	}
	root, _ := top[0]["Plan"].(map[string]any)
	if root == nil {
		return false
	}
	var build func(m map[string]any) *planNode
	build = func(m map[string]any) *planNode {
		n := &planNode{}
		typ := scalar(m["Node Type"])
		if j := scalar(m["Join Type"]); j != "" && j != "Inner" && strings.Contains(typ, "Join") {
			typ = j + " " + typ
		}
		if rel := scalar(m["Relation Name"]); rel != "" {
			typ += " on " + rel
			if a := scalar(m["Alias"]); a != "" && a != rel {
				typ += " " + a
			}
		}
		if idx := scalar(m["Index Name"]); idx != "" {
			typ += " using " + idx
		}
		if sub := scalar(m["Subplan Name"]); sub != "" {
			typ = sub + ": " + typ
		}
		var cost string
		if tc, ok := num(m["Total Cost"]); ok {
			sc, _ := num(m["Startup Cost"])
			rows, _ := num(m["Plan Rows"])
			cost = fmt.Sprintf("cost=%s..%s rows=%s", ms(sc), ms(tc), ms(rows))
			n.total = tc
		}
		if at, ok := num(m["Actual Total Time"]); ok {
			loops, _ := num(m["Actual Loops"])
			if loops <= 0 {
				loops = 1
			}
			rows, _ := num(m["Actual Rows"])
			cost += fmt.Sprintf(" | actual %s ms rows=%s loops=%s", ms(at), ms(rows), ms(loops))
			n.total = at * loops // time beats cost when the statement ran
		}
		n.text = strings.TrimSpace(typ + "  " + cost)
		keys := make([]string, 0, len(m))
		for k := range m {
			switch k {
			case "Filter", "Index Cond", "Recheck Cond", "Hash Cond", "Merge Cond", "Join Filter", "Sort Key", "Group Key", "Sort Method", "Rows Removed by Filter", "Heap Fetches", "Output":
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch x := m[k].(type) {
			case []any:
				var parts []string
				for _, e := range x {
					parts = append(parts, scalar(e))
				}
				n.details = append(n.details, k+": "+strings.Join(parts, ", "))
			default:
				n.details = append(n.details, k+": "+scalar(x))
			}
		}
		if kids, ok := m["Plans"].([]any); ok {
			for _, k := range kids {
				if km, ok := k.(map[string]any); ok {
					n.children = append(n.children, build(km))
				}
			}
		}
		return n
	}
	tree := build(root)
	var all []*planNode
	tree.emit(p, 0, &all)
	p.costRoots([]*planNode{tree}, all)
	if v, ok := num(top[0]["Planning Time"]); ok {
		p.Summary = append(p.Summary, "Planning time: "+ms(v)+" ms")
	}
	if v, ok := num(top[0]["Execution Time"]); ok {
		p.Summary = append(p.Summary, "Execution time: "+ms(v)+" ms")
	}
	return true
}

// parseMySQLJSON reads EXPLAIN FORMAT=JSON of MySQL and MariaDB: nested
// objects with a "table" object at each leaf.
func parseMySQLJSON(p *Plan, text string) bool {
	var top map[string]any
	if json.Unmarshal([]byte(text), &top) != nil {
		return false
	}
	qb, _ := top["query_block"].(map[string]any)
	if qb == nil {
		return false
	}
	var all []*planNode
	var walk func(name string, m map[string]any) *planNode
	walk = func(name string, m map[string]any) *planNode {
		n := &planNode{text: name}
		isTable := false
		if tn := scalar(m["table_name"]); tn != "" {
			isTable = true
			n.text = tn
			if at := scalar(m["access_type"]); at != "" {
				n.text += "  " + at
			}
			if k := scalar(m["key"]); k != "" {
				n.text += " using " + k
			}
			if r, ok := num(m["rows_examined_per_scan"]); ok {
				n.text += " rows=" + ms(r)
			} else if r, ok := num(m["rows"]); ok {
				n.text += " rows=" + ms(r)
			}
			if t, ok := num(m["r_total_time_ms"]); ok {
				n.text += " | actual " + ms(t) + " ms"
				n.total = t
			}
			if ci, _ := m["cost_info"].(map[string]any); ci != nil {
				rc, _ := num(ci["read_cost"])
				ec, _ := num(ci["eval_cost"])
				if n.total == 0 {
					n.total = rc + ec
				}
			}
			if ac := scalar(m["attached_condition"]); ac != "" {
				n.details = append(n.details, "Condition: "+ac)
			}
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch x := m[k].(type) {
			case map[string]any:
				if k == "cost_info" || (isTable && k == "used_columns") {
					continue
				}
				n.children = append(n.children, walk(k, x))
			case []any:
				if isTable && k == "used_columns" {
					continue
				}
				for i, e := range x {
					if em, ok := e.(map[string]any); ok {
						label := k
						if len(x) > 1 {
							label = k + " " + strconv.Itoa(i+1)
						}
						if k == "nested_loop" {
							// the tables of a join are the children
							n.children = append(n.children, walkWrapper(em, walk)...)
							continue
						}
						n.children = append(n.children, walk(label, em))
					}
				}
			}
		}
		if isTable {
			// a table node's total is its own work; children subtract it
			var sub float64
			for _, c := range n.children {
				sub += c.total
			}
			n.total += sub
		}
		return n
	}
	tree := walk("query_block", qb)
	if ci, _ := qb["cost_info"].(map[string]any); ci != nil {
		if qc, ok := num(ci["query_cost"]); ok {
			tree.text = "query_block  cost=" + ms(qc)
			sumTables := 0.0
			for _, c := range tree.children {
				sumTables += c.total
			}
			tree.total = math.Max(qc, sumTables)
		}
	}
	if tree.total == 0 {
		for _, c := range tree.children {
			tree.total += c.total
		}
	}
	tree.emit(p, 0, &all)
	p.costRoots([]*planNode{tree}, all)
	return true
}

func walkWrapper(m map[string]any, walk func(string, map[string]any) *planNode) []*planNode {
	if t, ok := m["table"].(map[string]any); ok {
		return []*planNode{walk("table", t)}
	}
	return []*planNode{walk("join", m)}
}

// parseMySQLTable reads the classic EXPLAIN table (or MariaDB's ANALYZE):
// one line per row, the table with its access type, key and row estimate.
func parseMySQLTable(p *Plan, res Result) {
	idx := map[string]int{}
	for i, c := range res.Columns {
		idx[strings.ToLower(c.Name)] = i
	}
	get := func(row []Cell, name string) string {
		if i, ok := idx[name]; ok && i < len(row) && !row[i].Null {
			return row[i].Text
		}
		return ""
	}
	var total float64
	rowsOf := make([]float64, len(res.Rows))
	for i, row := range res.Rows {
		rowsOf[i], _ = strconv.ParseFloat(get(row, "rows"), 64)
		total += rowsOf[i]
	}
	for i, row := range res.Rows {
		text := get(row, "table")
		if text == "" {
			text = get(row, "select_type")
		}
		if t := get(row, "type"); t != "" {
			text += "  " + t
		}
		if k := get(row, "key"); k != "" {
			text += " using " + k
		}
		if r := get(row, "rows"); r != "" {
			text += " rows=" + r
		}
		if r := get(row, "r_rows"); r != "" {
			text += " | actual rows=" + r
		}
		line := PlanLine{Text: text}
		if len(res.Rows) > 1 && total > 0 && rowsOf[i]/total >= costlyShare && strings.EqualFold(get(row, "type"), "ALL") {
			line.Costly = true
		}
		p.Lines = append(p.Lines, line)
		if e := get(row, "extra"); e != "" {
			p.Lines = append(p.Lines, PlanLine{Depth: 1, Text: e, Detail: true})
		}
	}
}

// parseSQLitePlan reads EXPLAIN QUERY PLAN: the "plan" column of text tree
// lines ("|--SCAN t"), or the id / parent / detail rows of other builds.
func parseSQLitePlan(p *Plan, res Result) bool {
	if hasColumn(res, "detail") && hasColumn(res, "parent") && hasColumn(res, "id") {
		col := map[string]int{}
		for i, c := range res.Columns {
			col[strings.ToLower(c.Name)] = i
		}
		depth := map[string]int{"0": -1}
		for _, row := range res.Rows {
			id, parent := row[col["id"]].Text, row[col["parent"]].Text
			d := depth[parent] + 1
			depth[id] = d
			p.Lines = append(p.Lines, PlanLine{Depth: d, Text: row[col["detail"]].Text})
		}
		sqliteCosts(p)
		return len(p.Lines) > 0
	}
	if len(res.Columns) != 1 {
		return false
	}
	for _, row := range res.Rows {
		line := row[0].Text
		if strings.TrimSpace(line) == "QUERY PLAN" {
			continue
		}
		depth, i := 0, 0
		for {
			switch {
			case strings.HasPrefix(line[i:], "|--"), strings.HasPrefix(line[i:], "`--"):
				i += 3
			case strings.HasPrefix(line[i:], "|  "), strings.HasPrefix(line[i:], "   "):
				i += 3
				depth++
				continue
			}
			break
		}
		p.Lines = append(p.Lines, PlanLine{Depth: depth, Text: strings.TrimSpace(line[i:])})
	}
	sqliteCosts(p)
	return len(p.Lines) > 0
}

// sqliteCosts marks a full table scan: SQLite has no costs, but a SCAN of
// a table (not a covering index) is the node to look at first.
func sqliteCosts(p *Plan) {
	for i, l := range p.Lines {
		if strings.HasPrefix(l.Text, "SCAN ") && !strings.Contains(l.Text, " USING ") && !strings.Contains(l.Text, "CONSTANT") {
			p.Lines[i].Costly = true
		}
	}
}

// parseIndentedText keeps a plan that arrives as text (PostgreSQL TEXT,
// the MySQL tree): the depth is the indent, "->" arrows are dropped.
func parseIndentedText(p *Plan, texts []string) {
	var lines []string
	for _, t := range texts {
		lines = append(lines, strings.Split(t, "\n")...)
	}
	base, nodeDepth := -1, 0
	var stack []int // the indents of the open nodes
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		indent := len(l) - len(strings.TrimLeft(l, " \t"))
		body := strings.TrimSpace(l)
		arrow := strings.HasPrefix(body, "->")
		if arrow {
			body = strings.TrimSpace(strings.TrimPrefix(body, "->"))
		}
		if base < 0 {
			base = indent
		}
		line := PlanLine{Text: body}
		switch {
		case arrow:
			for len(stack) > 0 && stack[len(stack)-1] >= indent {
				stack = stack[:len(stack)-1]
			}
			line.Depth = len(stack)
			stack = append(stack, indent)
			nodeDepth = line.Depth
		case len(p.Lines) == 0:
			stack = append(stack, indent)
		default:
			line.Depth, line.Detail = nodeDepth+1, true
		}
		p.Lines = append(p.Lines, line)
	}
}
