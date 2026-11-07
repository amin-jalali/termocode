package app

import (
	"context"
	"errors"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Generate Test Data (DBX-20): Databases: Generate Test Data… on a table →
// how many rows → the columns, keys and foreign keys are read, a few
// existing values of each referenced column are sampled (read-only) →
// an INSERT script is added to the end of the connection's console. Nothing
// runs: the user reads the script and runs it like any statement (so the
// read-only, production and transaction rules apply as usual).

const promptKindDBGenRows promptKindEnum = 4371

// dbGenSample is how many values of a referenced column are sampled.
const dbGenSample = 200

var (
	errDBNotSQL    = errors.New("this connection does not speak SQL")
	errDBNoColumns = errors.New("the table has no columns to fill")
)

type dbGenMsg struct {
	gen    uint64
	t      dbTable
	script string
	err    error
}

// databaseGenerateStart checks the target and asks for the row count.
func (m *Model) databaseGenerateStart() tea.Cmd {
	t, cmd, ok := m.databaseTableTarget()
	if !ok {
		return cmd
	}
	c, _ := m.databases.connection(t.conn)
	switch {
	case t.view:
		return m.pushToastDetail(toast.Info, "Cannot fill a view", "Select a table.")
	case c.ReadOnly:
		return m.pushToastDetail(toast.Warn, "Cannot add rows", "The connection is read-only.")
	}
	m.dbConsole().genTable = &t
	return m.openHTTPPrompt(promptKindDBGenRows, "Generate Test Data for "+t.label(), "Rows to make (1–"+strconv.Itoa(dbclient.GenMaxRows)+"):", "10")
}

// databaseGenerateRows reads the table and builds the script.
func (m *Model) databaseGenerateRows(value string) tea.Cmd {
	s := m.dbConsole()
	if s.genTable == nil {
		return nil
	}
	t := *s.genTable
	s.genTable = nil
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 || n > dbclient.GenMaxRows {
		return m.pushToastDetail(toast.Warn, "Not a row count", "Type a number from 1 to "+strconv.Itoa(dbclient.GenMaxRows)+".")
	}
	sess := m.databases.sessions[t.sessKey]
	if sess == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	gen := m.nextTableGen()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbMetaTimeout)
		defer cancel()
		script, err := dbBuildTestData(ctx, sess, t, n)
		return dbGenMsg{gen: gen, t: t, script: script, err: err}
	}
}

// dbBuildTestData reads the table's columns and foreign keys, samples the
// referenced values and writes the script.
func dbBuildTestData(ctx context.Context, sess dbclient.Session, t dbTable, n int) (string, error) {
	meta, err := dbLoadTableMeta(ctx, sess, t, t.request())
	if err != nil {
		return "", err
	}
	d := dbclient.DialectFor(t.driver)
	if d == nil {
		return "", errDBNotSQL
	}
	opt := dbclient.GenOptions{Rows: n, Seed: uint64(n)*2654435761 + uint64(len(t.name)), FKColumns: map[string]bool{}, FKValues: map[string][]string{}}
	for _, fk := range meta.ir.ForeignKeys {
		opt.FKColumns[fk.Column] = true
		if len(opt.FKValues[fk.Column]) > 0 || fk.RefTable == "" || fk.RefColumn == "" {
			continue
		}
		ref := d.QuoteIdent(fk.RefTable)
		if t.schema != "" {
			ref = d.QuoteIdent(t.schema) + "." + ref
		}
		col := d.QuoteIdent(fk.RefColumn)
		q := "SELECT DISTINCT " + col + " FROM " + ref + " WHERE " + col + " IS NOT NULL" + d.Limit(dbGenSample, 0)
		r := dbRunReadOnly(ctx, sess, q, dbclient.RunOptions{MaxRows: dbGenSample})
		if r.Err != nil {
			continue // no values: the column gets NULL and a note
		}
		for _, row := range r.Rows {
			if len(row) > 0 && !row[0].Null {
				opt.FKValues[fk.Column] = append(opt.FKValues[fk.Column], row[0].Text)
			}
		}
	}
	cols := dbGenColumns(meta)
	if len(cols) == 0 {
		return "", errDBNoColumns
	}
	return dbclient.GenerateTestData(d, t.qualified(), cols, opt), nil
}

// dbGenColumns are the table's columns for the generator: the detailed
// catalog rows (nullability, default) when there are any, else the plain
// introspection columns.
func dbGenColumns(meta dbTableMeta) []dbclient.GenColumn {
	var out []dbclient.GenColumn
	for _, c := range meta.cols {
		out = append(out, dbclient.GenColumn{Name: c.name, Type: c.typ, NotNull: c.notNull, PK: c.pk, Default: c.def})
	}
	if len(out) > 0 {
		return out
	}
	for _, c := range meta.ir.Columns {
		out = append(out, dbclient.GenColumn{Name: c.Name, Type: c.Type, PK: c.PK})
	}
	return out
}

// onDBGen adds the script to the end of the connection's console and puts
// the cursor on it. It does not run.
func (m *Model) onDBGen(msg dbGenMsg) tea.Cmd {
	if m.databases == nil || m.dbConsole().tableGen != msg.gen {
		return nil
	}
	if msg.err != nil {
		return m.dbConsoleError(msg.err)
	}
	if m.nvim == nil {
		return nil
	}
	path, err := m.databaseConsoleFile(msg.t.conn)
	if err != nil {
		return m.dbConsoleError(err)
	}
	m.ensureDBLua()
	m.ensureEditorWindowCurrent()
	lines := strings.Split(strings.TrimSuffix(msg.script, "\n"), "\n")
	_ = m.nvim.ExecLuaArgs(`local f, add = ...
vim.cmd('edit '..vim.fn.fnameescape(f)); vim.bo.filetype='sql'
local all = vim.api.nvim_buf_get_lines(0, 0, -1, false)
local n = #all
while n > 0 and all[n] == '' do n = n - 1 end
local at = n + 1
local out = {}
if n > 0 then out[1] = ''; at = n + 2 end
for _, l in ipairs(add) do out[#out + 1] = l end
out[#out + 1] = ''
vim.api.nvim_buf_set_lines(0, n, -1, false, out)
pcall(vim.cmd, 'silent write')
vim.api.nvim_win_set_cursor(0, {at, 0})`, path, lines)
	m.focus = FocusEditor
	return tea.Batch(m.fetchStateCmd(), m.pushToastDetail(toast.Info, "Test data script added", "Nothing ran yet. Read it in the console, then run it (Databases: Run File)."))
}
