package app

import (
	"strings"
	"testing"
)

func TestKubeScaleAsksTheNumberThenACancelFirstCard(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeCursorTo(t, m, "deployments", "worker")
	if cmd := kubeKey(t, m, "s"); cmd != nil {
		t.Error("s ran something before the prompt")
	}
	if !m.promptOpen || m.promptKind != promptKindKubeScale {
		t.Fatal("no replicas prompt")
	}
	if m.prompt.Value() != "2" {
		t.Errorf("prompt starts at %q, want the current 2", m.prompt.Value())
	}
	m.promptOpen = false
	if cmd, ok := m.handleKubePromptSubmit(" 5 "); !ok || cmd != nil {
		t.Fatalf("submit = %v, %v", cmd, ok)
	}
	if !m.confirmOpen || m.confirmKind != confirmKindKubeScale {
		t.Fatal("no scale card")
	}
	b := m.confirm.Buttons()
	if len(b) != 2 || b[0].ID != "cancel" || b[1].ID != "scale" || m.confirm.SafeID() != "cancel" {
		t.Errorf("buttons %+v safe %q", b, m.confirm.SafeID())
	}
	v := m.confirm.View()
	for _, want := range []string{"2 → 5", "Context: dev · Namespace: shop", "kubectl scale deployment/worker --replicas=5"} {
		if !strings.Contains(v, want) {
			t.Errorf("card lacks %q:\n%s", want, v)
		}
	}
	if f.Count("scale") != 0 {
		t.Fatal("scaled before the answer")
	}
	// Cancel (what Enter picks on a fresh card): nothing runs.
	m.confirmOpen = false
	if cmd := m.handleConfirmSelect("cancel"); cmd != nil {
		t.Error("cancel returned a command")
	}
	if f.Count("scale") != 0 {
		t.Error("cancel scaled")
	}
	// Scale: in the scope the card showed.
	kubeKey(t, m, "s")
	m.promptOpen = false
	m.handleKubePromptSubmit("5")
	m.confirmOpen = false
	cmd := m.handleConfirmSelect("scale")
	if cmd == nil {
		t.Fatal("no scale command")
	}
	msg := cmd().(kubeScaleMsg)
	if msg.err != nil || msg.name != "worker" || msg.replicas != 5 {
		t.Fatalf("scale = %+v", msg)
	}
	if f.Count("scale deployment/worker --replicas=5 --context dev --namespace shop") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	if next := m.onKubeScale(msg); next == nil {
		t.Error("no reload after the scale")
	}
}

func TestKubeScaleToZeroSaysEveryPodStops(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	kubeCursorTo(t, m, "deployments", "api")
	kubeKey(t, m, "s")
	m.promptOpen = false
	m.handleKubePromptSubmit("0")
	if !m.confirmOpen || !strings.Contains(m.confirm.View(), "stops every pod of api") {
		t.Errorf("zero card:\n%s", m.confirm.View())
	}
}

func TestKubeScaleBadNumberKeepsThePromptOpen(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeCursorTo(t, m, "deployments", "worker")
	for _, bad := range []string{"", "x", "-1", "2.5", "100000"} {
		kubeKey(t, m, "s")
		m.promptOpen = false
		m.handleKubePromptSubmit(bad)
		if m.confirmOpen {
			t.Errorf("%q opened the card", bad)
		}
		if !m.promptOpen || m.prompt.Error() == "" {
			t.Errorf("%q: prompt open %v, error %q", bad, m.promptOpen, m.prompt.Error())
		}
	}
	if f.Count("scale") != 0 {
		t.Error("a bad number scaled")
	}
}

func TestKubeScaleSameNumberRunsNothing(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeCursorTo(t, m, "deployments", "worker")
	kubeKey(t, m, "s")
	m.promptOpen = false
	if cmd, _ := m.handleKubePromptSubmit("2"); cmd == nil {
		t.Error("no toast for an unchanged number")
	}
	if m.confirmOpen || f.Count("scale") != 0 {
		t.Error("an unchanged number asked or scaled")
	}
}

func TestKubeScaleOnlyForDeployments(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	kubeCursorTo(t, m, "pods", "web-0")
	kubeKey(t, m, "s")
	if m.promptOpen || m.confirmOpen {
		t.Error("a scale prompt for a pod")
	}
}

func TestKubeScaleFailureIsAToast(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "scale.err", "Error from server (Forbidden): deployments.apps \"api\" is forbidden: User \"dev\" cannot patch resource \"deployments/scale\"\n")
	kubeCursorTo(t, m, "deployments", "api")
	kubeKey(t, m, "s")
	m.promptOpen = false
	m.handleKubePromptSubmit("1")
	m.confirmOpen = false
	msg := m.handleConfirmSelect("scale")().(kubeScaleMsg)
	if msg.err == nil || !strings.HasPrefix(msg.err.Error(), "Not allowed: ") {
		t.Errorf("err = %v", msg.err)
	}
	if m.onKubeScale(msg) == nil {
		t.Error("no toast")
	}
}
