package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-28: your own connection has a Connection Options… action; a
// project connection does not (its file belongs to the project).
func TestDBManageHasOptions(t *testing.T) {
	has := func(user bool) bool {
		for _, it := range dbManageItems(user) {
			if it.ID == "options" {
				return true
			}
		}
		return false
	}
	if !has(true) || has(false) {
		t.Fatalf("options in user menu %v, in project menu %v", has(true), has(false))
	}
}

// DBX-28: the options picker lists every field with its value, and a
// value typed in the prompt is saved to your connections file.
func TestDBConnOptionsFormSaves(t *testing.T) {
	withConfigDir(t)
	m := &Model{w: 120, h: 40, databases: newDBState()}
	c := dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432, Source: dbclient.SourceUser}
	m.databases.conns = []dbclient.Conn{c}
	m.databases.userConns = []dbclient.Conn{c}
	m.dbManage().id = "pg"
	m.databaseManagePick("options")
	if !m.pickerOpen || m.pickerKind != pickerKindDBOptions {
		t.Fatalf("options picker not open: %v %v", m.pickerOpen, m.pickerKind)
	}
	if n := len(dbOptionItems(dbclient.ConnOptions{})); n != len(dbclient.ConnOptionFields) {
		t.Fatalf("items %d", n)
	}
	m.databaseManagePicker("txMode")
	if !m.promptOpen || m.promptKind != promptKindDBOptionField {
		t.Fatal("field prompt not open")
	}
	m.databaseManagePrompt(promptKindDBOptionField, "manual")
	got := m.databases.userConns[0].Options
	if got == nil || got.TxMode != "manual" {
		t.Fatalf("txMode not saved: %+v", got)
	}
	m.databaseManagePicker("isolation")
	m.databaseManagePrompt(promptKindDBOptionField, "chaos")
	if !m.promptOpen || m.databases.userConns[0].Options.Isolation != "" {
		t.Fatal("a bad isolation level closed the prompt or was saved")
	}
}
