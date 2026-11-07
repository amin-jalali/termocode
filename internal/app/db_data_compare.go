package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/textdiff"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Data compare on the result page (DBX-12). gc marks the result on the
// page as the SOURCE; gc on another result (another tab, table or
// connection) compares it as the TARGET. A table's data page counts as
// the whole table (up to dbDataCompareMax rows, its filter kept), read
// again on a read-only session; a query result counts as the rows it
// loaded. Rows match on the source table's key (or a key typed when there
// is none). The diff card shows the rows that differ; when the target is
// a table it also copies or opens the sync script that makes the target
// hold the source's rows. Nothing here runs that script.

const promptKindDBDataCompareKey promptKindEnum = 5145

// dbDataCompareMax is the most rows read from each table.
const dbDataCompareMax = 10000

// dbDataCompareState is the marked source and the last comparison.
type dbDataCompareState struct {
	mark     *dbRun
	pendSrc  *dbRun // waiting for key columns
	pendTgt  *dbRun
	gen      uint64
	script   string
	title    string
	srcLabel string
	tgtLabel string
}

// dbDataSide is one side read for the compare.
type dbDataSide struct {
	res       dbclient.Result
	truncated bool
}

// dbRunLabel names a result for the card: the table, or the connection
// and the first line of the SQL.
func dbRunLabel(r *dbRun) string {
	if r.tv != nil {
		l := r.tv.label() + " (" + sanitizeDBText(r.tv.conn) + ")"
		if r.tv.where != "" {
			l += " WHERE " + dbOneLine(r.tv.where)
		}
		return l
	}
	sql := dbOneLine(r.sql)
	if len(sql) > 60 {
		sql = sql[:60] + "…"
	}
	return sanitizeDBText(r.conn) + ": " + sql
}

// dbDropColumn removes the column named name (the hidden rowid) from r.
func dbDropColumn(r dbclient.Result, name string) dbclient.Result {
	at := -1
	for i, c := range r.Columns {
		if c.Name == name {
			at = i
			break
		}
	}
	if at < 0 {
		return r
	}
	out := r
	out.Columns = append(append([]dbclient.Column(nil), r.Columns[:at]...), r.Columns[at+1:]...)
	out.Rows = make([][]dbclient.Cell, len(r.Rows))
	for i, row := range r.Rows {
		if at < len(row) {
			row = append(append([]dbclient.Cell(nil), row[:at]...), row[at+1:]...)
		}
		out.Rows[i] = row
	}
	out.Key = nil
	for _, k := range r.Key {
		switch {
		case k < at:
			out.Key = append(out.Key, k)
		case k > at:
			out.Key = append(out.Key, k-1)
		}
	}
	return out
}

// databaseDataCompareKey is gc on the result page.
func (m *Model) databaseDataCompareKey() tea.Cmd {
	s := m.dbConsole()
	r := s.shown
	if r == nil || r.redis != nil || r.plan != nil || r.result.Err != nil || len(r.result.Columns) == 0 {
		return m.pushToastDetail(toast.Info, "Nothing to compare here", "gc works on a result with rows or on table data.")
	}
	dc := &s.dataCmp
	switch {
	case dc.mark == nil:
		dc.mark = r
		return m.pushToastDetail(toast.Info, "Marked as the source: "+dbRunLabel(r), "Show another result or table, then gc compares it. gc here again unmarks.")
	case dc.mark == r:
		dc.mark = nil
		return m.pushToast(toast.Info, "Compare mark removed")
	}
	src := dc.mark
	dc.mark = nil
	var key []string
	if src.tv != nil {
		key = src.tv.keys
	}
	return m.databaseDataCompareRun(src, r, key)
}

// databaseDataCompareRun reads both sides (off the UI goroutine) and
// shows the card.
func (m *Model) databaseDataCompareRun(src, tgt *dbRun, key []string) tea.Cmd {
	s := m.dbConsole()
	read := func(r *dbRun) (func(ctx context.Context) dbDataSide, error) {
		if r.tv == nil {
			res := r.result
			return func(context.Context) dbDataSide { return dbDataSide{res: res, truncated: res.Truncated} }, nil
		}
		cp := *r.tv
		cp.offset = 0
		sql, err := cp.selectSQL(dbDataCompareMax + 1)
		if err != nil {
			return nil, err
		}
		sess := m.databases.sessions[cp.sessKey]
		if sess == nil {
			return nil, fmt.Errorf("%s is not connected", sanitizeDBText(cp.conn))
		}
		inTx := m.databases.txOpen(cp.sessKey)
		return func(ctx context.Context) dbDataSide {
			opt := dbclient.RunOptions{MaxRows: dbDataCompareMax + 1}
			var res dbclient.Result
			if inTx {
				res = dbReadInTx(ctx, sess, sql, opt)
			} else {
				res = dbRunReadOnly(ctx, sess, sql, opt)
			}
			side := dbDataSide{res: dbDropColumn(res, dbRowidAlias)}
			if len(side.res.Rows) > dbDataCompareMax {
				side.res.Rows, side.truncated = side.res.Rows[:dbDataCompareMax], true
			}
			return side
		}, nil
	}
	rs, err := read(src)
	if err == nil {
		var rt func(context.Context) dbDataSide
		if rt, err = read(tgt); err == nil {
			s.dataCmp.gen++
			gen := s.dataCmp.gen
			return tea.Batch(m.pushToast(toast.Info, "Comparing the rows…"), func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), dbCompareTimeout)
				defer cancel()
				a, b := rs(ctx), rt(ctx)
				return applyMsg(func(m *Model) tea.Cmd { return m.onDBDataCompare(gen, src, tgt, key, a, b) })
			})
		}
	}
	return m.pushToastDetail(toast.Warn, "Cannot compare", sanitizeDBText(err.Error()))
}

func (m *Model) onDBDataCompare(gen uint64, src, tgt *dbRun, key []string, a, b dbDataSide) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	dc := &m.dbConsole().dataCmp
	if dc.gen != gen {
		return nil
	}
	for _, side := range []dbDataSide{a, b} {
		if side.res.Err != nil {
			return m.pushToastDetail(toast.Errr, "Could not read the rows", dbclient.Redact(sanitizeDBText(side.res.Err.Error())))
		}
	}
	cmp, err := dbclient.CompareData(a.res, b.res, key)
	if err != nil {
		if len(key) == 0 {
			dc.pendSrc, dc.pendTgt = src, tgt
			cmd := m.openHTTPPrompt(promptKindDBDataCompareKey, "Compare rows", "Key columns that identify a row (comma separated):", "")
			return tea.Batch(cmd, m.pushToastDetail(toast.Info, "No key to match rows on", sanitizeDBText(err.Error())))
		}
		return m.pushToastDetail(toast.Warn, "Cannot compare", sanitizeDBText(err.Error()))
	}
	return m.databaseShowDataCompare(src, tgt, cmp, a.truncated || b.truncated)
}

// databaseDataCompareKeyTyped is the key columns typed after "no key".
func (m *Model) databaseDataCompareKeyTyped(value string) tea.Cmd {
	dc := &m.dbConsole().dataCmp
	src, tgt := dc.pendSrc, dc.pendTgt
	dc.pendSrc, dc.pendTgt = nil, nil
	if src == nil || tgt == nil {
		return nil
	}
	var key []string
	for _, k := range strings.Split(value, ",") {
		if k = strings.TrimSpace(k); k != "" {
			key = append(key, k)
		}
	}
	if len(key) == 0 {
		return m.pushToast(toast.Info, "Compare cancelled: no key columns")
	}
	return m.databaseDataCompareRun(src, tgt, key)
}

// dbDataDiffText is the rows that differ, one line per row in key order:
// old holds the target's rows, new the source's, so the diff reads as
// what the sync would change.
func dbDataDiffText(cmp dbclient.DataComparison) (old, new string) {
	line := func(cells []dbclient.Cell) string {
		parts := make([]string, len(cells))
		for i, c := range cells {
			v := c.Text
			if c.Null {
				v = "NULL"
			}
			parts[i] = cmp.Columns[i] + "=" + dbOneLine(v)
		}
		return strings.Join(parts, " | ")
	}
	type pair struct{ key, o, n string }
	rows := make([]pair, 0, len(cmp.Diffs))
	for _, d := range cmp.Diffs {
		k := make([]string, len(d.Key))
		for i, c := range d.Key {
			k[i] = c.Text
		}
		p := pair{key: strings.Join(k, "\x00")}
		if d.Target != nil {
			p.o = line(d.Target)
		}
		if d.Source != nil {
			p.n = line(d.Source)
		}
		rows = append(rows, p)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].key < rows[j].key })
	var ob, nb strings.Builder
	for _, p := range rows {
		if p.o != "" {
			ob.WriteString(p.o + "\n")
		}
		if p.n != "" {
			nb.WriteString(p.n + "\n")
		}
	}
	return ob.String(), nb.String()
}

func (m *Model) databaseShowDataCompare(src, tgt *dbRun, cmp dbclient.DataComparison, truncated bool) tea.Cmd {
	ai := m.aiS()
	if ai.review != nil && m.aiReviewPending() > 0 {
		return m.aiInfo("Finish the AI review first (AI: Review Pending Changes)")
	}
	dc := &m.dbConsole().dataCmp
	dc.srcLabel, dc.tgtLabel = dbRunLabel(src), dbRunLabel(tgt)
	dc.script, dc.title = "", ""
	if tv := tgt.tv; tv != nil && !tv.view {
		if d := dbclient.DialectFor(tv.driver); d != nil {
			dc.script = cmp.SyncScript(d, tv.schema, tv.name)
			dc.title = "SQL Data Sync: " + tv.label() + " <- " + dc.srcLabel
		}
	}
	old, nw := dbDataDiffText(cmp)
	d := textdiff.Compute(old, nw)
	files := []diffview.File{{Name: "rows (key: " + strings.Join(cmp.Key, ", ") + ")", Old: old, New: nw, Add: d.Added, Del: d.Removed, Status: ' '}}
	if old != nw {
		files[0].Status = 'M'
	}
	if len(cmp.OnlySource)+len(cmp.OnlyTarget) > 0 {
		cols := func(names []string) string {
			if len(names) == 0 {
				return ""
			}
			return strings.Join(names, "\n") + "\n"
		}
		files = append(files, diffview.File{Name: "columns not compared", Old: cols(cmp.OnlyTarget), New: cols(cmp.OnlySource), Status: 'M'})
	}
	subject := "The rows are the same"
	if len(cmp.Diffs) > 0 {
		subject = fmt.Sprintf("%d added · %d changed · %d removed · %d same", cmp.Added, cmp.Changed, cmp.Removed, cmp.Same)
	}
	if cmp.DupKeys+cmp.NullKeys > 0 {
		subject += fmt.Sprintf(" · %d rows skipped (duplicate or NULL key)", cmp.DupKeys+cmp.NullKeys)
	}
	if truncated {
		subject += fmt.Sprintf(" · only the first %d rows of a side", dbDataCompareMax)
	}
	var buttons []diffview.Button
	keys := map[string]string{}
	hints := []string{}
	if dc.script != "" && len(cmp.Diffs) > 0 {
		buttons = []diffview.Button{
			{ID: "copy-script", Label: "Copy sync script", Kind: "primary"},
			{ID: "open-script", Label: "Open script"},
		}
		keys = map[string]string{"y": "copy-script", "o": "open-script"}
		hints = []string{"y", "copy script", "o", "open script"}
	} else if len(cmp.Diffs) > 0 {
		subject += " · a sync script needs table data as the target"
	}
	card := diffview.NewCommit(diffview.Header{
		Title: "Compare data", Subject: subject, Files: len(files), Add: d.Added, Del: d.Removed,
		OldLabel: "− target: " + dc.tgtLabel, NewLabel: "+ source: " + dc.srcLabel,
	}, files, buttons)
	card.SetCustom(keys, append(hints, "n/p", "change", "tab", "file", "t", "split/unified", "esc", "close"))
	ai.review = &aiReview{card: card, onAction: func(m *Model, id string) tea.Cmd { return m.databaseDataCompareAction(id) }}
	return m.showAIReview()
}

// databaseDataCompareAction copies or opens the sync script; neither runs
// it.
func (m *Model) databaseDataCompareAction(id string) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	dc := &m.dbConsole().dataCmp
	if dc.script == "" {
		return nil
	}
	switch id {
	case "copy-script":
		return m.copyWithToast(dc.script, "Sync script copied · review it before you run it")
	case "open-script":
		if m.nvim == nil {
			return m.copyWithToast(dc.script, "No editor: sync script copied instead")
		}
		m.reviewOpen = false
		if m.ai != nil {
			m.ai.review = nil
		}
		m.ensureDBLua()
		m.ensureEditorWindowCurrent()
		_ = m.nvim.ExecLuaArgs(`_G._termocode_db_ddl(...)`, dc.title, strings.Split(strings.TrimRight(dc.script, "\n"), "\n"))
		m.focus = FocusEditor
		return m.fetchStateCmd()
	}
	return nil
}
