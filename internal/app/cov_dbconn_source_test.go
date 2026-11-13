package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestCovDBSourceHelpers(t *testing.T) {
	for k, want := range map[dbNodeKind]dbclient.ObjectKind{dbNodeView: dbclient.ObjectView, dbNodeRoutine: dbclient.ObjectFunction, dbNodeTrigger: dbclient.ObjectTrigger} {
		if got, ok := dbSourceKindOf(k); !ok || got != want {
			t.Errorf("%v: %v %v", k, got, ok)
		}
	}
	if _, ok := dbSourceKindOf(dbNodeTable); ok {
		t.Error("a table has no source")
	}
	long := dbSourceFileName(dbclient.ObjectView, strings.Repeat("a/../b", 30))
	if strings.ContainsAny(long, "/\\") || strings.Contains(long, "..") || !strings.HasSuffix(long, ".sql") || len(long) > 100 {
		t.Fatalf("file name %q", long)
	}
	if dbSourceFileName(dbclient.ObjectView, "a") == dbSourceFileName(dbclient.ObjectFunction, "a") {
		t.Fatal("kinds share a file")
	}
	e := dbclient.NewSourceEdit("pg", dbclient.ObjectView, "v1", "CREATE VIEW v1 AS SELECT 1\n")
	txt := dbSourceCardText(e, dbclient.SourcePlan{Statements: []string{"DROP VIEW v1", "CREATE VIEW v1 AS select 2;"}}, true)
	for _, w := range []string{"changed on the server", "not in one transaction", "DROP VIEW v1;", "CREATE VIEW v1 AS select 2;"} {
		if !strings.Contains(txt, w) {
			t.Errorf("card lacks %q: %s", w, txt)
		}
	}
	if strings.Contains(dbSourceCardText(e, dbclient.SourcePlan{Statements: []string{"x"}, Transactional: true}, false), "Warning") {
		t.Error("warning without need")
	}
}

func TestCovDBReadSource(t *testing.T) {
	ctx := context.Background()
	sess := &cdbSession{run: func(string) dbclient.Result {
		return dbclient.Result{Rows: [][]dbclient.Cell{{{Text: "CREATE VIEW v1 AS SELECT 1"}, {Text: "CREATE VIEW v1 AS SELECT 1"}, {Text: "x"}}}}
	}}
	got, err := dbReadSource(ctx, sess, "postgres", dbclient.ObjectView, "public.v1")
	if err != nil || !strings.Contains(got, "CREATE VIEW") {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := dbReadSource(ctx, sess, "oracle", dbclient.ObjectView, "v1"); err == nil {
		t.Fatal("unknown driver")
	}
	sess.roErr = errors.New("ro")
	if _, err := dbReadSource(ctx, sess, "postgres", dbclient.ObjectView, "v1"); err == nil {
		t.Fatal("ro error")
	}
	sess.roErr = nil
	sess.run = func(string) dbclient.Result { return dbclient.Result{Err: errors.New("boom")} }
	if _, err := dbReadSource(ctx, sess, "postgres", dbclient.ObjectView, "v1"); err == nil {
		t.Fatal("run error")
	}
	sess.run = func(string) dbclient.Result { return dbclient.Result{} }
	if _, err := dbReadSource(ctx, sess, "postgres", dbclient.ObjectView, "v1"); err == nil || !strings.Contains(err.Error(), "no stored source") {
		t.Fatalf("%v", err)
	}
}

func cdbSourceModel(t *testing.T, readOnly bool) (*Model, *cdbSession) {
	t.Helper()
	m := cdbRealModel(t, dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432, ReadOnly: readOnly})
	cdbPGTree(m.databases)
	sess := &cdbSession{run: func(q string) dbclient.Result {
		return dbclient.Result{Rows: [][]dbclient.Cell{{{Text: "CREATE VIEW v1 AS SELECT 1"}, {Text: "CREATE VIEW v1 AS SELECT 1"}, {Text: "CREATE VIEW v1 AS SELECT 1"}}}}
	}}
	m.databases.sessions["pg"] = sess
	return m, sess
}

func TestCovDBSourceEditNeedsASourceRow(t *testing.T) {
	m, _ := cdbSourceModel(t, false)
	m.databaseEditSourceStart() // cursor on the connection
	if !strings.Contains(covToastText(m), "Only these have a source") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.databases.databaseSelectPath("pg/db/app/schema/public/view/v1")
	m.databases.sessions = map[string]dbclient.Session{}
	m.databaseEditSourceStart()
	if !strings.Contains(covToastText(m), "Not connected") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.databases = nil
	m.databaseEditSourceStart()
	if !strings.Contains(covToastText(m), "Select a view") {
		t.Fatalf("toast %q", covToastText(m))
	}
}

func TestCovDBSourceEditSubmitFlow(t *testing.T) {
	m, sess := cdbSourceModel(t, false)
	s := m.databases
	s.databaseSelectPath("pg/db/app/schema/public/view/v1")
	msgs := cdbMsgs(m.databaseEditSourceStart())
	if len(msgs) != 1 {
		t.Fatalf("msgs %v", msgs)
	}
	loaded := msgs[0].(dbSourceLoadedMsg)
	if loaded.err != nil || loaded.name != `"public"."v1"` || !strings.Contains(loaded.text, "CREATE VIEW") {
		t.Fatalf("%+v", loaded)
	}
	m.onDBSourceLoaded(loaded)
	if len(s.sources.edits) != 1 {
		t.Fatalf("no edit: %q", covToastText(m))
	}
	var file string
	for f := range s.sources.edits {
		file = f
	}
	// loading again while the disk copy differs from the stored one keeps the edit
	if err := os.WriteFile(file, []byte("CREATE VIEW v1 AS SELECT 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.onDBSourceLoaded(loaded)
	if !strings.Contains(covToastText(m), "unsubmitted edit is kept") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// the buffer is the edit: change it
	if err := m.nvim.ExecLuaArgs(`vim.cmd('edit! ' .. vim.fn.fnameescape(...)); vim.api.nvim_buf_set_lines(0, 0, -1, false, {'CREATE VIEW v1 AS SELECT 3'})`, file); err != nil {
		t.Fatal(err)
	}
	// pending list, then open it
	m.databasePendingSources()
	if !m.pickerOpen || m.pickerKind != pickerKindDBSourcePending {
		t.Fatalf("no pending picker: %q", covToastText(m))
	}
	cdbPick(t, m, "")
	m.pickerKind, m.pickerOpen = pickerKindDBSourcePending, true
	cdbPick(t, m, file)

	cmds := cdbMsgs(m.databaseSubmitSourceStart())
	if len(cmds) != 1 {
		t.Fatalf("submit start: %v / %q", cmds, covToastText(m))
	}
	checked := cmds[0].(dbSourceCheckedMsg)
	m.onDBSourceChecked(checked)
	if !m.confirmOpen || m.confirmKind != confirmKindDBSourceSubmit {
		t.Fatal("no submit card")
	}
	// copy, cancel
	cdbConfirm(t, m, "copy")
	m.onDBSourceChecked(checked)
	cdbConfirm(t, m, "cancel")
	// the server changed meanwhile + a failed re-read
	changed := checked
	changed.storedNow = "CREATE VIEW v1 AS SELECT 99"
	m.onDBSourceChecked(changed)
	failed := checked
	failed.err = errors.New("lost")
	m.databases.manual = map[string]bool{"pg": true}
	m.onDBSourceChecked(failed)
	m.databases.manual = nil
	m.databases.conns[0].Environment = "production"
	m.onDBSourceChecked(checked)
	m.databases.conns[0].Environment = ""
	// submit
	m.confirmOpen = false
	m.databases.sources.submit = &dbSourceSubmit{file: file, plan: checked.plan}
	m.confirmKind = confirmKindDBSourceSubmit
	m.confirmOpen = true
	cmd, ok := m.databaseSourceConfirm("submit")
	if !ok {
		t.Fatal("not routed")
	}
	stored := cdbMsgs(cmd)
	if len(stored) != 1 || stored[0].(dbSourceStoredMsg).err != nil {
		t.Fatalf("stored %v", stored)
	}
	m.onDBSourceStored(stored[0].(dbSourceStoredMsg))
	if !strings.Contains(covToastText(m), "Stored view") {
		t.Fatalf("toast %q", covToastText(m))
	}
	if !strings.Contains(sess.ran(), "VIEW") {
		t.Fatalf("ran %q", sess.ran())
	}
	// store errors
	m.onDBSourceStored(dbSourceStoredMsg{file: file, err: errors.New("denied")})
	if !strings.Contains(covToastText(m), "Could not store") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.onDBSourceStored(dbSourceStoredMsg{file: "/nope"})
	m.onDBSourceChecked(dbSourceCheckedMsg{file: "/nope"})
	// no changes now (the buffer equals what is stored)
	if err := m.nvim.ExecLuaArgs(`vim.cmd('edit! ' .. vim.fn.fnameescape(...))`, file); err != nil {
		t.Fatal(err)
	}
	m.databaseSubmitSourceStart()
	_ = covToastText(m)
}

func TestCovDBSourceSubmitGuards(t *testing.T) {
	m, _ := cdbSourceModel(t, true)
	s := m.databases
	m.databaseSubmitSourceStart() // a normal buffer
	if !strings.Contains(covToastText(m), "Not an object source buffer") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// open one by hand
	file := t.TempDir() + "/v.sql"
	if err := os.WriteFile(file, []byte("CREATE VIEW v1 AS SELECT 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := s.sourceState()
	st.edits[file] = &dbSourceOpen{edit: dbclient.NewSourceEdit("pg", dbclient.ObjectView, "v1", "CREATE VIEW v1 AS SELECT 1\n"), driver: "postgres", sessKey: "pg"}
	m.openFileAtLine(file, 1)
	m.databaseSubmitSourceStart()
	if !strings.Contains(covToastText(m), "No changes") {
		t.Fatalf("toast %q", covToastText(m))
	}
	_ = m.nvim.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {'CREATE VIEW v1 AS SELECT 2'})`)
	m.databaseSubmitSourceStart()
	if !strings.Contains(covToastText(m), "read-only") {
		t.Fatalf("toast %q", covToastText(m))
	}
	s.conns[0].ReadOnly = false
	st.edits[file].driver = "oracle"
	m.databaseSubmitSourceStart()
	if !strings.Contains(covToastText(m), "Cannot submit") {
		t.Fatalf("toast %q", covToastText(m))
	}
	st.edits[file].driver = "postgres"
	st.edits[file].sessKey = "gone"
	m.databaseSubmitSourceStart()
	if !strings.Contains(covToastText(m), "Not connected") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// the card routes
	m.confirmKind = 0
	if _, ok := m.databaseSourceConfirm("x"); ok {
		t.Fatal("foreign card")
	}
	m.confirmKind = confirmKindDBSourceSubmit
	if cmd, ok := m.databaseSourceConfirm("submit"); !ok || cmd != nil { // no submit pending
		t.Fatal("no pending")
	}
	st.submit = &dbSourceSubmit{file: "/nope"}
	if cmd, ok := m.databaseSourceConfirm("submit"); !ok || cmd != nil {
		t.Fatal("unknown file")
	}
	st.submit = &dbSourceSubmit{file: file}
	m.databaseSourceConfirm("submit") // not connected
	st.submit = &dbSourceSubmit{file: file}
	if cmd, ok := m.databaseSourceConfirm("other"); !ok || cmd != nil {
		t.Fatal("other button")
	}
	if _, ok := m.databaseSourcePicker("x"); ok {
		t.Fatal("foreign picker")
	}
	saved := m.databases
	m.databases = nil
	if cmd, ok := m.databaseSourceConfirm("submit"); !ok || cmd != nil {
		t.Fatal("no databases")
	}
	m.databasePendingSources()
	m.databaseSubmitSourceStart()
	m.onDBSourceChecked(dbSourceCheckedMsg{})
	m.onDBSourceStored(dbSourceStoredMsg{})
	m.onDBSourceLoaded(dbSourceLoadedMsg{})
	m.databases = saved
}

func TestCovDBSourceSubmitRunsInATransaction(t *testing.T) {
	m, sess := cdbSourceModel(t, false)
	s := m.databases
	file := t.TempDir() + "/v.sql"
	_ = os.WriteFile(file, []byte("x"), 0o600)
	st := s.sourceState()
	st.edits[file] = &dbSourceOpen{edit: dbclient.NewSourceEdit("pg", dbclient.ObjectView, "v1", "x"), driver: "postgres", sessKey: "pg"}
	plan := dbclient.SourcePlan{Statements: []string{"DROP VIEW v1", "CREATE VIEW v1 AS SELECT 1"}, Transactional: true}
	st.submit = &dbSourceSubmit{file: file, plan: plan}
	m.confirmKind = confirmKindDBSourceSubmit
	cmd, _ := m.databaseSourceConfirm("submit")
	if got := cdbMsgs(cmd); len(got) != 1 || got[0].(dbSourceStoredMsg).err != nil {
		t.Fatalf("%v", got)
	}
	if !strings.Contains(sess.ran(), "CREATE VIEW") {
		t.Fatal("not run")
	}
	// the second statement fails: rolled back
	sess.run = func(q string) dbclient.Result {
		if strings.HasPrefix(q, "CREATE") {
			return dbclient.Result{Err: errors.New("syntax")}
		}
		return dbclient.Result{}
	}
	st.submit = &dbSourceSubmit{file: file, plan: plan}
	cmd, _ = m.databaseSourceConfirm("submit")
	if got := cdbMsgs(cmd); len(got) != 1 || got[0].(dbSourceStoredMsg).err == nil {
		t.Fatalf("%v", got)
	}
}

func TestCovDBSourceMySQLFunctionFallsBackToAProcedure(t *testing.T) {
	m := cdbModel(t, dbclient.Conn{ID: "my", Driver: "mysql", Host: "h", Port: 3306})
	s := m.databases
	cdbAddNode(s, "my", "my/db/shop", dbNodeDatabase, "shop")
	cdbAddNode(s, "my/db/shop", "my/db/shop/routine/p1", dbNodeRoutine, "p1")
	s.databaseSelectPath("my/db/shop/routine/p1")
	s.sessions["my"] = &cdbSession{run: func(q string) dbclient.Result {
		if strings.Contains(strings.ToUpper(q), "FUNCTION") {
			return dbclient.Result{Err: errors.New("no such function")}
		}
		return dbclient.Result{Rows: [][]dbclient.Cell{{{Text: "n"}, {Text: "n"}, {Text: "CREATE PROCEDURE p1() SELECT 1"}, {Text: "CREATE PROCEDURE p1() SELECT 1"}}}}
	}}
	msgs := cdbMsgs(m.databaseEditSourceStart())
	if len(msgs) != 1 {
		t.Fatalf("msgs %v", msgs)
	}
	got := msgs[0].(dbSourceLoadedMsg)
	if got.err != nil || got.kind != dbclient.ObjectProcedure {
		t.Fatalf("%+v", got)
	}
	// no editor: the source is copied
	var copied string
	t.Cleanup(SetClipboardSink(func(s string) { copied = s }))
	m.onDBSourceLoaded(got)
	if !strings.Contains(copied, "CREATE PROCEDURE") {
		t.Fatalf("copied %q", copied)
	}
	// a failed read says so
	m.onDBSourceLoaded(dbSourceLoadedMsg{err: errors.New("denied")})
	if !strings.Contains(covToastText(m), "Could not read the source") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// an unwritable config folder
	cdbBreakConfigDir(t)
	m.onDBSourceLoaded(got)
	if !strings.Contains(covToastText(m), "Could not open the source") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// nothing pending
	m.databases.sources = nil
	m.databasePendingSources()
	if !strings.Contains(covToastText(m), "No pending source changes") {
		t.Fatalf("toast %q", covToastText(m))
	}
	if cmd := m.databaseEditSourceStart(); cmd == nil {
		t.Fatal("a toast")
	}
	m.databases.conns = nil
	if cmd := m.databaseEditSourceStart(); cmd != nil {
		t.Log("row of a connection that is gone still shows")
	}
}

func TestCovDBSourceTransactionErrors(t *testing.T) {
	m, sess := cdbSourceModel(t, false)
	file := t.TempDir() + "/v.sql"
	st := m.databases.sourceState()
	st.edits[file] = &dbSourceOpen{edit: dbclient.NewSourceEdit("pg", dbclient.ObjectView, "v1", "x"), driver: "postgres", sessKey: "pg"}
	plan := dbclient.SourcePlan{Statements: []string{"A", "B"}, Transactional: true}
	m.confirmKind = confirmKindDBSourceSubmit
	for _, tc := range []struct{ begin, commit error }{{errors.New("b"), nil}, {nil, errors.New("c")}} {
		sess.beginErr, sess.commitErr = tc.begin, tc.commit
		st.submit = &dbSourceSubmit{file: file, plan: plan}
		cmd, _ := m.databaseSourceConfirm("submit")
		if got := cdbMsgs(cmd); len(got) != 1 || got[0].(dbSourceStoredMsg).err == nil {
			t.Fatalf("%v", got)
		}
	}
}
