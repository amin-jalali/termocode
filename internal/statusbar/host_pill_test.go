package statusbar

import (
	"strings"
	"testing"
)

func TestHostPillShowsTheContainerAndWhenItStopped(t *testing.T) {
	m := New()
	m.SetWidth(140)
	got := stripCSI(m.View(State{Path: "/app/a.go", Project: "app", Host: "api-1"}))
	if !strings.Contains(got, "api-1") || strings.Contains(got, "stopped") {
		t.Fatalf("running host pill: %q", got)
	}
	if i, j := strings.Index(got, "api-1"), strings.Index(got, "app"); i < 0 || j < i {
		t.Fatalf("the host pill comes before the project: %q", got)
	}
	got = stripCSI(m.View(State{Path: "/app/a.go", Project: "app", Host: "api-1", HostOffline: true}))
	if !strings.Contains(got, "api-1 · stopped") {
		t.Fatalf("stopped host pill: %q", got)
	}
	got = stripCSI(m.View(State{Path: "/a.go", Project: "proj"}))
	if strings.Contains(got, "▣") || strings.Contains(got, "stopped") {
		t.Fatalf("a local workspace has no host pill: %q", got)
	}
}
