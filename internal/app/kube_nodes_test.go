package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

// CTX-29: N lists the nodes; a node's actions ask first with Cancel
// focused; only the card's button runs kubectl.
func TestKubeNodesPickerThenCardCancelFirst(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	cmd := kubeKey(t, m, "N")
	if cmd == nil {
		t.Fatal("N ran nothing")
	}
	msg := cmd().(kubeNodesMsg)
	if msg.err != nil || len(msg.nodes) != 3 || msg.context != "dev" {
		t.Fatalf("nodes = %+v", msg)
	}
	m.onKubeNodes(msg)
	items := kubeNodeItems(m.kube.nodes)
	if len(items) != 3 || items[0].ID != "k8s:node:cp-1" {
		t.Fatalf("items = %+v", items)
	}
	// A cordoned node offers Uncordon, a ready one Cordon; both offer Drain.
	var worker1, cp1 kube.Node
	for _, n := range m.kube.nodes {
		switch n.Name {
		case "worker-1":
			worker1 = n
		case "cp-1":
			cp1 = n
		}
	}
	if a := kubeNodeActionItems(worker1); a[0].ID != "k8s:nodeact:uncordon/worker-1" || a[1].ID != "k8s:nodeact:drain/worker-1" {
		t.Errorf("worker-1 actions = %+v", a)
	}
	if a := kubeNodeActionItems(cp1); a[0].ID != "k8s:nodeact:cordon/cp-1" {
		t.Errorf("cp-1 actions = %+v", a)
	}
	m.handleKubePickerSelect("k8s:node:cp-1")
	m.handleKubePickerSelect("k8s:nodeact:drain/cp-1")
	if !m.confirmOpen || m.confirmKind != confirmKindKubeNode {
		t.Fatal("no node card")
	}
	b := m.confirm.Buttons()
	if len(b) != 2 || b[0].ID != "cancel" || b[1].ID != "drain" || m.confirm.SafeID() != "cancel" {
		t.Errorf("buttons %+v safe %q", b, m.confirm.SafeID())
	}
	if f.Count("drain") != 0 {
		t.Fatal("drained before the answer")
	}
	m.confirmOpen = false
	if c := m.handleConfirmSelect("cancel"); c != nil {
		t.Error("cancel returned a command")
	}
	if f.Count("drain") != 0 {
		t.Error("cancel drained")
	}
	m.handleKubePickerSelect("k8s:nodeact:cordon/cp-1")
	m.confirmOpen = false
	c := m.handleConfirmSelect("cordon")
	if c == nil {
		t.Fatal("no cordon command")
	}
	am := c().(kubeNodeActionMsg)
	if am.err != nil || am.action != "cordon" {
		t.Fatalf("cordon = %+v", am)
	}
	if f.Count("cordon cp-1 --context dev") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	if strings.Contains(strings.Join(f.Calls(), "\n"), "cordon cp-1 --context dev --namespace") {
		t.Error("a node call got a namespace")
	}
}

func TestKubeNodeActionFailureIsAToast(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "node.err", "Error from server (Forbidden): nodes \"cp-1\" is forbidden\n")
	m.kube.nodesContext = "dev"
	m.handleKubePickerSelect("k8s:nodeact:uncordon/cp-1")
	m.confirmOpen = false
	msg := m.handleConfirmSelect("uncordon")().(kubeNodeActionMsg)
	if msg.err == nil || m.onKubeNodeAction(msg) == nil {
		t.Errorf("err = %v", msg.err)
	}
}

func TestKubeHPATextAndKey(t *testing.T) {
	text := kubeHPAText([]kube.HPA{{Name: "web", TargetKind: "Deployment", TargetName: "web", Min: 2, Max: 10, Current: 3, Desired: 4, Targets: "cpu 91%/80%"}})
	for _, want := range []string{"NAME", "Deployment/web", "cpu 91%/80%"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in %q", want, text)
		}
	}
	if !strings.Contains(kubeHPAText(nil), "No horizontal pod autoscalers") {
		t.Error("no empty text")
	}
	f, m := kubeLoaded(t, 150, 42)
	cmd := kubeKey(t, m, "S")
	if cmd == nil {
		t.Fatal("H ran nothing")
	}
	msg := cmd().(kubePageMsg)
	if msg.title != "hpa: dev/shop" {
		t.Errorf("page = %+v", msg)
	}
	if f.Count("get horizontalpodautoscalers") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
}

func TestKubeNodesPaletteAndPolicy(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range kubePaletteItems() {
		ids[it.ID] = true
	}
	if !ids["kubernetes-nodes"] || !ids["kubernetes-hpa"] {
		t.Errorf("palette = %v", ids)
	}
	if p := aiActionPolicies["kubernetes-nodes"]; p.Gate != aiGateNever {
		t.Errorf("nodes policy = %+v", p)
	}
	if p := aiActionPolicies["kubernetes-hpa"]; p.Gate != aiGateAuto {
		t.Errorf("hpa policy = %+v", p)
	}
}
