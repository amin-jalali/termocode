package kube_test

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

func TestHelmRepoAddAndInstall(t *testing.T) {
	x, h := fakeHelm(nil)
	if err := h.RepoAdd(context.Background(), "bitnami", "https://charts.bitnami.com/bitnami"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Install(context.Background(), "web", "bitnami/nginx", "15.1.0"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"repo add bitnami https://charts.bitnami.com/bitnami --kube-context dev --namespace shop",
		"install web bitnami/nginx --version 15.1.0 --kube-context dev --namespace shop",
	}
	if strings.Join(x.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls = %q", x.calls)
	}
}

func TestHelmInstallRefusesFlagsAndBadNames(t *testing.T) {
	x, h := fakeHelm(nil)
	for _, b := range [][2]string{{"-x", "https://a"}, {"ok", "--set=a"}, {"ok", "file:///etc"}} {
		if err := h.RepoAdd(context.Background(), b[0], b[1]); err == nil {
			t.Errorf("RepoAdd accepted %q", b)
		}
	}
	for _, b := range [][3]string{{"--dry", "a/b", ""}, {"web", "--set", ""}, {"web", "a/b", "1 --set"}, {"web", "./local", ""}} {
		if _, err := h.Install(context.Background(), b[0], b[1], b[2]); err == nil {
			t.Errorf("Install accepted %q", b)
		}
	}
	if len(x.calls) != 0 {
		t.Errorf("ran %q", x.calls)
	}
	if kube.CheckChart("web", "oci://ghcr.io/org/chart", "") != nil {
		t.Error("an oci chart was refused")
	}
}

func TestKustomizeMasksSecrets(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "kustomize.txt", "apiVersion: v1\nkind: ConfigMap\ndata:\n  A: b\n---\napiVersion: v1\nkind: Secret\ndata:\n  pw: c2VjcmV0\n")
	out, err := c.Kustomize(context.Background(), "/repo/overlays/dev")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "c2VjcmV0") || !strings.Contains(out, "A: b") {
		t.Errorf("out = %q", out)
	}
	if f.Count("kustomize /repo/overlays/dev") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	if _, err := c.Kustomize(context.Background(), "-o"); err == nil {
		t.Error("a flag was taken as the folder")
	}
}
