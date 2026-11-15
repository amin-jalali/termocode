package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

const ctrCloneInspect = `[{"Id":"c5","Name":"/nginx","Config":{"Image":"nginx:1.27","Env":["A=1"]},
 "HostConfig":{"PortBindings":{"80/tcp":[{"HostIp":"","HostPort":"8081"}]},"RestartPolicy":{"Name":"always"}}}]`

// Duplicate and Recreate inspect the container on the fake engine, show
// their card, and the answer opens one tab with the plan.
func TestCovContainerCloneFlows(t *testing.T) {
	m, x := ctrModelScripted(t)
	x.on("inspect c5", ctrCloneInspect)
	c := m.services.containerRows[4]
	msg := ctrRun[containerCloneMsg](t, m.containerCloneStart("duplicate", c))
	if msg.err != nil || msg.opts.Image != "nginx:1.27" {
		t.Fatalf("%+v", msg)
	}
	_ = m.onContainerClone(msg)
	if p := m.services.clonePending; !m.confirmOpen || p.op != "duplicate" || p.name == "nginx" || !strings.Contains(p.line, "nginx:1.27") {
		t.Fatalf("duplicate plan %+v", p)
	}
	_ = m.answerContainerClone("duplicate")
	if !strings.Contains(ctrToast(m), "Cannot duplicate the container") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	msg.op = "recreate"
	_ = m.onContainerClone(msg)
	if !strings.Contains(m.confirm.View(), "Recreate nginx?") {
		t.Fatalf("recreate card %s", m.confirm.View())
	}
	ctrNvim(t, m)
	t.Setenv("DOCKER_CONTEXT", "")
	m.services.sessionContext = "prod"
	if m.answerContainerClone("recreate") != nil {
		t.Fatalf("recreate: %s", ctrToast(m))
	}
	if !m.servicesFocusTab(containerTabPrefix + "recreate:nginx") {
		t.Fatal("no recreate tab")
	}
	// Errors and unknown ops.
	if m.onContainerClone(containerCloneMsg{op: "dance"}) != nil {
		t.Fatal("unknown op")
	}
	_ = m.onContainerClone(containerCloneMsg{op: "duplicate", err: errors.New("gone")})
	if !strings.Contains(ctrToast(m), "Cannot duplicate the container") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onContainerClone(containerCloneMsg{op: "duplicate", opts: containers.RunOptions{}})
	_ = m.onContainerClone(containerCloneMsg{op: "recreate", id: "-bad", opts: containers.RunOptions{}})
	if !strings.Contains(ctrToast(m), "Cannot recreate the container") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	x.fail("inspect c5", "gone")
	if got := ctrRun[containerCloneMsg](t, m.containerCloneStart("duplicate", c)); got.err == nil {
		t.Fatal("no error")
	}
	m.services.containerEngine.Binary = ""
	_ = m.containerCloneStart("duplicate", c)
	if !strings.Contains(ctrToast(m), "No container engine") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}

// More options on the run card: every option word, and each mistake.
func TestCovContainerRunOptionsLine(t *testing.T) {
	var o containers.RunOptions
	err := parseRunOptionsLine("volume=/a:/b network=back restart=always user=1 workdir=/w hostname=h platform=linux/arm64 entrypoint=/bin/sh label=a=b detach rm no-rm rm -- sleep 5", &o)
	if err != nil || o.Hostname != "h" || o.Platform != "linux/arm64" || o.Entrypoint != "/bin/sh" || !o.Remove || !o.Detach || o.Labels["a"] != "b" || len(o.Command) != 2 {
		t.Fatalf("%+v %v", o, err)
	}
	for _, bad := range []string{"plain", "label==x", "nope=1", `"open`} {
		if err := parseRunOptionsLine(bad, &containers.RunOptions{}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	m, _ := ctrModel(t)
	m.pendingImageRef = "nginx"
	_ = m.onRunOptionsPrompt("nope=1")
	if !strings.Contains(ctrToast(m), "Options not used") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onRunOptionsPrompt("user=-x")
	_ = m.askRunOptions()
	if !m.promptOpen || m.promptKind != promptKindContainerRunOptions {
		t.Fatal("no options prompt")
	}
}
