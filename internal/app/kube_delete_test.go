package app

import (
	"strings"
	"testing"
)

func TestKubeDeleteAsksFirstWithCancelFocused(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeCursorTo(t, m, "pods", "worker-5c4b-77k2p")
	if cmd := kubeKey(t, m, "d"); cmd != nil {
		t.Error("d ran something before the card")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindKubeDelete {
		t.Fatal("no delete card")
	}
	b := m.confirm.Buttons()
	if len(b) != 2 || b[0].ID != "cancel" || b[1].ID != "delete" || m.confirm.SafeID() != "cancel" {
		t.Errorf("buttons %+v safe %q", b, m.confirm.SafeID())
	}
	if f.Count("delete") != 0 {
		t.Fatal("deleted before the answer")
	}
	// Cancel (also what Enter picks on a fresh card): nothing runs.
	m.confirmOpen = false
	if cmd := m.handleConfirmSelect("cancel"); cmd != nil {
		t.Error("cancel returned a command")
	}
	if f.Count("delete") != 0 {
		t.Error("cancel deleted")
	}
	// Delete: kubectl delete in the scope the card showed.
	kubeKey(t, m, "d")
	m.confirmOpen = false
	cmd := m.handleConfirmSelect("delete")
	if cmd == nil {
		t.Fatal("no delete command")
	}
	msg := cmd().(kubeDeleteMsg)
	if msg.err != nil || msg.kind != "pod" || msg.name != "worker-5c4b-77k2p" {
		t.Fatalf("delete = %+v", msg)
	}
	if f.Count("delete pod worker-5c4b-77k2p --wait=false --context dev --namespace shop") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	if next := m.onKubeDelete(msg); next == nil {
		t.Error("no reload after the delete")
	}
}

func TestKubeDeleteEveryKind(t *testing.T) {
	for _, c := range []struct{ section, name, kind string }{
		{"deployments", "api", "deployment"},
		{"services", "web", "service"},
	} {
		f, m := kubeLoaded(t, 150, 42)
		kubeCursorTo(t, m, c.section, c.name)
		kubeKey(t, m, "d")
		if !m.confirmOpen || m.confirmKind != confirmKindKubeDelete {
			t.Fatalf("%s: no delete card", c.section)
		}
		if p := m.kube.pendingDelete; p.kind != c.kind || p.name != c.name || p.context != "dev" || p.namespace != "shop" {
			t.Errorf("%s: pending %+v", c.section, p)
		}
		m.confirmOpen = false
		msg := m.handleConfirmSelect("delete")().(kubeDeleteMsg)
		if msg.err != nil || f.Count("delete "+c.kind+" "+c.name+" --wait=false") != 1 {
			t.Errorf("%s: %+v calls %v", c.section, msg, f.Calls())
		}
	}
}

func TestKubeDeleteUsesTheScopeTheCardShowed(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeCursorTo(t, m, "pods", "web-0")
	kubeKey(t, m, "d")
	m.kube.context, m.kube.namespace = "prod", "default" // switched while the card was open
	m.confirmOpen = false
	m.handleConfirmSelect("delete")()
	if f.Count("delete pod web-0 --wait=false --context dev --namespace shop") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
}

func TestKubeDeleteNotOnASectionHeader(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	for i, r := range m.kube.rows() {
		if r.kind == kubeRowSection {
			m.kube.cursor = i
			break
		}
	}
	kubeKey(t, m, "d")
	if m.confirmOpen {
		t.Error("a delete card for a section header")
	}
}

func TestKubeDeleteFailureIsAToast(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "delete.err", "Error from server (Forbidden): pods \"web-0\" is forbidden: User \"dev\" cannot delete resource \"pods\"\n")
	kubeCursorTo(t, m, "pods", "web-0")
	kubeKey(t, m, "d")
	m.confirmOpen = false
	msg := m.handleConfirmSelect("delete")().(kubeDeleteMsg)
	if msg.err == nil || !strings.HasPrefix(msg.err.Error(), "Not allowed: ") {
		t.Errorf("err = %v", msg.err)
	}
}
