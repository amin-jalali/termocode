package app_test

import (
	"strings"
	"testing"
	"time"
)

// * on a cell of the table data shows only the rows with the same value
// in that column, on the server; with a filter already set it adds to it.
func TestDBTableQuickFilterByCellValue(t *testing.T) {
	d, s := tableDriver(t, 100, 30)
	d.Key("o")
	dbWaitPage(t, d, "user-000@example.com")
	d.Key("*")
	want := "SELECT * FROM `app`.`users` WHERE (`id` = 0) ORDER BY `id` LIMIT 101 OFFSET 0"
	if !d.WaitFor(func() bool { return s.last() == want }, 2*time.Second) {
		t.Fatalf("quick filter SQL = %s", s.last())
	}
	d.Key("f")
	for i := 0; i < 20; i++ {
		d.Key("backspace")
	}
	d.Type("plan = 'pro'")
	d.Key("enter")
	if !d.WaitFor(func() bool { return strings.Contains(s.last(), "WHERE (plan = 'pro')") }, 2*time.Second) {
		t.Fatalf("filter SQL = %s", s.last())
	}
	dbWaitPage(t, d, "user-000@example.com")
	d.Key("*")
	want = "SELECT * FROM `app`.`users` WHERE ((plan = 'pro') AND `id` = 0) ORDER BY `id` LIMIT 101 OFFSET 0"
	if !d.WaitFor(func() bool { return s.last() == want }, 2*time.Second) {
		t.Fatalf("added quick filter SQL = %s", s.last())
	}
}
