package kube

import (
	"strings"
	"testing"
)

// CTX-39: manifest snippets: each is a valid-looking manifest with the
// name and namespace filled in.
func TestSnippetsRender(t *testing.T) {
	list := Snippets()
	if len(list) < 8 {
		t.Fatalf("only %d snippets", len(list))
	}
	seen := map[string]bool{}
	for _, s := range list {
		if s.ID == "" || s.Title == "" || seen[s.ID] {
			t.Fatalf("bad or repeated snippet %+v", s)
		}
		seen[s.ID] = true
		got, ok := Render(s.ID, "shop-api", "staging")
		if !ok {
			t.Fatalf("%s did not render", s.ID)
		}
		for _, want := range []string{"apiVersion: ", "kind: " + s.Kind, "name: shop-api", "namespace: staging"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: no %q in\n%s", s.ID, want, got)
			}
		}
		if strings.Contains(got, "\t") || strings.Contains(got, "{{") {
			t.Errorf("%s: tab or placeholder left:\n%s", s.ID, got)
		}
	}
}

func TestSnippetRenderDefaultsAndUnknown(t *testing.T) {
	got, ok := Render("deployment", "", "")
	if !ok || !strings.Contains(got, "name: my-app") || strings.Contains(got, "namespace:") {
		t.Errorf("defaults:\n%s", got)
	}
	// A name that is not a DNS label is made one.
	got, _ = Render("service", "My App!", "")
	if !strings.Contains(got, "name: my-app") {
		t.Errorf("name not cleaned:\n%s", got)
	}
	if _, ok := Render("nope", "a", "b"); ok {
		t.Error("unknown snippet rendered")
	}
}
