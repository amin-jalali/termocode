package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/textdiff"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Databases: Compare Schemas… (DB-15). Two pickers choose a SOURCE (the
// model) and a TARGET (the schema to change) among the schemas of the
// connected SQL databases; both are read on read-only sessions, written
// as canonical DDL (internal/dbclient/canon.go) and shown in the diff
// card, one file per object kind. The card copies or opens a basic
// migration script that makes the target match the source. Nothing here
// runs that script.

const (
	pickerKindDBCompareSource pickerKindEnum = 1550 + iota
	pickerKindDBCompareTarget
)

const dbCompareToastKey = "db-compare"

const (
	dbCompareTimeout  = 90 * time.Second
	dbCompareMaxSides = 200
)

// dbCompareSide is one schema the compare can pick.
type dbCompareSide struct {
	conn, name, sessKey, driver, database, schema string
}

func (s dbCompareSide) label() string {
	parts := []string{s.name}
	if s.database != "" {
		parts = append(parts, s.database)
	}
	return sanitizeDBText(strings.Join(append(parts, s.schema), " · "))
}

// dbCompareState is the compare in progress (or the last one shown).
type dbCompareState struct {
	gen    uint64
	sides  []dbCompareSide
	src    int
	script string
	title  string // the script buffer's name
}

type dbCompareSidesMsg struct {
	gen   uint64
	sides []dbCompareSide
	err   string
}

type dbCompareReadyMsg struct {
	gen      uint64
	src, tgt dbCompareSide
	cmp      dbclient.SchemaComparison
	err      error
}

func dbDriverFamily(driver string) string {
	switch strings.ToLower(driver) {
	case "postgres", "postgresql", "pg":
		return "postgres"
	case "mysql", "mariadb":
		return "mysql"
	case "sqlite", "sqlite3":
		return "sqlite"
	}
	return strings.ToLower(driver)
}

func dbDriverTitle(driver string) string {
	switch dbDriverFamily(driver) {
	case "postgres":
		return "PostgreSQL"
	case "mysql":
		return "MySQL"
	case "sqlite":
		return "SQLite"
	}
	return driver
}

func (m *Model) dbCompareS() *dbCompareState {
	if m.databases.compare == nil {
		m.databases.compare = &dbCompareState{}
	}
	return m.databases.compare
}

// databaseCompareStart lists the schemas of every connected SQL session
// (off the UI goroutine, read-only) and then opens the SOURCE picker.
func (m *Model) databaseCompareStart() tea.Cmd {
	type cand struct {
		key  string
		side dbCompareSide
		sess dbclient.Session
	}
	var cands []cand
	if s := m.databases; s != nil {
		keys := make([]string, 0, len(s.sessions))
		for k := range s.sessions {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			conn := key
			if n, ok := s.nodes[key]; ok && n.conn != "" {
				conn = n.conn
			} else if i := strings.Index(key, "/"); i > 0 {
				conn = key[:i]
			}
			c, ok := s.connection(conn)
			if !ok || dbclient.DialectFor(c.Driver) == nil || s.sessions[key] == nil {
				continue
			}
			cands = append(cands, cand{key: key, sess: s.sessions[key], side: dbCompareSide{conn: conn, name: dbConnName(c), sessKey: key, driver: dbDriverFamily(c.Driver)}})
		}
	}
	if len(cands) == 0 {
		return tea.Batch(m.revealDatabasesView(false), m.pushToastDetail(toast.Info, "No SQL database is connected",
			"Compare Schemas needs connected databases. Press Enter on a connection in the Databases view, then run Databases: Compare Schemas again."))
	}
	cs := m.dbCompareS()
	cs.gen++
	gen := cs.gen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbCompareTimeout)
		defer cancel()
		var sides []dbCompareSide
		seen := map[string]bool{}
		var firstErr string
		for _, c := range cands {
			ro, err := c.sess.ReadOnlySession()
			if err != nil || ro == nil {
				if firstErr == "" && err != nil {
					firstErr = err.Error()
				}
				continue
			}
			db, schemas, err := dbclient.ListSchemas(ctx, ro, c.side.driver)
			_ = ro.Close()
			if err != nil {
				if firstErr == "" {
					firstErr = err.Error()
				}
				continue
			}
			for _, sc := range schemas {
				// The schema name is what the catalogs are asked about:
				// the server's own text (label() cleans it for show).
				side := c.side
				side.database, side.schema = db, sc
				if side.driver != "postgres" {
					side.database = ""
				}
				k := side.conn + "\x00" + side.database + "\x00" + side.schema
				if seen[k] || len(sides) >= dbCompareMaxSides {
					continue
				}
				seen[k] = true
				sides = append(sides, side)
			}
		}
		return dbCompareSidesMsg{gen: gen, sides: sides, err: firstErr}
	}
}

func (m *Model) onDBCompareSides(msg dbCompareSidesMsg) tea.Cmd {
	if m.databases == nil || m.databases.compare == nil || m.databases.compare.gen != msg.gen {
		return nil
	}
	cs := m.databases.compare
	cs.sides = msg.sides
	switch len(msg.sides) {
	case 0:
		detail := "The connected databases list no schema."
		if msg.err != "" {
			detail = dbclient.Redact(sanitizeDBText(msg.err))
		}
		return m.pushToastDetail(toast.Warn, "No schema to compare", detail)
	case 1:
		return m.pushToastDetail(toast.Info, "Only one schema is connected",
			"Compare needs two schemas: connect a second database in the Databases view.")
	}
	items := make([]picker.Item, 0, len(cs.sides))
	first := m.databaseCompareTreeSide()
	for i, side := range cs.sides {
		it := picker.Item{ID: strconv.Itoa(i), Title: side.label(), Hint: dbDriverTitle(side.driver)}
		if i == first {
			items = append([]picker.Item{it}, items...)
			continue
		}
		items = append(items, it)
	}
	m.picker = picker.NewItems(" Compare schemas 1/2 · SOURCE (the one to copy) ", items)
	m.picker.SetWidth(dbComparePickerWidth(m.w))
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindDBCompareSource
	return nil
}

// databaseCompareTreeSide is the side under the tree cursor (a schema,
// database or connection row), listed first in the SOURCE picker; -1 if
// none.
func (m *Model) databaseCompareTreeSide() int {
	r, ok := m.databaseCurrentRow()
	if !ok || m.databases.compare == nil {
		return -1
	}
	n := m.databases.nodes[r.path]
	for i, side := range m.databases.compare.sides {
		if side.conn != r.conn {
			continue
		}
		switch n.kind {
		case dbNodeSchema:
			if side.schema == n.name && (side.database == "" || side.database == dbAncestorDatabase(r.path)) {
				return i
			}
		case dbNodeDatabase:
			if side.schema == n.name || side.database == n.name && side.schema == "public" {
				return i
			}
		}
	}
	return -1
}

// databaseComparePicker handles the SOURCE and TARGET pickers.
func (m *Model) databaseComparePicker(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBCompareSource && m.pickerKind != pickerKindDBCompareTarget {
		return nil, false
	}
	if m.databases == nil || m.databases.compare == nil {
		return nil, true
	}
	cs := m.databases.compare
	i, err := strconv.Atoi(id)
	if err != nil || i < 0 || i >= len(cs.sides) {
		return nil, true
	}
	if m.pickerKind == pickerKindDBCompareSource {
		cs.src = i
		src := cs.sides[i]
		var items []picker.Item
		for j, side := range cs.sides {
			if j == i {
				continue
			}
			hint := dbDriverTitle(side.driver)
			if side.driver != src.driver {
				hint += " · other type"
			}
			items = append(items, picker.Item{ID: strconv.Itoa(j), Title: side.label(), Hint: hint})
		}
		m.picker = picker.NewItems(" Compare schemas 2/2 · TARGET (to change) ", items)
		m.picker.SetWidth(dbComparePickerWidth(m.w))
		m.picker.SetSize(m.w, m.h)
		m.pickerOpen = true
		m.pickerKind = pickerKindDBCompareTarget
		return nil, true
	}
	if cs.src < 0 || cs.src >= len(cs.sides) {
		return nil, true
	}
	return m.databaseCompareRun(cs.sides[cs.src], cs.sides[i]), true
}

// databaseCompareRun reads both schemas on read-only sessions and builds
// the comparison.
func (m *Model) databaseCompareRun(src, tgt dbCompareSide) tea.Cmd {
	if src.driver != tgt.driver {
		return m.pushToastDetail(toast.Warn, "Cannot compare "+dbDriverTitle(src.driver)+" with "+dbDriverTitle(tgt.driver),
			"Schema compare needs two schemas of the same database type. Pick another target.")
	}
	s := m.databases
	srcSess, tgtSess := s.sessions[src.sessKey], s.sessions[tgt.sessKey]
	if srcSess == nil || tgtSess == nil {
		return m.pushToastDetail(toast.Warn, "Not connected any more", "Connect both databases again, then compare.")
	}
	cs := m.dbCompareS()
	cs.gen++
	gen := cs.gen
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.PushKeyed(dbCompareToastKey, toast.Info, "Comparing "+tgt.label()+" with "+src.label()+"…", "Reading both schemas (read-only).")
	return tea.Batch(toastCmd, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbCompareTimeout)
		defer cancel()
		read := func(side dbCompareSide, sess dbclient.Session) (dbclient.CanonSchema, error) {
			ro, err := sess.ReadOnlySession()
			if err != nil {
				return dbclient.CanonSchema{}, err
			}
			defer ro.Close()
			out, err := dbclient.ReadCanonSchema(ctx, ro, side.driver, side.schema)
			out.Label = side.label()
			return out, err
		}
		a, err := read(src, srcSess)
		if err != nil {
			return dbCompareReadyMsg{gen: gen, src: src, tgt: tgt, err: err}
		}
		b, err := read(tgt, tgtSess)
		if err != nil {
			return dbCompareReadyMsg{gen: gen, src: src, tgt: tgt, err: err}
		}
		return dbCompareReadyMsg{gen: gen, src: src, tgt: tgt, cmp: dbclient.CompareSchemas(a, b)}
	})
}

func (m *Model) onDBCompareReady(msg dbCompareReadyMsg) tea.Cmd {
	if m.databases == nil || m.databases.compare == nil || m.databases.compare.gen != msg.gen {
		return nil
	}
	// The progress toast would cover the card (or sit next to the error).
	m.toast = m.toast.Dismiss(dbCompareToastKey)
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Compare failed", dbclient.Redact(sanitizeDBText(msg.err.Error())))
	}
	return m.databaseShowCompare(msg.src.label(), msg.tgt.label(), msg.cmp)
}

// databaseShowCompare opens the diff card on a comparison.
func (m *Model) databaseShowCompare(srcLabel, tgtLabel string, cmp dbclient.SchemaComparison) tea.Cmd {
	s := m.aiS()
	if s.review != nil && m.aiReviewPending() > 0 {
		return m.aiInfo("Finish the AI review first (AI: Review Pending Changes)")
	}
	cs := m.dbCompareS()
	cs.script = cmp.Script
	cs.title = "SQL Migration: " + tgtLabel + " <- " + srcLabel
	files := make([]diffview.File, 0, len(cmp.Files))
	add, del := 0, 0
	for _, f := range cmp.Files {
		d := textdiff.Compute(f.Old, f.New)
		df := diffview.File{Name: f.Name, Old: f.Old, New: f.New, Add: d.Added, Del: d.Removed, Status: ' '}
		if f.Old != f.New {
			df.Status = 'M'
		}
		add += d.Added
		del += d.Removed
		files = append(files, df)
	}
	subject := "The schemas are the same"
	if cmp.Changes > 0 {
		word := "objects differ"
		if cmp.Changes == 1 {
			word = "object differs"
		}
		subject = fmt.Sprintf("%d %s · the script makes the target match the source", cmp.Changes, word)
		if cmp.Destructive > 0 {
			subject += " · " + dbPlural(cmp.Destructive, "object") + " dropped"
		}
	}
	if cmp.Truncated {
		subject += fmt.Sprintf(" · only the first %d tables", dbclient.CompareMaxTables)
	}
	card := diffview.NewCommit(diffview.Header{
		Title: "Compare schemas", Subject: subject, Files: len(files), Add: add, Del: del,
		OldLabel: "− target: " + tgtLabel, NewLabel: "+ source: " + srcLabel,
	}, files, []diffview.Button{
		{ID: "copy-script", Label: "Copy migration script", Kind: "primary"},
		{ID: "open-script", Label: "Open script"},
	})
	card.SetCustom(map[string]string{"y": "copy-script", "o": "open-script"},
		[]string{"y", "copy script", "o", "open script", "n/p", "change", "tab", "file", "t", "split/unified", "esc", "close"})
	// The first file that differs is shown first.
	for i, f := range files {
		if f.Status == 'M' {
			card.Select(i)
			break
		}
	}
	s.review = &aiReview{card: card, onAction: func(m *Model, id string) tea.Cmd { return m.databaseCompareAction(id) }}
	return m.showAIReview()
}

// databaseCompareAction runs a card button: copy the script, or open it
// in a read-only SQL buffer. Neither runs it.
func (m *Model) databaseCompareAction(id string) tea.Cmd {
	if m.databases == nil || m.databases.compare == nil {
		return nil
	}
	cs := m.databases.compare
	switch id {
	case "copy-script":
		return m.copyWithToast(cs.script, "Migration script copied · review it before you run it")
	case "open-script":
		if m.nvim == nil {
			return m.copyWithToast(cs.script, "No editor: migration script copied instead")
		}
		m.reviewOpen = false
		if m.ai != nil {
			m.ai.review = nil
		}
		m.ensureDBLua()
		m.ensureEditorWindowCurrent()
		_ = m.nvim.ExecLuaArgs(`_G._termocode_db_ddl(...)`, cs.title, strings.Split(strings.TrimRight(cs.script, "\n"), "\n"))
		m.focus = FocusEditor
		return m.fetchStateCmd()
	}
	return nil
}

// dbComparePickerWidth fits the step title ("… SOURCE (the one to
// copy)") on its row: the picker cuts a longer title, and the cut part
// says which side is which.
func dbComparePickerWidth(w int) int {
	return max(56, min(76, w-8))
}
