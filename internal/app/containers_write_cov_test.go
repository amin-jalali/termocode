package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/services"
)

// w opens the change list; each palette command opens its picker,
// prompt or card, and Apply runs the engine verb on the fake docker.
func TestCovContainerWriteFlows(t *testing.T) {
	m, f := ctrModel(t)
	cursorOnContainer(t, m, "c5")
	_ = ctrKey(m, "w")
	if !m.pickerOpen || !strings.Contains(m.picker.View(), "Change nginx") {
		t.Fatal("no change list")
	}
	c := m.services.containerRows[4]
	c.Networks = []string{"bridge"}
	m.services.containerRows[4] = c
	// Each palette command.
	if _, ok := m.containerWritePalette("dance", c); ok {
		t.Fatal("unknown palette id")
	}
	_, _ = m.containerWritePalette("containers-restart-policy", c)
	if !m.pickerOpen || !strings.Contains(m.picker.View(), "unless-stopped") {
		t.Fatal("no policy picker")
	}
	_, _ = m.containerWritePalette("containers-resources", c)
	if !m.promptOpen || m.promptKind != promptKindContainerResources {
		t.Fatal("no limits prompt")
	}
	_, _ = m.containerWritePalette("containers-network-connect", c)
	if !strings.Contains(m.picker.View(), "Connect nginx to") || !strings.Contains(m.picker.View(), "termocode_default") {
		t.Fatalf("connect picker:\n%s", m.picker.View())
	}
	_, _ = m.containerWritePalette("containers-network-disconnect", c)
	if !strings.Contains(m.picker.View(), "Disconnect nginx from") {
		t.Fatal("no disconnect picker")
	}
	// A container on every network / on none.
	all := c
	all.Networks = []string{"bridge", "termocode_default"}
	_ = m.containerWritePick("ctr-net-connect", all)
	if !strings.Contains(ctrToast(m), "No other network") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	none := c
	none.Networks = nil
	_ = m.containerWritePick("ctr-net-disconnect", none)
	if !strings.Contains(ctrToast(m), "Not on a network") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// The pickers answer into cards; Apply runs.
	for _, tc := range []struct{ verb, key, want string }{
		{"ctr-policy", "c5|always", "update --restart always c5"},
		{"ctr-connect", "c5|termocode_default", "network connect termocode_default c5"},
		{"ctr-disconnect", "c5|bridge", "network disconnect bridge c5"},
	} {
		_ = m.onContainerWritePicked(tc.verb, tc.key)
		if !m.confirmOpen || m.confirmKind != confirmKindContainerWrite {
			t.Fatalf("%s: no card", tc.verb)
		}
		done := ctrRun[containerActionDoneMsg](t, m.runContainerWrite("apply"))
		if done.err != nil || f.Count(tc.want) != 1 {
			t.Fatalf("%s: %+v %v", tc.verb, done, f.Calls())
		}
		delete(m.services.busy, containerBusyPrefix+"c5")
	}
	_ = m.onContainerWritePicked("ctr-do", "c5|ctr-kill")
	if !m.confirmOpen || !strings.Contains(m.confirm.View(), "Kill container?") {
		t.Fatal("ctr-do kill: no card")
	}
	done := ctrRun[containerActionDoneMsg](t, m.runContainerWrite("term"))
	if done.verb != "kill" || f.Count("kill --signal TERM c5") != 1 {
		t.Fatalf("%+v %v", done, f.Calls())
	}
	delete(m.services.busy, containerBusyPrefix+"c5")
	_ = m.onContainerWritePicked("ctr-policy", "gone|no")
	if !strings.Contains(ctrToast(m), "Container is gone") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if m.onContainerWritePicked("ctr-dance", "c5|x") != nil {
		t.Fatal("unknown verb")
	}
	// Limits: a bad answer, then a card with swap.
	_ = m.containerWritePick("ctr-resources", c)
	_ = m.onContainerWritePrompt(promptKindContainerResources, "1 2 3 4")
	if !strings.Contains(ctrToast(m), "Limits not changed") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.containerWritePick("ctr-resources", c)
	_ = m.onContainerWritePrompt(promptKindContainerResources, "1.5 512m 1g")
	if !strings.Contains(m.confirm.View(), "--memory-swap 1g") {
		t.Fatalf("limits card %s", m.confirm.View())
	}
	done = ctrRun[containerActionDoneMsg](t, m.runContainerWrite("apply"))
	if done.verb != "change the limits of" || f.Count("update --cpus 1.5 --memory 512m --memory-swap 1g c5") != 1 {
		t.Fatalf("%+v %v", done, f.Calls())
	}
	delete(m.services.busy, containerBusyPrefix+"c5")
	// Rename.
	_ = m.containerWritePick("ctr-rename", c)
	if m.onContainerWritePrompt(promptKindContainerRename, "nginx") != nil {
		t.Fatal("same name ran")
	}
	_ = m.containerWritePick("ctr-rename", c)
	done = ctrRun[containerActionDoneMsg](t, m.onContainerWritePrompt(promptKindContainerRename, "web"))
	if done.verb != "rename" || f.Count("rename c5 web") != 1 {
		t.Fatalf("%+v %v", done, f.Calls())
	}
	m.pendingContainerWrite = containerWrite{verb: "rename", id: "gone"}
	_ = m.onContainerWritePrompt(promptKindContainerRename, "x")
	if !strings.Contains(ctrToast(m), "Container is gone") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// Busy, unknown verb, no engine.
	m.services.busy[containerBusyPrefix+"c5"] = services.Restarting
	_ = m.runContainerWriteNow(containerWrite{verb: "kill", id: "c5"})
	if !strings.Contains(ctrToast(m), "Container is busy") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	delete(m.services.busy, containerBusyPrefix+"c5")
	if m.runContainerWriteNow(containerWrite{verb: "dance", id: "c5"}) != nil {
		t.Fatal("unknown verb ran")
	}
	m.askContainerWrite(containerWrite{verb: "dance", id: "c5"})
	if containerWriteArgs(containerWrite{verb: "dance"}) != nil {
		t.Fatal("args for an unknown verb")
	}
	if got := shortContainerID("0123456789abcdef"); got != "0123456789ab" {
		t.Fatal(got)
	}
	m.services.containerEngine.Reachable = false
	_ = m.runContainerWriteNow(containerWrite{verb: "kill", id: "c5"})
	_ = m.containerWritePick("ctr-kill", c)
	if !strings.Contains(ctrToast(m), "Container engine is not ready") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}
