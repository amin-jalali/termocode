package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// SQL rename refactor (DBX-5): Databases: Rename in SQL… changes a table,
// column or routine name in the SQL text of the open buffer and of the
// project's .sql files, after a card that says how many places change
// (Cancel first). The database itself is never changed.

const (
	promptKindDBSQLRename promptKindEnum  = 4931
	confirmKindDBRename   confirmKindEnum = 4932
)

// dbRenameState is the rename being asked for.
type dbRenameState struct {
	old, new string
	file     string // the current buffer's file
	buf      string // its new text
	bufCount int
	files    []dbRenameFile
}

// dbRenameFile is one project file and its renamed text.
type dbRenameFile struct {
	path  string
	text  string
	count int
}

var dbRenameNoAlias = map[string]bool{
	"where": true, "on": true, "set": true, "join": true, "left": true, "right": true, "inner": true, "full": true, "cross": true,
	"group": true, "order": true, "limit": true, "union": true, "using": true, "values": true, "having": true, "offset": true,
	"returning": true, "select": true, "from": true, "natural": true, "window": true, "fetch": true, "for": true, "as": true,
	"outer": true, "lateral": true, "except": true, "intersect": true, "default": true,
}

// dbStatementAliases maps the lower-case aliases and table names of one
// statement (FROM, JOIN, UPDATE, INTO) to the table name.
func dbStatementAliases(stmt string) map[string]string {
	out := map[string]string{}
	parts := dbIdentParts(stmt)
	for i := 0; i < len(parts); i++ {
		switch strings.ToLower(parts[i].text) {
		case "from", "join", "update", "into":
		default:
			continue
		}
		j := i + 1
		if j >= len(parts) {
			break
		}
		// schema.table: take the last part of a dotted run.
		for j+1 < len(parts) && parts[j].end < len(stmt) && stmt[parts[j].end] == '.' && parts[j+1].start == parts[j].end+1 {
			j++
		}
		table := parts[j].text
		if dbRenameNoAlias[strings.ToLower(table)] {
			continue
		}
		out[strings.ToLower(table)] = table
		k := j + 1
		if k < len(parts) && strings.EqualFold(parts[k].text, "as") {
			k++
		}
		if k < len(parts) {
			alias := strings.ToLower(parts[k].text)
			between := stmt[parts[j].end:parts[k].start]
			if !dbRenameNoAlias[alias] && !strings.ContainsAny(between, ",;()=") {
				out[alias] = table
			}
		}
		i = j
	}
	return out
}

// dbRenameText replaces every whole name old (case insensitive, plain or
// quoted) with new in SQL text; string literals and comments stay as
// they are. A quoted name keeps its quote; a plain one is quoted only
// when new needs it.
func dbRenameText(text, old, new string) (string, int) {
	if old == "" {
		return text, 0
	}
	var b strings.Builder
	n := 0
	quoteAs := func(q byte, name string) string {
		s := string(q)
		return s + strings.ReplaceAll(name, s, s+s) + s
	}
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '\'':
			j := i + 1
			for j < len(text) {
				if text[j] == '\'' {
					if j+1 < len(text) && text[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			j = min(j+1, len(text))
			b.WriteString(text[i:j])
			i = j
		case c == '-' && i+1 < len(text) && text[i+1] == '-':
			j := strings.IndexByte(text[i:], '\n')
			if j < 0 {
				j = len(text) - i
			}
			b.WriteString(text[i : i+j])
			i += j
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			j := strings.Index(text[i+2:], "*/")
			end := len(text)
			if j >= 0 {
				end = i + 2 + j + 2
			}
			b.WriteString(text[i:end])
			i = end
		case c == '"' || c == '`':
			j := strings.IndexByte(text[i+1:], c)
			if j < 0 {
				b.WriteString(text[i:])
				i = len(text)
				break
			}
			inner := text[i+1 : i+1+j]
			if strings.EqualFold(inner, old) {
				b.WriteString(quoteAs(c, new))
				n++
			} else {
				b.WriteString(text[i : i+j+2])
			}
			i += j + 2
		case dbIdentByte(c):
			j := i
			for j < len(text) && dbIdentByte(text[j]) {
				j++
			}
			word := text[i:j]
			if strings.EqualFold(word, old) {
				if dbPlainIdent(new) {
					b.WriteString(new)
				} else {
					b.WriteString(quoteAs('"', new))
				}
				n++
			} else {
				b.WriteString(word)
			}
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String(), n
}

// dbRenameFiles renames old to new in the project's .sql files under root
// (not skip, the open buffer's file); it returns the files that change.
func dbRenameFiles(root, skip, old, new string) []dbRenameFile {
	var paths []string
	skip = filepath.Clean(skip)
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if b := d.Name(); p != root && (strings.HasPrefix(b, ".") || b == "node_modules" || b == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(p), ".sql") && d.Type().IsRegular() && filepath.Clean(p) != skip {
			paths = append(paths, p)
		}
		return nil
	})
	sort.Strings(paths)
	var out []dbRenameFile
	for _, p := range paths {
		if info, err := os.Stat(p); err != nil || info.Size() > dbUsagesMaxFile {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		text, n := dbRenameText(string(b), old, new)
		if n > 0 {
			out = append(out, dbRenameFile{path: p, text: text, count: n})
		}
	}
	return out
}

// databaseRenameStart asks for the new name of the name under the cursor.
func (m *Model) databaseRenameStart() tea.Cmd {
	file, _, _, name, cmd := m.databaseNameAtCursor()
	if name == "" {
		return cmd
	}
	m.dbConsole().rename = &dbRenameState{old: name, file: file}
	return m.openHTTPPrompt(promptKindDBSQLRename, "Rename in SQL", "New name for "+sanitizeDBText(name)+":", name)
}

// databaseRenamePlan counts the places and opens the card.
func (m *Model) databaseRenamePlan(value string) tea.Cmd {
	s := m.dbConsole()
	r := s.rename
	value = strings.TrimSpace(value)
	if r == nil {
		return nil
	}
	if value == "" || value == r.old || strings.ContainsAny(value, "\n\r\x00") {
		s.rename = nil
		return m.pushToast(toast.Info, "Rename cancelled: the name did not change")
	}
	r.new = value
	_, lines, ok := m.httpBufText(0)
	if ok {
		r.buf, r.bufCount = dbRenameText(strings.Join(lines, "\n"), r.old, r.new)
	}
	r.files = dbRenameFiles(m.databaseWorkspaceRoot(), r.file, r.old, r.new)
	total, places := 0, r.bufCount
	for _, f := range r.files {
		total++
		places += f.count
	}
	if places == 0 {
		s.rename = nil
		return m.pushToastDetail(toast.Info, "Nothing to rename", "No SQL text names "+sanitizeDBText(r.old)+".")
	}
	body := fmt.Sprintf("%d in this file", r.bufCount)
	if total > 0 {
		body += fmt.Sprintf(", %d in %d other project .sql file", places-r.bufCount, total)
		if total > 1 {
			body += "s"
		}
	}
	var list []string
	root := m.databaseWorkspaceRoot()
	for i, f := range r.files {
		if i == 8 {
			list = append(list, fmt.Sprintf("… %d more files", len(r.files)-i))
			break
		}
		rel := f.path
		if x, err := filepath.Rel(root, f.path); err == nil {
			rel = x
		}
		list = append(list, fmt.Sprintf("%s (%d)", filepath.ToSlash(rel), f.count))
	}
	text := "Places: " + body + ".\nOnly the SQL text changes, not the database (use ALTER … RENAME for that). Strings and comments are left alone."
	if len(list) > 0 {
		text += "\n\n" + sanitizeDBText(strings.Join(list, "\n"))
	}
	m.confirm = confirm.New("Rename "+sanitizeDBText(r.old)+" to "+sanitizeDBText(r.new)+"?", text, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "rename", Title: "Rename", Style: confirm.StylePrimary},
	}).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBRename
	return nil
}

// databaseRenameApply writes the renamed text: the open buffer through
// nvim (one undo step), the other files on disk; a file open with unsaved
// changes is skipped and named.
func (m *Model) databaseRenameApply(id string) tea.Cmd {
	s := m.dbConsole()
	r := s.rename
	s.rename = nil
	if id != "rename" || r == nil || m.nvim == nil {
		return nil
	}
	places, files := 0, 0
	if r.bufCount > 0 {
		_ = m.nvim.ExecLuaArgs(`local name,lines=...;local b=vim.api.nvim_get_current_buf();if vim.api.nvim_buf_get_name(b)==name then vim.api.nvim_buf_set_lines(b,0,-1,false,lines) end`, r.file, strings.Split(r.buf, "\n"))
		places += r.bufCount
		files++
	}
	var raw string
	_ = m.nvim.ExecLuaResult(`local out={};for _,b in ipairs(vim.api.nvim_list_bufs()) do if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].modified then out[#out+1]=vim.api.nvim_buf_get_name(b) end end;return vim.json.encode(out)`, &raw)
	var dirty []string
	_ = json.Unmarshal([]byte(raw), &dirty)
	isDirty := map[string]bool{}
	for _, p := range dirty {
		isDirty[filepath.Clean(p)] = true
	}
	var skipped, failed []string
	for _, f := range r.files {
		if isDirty[filepath.Clean(f.path)] {
			skipped = append(skipped, filepath.Base(f.path))
			continue
		}
		if err := dbWriteFileKeepMode(f.path, f.text); err != nil {
			failed = append(failed, filepath.Base(f.path))
			continue
		}
		places += f.count
		files++
	}
	_ = m.nvim.ExecLua(`pcall(vim.cmd,'checktime')`)
	msg := fmt.Sprintf("Renamed %s to %s in %d places (%d files)", sanitizeDBText(r.old), sanitizeDBText(r.new), places, files)
	if len(skipped)+len(failed) > 0 {
		var detail []string
		if len(skipped) > 0 {
			detail = append(detail, "Unsaved, left alone: "+sanitizeDBText(strings.Join(skipped, ", ")))
		}
		if len(failed) > 0 {
			detail = append(detail, "Could not write: "+sanitizeDBText(strings.Join(failed, ", ")))
		}
		return tea.Batch(m.pushToastDetail(toast.Warn, msg, strings.Join(detail, ". ")), m.fetchStateCmd())
	}
	return tea.Batch(m.pushToast(toast.Info, msg), m.fetchStateCmd())
}

// dbWriteFileKeepMode replaces a file through a temp file, keeping its mode.
func dbWriteFileKeepMode(path, text string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp := path + ".termocode-rename"
	if err := os.WriteFile(tmp, []byte(text), mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ── go to definition of a column or routine ────────────────────────────

type dbSourceMsg struct {
	gen   uint64
	title string
	text  string
	err   error
}

// databaseGotoRoutine opens the stored source of the routine called name.
func (m *Model) databaseGotoRoutine(conn, name string) (tea.Cmd, bool) {
	var path string
	for p, n := range m.databases.nodes {
		if n.conn == conn && n.kind == dbNodeRoutine && strings.EqualFold(n.name, name) && (path == "" || p < path) {
			path = p
		}
	}
	if path == "" {
		return nil, false
	}
	c, ok := m.databases.connection(conn)
	if !ok {
		return nil, false
	}
	sess := m.databases.sessions[m.databases.sessionKeyFor(path, conn)]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection."), true
	}
	qualified := m.databases.nodes[path].name
	if _, pg := dbclient.DialectFor(c.Driver).(dbclient.PostgresDialect); pg {
		if sc := dbAncestorSchema(path); sc != "" && !strings.Contains(qualified, ".") {
			qualified = dbclient.PostgresDialect{}.QuoteIdent(sc) + "." + dbclient.PostgresDialect{}.QuoteIdent(qualified)
		}
	}
	gen := m.nextTableGen()
	driver := c.Driver
	title := "SQL source: " + sanitizeDBText(m.databases.nodes[path].name)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbMetaTimeout)
		defer cancel()
		var lastErr error
		for _, kind := range []dbclient.ObjectKind{dbclient.ObjectFunction, dbclient.ObjectProcedure} {
			q, err := dbclient.ObjectSourceQuery(driver, kind, qualified)
			if err != nil {
				lastErr = err
				continue
			}
			res := dbRunReadOnly(ctx, sess, q.SQL, dbclient.RunOptions{MaxRows: 1, Args: q.Args})
			if res.Err != nil {
				lastErr = res.Err
				continue
			}
			if len(res.Rows) > 0 && q.Column < len(res.Rows[0]) && !res.Rows[0][q.Column].Null {
				return dbSourceMsg{gen: gen, title: title, text: res.Rows[0][q.Column].Text}
			}
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("no source found for %s", qualified)
		}
		return dbSourceMsg{gen: gen, title: title, err: lastErr}
	}, true
}

func (m *Model) onDBSource(msg dbSourceMsg) tea.Cmd {
	if m.databases == nil || m.dbConsole().tableGen != msg.gen || m.nvim == nil {
		return nil
	}
	if msg.err != nil {
		return m.dbConsoleError(msg.err)
	}
	m.ensureDBLua()
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLuaArgs(`_G._termocode_db_ddl(...)`, msg.title, strings.Split(strings.TrimRight(dbSafeMultiline(msg.text), "\n"), "\n"))
	m.focus = FocusEditor
	return m.fetchStateCmd()
}

// databaseFocusColumn puts the cursor of the DDL buffer on the column's
// line (after the DDL opened).
func (m *Model) databaseFocusColumn(col string) {
	if m.nvim == nil || col == "" {
		return
	}
	_ = m.nvim.ExecLuaArgs(`local col=...;local b=vim.api.nvim_get_current_buf()
local lc=col:lower()
for i,l in ipairs(vim.api.nvim_buf_get_lines(b,0,-1,false)) do
 local s=l:lower()
 local a=s:find('%f[%w_$]'..vim.pesc(lc)..'%f[^%w_$]') or s:find('"'..vim.pesc(lc)..'"',1,true) or s:find('\96'..vim.pesc(lc)..'\96',1,true)
 if a and not s:match('^%s*create') then vim.api.nvim_win_set_cursor(0,{i,a-1});return end
end`, col)
}
