package app

import (
	"strings"
	"testing"
)

// CTX-27: x reads the four lists and opens the report page (read only).
func TestKubeReportKeyOpensThePage(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	before := len(f.Calls())
	cmd := kubeKey(t, m, "x")
	if cmd == nil {
		t.Fatal("x ran nothing")
	}
	msg := cmd().(kubePageMsg)
	if msg.err != nil {
		t.Fatalf("report = %v", msg.err)
	}
	if msg.title != "report: dev/shop" {
		t.Errorf("title = %q", msg.title)
	}
	for _, part := range []string{"Pulse", "Xray", "Report"} {
		if !strings.Contains(msg.text, part) {
			t.Errorf("no %s in %q", part, msg.text)
		}
	}
	for _, c := range f.Calls()[before:] {
		if !strings.HasPrefix(c, "get ") {
			t.Errorf("a non-read call: %q", c)
		}
	}
	if m.onKubePage(msg) != nil {
		t.Error("a toast on success")
	}
}

func TestKubeReportErrorIsAToast(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "pods.err", "Error from server (Forbidden): pods is forbidden\n")
	msg := m.kubeReport()().(kubePageMsg)
	if msg.err == nil || m.onKubePage(msg) == nil {
		t.Errorf("err = %v", msg.err)
	}
}

func TestKubeReportPaletteAndPolicy(t *testing.T) {
	found := false
	for _, it := range kubePaletteItems() {
		found = found || it.ID == "kubernetes-report"
	}
	if !found {
		t.Error("no report palette row")
	}
	if p, ok := aiActionPolicies["kubernetes-report"]; !ok || p.Gate != aiGateAuto {
		t.Errorf("policy = %+v", p)
	}
}
