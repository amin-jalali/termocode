package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// Spec (plan §4.3, §5.1): turning a schedule off while its run works
// asks whether the run stops too. Keep running is first and focused
// (Enter and Esc keep it); Stop run ends it, finished items keep their
// results. The card matches its golden (UPDATE_GOLDEN=1 writes it).
func TestWorkflowsOffAsksToStopTheRun(t *testing.T) {
	m, _ := wfxRig(t)
	wfxAddScheduled(t, m, "tick", "", true)
	st := m.wfxS()
	st.sel = "tick"
	e, _ := wire.Find(st.list, "tick")
	id, _, _, err := m.wfxStartID(e, false)
	if err != nil {
		t.Fatal(err)
	}
	wfxUntilGate(t, m, id)
	m.confirmOpen = false // the write card waits; the user turns the schedule off

	off := func() {
		t.Helper()
		m.wfxAskToggle()
		if !m.confirmOpen || m.confirmKind != confirmKindWorkflowOnOff {
			t.Fatalf("no on/off card: %v %v", m.confirmOpen, m.confirmKind)
		}
		m.confirmOpen = false
		m.answerWorkflowCard("toggle")
	}
	off()
	if !m.confirmOpen || m.confirmKind != confirmKindWorkflowOffStop || m.confirm.SafeID() != "keep" {
		t.Fatalf("stop card: open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	box := ansi.Strip(m.confirm.Box()) + "\n"
	path := filepath.Join("testdata", "workflows", "offstop_card.golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(box), 0o644); err != nil {
			t.Fatal(err)
		}
	} else if want, err := os.ReadFile(path); err != nil || string(want) != box {
		t.Errorf("card differs from %s (%v):\n%s", path, err, box)
	}
	m.confirmOpen = false
	m.answerWorkflowCard("keep")
	if st.live[id] == nil || st.live[id].h.Snapshot().State == run.RunCancel {
		t.Fatal("Keep running stopped the run")
	}
	if e, _ := wire.Find(st.list, "tick"); e.Enabled {
		t.Fatal("the schedule is still on")
	}

	// On again, then off again: Stop run ends it.
	m.wfxAskToggle()
	m.confirmOpen = false
	m.answerWorkflowCard("toggle")
	if m.confirmOpen && m.confirmKind == confirmKindWorkflowOffStop {
		t.Fatal("turning on asked to stop the run")
	}
	off()
	m.confirmOpen = false
	m.answerWorkflowCard("stop")
	if rf := wfxPump(t, m, id, nil); rf.State != run.RunCancel {
		t.Fatalf("Stop run: %s", rf.State)
	}
	// Off with no run working asks nothing more.
	m.wfxAskToggle()
	m.confirmOpen = false
	m.answerWorkflowCard("toggle")
	off()
	if m.confirmOpen && m.confirmKind == confirmKindWorkflowOffStop {
		t.Fatal("off without a working run asked to stop one")
	}
}

// wfxUntilGate handles run id's messages until its first write card.
func wfxUntilGate(t *testing.T, m *Model, id string) {
	t.Helper()
	l := m.wfxS().live[id]
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-l.ch:
			m.handleWfxMsg(msg)
			if _, ok := msg.(wfxGateMsg); ok {
				return
			}
			if strings.HasPrefix(l.h.Snapshot().State, "done") {
				t.Fatal("the run ended before its write")
			}
		case <-deadline:
			t.Fatal("no write card")
		}
	}
}
