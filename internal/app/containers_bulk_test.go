package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/services"
)

// bulkState is the containers fixture with every section open.
func bulkState() *servicesState {
	st := svcHostState()
	st.loaded = true
	for sec := range st.collapsed {
		st.collapsed[sec] = false
	}
	return st
}

// bulkCursorOn puts the cursor on the container row with this id.
func bulkCursorOn(t *testing.T, st *servicesState, id string) {
	t.Helper()
	for i, r := range st.rows() {
		if r.kind == svcRowHostItem && r.host == hostContainer && r.hostID == id {
			st.cursor = i
			return
		}
	}
	t.Fatalf("no container row %q", id)
}

func bulkMark(t *testing.T, m Model, ids ...string) Model {
	t.Helper()
	for _, id := range ids {
		bulkCursorOn(t, m.services, id)
		m, _ = contKey(m, "v")
	}
	return m
}

// CTX-23: v marks the container under the cursor, moves down, and a
// second v unmarks it; the row shows the mark.
func TestContainersBulkMarkToggles(t *testing.T) {
	st := bulkState()
	m := svcTestModel(st, 100, 40)
	bulkCursorOn(t, st, "c1")
	before := st.cursor
	m, _ = contKey(m, "v")
	if !st.containerMarks["c1"] {
		t.Fatal("v did not mark c1")
	}
	if st.cursor != before+1 {
		t.Errorf("cursor %d, want %d (moves down after a mark)", st.cursor, before+1)
	}
	line := lineWith(svcRender(st, 60, 40), "postgres")
	if !strings.Contains(line, "*") {
		t.Errorf("marked row has no mark: %q", line)
	}
	if other := lineWith(svcRender(st, 60, 40), "redis-1"); strings.Contains(other, "*") {
		t.Errorf("unmarked row shows a mark: %q", other)
	}
	bulkCursorOn(t, st, "c1")
	m, _ = contKey(m, "v")
	if st.containerMarks["c1"] {
		t.Fatal("second v did not unmark c1")
	}
	_ = m
}

// CTX-23: V clears every mark.
func TestContainersBulkClearMarks(t *testing.T) {
	st := bulkState()
	m := bulkMark(t, svcTestModel(st, 100, 40), "c1", "c2")
	m, _ = contKey(m, "V")
	if len(st.markedContainers()) != 0 {
		t.Fatalf("marks left: %v", st.containerMarks)
	}
}

// CTX-23: with marks, x opens ONE card that lists every marked container
// (and none other); Cancel is first and does nothing.
func TestContainersBulkStopCardListsAllMarked(t *testing.T) {
	st := bulkState()
	m := bulkMark(t, svcTestModel(st, 100, 40), "c1", "c2", "c5")
	bulkCursorOn(t, st, "c4")
	m, _ = contKey(m, "x")
	if !m.confirmOpen || m.confirmKind != confirmKindContainerBulk {
		t.Fatalf("no bulk card: open=%v kind=%d", m.confirmOpen, m.confirmKind)
	}
	b := m.confirm.Buttons()
	if len(b) != 2 || b[0].ID != "cancel" || b[1].ID != "stop" {
		t.Fatalf("buttons %+v, want Cancel first then stop", b)
	}
	view := ansi.Strip(m.confirm.View())
	for _, want := range []string{"Stop 3 containers?", "postgres", "redis", "nginx"} {
		if !strings.Contains(view, want) {
			t.Errorf("card misses %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "shop-web") {
		t.Errorf("card lists the unmarked container under the cursor:\n%s", view)
	}
	if cmd, ok := m.handleServicesConfirm("cancel"); !ok || cmd != nil {
		t.Fatalf("cancel: ok=%v cmd=%v", ok, cmd != nil)
	}
	for _, id := range []string{"c1", "c2", "c5"} {
		if st.busy[containerBusyPrefix+id].Busy() {
			t.Errorf("%s busy after Cancel", id)
		}
	}
	if len(st.markedContainers()) != 3 {
		t.Error("Cancel must keep the marks")
	}
}

// CTX-23: the risky button marks every listed container busy at once and
// clears the marks; the work runs in one command.
func TestContainersBulkConfirmRunsOnEveryMarked(t *testing.T) {
	st := bulkState()
	m := bulkMark(t, svcTestModel(st, 100, 40), "c1", "c5")
	m, _ = contKey(m, "x")
	cmd, ok := m.handleServicesConfirm("stop")
	if !ok || cmd == nil {
		t.Fatalf("stop: ok=%v cmd=%v", ok, cmd != nil)
	}
	for _, id := range []string{"c1", "c5"} {
		if st.busy[containerBusyPrefix+id] != services.Stopping {
			t.Errorf("%s state %v, want Stopping", id, st.busy[containerBusyPrefix+id])
		}
	}
	if st.busy[containerBusyPrefix+"c2"].Busy() {
		t.Error("an unmarked container became busy")
	}
	if len(st.markedContainers()) != 0 {
		t.Error("marks must clear once the action starts")
	}
}

// CTX-23: D with marks asks once to remove them all; running ones are
// stopped first and the card says so.
func TestContainersBulkRemoveCard(t *testing.T) {
	st := bulkState()
	m := bulkMark(t, svcTestModel(st, 100, 40), "c1", "c3")
	m, _ = contKey(m, "D")
	if !m.confirmOpen || m.confirmKind != confirmKindContainerBulk {
		t.Fatal("no bulk remove card")
	}
	b := m.confirm.Buttons()
	if b[0].ID != "cancel" || b[1].ID != "remove" {
		t.Fatalf("buttons %+v", b)
	}
	view := ansi.Strip(m.confirm.View())
	if !strings.Contains(view, "Remove 2 containers?") || !strings.Contains(view, "stopped first") {
		t.Errorf("card:\n%s", view)
	}
}

// CTX-23: without marks the keys keep acting on the one container.
func TestContainersBulkKeysWithoutMarksAreUnchanged(t *testing.T) {
	st := bulkState()
	m := svcTestModel(st, 100, 40)
	bulkCursorOn(t, st, "c1")
	m, _ = contKey(m, "D")
	if m.confirmKind != confirmKindContainerRemove {
		t.Fatalf("kind %d, want the single remove card", m.confirmKind)
	}
}

// CTX-23: the done message frees every container and says how many failed.
func TestContainersBulkDoneReportsFailures(t *testing.T) {
	st := bulkState()
	m := svcTestModel(st, 100, 40)
	st.busy[containerBusyPrefix+"c1"] = services.Stopping
	st.busy[containerBusyPrefix+"c2"] = services.Stopping
	_ = m.onContainerBulkDone(containerBulkDoneMsg{verb: "stop", ids: []string{"c1", "c2"}, failed: []string{"termocode-redis-1: boom"}})
	if st.busy[containerBusyPrefix+"c1"].Busy() || st.busy[containerBusyPrefix+"c2"].Busy() {
		t.Error("busy left after done")
	}
	if got := regToast(&m); !strings.Contains(got, "1 of 2") {
		t.Errorf("toast %q", got)
	}
}

// CTX-23: a mark of a container that is gone is dropped.
func TestContainersBulkDropsGoneMarks(t *testing.T) {
	st := bulkState()
	st.containerMarks = map[string]bool{"c1": true, "gone": true}
	got := st.markedContainers()
	if len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("%+v", got)
	}
}
