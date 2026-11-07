package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

// CTX-28: A opens the RBAC page; only read calls run.
func TestKubeRBACKeyOpensThePage(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "can-i.txt", "Resources   Non-Resource URLs   Resource Names   Verbs\npods        []                  []               [get list watch]\n")
	before := len(f.Calls())
	cmd := kubeKey(t, m, "P")
	if cmd == nil {
		t.Fatal("A ran nothing")
	}
	msg := cmd().(kubePageMsg)
	if msg.err != nil || msg.title != "rbac: dev/shop" {
		t.Fatalf("page = %+v", msg)
	}
	if !strings.Contains(msg.text, "pods") || !strings.Contains(msg.text, "get list watch") || !strings.Contains(msg.text, "Role bindings") {
		t.Errorf("text = %q", msg.text)
	}
	calls := f.Calls()[before:]
	if f.Count("auth can-i --list --context dev --namespace shop") != 1 {
		t.Errorf("calls = %v", calls)
	}
	for _, c := range calls {
		if !strings.HasPrefix(c, "get ") && !strings.HasPrefix(c, "auth can-i") {
			t.Errorf("a write call: %q", c)
		}
	}
}

func TestKubeRBACTextShowsAFailedPartInPlace(t *testing.T) {
	perms := []kube.Permission{{Resource: "pods", Verbs: []string{"get"}}, {URLs: []string{"/healthz"}, Verbs: []string{"get"}}}
	rbs := []kube.RoleBinding{{Name: "devs", Role: "ClusterRole/edit", Subjects: []string{"Group/dev"}}}
	text := kubeRBACText(perms, nil, rbs, nil, nil, errors.New("forbidden"))
	for _, want := range []string{"/healthz", "devs → ClusterRole/edit", "Group/dev", "Service accounts\n  cannot read: forbidden"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in %q", want, text)
		}
	}
}

func TestKubeRBACPaletteAndPolicy(t *testing.T) {
	found := false
	for _, it := range kubePaletteItems() {
		found = found || it.ID == "kubernetes-rbac"
	}
	if !found {
		t.Error("no RBAC palette row")
	}
	if p, ok := aiActionPolicies["kubernetes-rbac"]; !ok || p.Gate != aiGateAuto {
		t.Errorf("policy = %+v", p)
	}
}
