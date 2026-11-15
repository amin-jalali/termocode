package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

// E asks shell, user and folder, then opens one exec tab per command.
func TestCovContainerExecOptions(t *testing.T) {
	m, _ := ctrModel(t)
	_ = m.askContainerExecOptions(containers.Container{ID: "x", State: "exited"})
	if !strings.Contains(ctrToast(m), "Start the container first") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if m.onContainerExecPrompt(promptKindContainerExecRun, "sh") != nil {
		t.Fatal("no container: ran")
	}
	cursorOnContainer(t, m, "c5")
	_ = ctrKey(m, "E")
	if !m.promptOpen || m.promptKind != promptKindContainerExecRun {
		t.Fatal("no first prompt")
	}
	_ = m.onContainerExecPrompt(promptKindContainerExecRun, "ls -la")
	_ = m.onContainerExecPrompt(promptKindContainerExecUser, "root")
	if m.promptKind != promptKindContainerExecWorkdir {
		t.Fatal("no workdir prompt")
	}
	_ = m.onContainerExecPrompt(promptKindContainerExecWorkdir, "/app")
	if !strings.Contains(ctrToast(m), "Cannot open exec") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	ctrNvim(t, m)
	run := func(cmdline, user, dir string) {
		_ = m.askContainerExecOptions(m.services.containerRows[4])
		_ = m.onContainerExecPrompt(promptKindContainerExecRun, cmdline)
		_ = m.onContainerExecPrompt(promptKindContainerExecUser, user)
		if cmd := m.onContainerExecPrompt(promptKindContainerExecWorkdir, dir); cmd != nil {
			t.Fatalf("%q: %s", cmdline, ctrToast(m))
		}
	}
	run("", "", "")
	run("ls -la", "root", "/app")
	n := len(m.terminalTabs)
	run("ls -la", "root", "/app")
	if n != 2 || len(m.terminalTabs) != n {
		t.Fatalf("tabs %d then %d", n, len(m.terminalTabs))
	}
	_ = m.askContainerExecOptions(m.services.containerRows[4])
	_ = m.onContainerExecPrompt(promptKindContainerExecRun, "")
	_ = m.onContainerExecPrompt(promptKindContainerExecUser, "-bad")
	_ = m.onContainerExecPrompt(promptKindContainerExecWorkdir, "")
	if !strings.Contains(ctrToast(m), "Cannot open exec") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if o := containerExecOptions("zsh", " u ", " /w "); o.Shell != "zsh" || o.User != "u" || o.Workdir != "/w" {
		t.Fatalf("%+v", o)
	}
}
