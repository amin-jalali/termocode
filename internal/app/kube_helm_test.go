package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// appHelmExec is a fake helm: it logs each argv and answers by the helm
// subcommand ("list", "status", "history", "get values", …).
type appHelmExec struct {
	calls   []string
	answers map[string]string
}

func (h *appHelmExec) Capture(_ context.Context, _ string, argv ...string) (string, string, int, error) {
	h.calls = append(h.calls, strings.Join(argv[1:], " "))
	key := argv[1]
	if len(argv) > 2 && argv[1] == "get" {
		key += " " + argv[2]
	}
	return h.answers[key], "", 0, nil
}

func (h *appHelmExec) CaptureInput(ctx context.Context, dir string, _ []byte, argv ...string) (string, string, int, error) {
	return h.Capture(ctx, dir, argv...)
}

func (h *appHelmExec) Stream(context.Context, string, ...string) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("no stream")
}

func (h *appHelmExec) count(sub string) int {
	n := 0
	for _, c := range h.calls {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

func helmTestModel(t *testing.T) (*appHelmExec, *Model) {
	t.Helper()
	x := &appHelmExec{answers: map[string]string{
		"list":       `[{"name":"shop","namespace":"shop","revision":"3","updated":"2026-11-01","status":"deployed","chart":"shop-1.2.0","app_version":"1.2"}]`,
		"status":     `{"name":"shop","namespace":"shop","version":3,"info":{"status":"deployed","description":"Upgrade complete","notes":"Visit /shop"},"chart":{"metadata":{"name":"shop","version":"1.2.0","appVersion":"1.2"}}}`,
		"history":    `[{"revision":2,"status":"superseded","chart":"shop-1.1.0","description":"Upgrade"},{"revision":3,"status":"deployed","chart":"shop-1.2.0","description":"Upgrade complete"}]`,
		"get values": "replicas: 2\ndbPassword: hunter2\n",
	}}
	oldX, oldFind := kubeHelmExec, kubeFindHelm
	kubeHelmExec, kubeFindHelm = x, func() string { return "/bin/helm" }
	t.Cleanup(func() { kubeHelmExec, kubeFindHelm = oldX, oldFind })
	_, m := kubeLoaded(t, 150, 42)
	return x, m
}

func TestKubeHelmListsReleasesInThePicker(t *testing.T) {
	x, m := helmTestModel(t)
	cmd := kubeKey(t, m, "H")
	if cmd == nil {
		t.Fatal("H ran nothing")
	}
	msg := cmd().(kubeHelmListMsg)
	if msg.err != nil || len(msg.releases) != 1 {
		t.Fatalf("list = %+v", msg)
	}
	if x.count("list -o json --kube-context dev --namespace shop") != 1 {
		t.Errorf("calls = %v", x.calls)
	}
	items := kubeHelmReleaseItems(msg.releases)
	if len(items) != 1 || items[0].ID != "k8s:helm:shop/shop" || !strings.Contains(items[0].Title, "shop-1.2.0") {
		t.Errorf("items = %+v", items)
	}
	acts := kubeHelmActionItems("shop", "shop")
	var ids []string
	for _, it := range acts {
		ids = append(ids, it.ID)
	}
	want := "k8s:helm-status:shop/shop k8s:helm-values:shop/shop k8s:helm-history:shop/shop k8s:helm-rollback:shop/shop k8s:helm-uninstall:shop/shop"
	if strings.Join(ids, " ") != want {
		t.Errorf("actions = %v", ids)
	}
}

func TestKubeHelmValuesAreMasked(t *testing.T) {
	x, m := helmTestModel(t)
	cmd := m.handleKubePickerSelect("k8s:helm-values:shop/shop")
	if cmd == nil {
		t.Fatal("no values command")
	}
	msg := cmd().(kubeHelmPageMsg)
	if msg.err != nil || strings.Contains(msg.text, "hunter2") || !strings.Contains(msg.text, "replicas: 2") {
		t.Errorf("values page = %+v", msg)
	}
	if x.count("rollback") != 0 || x.count("uninstall") != 0 {
		t.Error("a read wrote")
	}
}

func TestKubeHelmStatusAndHistoryPages(t *testing.T) {
	_, m := helmTestModel(t)
	msg := m.handleKubePickerSelect("k8s:helm-status:shop/shop")().(kubeHelmPageMsg)
	if !strings.Contains(msg.text, "deployed") || !strings.Contains(msg.text, "Visit /shop") {
		t.Errorf("status page = %q", msg.text)
	}
	msg = m.handleKubePickerSelect("k8s:helm-history:shop/shop")().(kubeHelmPageMsg)
	if !strings.Contains(msg.text, "superseded") || strings.Index(msg.text, "3 ") > strings.Index(msg.text, "2 ") {
		t.Errorf("history page = %q", msg.text)
	}
}

func TestKubeHelmRollbackAsksFirst(t *testing.T) {
	x, m := helmTestModel(t)
	hist := m.handleKubePickerSelect("k8s:helm-rollback:shop/shop")().(kubeHelmHistoryMsg)
	items := kubeHelmRollbackItems(hist)
	if len(items) != 1 || items[0].ID != "k8s:helm-rb:shop/shop:2" {
		t.Fatalf("rollback items = %+v", items)
	}
	if cmd := m.handleKubePickerSelect("k8s:helm-rb:shop/shop:2"); cmd != nil {
		t.Error("the pick ran something before the card")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindKubeHelm || m.confirm.SafeID() != "cancel" {
		t.Fatal("no Cancel-first rollback card")
	}
	m.confirmOpen = false
	if cmd := m.handleConfirmSelect("cancel"); cmd != nil || x.count("rollback") != 0 {
		t.Fatal("cancel rolled back")
	}
	m.handleKubePickerSelect("k8s:helm-rb:shop/shop:2")
	m.confirmOpen = false
	msg := m.handleConfirmSelect("rollback")().(kubeHelmWriteMsg)
	if msg.err != nil || x.count("rollback shop 2 --wait --kube-context dev --namespace shop") != 1 {
		t.Errorf("msg %+v calls %v", msg, x.calls)
	}
}

func TestKubeHelmUninstallAsksFirst(t *testing.T) {
	x, m := helmTestModel(t)
	if cmd := m.handleKubePickerSelect("k8s:helm-uninstall:shop/shop"); cmd != nil {
		t.Error("ran before the card")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindKubeHelm || m.confirm.SafeID() != "cancel" {
		t.Fatal("no Cancel-first uninstall card")
	}
	m.confirmOpen = false
	msg := m.handleConfirmSelect("uninstall")().(kubeHelmWriteMsg)
	if msg.err != nil || x.count("uninstall shop --kube-context dev --namespace shop") != 1 {
		t.Errorf("msg %+v calls %v", msg, x.calls)
	}
}

func TestKubeHelmNotInstalledIsAHint(t *testing.T) {
	_, m := helmTestModel(t)
	kubeFindHelm = func() string { return "" }
	cmd := kubeKey(t, m, "H")
	if cmd == nil {
		t.Fatal("no hint")
	}
	if _, ok := cmd().(kubeHelmListMsg); ok {
		t.Error("listed without helm")
	}
}

func TestKubeHelmPaletteAIPolicy(t *testing.T) {
	if p := aiActionPolicies["kubernetes-helm"]; p.Gate != aiGateNever {
		t.Errorf("policy = %+v", p)
	}
}
