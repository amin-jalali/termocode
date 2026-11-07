package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/containers"
)

func TestContainersSystemPaletteIsClassified(t *testing.T) {
	found := false
	for _, it := range containersExtraPaletteItems() {
		if it.ID == "containers-system" {
			found = true
		}
		if _, ok := aiActionPolicies[it.ID]; !ok {
			t.Fatalf("%s has no AI policy", it.ID)
		}
	}
	if !found {
		t.Fatal("containers-system is not in the palette")
	}
}

func TestContainersSystemKeepsRecentEvents(t *testing.T) {
	st := newServicesState()
	base := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	var evs []containers.Event
	for i := 0; i < maxRecentEvents+10; i++ {
		evs = append(evs, containers.Event{Type: "container", Action: "start", Actor: "web", Time: base.Add(time.Duration(i) * time.Second)})
	}
	st.noteEvents(evs)
	if len(st.recentEvents) != maxRecentEvents {
		t.Fatalf("kept %d events, want %d", len(st.recentEvents), maxRecentEvents)
	}
	if !st.recentEvents[len(st.recentEvents)-1].Time.Equal(evs[len(evs)-1].Time) {
		t.Fatal("the newest event was dropped")
	}
}

func TestContainersSystemActionLog(t *testing.T) {
	st := newServicesState()
	at := time.Date(2026, 11, 7, 9, 30, 5, 0, time.UTC)
	for i := 0; i < maxActionLog+5; i++ {
		st.noteAction("start web", nil, at)
	}
	if len(st.actionLog) != maxActionLog {
		t.Fatalf("kept %d actions, want %d", len(st.actionLog), maxActionLog)
	}
	st.noteAction("remove image old:1", errors.New("image is in use\nmore"), at.Add(time.Minute))
	text := actionLogText(st.actionLog, time.UTC)
	if !strings.Contains(text, "Actions (this session)") {
		t.Fatalf("no heading:\n%s", text)
	}
	if !strings.Contains(text, "09:31:05  failed  remove image old:1: image is in use") {
		t.Fatalf("no failed line:\n%s", text)
	}
	if strings.Index(text, "failed") > strings.Index(text, "ok  ") {
		t.Fatalf("newest action must come first:\n%s", text)
	}
	if got := actionLogText(nil, time.UTC); !strings.Contains(got, "No actions yet.") {
		t.Fatalf("empty log: %q", got)
	}
}
