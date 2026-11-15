package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/containers"
)

// The System page: engine info, disk summary, recent events and the
// session's action log (newest first, failures marked).
func TestCovContainerSystemPage(t *testing.T) {
	m, _ := ctrModel(t)
	x := ctrScripted(t, m)
	x.on("info", `{"ServerVersion":"27.3.1","OperatingSystem":"Ubuntu","NCPU":8,"MemTotal":16000000000,"Containers":6,"ContainersRunning":4,"Images":3}`)
	x.fail("system df", "df is down")
	t.Cleanup(func() { m.services.stopHostWatch(); m.services.stopWatch() })
	at := time.Date(2026, 11, 15, 10, 0, 0, 0, time.Local)
	st := m.services
	st.noteActionMsg(containerActionDoneMsg{id: "c5", verb: "stop"}, at)
	st.noteActionMsg(containerGroupDoneMsg{project: "shop", err: errors.New("boom")}, at)
	st.noteActionMsg(hostActionDoneMsg{operation: "tag", target: "a b"}, at)
	st.noteActionMsg(containerBulkDoneMsg{verb: "stop", ids: []string{"a", "b", "c", "d"}, failed: []string{"a: x"}}, at)
	st.noteActionMsg(containerBulkDoneMsg{verb: "start", ids: []string{"a"}}, at)
	st.noteActionMsg(containersExtraDoneMsg{label: "prune build cache"}, at)
	st.noteActionMsg("other", at)
	if len(st.actionLog) != 6 {
		t.Fatalf("log %+v", st.actionLog)
	}
	st.noteEvents([]containers.Event{{Type: "container", Action: "start", Actor: "nginx", Time: at}})
	if _, ok := m.dispatchContainersExtra("containers-system"); !ok {
		t.Fatal("not handled")
	}
	page := ctrRun[containersPageMsg](t, m.containersSystemPage())
	for _, want := range []string{"27.3.1", "Disk summary: df is down", "Actions (this session)", "failed  project shop: boom", "stop a, b, c …", "ok      prune build cache"} {
		if !strings.Contains(page.text, want) {
			t.Errorf("page lacks %q:\n%s", want, page.text)
		}
	}
	x.fail("info", "no daemon")
	page = ctrRun[containersPageMsg](t, m.containersSystemPage())
	if page.err == nil {
		t.Fatal("no error")
	}
	// The other hubs and an unknown id.
	for _, id := range []string{"containers-builds", "containers-image-load", "containers-volume-create-options"} {
		if _, ok := m.dispatchContainersExtra(id); !ok {
			t.Fatalf("%s not handled", id)
		}
	}
	if _, ok := m.dispatchContainersExtra("dance"); ok {
		t.Fatal("unknown id handled")
	}
	if n := len(containersExtraPaletteItems()); n != 1+4+8+6 {
		t.Fatalf("palette rows %d", n)
	}
	if got := actionLogText(nil, nil); !strings.Contains(got, "No actions yet.") {
		t.Fatal(got)
	}
}

// The logs keep only the newest entries; extra jobs report done or
// failed and reload their section; the job card's Cancel runs nothing.
func TestCovContainerSystemLogsAndJobs(t *testing.T) {
	m, _ := ctrModel(t)
	x := ctrScripted(t, m)
	st := m.services
	for i := 0; i < maxActionLog+5; i++ {
		st.noteAction(fmt.Sprint("a", i), nil, time.Now())
	}
	if len(st.actionLog) != maxActionLog || st.actionLog[0].What != "a5" {
		t.Fatalf("log %d first %q", len(st.actionLog), st.actionLog[0].What)
	}
	for i := 0; i < maxRecentEvents+3; i++ {
		st.noteEvents([]containers.Event{{Actor: fmt.Sprint(i)}})
	}
	if len(st.recentEvents) != maxRecentEvents || st.recentEvents[0].Actor != "3" {
		t.Fatalf("events %d", len(st.recentEvents))
	}
	_ = m.onContainersExtraDone(containersExtraDoneMsg{label: "load x", err: errors.New("bad tar")})
	if !strings.Contains(ctrToast(m), "Could not load x") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onContainersExtraDone(containersExtraDoneMsg{label: "load x", section: "images", output: "Loaded image: a\n"})
	if !strings.Contains(ctrToast(m), "Done: load x") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.askContainersExtra("T", "B", nil, map[string]containersExtraJob{"run": {label: "j", args: []string{"version"}}})
	if m.onContainersExtraConfirm("cancel") != nil || m.services.pendingExtra != nil {
		t.Fatal("cancel ran")
	}
	x.fail("version", "nope")
	done := ctrRun[containersExtraDoneMsg](t, m.runContainersExtraJob(containersExtraJob{label: "v", args: []string{"version"}}))
	if done.err == nil || !strings.Contains(done.err.Error(), "nope") {
		t.Fatalf("%+v", done)
	}
	_ = m.onContainersPage(containersPageMsg{title: "x", text: "y"})
	if m.containersCommandLine([]string{"ps", "-a"}) != "docker ps -a" {
		t.Fatal(m.containersCommandLine([]string{"ps", "-a"}))
	}
	if errString("e").Error() != "e" {
		t.Fatal("errString")
	}
}
