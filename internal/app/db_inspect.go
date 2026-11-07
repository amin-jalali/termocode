package app

import (
	"encoding/json"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Live SQL inspections (DBX-4): on each buffer notify (150 ms debounce,
// the same one the lens uses) Go inspects the console text against the
// loaded tree and sets nvim diagnostics in their own namespace. Nothing
// is asked of the server.

// Diagnostic severities as vim.diagnostic.severity numbers.
const (
	dbDiagError = 1
	dbDiagWarn  = 2
)

// inspectSchema is what the tree of conn knows: the table and view names
// per schema whose Tables/Views lists are loaded, and the loaded columns.
func (s *dbState) inspectSchema(conn string) dbclient.InspectSchema {
	sc := dbclient.InspectSchema{Tables: map[string]map[string]bool{}, Columns: map[string][]string{}}
	parent := map[string]string{}
	for path, n := range s.nodes {
		if n.conn != conn {
			continue
		}
		for _, c := range n.children {
			parent[c] = path
		}
	}
	// owner is the schema (or database) name above a node.
	owner := func(path string) string {
		for p := parent[path]; p != ""; p = parent[p] {
			if n := s.nodes[p]; n.kind == dbNodeSchema || n.kind == dbNodeDatabase {
				return strings.ToLower(n.name)
			}
		}
		return ""
	}
	groups, loaded := 0, 0
	for path, n := range s.nodes {
		if n.conn != conn || n.kind != dbNodeGroup || (n.name != "Tables" && n.name != "Views") {
			continue
		}
		groups++
		if !n.loaded {
			continue
		}
		loaded++
		sch := owner(path)
		if sc.Tables[sch] == nil {
			sc.Tables[sch] = map[string]bool{}
		}
		for _, c := range n.children {
			if t := s.nodes[c]; t.kind == dbNodeTable || t.kind == dbNodeView {
				sc.Tables[sch][strings.ToLower(t.name)] = true
			}
		}
	}
	sc.Complete = groups > 0 && loaded == groups
	for table, cols := range s.cols[conn] {
		sc.Columns[table] = cols
	}
	return sc
}

// dbInspectDiagnostics inspects each statement of text and returns the
// diagnostics (0-based lines, byte columns) for vim.diagnostic.set; an
// empty list (never nil) clears the buffer.
func dbInspectDiagnostics(text, driver string, sc dbclient.InspectSchema) []map[string]any {
	out := []map[string]any{}
	var pieces [][2]int
	if spans := dbStatementSpans(text, driver); len(spans) > 0 {
		for _, sp := range spans {
			pieces = append(pieces, [2]int{sp.start, sp.end})
		}
	} else if strings.TrimSpace(text) != "" {
		pieces = [][2]int{{0, len(text)}} // Split failed (an unclosed quote): inspect it all
	}
	for _, p := range pieces {
		for _, is := range dbclient.Inspect(text[p[0]:p[1]], sc) {
			start, end := p[0]+is.Start, p[0]+is.End
			lnum, col := dbLineCol(text, start)
			elnum, ecol := dbLineCol(text, end)
			sev := dbDiagWarn
			if is.Kind == dbclient.IssueSyntax {
				sev = dbDiagError
			}
			d := map[string]any{"lnum": lnum, "col": col, "end_lnum": elnum, "end_col": ecol,
				"severity": sev, "message": sanitizeDBText(is.Msg), "source": "termocode"}
			if is.Fix != "" {
				d["user_data"] = map[string]any{"fix": is.Fix}
			}
			out = append(out, d)
		}
	}
	return out
}

// dbLineCol is the 0-based line and byte column of offset in text.
func dbLineCol(text string, offset int) (int, int) {
	head := text[:offset]
	line := strings.Count(head, "\n")
	return line, offset - (strings.LastIndexByte(head, '\n') + 1)
}

// databaseInspectBuffer sets the inspection diagnostics of a SQL buffer
// (none when the buffer has no connection).
func (m *Model) databaseInspectBuffer(buf int, lines []string, c *dbclient.Conn) {
	diags := []map[string]any{}
	if c != nil {
		diags = dbInspectDiagnostics(strings.Join(lines, "\n"), c.Driver, m.databases.inspectSchema(c.ID))
	}
	_ = m.nvim.ExecLuaArgs(`local b,d=...;if vim.api.nvim_buf_is_valid(b) then vim.diagnostic.set(vim.api.nvim_create_namespace('termocode_sql_inspect'),b,d) end`, buf, diags)
}

// dbQuickFixLua applies the fix of the inspection diagnostic under the
// cursor (else the first fixable one on the cursor line): the name is
// replaced in place, its quote kept. It returns {old=, fix=} or {}.
const dbQuickFixLua = `local ns=vim.api.nvim_create_namespace('termocode_sql_inspect')
local b=vim.api.nvim_get_current_buf()
local c=vim.api.nvim_win_get_cursor(0)
local row,col=c[1]-1,c[2]
local hit,line
for _,d in ipairs(vim.diagnostic.get(b,{namespace=ns})) do
 local f=d.user_data and d.user_data.fix
 if f and f~='' and d.lnum==row and (d.end_lnum or d.lnum)==d.lnum then
  if col>=d.col and col<=(d.end_col or d.col) then hit=d;break end
  line=line or d
 end
end
hit=hit or line
if not hit then return vim.fn.json_encode({}) end
local text=vim.api.nvim_buf_get_lines(b,hit.lnum,hit.lnum+1,false)[1] or ''
local old=text:sub(hit.col+1,hit.end_col)
local fix=hit.user_data.fix
local q=old:sub(1,1)
if (q=='"' or q=='\96') and old:sub(-1)==q then fix=q..fix:gsub(q,q..q)..q end
vim.api.nvim_buf_set_text(b,hit.lnum,hit.col,hit.lnum,hit.end_col,{fix})
vim.api.nvim_win_set_cursor(0,{hit.lnum+1,hit.col})
vim.diagnostic.set(ns,b,vim.tbl_filter(function(d) return not (d.lnum==hit.lnum and d.col==hit.col) end,vim.diagnostic.get(b,{namespace=ns})))
return vim.fn.json_encode({old=old,fix=fix})`

// databaseQuickFix applies the quick fix of the inspection under the
// cursor (DBX-4): "Did you mean email?" becomes the text. One undo step.
func (m *Model) databaseQuickFix() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	file, _, ok := m.httpBufText(0)
	if !ok || !strings.EqualFold(lastExt(file), ".sql") {
		return m.pushToastDetail(toast.Info, "Open a SQL file", "Quick fixes are for SQL consoles and files.")
	}
	var raw string
	_ = m.nvim.ExecLuaResult(dbQuickFixLua, &raw)
	var res struct {
		Old string `json:"old"`
		Fix string `json:"fix"`
	}
	_ = json.Unmarshal([]byte(raw), &res)
	if res.Fix == "" {
		return m.pushToastDetail(toast.Info, "No quick fix here", "Put the cursor on an underlined name that says \"Did you mean …?\".")
	}
	return tea.Batch(m.pushToast(toast.Info, "Replaced "+sanitizeDBText(res.Old)+" with "+sanitizeDBText(res.Fix)), m.fetchStateCmd())
}

func init() {
	registerNotifyHandler("termocode_db_quickfix", func(m *Model, _ []any) tea.Cmd { return m.databaseQuickFix() })
}
