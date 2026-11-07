package app

import (
	"strings"
	"testing"
)

// kubeSectionTo puts the cursor on a section header.
func kubeSectionTo(t *testing.T, m *Model, section string) {
	t.Helper()
	for i, r := range m.kube.rows() {
		if r.kind == kubeRowSection && r.section == section {
			m.kube.cursor = i
			return
		}
	}
	t.Fatalf("no section %s", section)
}

func TestKubeExtraSectionsStartFoldedAndAreNotLoaded(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	for _, sec := range kubeExtraSections {
		if !m.kube.collapsed[sec] {
			t.Errorf("%s starts open", sec)
		}
		kubeSectionTo(t, m, sec)
	}
	for _, kind := range []string{"statefulsets", "daemonsets", "replicasets", "cronjobs", "configmaps", "secrets", "ingresses", "persistentvolumeclaims", "nodes"} {
		if n := f.Count("get " + kind); n != 0 {
			t.Errorf("a folded section loaded %s (%d calls)", kind, n)
		}
	}
}

func TestKubeOpeningAnExtraSectionLoadsItInTheScope(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeSectionTo(t, m, "statefulsets")
	cmd := kubeKey(t, m, " ")
	if m.kube.collapsed["statefulsets"] {
		t.Fatal("space did not open the section")
	}
	kubeRunLoad(t, m, cmd)
	if f.Count("get statefulsets -o json --context dev --namespace shop") != 1 {
		t.Errorf("no scoped get statefulsets in %v", f.Calls())
	}
	if f.Count("get daemonsets") != 0 {
		t.Error("a folded section loaded too")
	}
	kubeCursorTo(t, m, "statefulsets", "db")
	if got := m.kube.footer(m.kube.loadedAt)[0]; !strings.Contains(got, "ready") {
		t.Errorf("footer = %q", got)
	}
}

func TestKubeNodesAreClusterScoped(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeSectionTo(t, m, "nodes")
	kubeRunLoad(t, m, kubeKey(t, m, " "))
	for _, c := range f.Calls() {
		if strings.HasPrefix(c, "get nodes") && strings.Contains(c, "--namespace") {
			t.Errorf("nodes got a namespace: %q", c)
		}
	}
	kubeCursorTo(t, m, "nodes", "worker-1")
	if cmd := kubeKey(t, m, "d"); cmd == nil || m.confirmOpen {
		t.Error("d on a node opened a card or did nothing")
	}
}

func TestKubeExtraRowKinds(t *testing.T) {
	want := map[string]string{
		"statefulsets": "statefulset", "daemonsets": "daemonset", "replicasets": "replicaset",
		"cronjobs": "cronjob", "configmaps": "configmap", "secrets": "secret",
		"ingresses": "ingress", "pvcs": "persistentvolumeclaim", "nodes": "node",
	}
	for sec, kind := range want {
		if got := kubeKindOf(sec); got != kind {
			t.Errorf("kubeKindOf(%s) = %q, want %q", sec, got, kind)
		}
	}
}

func TestKubeSecretsShowKeyNamesOnly(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	kubeSectionTo(t, m, "secrets")
	kubeRunLoad(t, m, kubeKey(t, m, " "))
	if len(m.kube.extra["secrets"]) == 0 {
		t.Fatal("no secrets")
	}
	kubeCursorTo(t, m, "secrets", m.kube.extra["secrets"][0].Name)
	if got := m.kube.footer(m.kube.loadedAt)[0]; !strings.Contains(got, "keys") {
		t.Errorf("footer = %q", got)
	}
}
