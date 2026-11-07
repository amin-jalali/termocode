package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	tea "github.com/charmbracelet/bubbletea"
)

// SQL navigation (DBX-5): Databases: Go to Definition opens the DDL of
// the table or view named under the cursor (it must be loaded in the
// tree); Databases: Find Usages lists the lines of the project's .sql
// files that name it.

const pickerKindDBUsages pickerKindEnum = 3856

const (
	dbUsagesLimit   = 500
	dbUsagesMaxFile = 2 << 20
)

// dbUsage is one line that names a table.
type dbUsage struct {
	path string
	line int
	text string
}

func dbIdentByte(c byte) bool { return dbIsWordByte(c) || c == '$' }

// dbIdentAt returns the (schema-)qualified name at byte col of line:
// plain words, "double quoted" and `back quoted` names. Not on a name: "".
func dbIdentAt(line string, col int) (schema, name string) {
	if col < 0 || col >= len(line) {
		return "", ""
	}
	parts := dbIdentParts(line)
	for i, p := range parts {
		if col < p.start || col >= p.end {
			continue
		}
		// a.b: the cursor on a or on b names b in schema a.
		if i+1 < len(parts) && parts[i+1].start == p.end+1 && line[p.end] == '.' {
			return parts[i].text, parts[i+1].text
		}
		if i > 0 && parts[i-1].end+1 == p.start && line[p.start-1] == '.' {
			return parts[i-1].text, p.text
		}
		return "", p.text
	}
	return "", ""
}

type dbIdentPart struct {
	start, end int // byte span, quotes included
	text       string
}

func dbIdentParts(line string) []dbIdentPart {
	var out []dbIdentPart
	for i := 0; i < len(line); {
		c := line[i]
		switch {
		case c == '"' || c == '`':
			j := strings.IndexByte(line[i+1:], c)
			if j < 0 {
				return out
			}
			out = append(out, dbIdentPart{i, i + j + 2, line[i+1 : i+1+j]})
			i += j + 2
		case c == '\'': // a string literal is not a name
			j := strings.IndexByte(line[i+1:], '\'')
			if j < 0 {
				return out
			}
			i += j + 2
		case dbIdentByte(c):
			j := i
			for j < len(line) && dbIdentByte(line[j]) {
				j++
			}
			out = append(out, dbIdentPart{i, j, line[i:j]})
			i = j
		default:
			i++
		}
	}
	return out
}

// dbFindTablePath is the tree path of the table or view called name
// (case insensitive) on conn, in schema when it is set; the first path
// wins. "" = not loaded in the tree.
func (s *dbState) dbFindTablePath(conn, schema, name string) string {
	if name == "" {
		return ""
	}
	var paths []string
	for path, n := range s.nodes {
		if n.conn != conn || (n.kind != dbNodeTable && n.kind != dbNodeView) || !strings.EqualFold(n.name, name) {
			continue
		}
		if schema != "" && !strings.EqualFold(dbAncestorSchema(path), schema) && !strings.EqualFold(dbAncestorDatabase(path), schema) {
			continue
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return ""
	}
	sort.Strings(paths)
	return paths[0]
}

// dbFindUsages lists the lines of the .sql files under root that hold
// name as a whole word (case insensitive), at most limit, by path then
// line. Hidden folders, node_modules and vendor are skipped.
func dbFindUsages(root, name string, limit int) []dbUsage {
	var out []dbUsage
	if name == "" {
		return nil
	}
	lower := strings.ToLower(name)
	var files []string
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
		if strings.EqualFold(filepath.Ext(p), ".sql") && d.Type().IsRegular() {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	for _, p := range files {
		if info, err := os.Stat(p); err != nil || info.Size() > dbUsagesMaxFile {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), dbUsagesMaxFile)
		for n := 1; sc.Scan(); n++ {
			if dbHasWord(strings.ToLower(sc.Text()), lower) {
				out = append(out, dbUsage{path: p, line: n, text: strings.TrimRight(sc.Text(), "\r")})
				if len(out) >= limit {
					f.Close()
					return out
				}
			}
		}
		f.Close()
	}
	return out
}

func dbHasWord(s, w string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], w)
		if j < 0 {
			return false
		}
		a, b := i+j, i+j+len(w)
		if (a == 0 || !dbIdentByte(s[a-1])) && (b == len(s) || !dbIdentByte(s[b])) {
			return true
		}
		i = a + 1
	}
}

const dbCursorLua = `local c=vim.api.nvim_win_get_cursor(0)
return vim.fn.json_encode({line=vim.api.nvim_get_current_line(),col=c[2]})`

// databaseNameAtCursor reads the SQL buffer and the name under its
// cursor.
func (m *Model) databaseNameAtCursor() (file string, lines []string, schema, name string, cmd tea.Cmd) {
	if m.nvim == nil {
		return "", nil, "", "", nil
	}
	file, lines, ok := m.httpBufText(0)
	if !ok || !strings.EqualFold(lastExt(file), ".sql") {
		return "", nil, "", "", m.pushToastDetail(toast.Info, "Open a SQL file", "Put the cursor on a table name in a SQL console or file.")
	}
	var pos struct {
		Line string `json:"line"`
		Col  int    `json:"col"`
	}
	var raw string
	_ = m.nvim.ExecLuaResult(dbCursorLua, &raw)
	_ = json.Unmarshal([]byte(raw), &pos)
	schema, name = dbIdentAt(pos.Line, pos.Col)
	if name == "" {
		return "", nil, "", "", m.pushToastDetail(toast.Info, "No name under the cursor", "Put the cursor on a table or view name.")
	}
	return file, lines, schema, name, nil
}

// databaseGotoDefinition opens the DDL of the table or view under the
// cursor.
func (m *Model) databaseGotoDefinition() tea.Cmd {
	file, lines, schema, name, cmd := m.databaseNameAtCursor()
	if name == "" {
		return cmd
	}
	m.ensureDatabasesLoaded()
	conn := m.dbConsole().consoleConn(file, lines)
	if conn == "" {
		return m.pushToastDetail(toast.Info, "No connection for this file", "Run a statement first to pick its connection.")
	}
	path := m.databases.dbFindTablePath(conn, schema, name)
	t, ok := m.databaseTableAt(path)
	if !ok {
		return m.pushToastDetail(toast.Info, "Not in the Databases tree", sanitizeDBText(name)+": expand its schema in the Databases view, or Refresh.")
	}
	return m.databaseLoadMeta(t, true)
}

// databaseFindUsages lists the project's SQL lines that name the table
// under the cursor.
func (m *Model) databaseFindUsages() tea.Cmd {
	_, _, _, name, cmd := m.databaseNameAtCursor()
	if name == "" {
		return cmd
	}
	root := m.databaseWorkspaceRoot()
	found := dbFindUsages(root, name, dbUsagesLimit)
	if len(found) == 0 {
		return m.pushToastDetail(toast.Info, "No usages", "No project .sql file names "+sanitizeDBText(name)+".")
	}
	items := make([]picker.Item, 0, len(found))
	for _, u := range found {
		rel := u.path
		if r, err := filepath.Rel(root, u.path); err == nil {
			rel = r
		}
		loc := fmt.Sprintf("%s:%d", filepath.ToSlash(rel), u.line)
		items = append(items, picker.Item{ID: u.path + "\x00" + fmt.Sprint(u.line), Title: sanitizeDBText(strings.TrimSpace(u.text)), Hint: sanitizeDBText(loc)})
	}
	title := fmt.Sprintf("Usages of %s (%d)", sanitizeDBText(name), len(found))
	if len(found) >= dbUsagesLimit {
		title = fmt.Sprintf("Usages of %s (first %d)", sanitizeDBText(name), dbUsagesLimit)
	}
	m.picker = picker.NewItems(title, items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBUsages
	m.pickerOpen = true
	return nil
}

// databaseUsagesPicker opens the chosen usage.
func (m *Model) databaseUsagesPicker(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBUsages {
		return nil, false
	}
	path, line, ok := strings.Cut(id, "\x00")
	if !ok {
		return nil, true
	}
	var n int
	_, _ = fmt.Sscan(line, &n)
	m.openFileAtLine(path, n)
	return m.fetchStateCmd(), true
}
