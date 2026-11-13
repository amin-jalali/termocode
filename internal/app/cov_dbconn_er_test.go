package app

import (
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func cdbCell(s string) dbclient.Cell { return dbclient.Cell{Text: s} }

// cdbERSession answers the five catalog reads of a PostgreSQL schema
// (columns, constraints, indexes, views, routines): orders -> customers,
// and a lone logs table.
func cdbERSession(fail bool) *cdbSession {
	var n atomic.Int32
	return &cdbSession{run: func(string) dbclient.Result {
		if fail {
			return dbclient.Result{Err: errors.New("catalog denied")}
		}
		switch n.Add(1) % 5 {
		case 1:
			return dbclient.Result{Rows: [][]dbclient.Cell{
				{cdbCell("customers"), cdbCell("id"), cdbCell("integer"), cdbCell("t"), cdbCell(""), cdbCell("")},
				{cdbCell("orders"), cdbCell("id"), cdbCell("integer"), cdbCell("t"), cdbCell(""), cdbCell("")},
				{cdbCell("orders"), cdbCell("customer_id"), cdbCell("integer"), cdbCell("f"), cdbCell(""), cdbCell("")},
				{cdbCell("logs"), cdbCell("id"), cdbCell("integer"), cdbCell("t"), cdbCell(""), cdbCell("")}}}
		case 2:
			return dbclient.Result{Rows: [][]dbclient.Cell{
				{cdbCell("orders"), cdbCell("orders_customer_fk"), cdbCell("f"), cdbCell("customer_id"), cdbCell("customers"), cdbCell("id"), cdbCell("a"), cdbCell("a"), cdbCell("")}}}
		}
		return dbclient.Result{}
	}}
}

func cdbERModel(t *testing.T, sess *cdbSession) *Model {
	t.Helper()
	m := cdbRealModel(t, dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432}, dbclient.Conn{ID: "mongo", Driver: "redis", Host: "h"})
	cdbPGTree(m.databases)
	m.databases.sessions["pg"] = sess
	return m
}

func TestCovDBERSchemaOf(t *testing.T) {
	nodes := map[string]dbNode{
		"pg/db/app":                           {kind: dbNodeDatabase, name: "app"},
		"pg/db/app/schema/public":             {kind: dbNodeSchema, name: "public"},
		"pg/db/app/schema/public/table/users": {kind: dbNodeTable, name: "users"},
	}
	if got := dbERSchemaOf(nodes, "pg/db/app/schema/public/table/users", "postgres"); got != "public" {
		t.Fatalf("%q", got)
	}
	if got := dbERSchemaOf(nodes, "pg/db/app", "mysql"); got != "app" {
		t.Fatalf("mysql database is the schema: %q", got)
	}
	if got := dbERSchemaOf(nodes, "pg/db/app", "postgres"); got != "" {
		t.Fatalf("%q", got)
	}
	if got := dbERSchemaOf(nodes, "x", "sqlite"); got != "main" {
		t.Fatalf("%q", got)
	}
	if got := dbERSchemaOf(nodes, "nothing", "postgres"); got != "" {
		t.Fatalf("%q", got)
	}
	if f, c := dbERFormatID("dot+color"); f != "dot" || !c {
		t.Fatal("format id")
	}
}

func TestCovDBERDiagramFlow(t *testing.T) {
	sess := cdbERSession(false)
	m := cdbERModel(t, sess)
	s := m.databases
	toastHas := func(w string) {
		t.Helper()
		if !strings.Contains(covToastText(m), w) {
			t.Fatalf("toast lacks %q: %q", w, covToastText(m))
		}
	}
	s.databaseSelectPath("mongo")
	m.databaseERStart()
	toastHas("ER diagrams need a SQL database")
	s.databaseSelectPath("pg")
	m.databaseERStart()
	toastHas("Select a schema first")
	s.databaseSelectPath("pg/db/app/schema/public")
	delete(s.sessions, "pg")
	m.databaseERStart()
	toastHas("Not connected")
	s.sessions["pg"] = sess
	m.databaseERStart()
	if !m.pickerOpen || m.pickerKind != pickerKindDBERFormat {
		t.Fatal("no format picker")
	}
	m.pickerOpen = false
	m.pickerKind = 0
	if _, ok := m.databaseERPicker("mermaid"); ok {
		t.Fatal("foreign picker")
	}
	m.pickerKind = pickerKindDBERFormat
	msgs := cdbMsgs(func() func() tea.Msg { cmd, _ := m.databaseERPicker("mermaid+color"); return cmd }())
	if len(msgs) != 1 {
		t.Fatalf("msgs %v", msgs)
	}
	ready := msgs[0].(dbERReadyMsg)
	if ready.err != nil || !strings.Contains(ready.text, "orders") || !strings.Contains(ready.text, "customers") {
		t.Fatalf("%+v", ready)
	}
	m.onDBERReady(ready)
	_, lines, _ := m.httpBufText(0)
	if !strings.Contains(strings.Join(lines, "\n"), "erDiagram") {
		t.Fatalf("diagram not in the editor: %q", lines)
	}
	m.onDBERReady(dbERReadyMsg{err: errors.New("bad")})
	toastHas("ER diagram failed")
	// no editor: copied
	var copied string
	t.Cleanup(SetClipboardSink(func(x string) { copied = x }))
	nvim := m.nvim
	m.nvim = nil
	m.onDBERReady(ready)
	m.nvim = nvim
	if !strings.Contains(copied, "erDiagram") {
		t.Fatalf("copied %q", copied)
	}
	// the session goes away
	delete(s.sessions, "pg")
	m.pickerKind = pickerKindDBERFormat
	m.databaseERPicker("dot")
	toastHas("Not connected any more")
	m.databases = nil
	if cmd, ok := m.databaseERPicker("dot"); !ok || cmd != nil {
		t.Fatal("no databases")
	}
	m.databaseERStart()
	// the read itself fails
	m = cdbERModel(t, cdbERSession(true))
	m.databases.databaseSelectPath("pg/db/app/schema/public")
	m.databaseERStart()
	m.pickerOpen = false
	cmd, _ := m.databaseERPicker("dot")
	if got := cdbMsgs(cmd); len(got) != 1 || got[0].(dbERReadyMsg).err == nil {
		t.Fatalf("%v", got)
	}
	m.databases.sessions["pg"].(*cdbSession).roErr = errors.New("ro")
	cmd, _ = m.databaseERPicker("dot")
	if got := cdbMsgs(cmd); len(got) != 1 || got[0].(dbERReadyMsg).err == nil {
		t.Fatalf("%v", got)
	}
}

func TestCovDBERPathFlow(t *testing.T) {
	sess := cdbERSession(false)
	m := cdbERModel(t, sess)
	s := m.databases
	toastHas := func(w string) {
		t.Helper()
		if !strings.Contains(covToastText(m), w) {
			t.Fatalf("toast lacks %q: %q", w, covToastText(m))
		}
	}
	s.databaseSelectPath("pg")
	m.databaseERPathStart()
	toastHas("Select a table first")
	s.databaseSelectPath("pg/db/app/schema/public/table/orders")
	delete(s.sessions, "pg")
	m.databaseERPathStart()
	toastHas("Not connected")
	s.sessions["pg"] = sess
	msgs := cdbMsgs(m.databaseERPathStart())
	if len(msgs) != 1 {
		t.Fatalf("msgs %v", msgs)
	}
	sm := msgs[0].(dbERPathSchemaMsg)
	if sm.err != nil || len(sm.schema.Tables) != 3 {
		t.Fatalf("%+v", sm)
	}
	m.onDBERPathSchema(sm)
	if !m.pickerOpen || m.pickerKind != pickerKindDBERPathTo {
		t.Fatal("no table picker")
	}
	// to a table that is joined: a path diagram opens
	cdbPick2er := func(id string) {
		m.pickerOpen = true
		m.pickerKind = pickerKindDBERPathTo
		cmd, ok := m.databaseERPathPicker(id)
		if !ok {
			t.Fatal("not routed")
		}
		cdbRun(cmd)
	}
	cdbPick2er("customers")
	_, lines, _ := m.httpBufText(0)
	if !strings.Contains(strings.Join(lines, "\n"), "path:") {
		t.Fatalf("no path comment: %q", lines)
	}
	toastHas("Path: ")
	// to the lone table: no path
	s.erPath = &dbERPathState{target: dbERTarget{label: "pg · public"}, from: "orders", schema: sm.schema}
	cdbPick2er("logs")
	toastHas("No foreign-key path")
	// the picker without a pending path
	cdbPick2er("logs")
	m.pickerKind = 0
	if _, ok := m.databaseERPathPicker("x"); ok {
		t.Fatal("foreign picker")
	}
	// errors and empty schema
	s.erPath = &dbERPathState{target: dbERTarget{label: "l"}, from: "orders"}
	m.onDBERPathSchema(dbERPathSchemaMsg{err: errors.New("denied")})
	toastHas("Reading the schema failed")
	s.erPath = &dbERPathState{target: dbERTarget{label: "l"}, from: "orders"}
	m.onDBERPathSchema(dbERPathSchemaMsg{})
	toastHas("No other tables")
	if cmd := m.onDBERPathSchema(dbERPathSchemaMsg{}); cmd != nil {
		t.Fatal("no path pending")
	}
	// the read fails
	m = cdbERModel(t, cdbERSession(true))
	m.databases.databaseSelectPath("pg/db/app/schema/public/table/orders")
	if got := cdbMsgs(m.databaseERPathStart()); len(got) != 1 || got[0].(dbERPathSchemaMsg).err == nil {
		t.Fatalf("%v", got)
	}
	m.databases.sessions["pg"].(*cdbSession).roErr = errors.New("ro")
	if got := cdbMsgs(m.databaseERPathStart()); len(got) != 1 || got[0].(dbERPathSchemaMsg).err == nil {
		t.Fatalf("%v", got)
	}
	m.databases.databaseSelectPath("pg/db/app/schema/public/table/orders")
	m.databases.conns = append(m.databases.conns[:0:0], dbclient.Conn{ID: "mongo", Driver: "redis"})
	m.databaseERPathStart()
	m.databases = nil
	m.databaseERPathStart()
	m.pickerKind = pickerKindDBERPathTo
	if cmd, ok := m.databaseERPathPicker("x"); !ok || cmd != nil {
		t.Fatal("no databases")
	}
}

func TestCovDBERPathText(t *testing.T) {
	cs := dbclient.CanonSchema{Driver: "postgres", Schema: "public", Tables: []dbclient.CanonTable{{Name: "a"}, {Name: "b"}}}
	if _, _, err := dbERPathText(cs, "a", "b"); err == nil {
		t.Fatal("a path between unrelated tables")
	}
}
