package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

// Bulk: one card for every marked container (the list is capped), the
// verbs run on the fake docker, a failure is listed.
func TestCovContainerBulkRuns(t *testing.T) {
	m, f := ctrModel(t)
	st := m.services
	for _, id := range []string{"c1", "c3"} {
		cursorOnContainer(t, m, id)
		_ = ctrKey(m, "v")
	}
	if _, ok := m.containerBulkKey("V"); !ok || len(st.markedContainers()) != 0 {
		t.Fatal("V did not clear")
	}
	if _, ok := m.containerBulkKey("V"); ok {
		t.Fatal("V with no marks")
	}
	m.services.cursor = 0
	if _, ok := m.containerBulkKey("v"); ok {
		t.Fatal("v off a container row")
	}
	for _, tc := range []struct{ key, verb, call string }{{"s", "start", "start c1"}, {"x", "stop", "stop c1"}, {"r", "restart", "restart c1"}, {"D", "remove", "rm c1"}} {
		st.containerMarks = map[string]bool{"c1": true, "c3": true}
		if _, ok := m.containerBulkKey(tc.key); !ok || !m.confirmOpen {
			t.Fatalf("%s: no card", tc.key)
		}
		done := ctrRun[containerBulkDoneMsg](t, m.runContainerBulk(tc.verb))
		if len(done.failed) != 0 || f.Count(tc.call) != 1 {
			t.Fatalf("%s: %+v %v", tc.verb, done, f.Calls())
		}
	}
	m.askContainerBulk("start", []containers.Container{{ID: "x", Names: []string{"one"}}})
	if !strings.Contains(m.confirm.View(), "Start 1 container?") {
		t.Fatal(m.confirm.View())
	}
	var many []containers.Container
	for i := 0; i < containerBulkListMax+3; i++ {
		many = append(many, containers.Container{ID: "m", Names: []string{"n"}})
	}
	m.askContainerBulk("stop", many)
	if !strings.Contains(m.confirm.Message(), "and 3 more") {
		t.Fatal(m.confirm.View())
	}
	f.Set(t, "all.err", "daemon gone")
	st.containerMarks = map[string]bool{"c1": true}
	m.pendingContainerBulk = "stop"
	if done := ctrRun[containerBulkDoneMsg](t, m.runContainerBulk("stop")); len(done.failed) != 1 {
		t.Fatalf("%+v", done)
	}
	m.pendingContainerBulk = "stop"
	st.containerMarks = nil
	if m.runContainerBulk("stop") != nil {
		t.Fatal("ran with no marks")
	}
}
