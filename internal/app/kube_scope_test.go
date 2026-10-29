package app

import (
	"strings"
	"testing"
)

func TestKubeContextPickerSwitchesForThisSessionOnly(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	cmd := kubeKey(t, m, "c")
	if cmd == nil {
		t.Fatal("c gave no command")
	}
	msg, ok := cmd().(kubePickMsg)
	if !ok || msg.err != nil {
		t.Fatalf("pick = %+v", msg)
	}
	m.onKubePick(msg)
	if !m.pickerOpen {
		t.Fatal("no picker")
	}
	items := kubePickItems(msg.kind, msg.names, msg.current)
	if len(items) != 2 || items[0].ID != "k8s:ctx:dev" || items[0].Hint != "current" || items[1].ID != "k8s:ctx:prod" || items[1].Hint != "" {
		t.Errorf("items = %+v", items)
	}
	f.Set(t, "namespace.txt", "") // prod names no namespace
	kubeRunLoad(t, m, m.handleTasksPickerSelect("k8s:ctx:prod"))
	st := m.kube
	if st.context != "prod" || st.namespace != "default" {
		t.Errorf("scope = %s/%s, want prod/default", st.context, st.namespace)
	}
	if f.Count("get pods -o json --context prod --namespace default") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	for _, c := range f.Calls() {
		if strings.Contains(c, "use-context") || strings.Contains(c, "set-context") {
			t.Errorf("kubeconfig written: %q", c)
		}
	}
}

func TestKubeNamespacePicker(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	msg := kubeKey(t, m, "n")().(kubePickMsg)
	if msg.kind != "ns" || len(msg.names) != 3 || msg.current != "shop" {
		t.Fatalf("pick = %+v", msg)
	}
	m.onKubePick(msg)
	kubeRunLoad(t, m, m.handleTasksPickerSelect("k8s:ns:kube-system"))
	if m.kube.namespace != "kube-system" || f.Count("--context dev --namespace kube-system") != 4 {
		t.Errorf("namespace %q calls %v", m.kube.namespace, f.Calls())
	}
	// Picking the current one does nothing.
	if cmd := m.handleTasksPickerSelect("k8s:ns:kube-system"); cmd != nil {
		t.Error("same namespace reloaded")
	}
}

func TestKubePickerErrorIsAToast(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "namespaces.err", "Error from server (Forbidden): namespaces is forbidden: User \"dev\" cannot list resource \"namespaces\"\n")
	m.onKubePick(kubeKey(t, m, "n")().(kubePickMsg))
	if m.pickerOpen {
		t.Error("picker opened on an error")
	}
}
