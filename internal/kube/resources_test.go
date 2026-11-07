package kube_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

// CTX-20: CRDs and any kind by name, all namespaces, aliases.

func setFile(t *testing.T, f interface {
	Set(testing.TB, string, string)
}, name string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	f.Set(t, name, string(b))
}

func TestParseAPIResources(t *testing.T) {
	b, _ := os.ReadFile(filepath.Join("testdata", "api-resources.txt"))
	rs, err := kube.ParseAPIResources(string(b))
	if err != nil || len(rs) != 12 {
		t.Fatalf("%d resources, %v", len(rs), err)
	}
	cert := rs[8]
	want := kube.APIResource{Name: "certificates", ShortNames: []string{"cert", "certs"}, Group: "cert-manager.io", Version: "v1", Namespaced: true, Kind: "Certificate"}
	if !reflect.DeepEqual(cert, want) {
		t.Fatalf("certificates = %+v", cert)
	}
	if cert.Ref() != "certificates.cert-manager.io" || rs[4].Ref() != "pods" || !cert.Custom() || rs[6].Custom() || rs[5].Custom() {
		t.Fatalf("refs %q %q custom cert %v deploy %v crd %v", cert.Ref(), rs[4].Ref(), cert.Custom(), rs[6].Custom(), rs[5].Custom())
	}
	if rs[9].ShortNames != nil || rs[9].Namespaced {
		t.Fatalf("clusterissuers %+v", rs[9])
	}
	// Old kubectl prints APIGROUP instead of APIVERSION.
	old := "NAME        SHORTNAMES   APIGROUP   NAMESPACED   KIND\npods        po                      true         Pod\ndeployments deploy       apps       true         Deployment\n"
	rs, err = kube.ParseAPIResources(old)
	if err != nil || len(rs) != 2 || rs[1].Group != "apps" || rs[0].Group != "" || rs[0].ShortNames[0] != "po" {
		t.Fatalf("old table: %+v %v", rs, err)
	}
	if _, err := kube.ParseAPIResources("error: the server is gone\n"); err == nil {
		t.Fatal("no header is an error")
	}
}

func TestResolveKind(t *testing.T) {
	b, _ := os.ReadFile(filepath.Join("testdata", "api-resources.txt"))
	rs, _ := kube.ParseAPIResources(string(b))
	aliases := map[string]string{"tls": "certificates", "w": "widgets.example.com", "loop": "loop"}
	for q, want := range map[string]string{
		"pods": "pods", "pod": "pods", "po": "pods", "Pod": "pods",
		"deploy": "deployments.apps", "Deployment": "deployments.apps",
		"cert": "certificates.cert-manager.io", "certificate": "certificates.cert-manager.io",
		"certificates.cert-manager.io": "certificates.cert-manager.io",
		"events":                       "events", "events.events.k8s.io": "events.events.k8s.io", "event.events.k8s.io": "events.events.k8s.io",
		"tls": "certificates.cert-manager.io", "w": "widgets.example.com", " crd ": "customresourcedefinitions.apiextensions.k8s.io",
	} {
		r, ok := kube.ResolveKind(rs, aliases, q)
		if !ok || r.Ref() != want {
			t.Errorf("ResolveKind(%q) = %q %v, want %q", q, r.Ref(), ok, want)
		}
	}
	for _, q := range []string{"", "nothing", "loop", "certificates.example.com"} {
		if r, ok := kube.ResolveKind(rs, aliases, q); ok {
			t.Errorf("ResolveKind(%q) = %q, want no match", q, r.Ref())
		}
	}
}

func TestAPIResourcesAndObjects(t *testing.T) {
	f, c := fakeClient(t)
	setFile(t, f, "api-resources.txt")
	setFile(t, f, "certificates.cert-manager.io.json")
	ctx := context.Background()
	rs, err := c.APIResources(ctx)
	if err != nil || len(rs) != 12 {
		t.Fatalf("api resources %d %v", len(rs), err)
	}
	cert, _ := kube.ResolveKind(rs, nil, "cert")
	objs, err := c.Objects(ctx, cert)
	if err != nil || len(objs) != 4 {
		t.Fatalf("objects %d %v", len(objs), err)
	}
	if o := objs[0]; o.Name != "web-tls" || o.Namespace != "shop" || o.Status != "Ready" || o.Created.IsZero() {
		t.Fatalf("first %+v", o)
	}
	if objs[1].Status != "Not ready" || objs[2].Status != "Pending" || objs[3].Status != "" {
		t.Fatalf("status %q %q %q", objs[1].Status, objs[2].Status, objs[3].Status)
	}
	// All namespaces: -A instead of the session namespace; a cluster
	// kind never gets a namespace.
	c.AllNamespaces = true
	if _, err := c.Objects(ctx, cert); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Pods(ctx); err != nil {
		t.Fatal(err)
	}
	issuers, _ := kube.ResolveKind(rs, nil, "clusterissuers")
	_, _ = c.Objects(ctx, issuers)
	want := []string{
		"api-resources --verbs=list --context dev",
		"get certificates.cert-manager.io -o json --context dev --namespace shop",
		"get certificates.cert-manager.io -o json --context dev --all-namespaces",
		"get pods -o json --context dev --all-namespaces",
		"get clusterissuers.cert-manager.io -o json --context dev",
	}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	f.Set(t, "api-resources.err", "error: the server doesn't have a resource type \"x\"\n")
	if _, err := c.APIResources(ctx); err == nil {
		t.Fatal("a failed api-resources call is an error")
	}
}
