package app

import (
	"strings"
	"testing"
)

func TestKubeScaleAStatefulSetAsksThenACancelFirstCard(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeSectionTo(t, m, "statefulsets")
	kubeRunLoad(t, m, kubeKey(t, m, " "))
	kubeCursorTo(t, m, "statefulsets", "db")
	want := m.kube.extra["statefulsets"][0].Replicas
	kubeKey(t, m, "s")
	if !m.promptOpen || m.promptKind != promptKindKubeScale {
		t.Fatal("no replicas prompt")
	}
	if m.prompt.Value() != itoa(want) {
		t.Errorf("prompt starts at %q, want %d", m.prompt.Value(), want)
	}
	m.promptOpen = false
	m.handleKubePromptSubmit(itoa(want + 2))
	if !m.confirmOpen || m.confirmKind != confirmKindKubeScale || m.confirm.SafeID() != "cancel" {
		t.Fatal("no Cancel-first scale card")
	}
	if v := m.confirm.View(); !strings.Contains(v, "kubectl scale statefulset/db --replicas="+itoa(want+2)) || !strings.Contains(v, "Scale statefulset?") {
		t.Errorf("card:\n%s", v)
	}
	m.confirmOpen = false
	msg := m.handleConfirmSelect("scale")().(kubeScaleMsg)
	if msg.err != nil || msg.kind != "statefulset" {
		t.Fatalf("scale = %+v", msg)
	}
	if f.Count("scale statefulset/db --replicas="+itoa(want+2)+" --context dev --namespace shop") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
}

func TestKubeScaleBadNumberOnAStatefulSetKeepsItsKind(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	kubeSectionTo(t, m, "statefulsets")
	kubeRunLoad(t, m, kubeKey(t, m, " "))
	kubeCursorTo(t, m, "statefulsets", "db")
	kubeKey(t, m, "s")
	m.promptOpen = false
	m.handleKubePromptSubmit("x")
	if m.kube.pendingScale.kind != "statefulset" || m.kube.pendingScale.name != "db" {
		t.Errorf("pending = %+v", m.kube.pendingScale)
	}
}
