package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
	tea "github.com/charmbracelet/bubbletea"
)

// databaseResultDemo fills the SQL Result page with a canned result for the
// shotkit goldens (dbdemo result / messages / error).
func (m *Model) databaseResultDemo(variant string) tea.Cmd {
	m.databasesDemo("")
	names := []string{"ada", "grace", "linus", "mary", "ken", "ravi", "sue", "olu", "ying", "ben", "zoe", "ivan", "nia", "tom", "amy", "raj", "eve", "max", "lee", "kim", "dan", "ana", "sam", "joy", "uma", "ali", "bea", "cy", "dee", "fay"}
	plans := []string{"free", "pro", "team"}
	res := dbclient.Result{
		Columns: []dbclient.Column{{Name: "id"}, {Name: "email"}, {Name: "plan"}, {Name: "created_at"}},
		Elapsed: 118 * time.Millisecond,
	}
	for i, n := range names {
		plan := dbclient.Cell{Text: plans[i%3]}
		email := dbclient.Cell{Text: n + "@example.com"}
		if i == 2 {
			email = dbclient.Cell{Null: true}
		}
		res.Rows = append(res.Rows, []dbclient.Cell{
			{Text: fmt.Sprint(48213 - i), Kind: "number"}, email, plan,
			{Text: fmt.Sprintf("2026-10-18 09:%02d:%02d+00", 59-i, (i*37)%60)},
		})
	}
	r := &dbRun{conn: "local-pg", sql: "SELECT id, email, plan, created_at\nFROM users\nORDER BY id DESC;", by: "user", line: 3, result: res}
	s := m.dbConsole()
	s.shown, s.cache, s.size, s.page, s.sortCol, s.filter = r, nil, 100, 0, -1, ""
	s.view = dbViewGrid
	switch variant {
	case "messages":
		r.sql = "UPDATE users SET plan = 'pro' WHERE id = 48212;"
		r.result = dbclient.Result{Affected: 1, Elapsed: 9 * time.Millisecond, Notices: []string{"NOTICE: trigger users_audit wrote 1 row"}}
		s.view = dbViewMessages
	case "error":
		r.sql = "SELECT * FROM userz;"
		r.result = dbclient.Result{Err: errors.New(`ERROR: relation "userz" does not exist (SQLSTATE 42P01)`), Elapsed: 3 * time.Millisecond}
		s.view = dbViewMessages
	}
	return m.databaseShowPage()
}
