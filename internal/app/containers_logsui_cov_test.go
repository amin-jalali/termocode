package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// L on a container opens the logs picker; every follow row opens one
// tab per option set; the prompts check what was typed; ts switches.
func TestCovContainerLogsFollow(t *testing.T) {
	m, _ := ctrModel(t)
	if m.onContainerLogsPicked("follow:200") != nil || m.onContainerLogsPrompt(promptKindContainerLogsTail, "5") != nil {
		t.Fatal("no container picked: something ran")
	}
	cursorOnContainer(t, m, "c5")
	_ = ctrKey(m, "L")
	if !m.pickerOpen || m.pickerKind != pickerKindContainerLogs || !strings.Contains(m.picker.View(), "Timestamps: off") {
		t.Fatal("no logs picker")
	}
	_ = m.onContainerLogsPicked("ts")
	if !m.services.logsOpts.ts || !strings.Contains(m.picker.View(), "Timestamps: on") {
		t.Fatal("ts did not switch")
	}
	// Reopening on the same container keeps ts; another one resets it.
	m.openContainerLogsOptions(m.services.containerRows[4])
	if !m.services.logsOpts.ts {
		t.Fatal("ts lost")
	}
	_ = m.onContainerLogsPicked("follow:200")
	if !strings.Contains(ctrToast(m), "Cannot open logs") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	ctrNvim(t, m)
	for _, id := range []string{"follow:200", "follow:1000", "follow:all", "since:10m", "since:1h"} {
		if cmd := m.onContainerLogsPicked(id); cmd != nil {
			t.Fatalf("%s: %s", id, ctrToast(m))
		}
	}
	n := len(m.terminalTabs)
	if n != 5 {
		t.Fatalf("tabs %d", n)
	}
	_ = m.onContainerLogsPicked("follow:200")
	if len(m.terminalTabs) != n {
		t.Fatal("same options made a second tab")
	}
	for id, kind := range map[string]promptKindEnum{"since:custom": promptKindContainerLogsSince, "tail:custom": promptKindContainerLogsTail, "search": promptKindContainerLogsSearch} {
		m.promptOpen = false
		_ = m.onContainerLogsPicked(id)
		if !m.promptOpen || m.promptKind != kind {
			t.Fatalf("%s: no prompt", id)
		}
	}
	if m.onContainerLogsPicked("dance") != nil {
		t.Fatal("unknown row")
	}
	if m.onContainerLogsPrompt(promptKindContainerLogsTail, "  ") != nil {
		t.Fatal("empty value ran")
	}
	_ = m.onContainerLogsPrompt(promptKindContainerLogsTail, "-3")
	if !strings.Contains(ctrToast(m), "Type a number of lines") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onContainerLogsPrompt(promptKindContainerLogsTail, "50")
	_ = m.onContainerLogsPrompt(promptKindContainerLogsSince, "30m")
	if len(m.terminalTabs) != n+2 {
		t.Fatalf("tabs %d", len(m.terminalTabs))
	}
	_ = m.onContainerLogsPrompt(promptKindContainerLogsSince, "-bad since")
	if !strings.Contains(ctrToast(m), "Cannot follow logs") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if m.onContainerLogsPrompt(promptKindContainerTag, "x") != nil {
		t.Fatal("other prompt")
	}
	m.openContainerLogsOptions(m.services.containerRows[0])
	if m.services.logsOpts.ts {
		t.Fatal("ts kept for another container")
	}
	if _, ok := containerLogsChoice("follow:x", false); ok {
		t.Fatal("bad tail accepted")
	}
}

// Save writes the last lines into a new file in Downloads; search opens
// the matching lines; the engine failing toasts.
func TestCovContainerLogsSaveAndSearch(t *testing.T) {
	m, f := ctrModel(t)
	dl := filepath.Join(os.Getenv("HOME"), "Downloads")
	t.Setenv("XDG_DOWNLOAD_DIR", dl)
	f.Set(t, "logs.txt", "boot ok\nERROR disk\nerror again\n")
	m.openContainerLogsOptions(m.services.containerRows[4])
	_ = ctrUI(t, m, m.onContainerLogsPicked("save"))
	if !strings.Contains(ctrToast(m), "Saved the logs of nginx") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	ents, _ := os.ReadDir(dl)
	if len(ents) != 1 || !strings.HasPrefix(ents[0].Name(), "nginx-logs-") {
		t.Fatalf("downloads %v", ents)
	}
	if b, _ := os.ReadFile(filepath.Join(dl, ents[0].Name())); !strings.Contains(string(b), "ERROR disk") {
		t.Fatalf("saved %q", b)
	}
	_ = ctrUI(t, m, m.onContainerLogsPrompt(promptKindContainerLogsSearch, "error"))
	if !strings.Contains(ctrToast(m), `2 lines have "error"`) {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = ctrUI(t, m, m.onContainerLogsPrompt(promptKindContainerLogsSearch, "nothing-here"))
	if !strings.Contains(ctrToast(m), "No log line has") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	f.Set(t, "all.err", "daemon gone")
	_ = ctrUI(t, m, m.searchContainerLogs(m.services.containerRows[4], "x"))
	if !strings.Contains(ctrToast(m), "Cannot read the logs of nginx") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = ctrUI(t, m, m.saveContainerLogs(m.services.containerRows[4]))
	if !strings.Contains(ctrToast(m), "Cannot save the logs of nginx") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if got := containerLogsFileName("../..", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)); got != "container-logs-20260102-030405.log" {
		t.Fatal(got)
	}
}
