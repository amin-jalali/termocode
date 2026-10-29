package app

import (
	"strings"
	"testing"
)

func TestKubeRestartAsksFirstWithCancelFocused(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeCursorTo(t, m, "deployments", "worker")
	if cmd := kubeKey(t, m, "r"); cmd != nil {
		t.Error("r ran something before the card")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindKubeRestart {
		t.Fatal("no restart card")
	}
	b := m.confirm.Buttons()
	if len(b) != 2 || b[0].ID != "cancel" || b[1].ID != "restart" || m.confirm.SafeID() != "cancel" {
		t.Errorf("buttons %+v safe %q", b, m.confirm.SafeID())
	}
	if f.Count("rollout") != 0 {
		t.Fatal("restarted before the answer")
	}
	// Cancel (also what Enter picks on a fresh card): nothing runs.
	m.confirmOpen = false
	if cmd := m.handleConfirmSelect("cancel"); cmd != nil {
		t.Error("cancel returned a command")
	}
	if f.Count("rollout") != 0 {
		t.Error("cancel restarted")
	}
	// Restart: rollout restart in the scope the card showed.
	kubeKey(t, m, "r")
	m.confirmOpen = false
	cmd := m.handleConfirmSelect("restart")
	if cmd == nil {
		t.Fatal("no restart command")
	}
	msg := cmd().(kubeRestartMsg)
	if msg.err != nil || msg.name != "worker" {
		t.Fatalf("restart = %+v", msg)
	}
	if f.Count("rollout restart deployment/worker --context dev --namespace shop") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	if next := m.onKubeRestart(msg); next == nil {
		t.Error("no reload after the restart")
	}
}

func TestKubeRestartOnlyForDeployments(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	kubeCursorTo(t, m, "pods", "web-0")
	kubeKey(t, m, "r")
	if m.confirmOpen {
		t.Error("a restart card for a pod")
	}
}

func TestKubeRestartFailureIsAToast(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "restart.err", "Error from server (Forbidden): deployments.apps \"api\" is forbidden: User \"dev\" cannot patch resource \"deployments\"\n")
	kubeCursorTo(t, m, "deployments", "api")
	kubeKey(t, m, "r")
	m.confirmOpen = false
	msg := m.handleConfirmSelect("restart")().(kubeRestartMsg)
	if msg.err == nil || !strings.HasPrefix(msg.err.Error(), "Not allowed: ") {
		t.Errorf("err = %v", msg.err)
	}
}
