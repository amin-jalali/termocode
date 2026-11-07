package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Databases: Open CSV/TSV as Table… (DBX-38): the file is loaded into a new
// SQLite database in termocode's cache folder (the CSV is never changed),
// added as a session-only connection "csv-<name>" (never written to a
// config file) and SELECT * runs in its console. The result grid sorts,
// hides columns, transposes and filters; the console queries it with SQL.

const (
	pickerKindDBOpenCSV pickerKindEnum = 5381
	promptKindDBOpenCSV promptKindEnum = 5381
)

const dbOpenCSVTypePath = "\x00type-path"

// dbFindCSVOrTSV lists the .csv and .tsv files of the workspace (same walk
// limits as the import picker).
func dbFindCSVOrTSV(root string) []string {
	var out []string
	visited := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		visited++
		if visited > 5000 || len(out) >= 200 {
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			name := d.Name()
			if p != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") || strings.Count(rel, string(filepath.Separator)) >= 4 {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(d.Name())) {
		case ".csv", ".tsv":
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func dbIsCSVPath(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".csv", ".tsv":
		return true
	}
	return false
}

type dbOpenCSVFilesMsg struct{ files []string }

// databaseOpenCSVStart opens the active buffer when it is a .csv/.tsv,
// else a picker of the workspace's files.
func (m *Model) databaseOpenCSVStart() tea.Cmd {
	m.ensureDatabasesLoaded()
	if p := m.editor.Path(); dbIsCSVPath(p) {
		return m.databaseOpenCSVPath(p)
	}
	root := m.databaseWorkspaceRoot()
	return func() tea.Msg { return dbOpenCSVFilesMsg{files: dbFindCSVOrTSV(root)} }
}

func (m *Model) onDBOpenCSVFiles(msg dbOpenCSVFilesMsg) tea.Cmd {
	const title = "Open CSV/TSV as table"
	if len(msg.files) == 0 {
		return m.openHTTPPrompt(promptKindDBOpenCSV, title, "CSV or TSV file (in the workspace, or a full path):", "")
	}
	items := make([]picker.Item, 0, len(msg.files)+1)
	for _, f := range msg.files {
		items = append(items, picker.Item{ID: f, Title: sanitizeDBText(f)})
	}
	items = append(items, picker.Item{ID: dbOpenCSVTypePath, Title: "Type a path…"})
	m.picker = picker.NewItems(title, items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBOpenCSV
	m.pickerOpen = true
	return nil
}

// databaseOpenCSVPicked handles the file picker. ok=false: not ours.
func (m *Model) databaseOpenCSVPicked(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBOpenCSV {
		return nil, false
	}
	if id == dbOpenCSVTypePath {
		return m.openHTTPPrompt(promptKindDBOpenCSV, "Open CSV/TSV as table", "CSV or TSV file (in the workspace, or a full path):", ""), true
	}
	return m.databaseOpenCSVPath(id), true
}

// databaseOpenCSVPath loads the file (relative paths are in the workspace)
// off the UI goroutine.
func (m *Model) databaseOpenCSVPath(p string) tea.Cmd {
	p = strings.TrimSpace(p)
	if p == "" {
		return nil
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(m.databaseWorkspaceRoot(), p)
	}
	return dbOpenCSVLoad(p, dbOpenCSVCacheDir())
}

// dbOpenCSVCacheDir holds the SQLite copies of opened CSV files.
func dbOpenCSVCacheDir() string {
	return filepath.Join(agentPacksCacheDir(), "csv-tables")
}

type dbOpenCSVMsg struct {
	csv    string
	dbPath string
	tbl    dbclient.CSVTable
	err    error
}

// dbOpenCSVLoad builds a fresh SQLite copy of csvPath in dir: one file per
// CSV path, replaced on every open so it shows the file as it is now.
func dbOpenCSVLoad(csvPath, dir string) tea.Cmd {
	return func() tea.Msg {
		abs, err := filepath.Abs(csvPath)
		if err != nil {
			return dbOpenCSVMsg{csv: csvPath, err: err}
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return dbOpenCSVMsg{csv: abs, err: fmt.Errorf("cannot create the cache folder: %w", err)}
		}
		sum := sha256.Sum256([]byte(abs))
		dbPath := filepath.Join(dir, dbclient.CSVTableName(abs)+"-"+hex.EncodeToString(sum[:4])+".sqlite")
		if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
			return dbOpenCSVMsg{csv: abs, err: fmt.Errorf("cannot replace the old copy: %w", err)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		tbl, err := dbclient.OpenCSVAsTable(ctx, abs, dbPath)
		return dbOpenCSVMsg{csv: abs, dbPath: dbPath, tbl: tbl, err: err}
	}
}

// dbOpenCSVSelect is the statement the console runs on the new table.
func dbOpenCSVSelect(t dbclient.CSVTable) string {
	return "SELECT * FROM " + dbclient.SQLiteDialect{}.QuoteIdent(t.Table) + ";"
}

// databaseAddCSVConn adds (or replaces) the session-only connection of a
// loaded CSV file. It is kept in csvConns so a reload keeps it.
func (m *Model) databaseAddCSVConn(msg dbOpenCSVMsg) dbclient.Conn {
	s := m.databases
	sum := sha256.Sum256([]byte(msg.csv))
	id := "csv-" + strings.ReplaceAll(msg.tbl.Table, "_", "-") + "-" + hex.EncodeToString(sum[:2])
	c := dbclient.Conn{ID: id, Name: filepath.Base(msg.csv) + " (CSV)", Driver: "sqlite", Path: msg.dbPath, Source: dbclient.SourceUser}
	if sess := s.sessions[id]; sess != nil {
		_ = sess.Close()
		delete(s.sessions, id)
	}
	s.csvConns = dbReplaceConn(s.csvConns, c)
	s.conns = dbReplaceConn(s.conns, c)
	s.nodes[id] = dbNode{kind: dbNodeConnection, name: c.Name, path: id, conn: id, detail: "sqlite · CSV copy"}
	s.touch()
	return c
}

func dbReplaceConn(list []dbclient.Conn, c dbclient.Conn) []dbclient.Conn {
	for i := range list {
		if list[i].ID == c.ID {
			list[i] = c
			return list
		}
	}
	return append(list, c)
}

func (m *Model) onDBOpenCSV(msg dbOpenCSVMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot open "+sanitizeDBText(filepath.Base(msg.csv))+" as a table", dbErrorText(msg.err))
	}
	if m.databases == nil {
		m.ensureDatabasesLoaded()
	}
	c := m.databaseAddCSVConn(msg)
	note := m.pushToastDetail(toast.Info, "Opened "+dbPlural(int(msg.tbl.Rows), "row")+" as table "+sanitizeDBText(msg.tbl.Table), "Query it with SQL in the console · a copy: the file is not changed.")
	return tea.Batch(note, m.revealDatabasesView(false), m.databaseRunInConsole(c.ID, dbOpenCSVSelect(msg.tbl)))
}
