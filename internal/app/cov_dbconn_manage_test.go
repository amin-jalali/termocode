package app

import (
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func cdbUserConns() []dbclient.Conn {
	return []dbclient.Conn{
		{ID: "pg", Driver: "postgres", Host: "db.local", Port: 5432, User: "app", Database: "shop"},
		{ID: "lite", Driver: "sqlite", Path: "/tmp/x.db"},
		{ID: "ssl", Driver: "postgres", Host: "h", Port: 5432, SSL: &dbclient.SSLConfig{Mode: "require"}},
		{ID: "tun", Driver: "postgres", Host: "h", Port: 5432, SSH: &dbclient.SSHConfig{Host: "bastion", User: "me", Port: 22}},
	}
}

func cdbManageModel(t *testing.T, id string) *Model {
	t.Helper()
	m := cdbModel(t, cdbUserConns()...)
	if !m.databases.databaseSelectPath(id) {
		t.Fatalf("no row %s", id)
	}
	m.databaseManageStart()
	if !m.pickerOpen || m.pickerKind != pickerKindDBManage {
		t.Fatal("manage menu not open")
	}
	m.pickerOpen = false
	return m
}

func TestCovDBManageStartNeedsAConnection(t *testing.T) {
	m := cdbModel(t)
	m.databaseManageStart()
	if !strings.Contains(covToastText(m), "Select a connection first") {
		t.Fatalf("toast: %q", covToastText(m))
	}
	m.databases = nil
	m.databaseManageStart()
	// a row whose connection is gone
	m = cdbModel(t, cdbUserConns()...)
	m.databases.databaseSelectPath("pg")
	m.databases.conns = nil
	m.databaseManageStart()
	if m.pickerOpen {
		t.Fatal("picker for a missing connection")
	}
	if _, ok := m.dbManaged(); ok {
		t.Fatal("managed a missing connection")
	}
	m.databases.manage = &dbManageState{id: "pg"}
	if _, ok := m.dbManaged(); ok {
		t.Fatal("conn is gone")
	}
	if cmd := m.databaseManagePick("edit"); cmd != nil {
		t.Fatal("pick on a missing connection")
	}
	if cmd, ok := m.databaseManagePicker("x"); ok && cmd != nil {
		t.Fatal("unrelated picker")
	}
}

func TestCovDBManageEditFlow(t *testing.T) {
	m := cdbManageModel(t, "pg")
	m.databaseManagePick("edit")
	if !m.promptOpen || m.promptKind != promptKindDBEditField {
		t.Fatal("no edit prompt")
	}
	for _, v := range []string{"Shop DB", "db2.local", "nope", "6543", "bob", "orders"} {
		cdbSubmit(t, m, v)
		if v == "nope" {
			if !m.promptOpen {
				t.Fatal("bad port must re-ask")
			}
		}
	}
	if m.promptOpen {
		t.Fatal("prompt left open")
	}
	c, _ := m.databases.connection("pg")
	if c.Name != "Shop DB" || c.Host != "db2.local" || c.Port != 6543 || c.User != "bob" || c.Database != "orders" {
		t.Fatalf("edit not saved: %+v", c)
	}
	// an open session says to reconnect
	m.databases.sessions["pg"] = &cdbSession{}
	ms := m.dbManage()
	ms.edit = dbclient.ConnEdit{Name: "n", Host: "h", Port: 1, User: "u", Database: "d"}
	m.dbApplyEdit()
	if !strings.Contains(covToastText(m), "Connect again") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// out of range port
	m.databaseManagePick("edit")
	cdbSubmit(t, m, "n")
	cdbSubmit(t, m, "h")
	cdbSubmit(t, m, "70000")
	if !m.promptOpen {
		t.Fatal("port 70000 accepted")
	}
	m.promptOpen = false
	// bad edit: an empty host is refused by the store or accepted; either way no panic
	m.databases.manage.id = "nope"
	m.dbApplyEdit()
	if !strings.Contains(covToastText(m), "Cannot save") {
		t.Fatalf("toast %q", covToastText(m))
	}
}

func TestCovDBManageEditSQLiteHasOneStep(t *testing.T) {
	m := cdbManageModel(t, "lite")
	m.databaseManagePick("edit")
	if m.dbEditCount() != 1 {
		t.Fatal("sqlite edit steps")
	}
	cdbSubmit(t, m, "Local file")
	c, _ := m.databases.connection("lite")
	if c.Name != "Local file" || m.promptOpen {
		t.Fatalf("%+v", c)
	}
}

func TestCovDBManageCommentRenameDuplicateCopy(t *testing.T) {
	m := cdbManageModel(t, "pg")
	var copied string
	t.Cleanup(SetClipboardSink(func(s string) { copied = s }))
	m.databaseManagePick("comment")
	cdbSubmit(t, m, "the shop")
	c, _ := m.databases.connection("pg")
	if c.Comment != "the shop" {
		t.Fatalf("comment %q", c.Comment)
	}
	if !strings.Contains(m.databases.nodes["pg"].detail, "the shop") {
		t.Fatal("comment not on the row")
	}

	m.databaseManagePick("copy-json")
	if !strings.Contains(copied, `"pg"`) || strings.Contains(copied, "password") {
		t.Fatalf("copied %q", copied)
	}

	m.databaseManagePick("duplicate")
	if len(m.databases.userConns) != len(cdbUserConns())+1 {
		t.Fatalf("duplicate: %d conns", len(m.databases.userConns))
	}

	m.databases.manage.id = "pg"
	m.databaseManagePick("rename")
	cdbSubmit(t, m, "pg2")
	if _, ok := m.databases.connection("pg2"); !ok {
		t.Fatal("not renamed")
	}
	// same id, empty id, taken id
	if cmd := m.databaseRenameConn("pg2"); cmd != nil {
		t.Fatal("same id")
	}
	if cmd := m.databaseRenameConn("  "); cmd != nil {
		t.Fatal("empty id")
	}
	m.databaseRenameConn("lite")
	if !strings.Contains(covToastText(m), "Cannot rename") {
		t.Fatalf("toast %q", covToastText(m))
	}
}

func TestCovDBManageProjectConnectionDuplicates(t *testing.T) {
	m := cdbModel(t, cdbUserConns()...)
	m.databases.conns = append(m.databases.conns, dbclient.Conn{ID: "proj", Driver: "postgres", Host: "p", Port: 5432, Source: dbclient.SourceProject})
	m.databases.manage = &dbManageState{id: "proj"}
	m.databaseManagePick("duplicate")
	if len(m.databases.userConns) != len(cdbUserConns())+1 {
		t.Fatalf("user conns %d", len(m.databases.userConns))
	}
	if cmd := m.databaseDeleteConn(); cmd != nil {
		t.Fatal("a project connection cannot be deleted")
	}
}

func TestCovDBManageCopySSLAndSSHProfiles(t *testing.T) {
	m := cdbManageModel(t, "pg")
	m.databaseManagePick("copy-ssl")
	if !m.pickerOpen || m.pickerKind != pickerKindDBCopySSL {
		t.Fatal("no ssl picker")
	}
	cdbPick(t, m, "missing")
	cdbPick2(t, m, "pg", "copy-ssl", "ssl")
	c, _ := m.databases.connection("pg")
	if c.SSL == nil || c.SSL.Mode != "require" {
		t.Fatalf("ssl not copied: %+v", c.SSL)
	}
	// no other connection has ssl
	m2 := cdbModel(t, dbclient.Conn{ID: "a", Driver: "postgres", Host: "h"})
	m2.databases.manage = &dbManageState{id: "a"}
	m2.databaseManagePick("copy-ssl")
	if m2.pickerOpen || !strings.Contains(covToastText(m2), "No other connection") {
		t.Fatal("expected a toast")
	}

	// ssh profiles: none yet
	m = cdbManageModel(t, "tun")
	m.databaseManagePick("ssh-profile")
	if !strings.Contains(covToastText(m), "No SSH profiles") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// save one, then use it
	m.databaseManagePick("save-ssh-profile")
	cdbSubmit(t, m, "bastion-profile")
	m.databaseManagePick("save-ssh-profile")
	cdbSubmit(t, m, "  ")
	ps, err := dbLoadSSHProfiles()
	if err != nil || len(ps) != 1 {
		t.Fatalf("profiles %v %v", ps, err)
	}
	m.databaseManagePick("ssh-profile")
	if !m.pickerOpen || m.pickerKind != pickerKindDBSSHProfile {
		t.Fatal("no profile picker")
	}
	cdbPick(t, m, "x")
	cdbPick2(t, m, "tun", "ssh-profile", "7")
	cdbPick2(t, m, "tun", "ssh-profile", "0")
	// no ssh on a plain connection
	m = cdbManageModel(t, "pg")
	m.databaseManagePick("save-ssh-profile")
	if !strings.Contains(covToastText(m), "no SSH tunnel") {
		t.Fatalf("toast %q", covToastText(m))
	}
}

// cdbPick2 opens the menu entry on the managed connection and picks id.
func cdbPick2(t *testing.T, m *Model, conn, action, id string) {
	t.Helper()
	m.databases.manage = &dbManageState{id: conn}
	m.databaseManagePick(action)
	if !m.pickerOpen {
		t.Fatalf("%s opened no picker", action)
	}
	cdbPick(t, m, id)
}

func TestCovDBManageTemplates(t *testing.T) {
	m := cdbManageModel(t, "pg")
	m.databaseManagePick("save-template")
	cdbSubmit(t, m, "My PG")
	cdbSubmit0 := func() {
		m.databaseManagePick("save-template")
		cdbSubmit(t, m, "My PG") // replaces the first
	}
	cdbSubmit0()
	m.databaseManagePick("save-template")
	cdbSubmit(t, m, "")
	ts, err := dbLoadTemplates()
	if err != nil || len(ts) != 1 {
		t.Fatalf("templates %v %v", ts, err)
	}
	m.databaseTemplateStart()
	if !m.pickerOpen || m.pickerKind != pickerKindDBTemplate {
		t.Fatal("no template picker")
	}
	n := len(m.databases.manage.tpls)
	if n < 2 {
		t.Fatalf("templates %d", n)
	}
	m.pickerOpen = false
	if cmd := m.databaseTemplatePick("zz"); cmd != nil {
		t.Fatal("bad id")
	}
	if cmd := m.databaseTemplatePick("99"); cmd != nil {
		t.Fatal("out of range")
	}
	m.databaseTemplatePick("0")
	if !m.promptOpen || m.promptKind != promptKindDBPassword {
		t.Fatalf("no password prompt (kind %v)", m.promptKind)
	}
}

func TestCovDBManageVersion(t *testing.T) {
	m := cdbManageModel(t, "pg")
	c, _ := m.dbManaged()
	m.databaseVersion(c) // not connected
	if !strings.Contains(covToastText(m), "Connect first") {
		t.Fatalf("toast %q", covToastText(m))
	}
	rc := dbclient.Conn{ID: "r", Driver: "redis"}
	m.databaseVersion(rc)
	if !strings.Contains(covToastText(m), "Show Server Info") {
		t.Fatalf("toast %q", covToastText(m))
	}
	sess := &cdbSession{run: func(string) dbclient.Result {
		return dbclient.Result{Rows: [][]dbclient.Cell{{{Text: "PostgreSQL 16"}}}}
	}}
	m.databases.sessions["pg"] = sess
	msgs := cdbMsgs(m.databaseVersion(c))
	if len(msgs) != 1 {
		t.Fatalf("msgs %v", msgs)
	}
	vm := msgs[0].(dbVersionMsg)
	if vm.version != "PostgreSQL 16" || vm.err != nil {
		t.Fatalf("%+v", vm)
	}
	m.onDBVersion(vm)
	if !strings.Contains(covToastText(m), "PostgreSQL 16") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// errors: read-only session fails, run fails
	sess.roErr = os.ErrPermission
	vm = cdbMsgs(m.databaseVersion(c))[0].(dbVersionMsg)
	if vm.err == nil {
		t.Fatal("expected ro error")
	}
	m.onDBVersion(vm)
	if !strings.Contains(covToastText(m), "Could not read the server version") {
		t.Fatalf("toast %q", covToastText(m))
	}
	sess.roErr = nil
	sess.run = func(string) dbclient.Result { return dbclient.Result{Err: os.ErrClosed} }
	if vm = cdbMsgs(m.databaseVersion(c))[0].(dbVersionMsg); vm.err == nil {
		t.Fatal("expected run error")
	}
	sess.run = func(string) dbclient.Result { return dbclient.Result{} }
	if vm = cdbMsgs(m.databaseVersion(c))[0].(dbVersionMsg); vm.version != "" || vm.err != nil {
		t.Fatalf("%+v", vm)
	}
}

func TestCovDBManageDelete(t *testing.T) {
	m := cdbManageModel(t, "pg")
	m.databaseManagePick("delete")
	if !m.confirmOpen || m.confirmKind != confirmKindDBDeleteConn {
		t.Fatal("no delete card")
	}
	cdbConfirm(t, m, "cancel")
	if _, ok := m.databases.connection("pg"); !ok {
		t.Fatal("cancel deleted")
	}
	if cmd, ok := m.databaseManageConfirm("x"); !ok || cmd != nil {
		t.Fatal("a wrong button must do nothing")
	}
	m.confirmKind = 0
	if cmd, ok := m.databaseManageConfirm("x"); ok || cmd != nil {
		t.Fatal("foreign card")
	}
	closed := &cdbSession{}
	m.databases.sessions["pg"] = closed
	m.databases.sessions["pg/db"] = closed
	m.databases.nodes["pg/db"] = dbNode{kind: dbNodeDatabase, name: "db", path: "pg/db", conn: "pg"}
	m.databaseManagePick("delete")
	cdbConfirm(t, m, "delete") 
	if _, ok := m.databases.connection("pg"); ok {
		t.Fatal("not deleted")
	}
	if len(m.databases.sessions) != 0 {
		t.Fatal("sessions kept")
	}
	// unknown id fails
	m.databases.manage = &dbManageState{id: "lite"}
	m.databases.userConns = nil
	m.databaseDeleteConn()
	if !strings.Contains(covToastText(m), "Cannot delete") {
		t.Fatalf("toast %q", covToastText(m))
	}
}

func TestCovDBManageUserFiles(t *testing.T) {
	m := cdbModel(t)
	_ = m
	if b, err := dbReadUserJSON("none.json"); b != nil || err != nil {
		t.Fatalf("%v %v", b, err)
	}
	if err := dbWriteUserJSON("t.json", func() ([]byte, error) { return nil, os.ErrInvalid }); err == nil {
		t.Fatal("data error swallowed")
	}
	p, _ := dbUserFile("bad.json")
	_ = os.MkdirAll(p[:strings.LastIndex(p, "/")], 0o700)
	_ = os.WriteFile(p, []byte("{nope"), 0o600)
	if _, err := dbReadUserJSON("bad.json"); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(strings.Replace(p, "bad.json", "ssh_profiles.json", 1), []byte("{nope"), 0o600)
	_ = os.WriteFile(strings.Replace(p, "bad.json", "templates.json", 1), []byte("{nope"), 0o600)
	if _, err := dbLoadSSHProfiles(); err == nil {
		t.Fatal("bad profiles accepted")
	}
	if _, err := dbLoadTemplates(); err == nil {
		t.Fatal("bad templates accepted")
	}
	m.databaseTemplateStart()
	if !strings.Contains(covToastText(m), "Cannot read your templates") {
		t.Fatalf("toast %q", covToastText(m))
	}
}

func TestCovDBManagePasteAndRouting(t *testing.T) {
	m := cdbModel(t)
	m.databasePasteConn() // headless: no clipboard text
	if !strings.Contains(covToastText(m), "clipboard has no connection") {
		t.Fatalf("toast %q", covToastText(m))
	}
	if _, ok := m.databaseManagePrompt(promptKindDBOptionField+9999, "x"); ok {
		t.Fatal("foreign prompt")
	}
	// a managed connection that vanished: prompts are swallowed
	if cmd, ok := m.databaseManagePrompt(promptKindDBComment, "x"); !ok || cmd != nil {
		t.Fatal("prompt without a managed connection")
	}
}

// cdbBreakConfigDir makes the db config folder a file: every write below
// it fails.
func cdbBreakConfigDir(t *testing.T) {
	t.Helper()
	p, err := dbUserFile("x.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := strings.TrimSuffix(p, "/x.json")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(strings.TrimSuffix(dir, "/db"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCovDBManageFailuresOnAnUnwritableConfig(t *testing.T) {
	m := cdbManageModel(t, "tun")
	cdbBreakConfigDir(t)
	for _, act := range []string{"duplicate", "comment"} {
		m.databaseManagePick(act)
		if act == "comment" {
			cdbSubmit(t, m, "x")
		}
		if !strings.Contains(covToastText(m), "Cannot") {
			t.Fatalf("%s: toast %q", act, covToastText(m))
		}
	}
	m.databaseManagePick("save-ssh-profile")
	cdbSubmit(t, m, "p")
	m.databaseManagePick("save-template")
	cdbSubmit(t, m, "t")
	m.databaseRenameConn("renamed")
	m.databases.manage.id = "tun"
	m.databaseDeleteConn()
	m.databaseManagePick("duplicate")
	if err := dbWriteUserJSON("a.json", func() ([]byte, error) { return []byte("x"), nil }); err == nil {
		t.Fatal("write below a file")
	}
	if _, err := dbReadUserJSON("a.json"); err == nil {
		t.Fatal("read below a file")
	}
	// the profiles / templates file is unreadable
	m.databases.manage.id = "tun"
	m.databaseManagePick("ssh-profile")
	m.databaseTemplateStart()
}

func TestCovDBManageNoConfigDir(t *testing.T) {
	m := cdbManageModel(t, "pg")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := dbUserFile("x"); err == nil {
		t.Skip("a home dir exists")
	}
	if err := dbWriteUserJSON("x", func() ([]byte, error) { return nil, nil }); err == nil {
		t.Fatal("no dir: write")
	}
	if _, err := dbReadUserJSON("x"); err == nil {
		t.Fatal("no dir: read")
	}
	if err := m.dbSaveUserConns(nil); err == nil {
		t.Fatal("no dir: save")
	}
}

func TestCovDBManageMenusAndRouting(t *testing.T) {
	if got := len(dbManageItems(false)); got != 4 {
		t.Fatalf("project menu %d", got)
	}
	m := cdbManageModel(t, "pg")
	// project connection: duplicate runs the template path; "version" goes on
	m.databases.conns = append(m.databases.conns, dbclient.Conn{ID: "proj", Driver: "postgres", Host: "p", Port: 5432, Source: dbclient.SourceProject})
	m.databases.manage = &dbManageState{id: "proj"}
	m.databaseManagePick("version")
	m.databaseManagePick("unknown")
	// the router
	m.databases.manage = &dbManageState{id: "pg"}
	for _, k := range []pickerKindEnum{pickerKindDBManage, pickerKindDBTemplate, pickerKindDBOptions} {
		m.pickerKind = k
		if _, ok := m.databaseManagePicker("nothing"); !ok {
			t.Fatalf("kind %v not routed", k)
		}
	}
	m.databases.manage = nil
	m.pickerKind = pickerKindDBCopySSL
	if cmd, ok := m.databaseManagePicker("x"); !ok || cmd != nil {
		t.Fatal("ssl picker without a managed connection")
	}
	m.databases.manage = &dbManageState{id: "pg"}
	if cmd, ok := m.databaseManagePicker("x"); !ok || cmd != nil {
		t.Fatal("unknown source")
	}
	// the option prompt
	if _, ok := m.databaseManagePrompt(promptKindDBOptionField, "x"); !ok {
		t.Fatal("option prompt not routed")
	}
}

func TestCovDBManageRenameMovesTheSavedPassword(t *testing.T) {
	m := cdbModel(t, dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432, Password: "${secret:db.pg}"})
	store := databaseSecretStore()
	if store == nil || store.Locked() {
		t.Skip("no secret store")
	}
	if err := store.Set("db.pg", "hunter2"); err != nil {
		t.Fatal(err)
	}
	m.databases.manage = &dbManageState{id: "pg"}
	m.databaseRenameConn("pg2")
	if v, ok, _ := store.Get("db.pg2"); !ok || v != "hunter2" {
		t.Fatalf("password not moved: %q %v", v, ok)
	}
	if _, ok, _ := store.Get("db.pg"); ok {
		t.Fatal("old secret kept")
	}
	c, _ := m.databases.connection("pg2")
	if c.Password != "${secret:db.pg2}" {
		t.Fatalf("ref %q", c.Password)
	}
	// delete drops its secret
	m.databaseDeleteConn()
	if _, ok, _ := store.Get("db.pg2"); ok {
		t.Fatal("secret kept after delete")
	}
}

func TestCovDBManageTemplateReplacesItsNamesake(t *testing.T) {
	m := cdbManageModel(t, "pg")
	for _, name := range []string{"one", "two", "one"} {
		m.databaseManagePick("save-template")
		cdbSubmit(t, m, name)
	}
	ts, _ := dbLoadTemplates()
	if len(ts) != 2 {
		t.Fatalf("templates %d", len(ts))
	}
}

func TestCovDBManageOptionsAndMisc(t *testing.T) {
	m := cdbManageModel(t, "pg")
	m.databaseManagePick("options")
	if !m.pickerOpen || m.pickerKind != pickerKindDBOptions {
		t.Fatal("no options picker")
	}
	// a managed connection without an id: the pickers say nothing
	m.databases.manage = &dbManageState{id: "lite"}
	m.databaseManagePick("copy-json")
	// an invalid connection cannot be pasted / duplicated onto a taken id
	m.databaseTemplateStart()
	m.pickerOpen = false
	m.databases.conns = append(m.databases.conns, dbclient.Conn{ID: "local-postgres", Driver: "postgres"})
	m.databaseTemplatePick("0")
}
