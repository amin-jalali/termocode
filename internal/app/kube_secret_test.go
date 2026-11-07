package app

import (
	"strings"
	"testing"
)

// CTX-40: V on a secret asks first (Cancel focused), then shows one value;
// nothing is read before the answer and Cancel reads nothing.
func TestKubeSecretRevealAsksFirst(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "secret.txt", "aHVudGVyMg==")
	kubeSectionTo(t, m, "secrets")
	kubeRunLoad(t, m, kubeKey(t, m, " "))
	kubeCursorTo(t, m, "secrets", "db-password")
	kubeKey(t, m, "V") // two keys: a picker first
	if m.kube.revealSecret != "db-password" {
		t.Fatalf("pending secret %q", m.kube.revealSecret)
	}
	if cmd := m.handleKubePickerSelect("k8s:secretkey:password"); cmd != nil {
		t.Error("picking a key ran something before the card")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindKubeReveal {
		t.Fatal("no reveal card")
	}
	if b := m.confirm.Buttons(); len(b) != 2 || b[0].ID != "cancel" || m.confirm.SafeID() != "cancel" {
		t.Errorf("buttons %+v safe %q", b, m.confirm.SafeID())
	}
	if f.Count("get secret ") != 0 {
		t.Fatal("read the value before the answer")
	}
	m.confirmOpen = false
	if cmd := m.handleConfirmSelect("cancel"); cmd != nil {
		t.Error("cancel returned a command")
	}
	if f.Count("get secret ") != 0 {
		t.Error("cancel read the value")
	}
	m.askKubeReveal("db-password", "password")
	m.confirmOpen = false
	cmd := m.handleConfirmSelect("reveal")
	if cmd == nil {
		t.Fatal("no reveal command")
	}
	msg := cmd().(kubeSecretValueMsg)
	if msg.err != nil || msg.value != "hunter2" {
		t.Fatalf("msg %+v", msg)
	}
	m.onKubeSecretValue(msg)
	if !m.confirmOpen || m.confirmKind != confirmKindKubeSecretValue || !strings.Contains(m.confirm.View(), "hunter2") {
		t.Fatal("value card does not show the value")
	}
	// Closing the card forgets the value.
	m.confirmOpen = false
	m.handleConfirmSelect("close")
	if m.kube.revealedValue != "" {
		t.Error("value kept after close")
	}
}
