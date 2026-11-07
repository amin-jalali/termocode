package app

import (
	"strings"
	"testing"
)

// CTX-39: "Kubernetes: New Manifest from Snippet…" lists the snippets in
// a picker; the AI never opens the picker.
func TestKubeSnippetPaletteAndPolicy(t *testing.T) {
	found := false
	for _, it := range kubePaletteItems() {
		if it.ID == "kubernetes-snippet" && it.Title == "Kubernetes: New Manifest from Snippet…" {
			found = true
		}
	}
	if !found {
		t.Error("no snippet palette row")
	}
	if p, ok := aiActionPolicies["kubernetes-snippet"]; !ok || p.Gate != aiGateNever {
		t.Errorf("policy = %+v", p)
	}
	items := kubeSnippetItems()
	if len(items) < 8 {
		t.Fatalf("items = %d", len(items))
	}
	for _, it := range items {
		if !strings.HasPrefix(it.ID, "k8s:snippet:") {
			t.Errorf("item id %q", it.ID)
		}
	}
}

func TestKubeSnippetLines(t *testing.T) {
	lines, ok := kubeSnippetLines("configmap", "staging")
	if !ok || len(lines) < 4 || lines[0] != "apiVersion: v1" {
		t.Fatalf("lines = %q", lines)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "namespace: staging") {
		t.Errorf("namespace missing: %q", lines)
	}
	if _, ok := kubeSnippetLines("nope", ""); ok {
		t.Error("unknown snippet")
	}
}
