package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// helmRec records helm calls (the app has no fake helm binary).
type helmRec struct{ calls []string }

func (h *helmRec) Capture(_ context.Context, _ string, argv ...string) (string, string, int, error) {
	h.calls = append(h.calls, strings.Join(argv[1:], " "))
	return "", "", 0, nil
}

func (h *helmRec) CaptureInput(ctx context.Context, dir string, _ []byte, argv ...string) (string, string, int, error) {
	return h.Capture(ctx, dir, argv...)
}

func (h *helmRec) Stream(context.Context, string, ...string) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("no stream")
}

func withHelm(t *testing.T) *helmRec {
	t.Helper()
	rec := &helmRec{}
	oldFind, oldX := kubeHelmFind, kubeHelmExec
	kubeHelmFind = func() string { return "/bin/helm" }
	kubeHelmExec = rec
	t.Cleanup(func() { kubeHelmFind, kubeHelmExec = oldFind, oldX })
	return rec
}

func TestKubeInstallPaletteAndPolicy(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range kubePaletteItems() {
		ids[it.ID] = true
	}
	for _, id := range []string{"kubernetes-helm-repo-add", "kubernetes-helm-install", "kubernetes-kustomize"} {
		if !ids[id] {
			t.Errorf("no palette row %s", id)
		}
		if p, ok := aiActionPolicies[id]; !ok || p.Gate != aiGateNever {
			t.Errorf("%s policy = %+v", id, p)
		}
	}
}

func TestKubeHelmRepoAddAsksFirst(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	rec := withHelm(t)
	m.dispatchKubePalette("kubernetes-helm-repo-add")
	if !m.promptOpen || m.promptKind != promptKindKubeRepoAdd {
		t.Fatal("no repo prompt")
	}
	if _, ok := m.handleKubePromptSubmit("bitnami https://charts.bitnami.com/bitnami"); !ok {
		t.Fatal("answer not taken")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindKubeRepoAdd || m.confirm.SafeID() != "cancel" {
		t.Fatal("no card with Cancel focused")
	}
	m.confirmOpen = false
	if cmd := m.handleConfirmSelect("cancel"); cmd != nil || len(rec.calls) != 0 {
		t.Error("cancel ran helm")
	}
	m.dispatchKubePalette("kubernetes-helm-repo-add")
	m.handleKubePromptSubmit("bitnami https://charts.bitnami.com/bitnami")
	m.confirmOpen = false
	cmd := m.handleConfirmSelect("add")
	if cmd == nil {
		t.Fatal("no add command")
	}
	if msg := cmd().(kubeHelmMsg); msg.err != nil {
		t.Fatal(msg.err)
	}
	if len(rec.calls) != 1 || !strings.HasPrefix(rec.calls[0], "repo add bitnami https://charts.bitnami.com/bitnami") {
		t.Errorf("calls = %q", rec.calls)
	}
}

func TestKubeHelmInstallAsksFirstInTheViewScope(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	rec := withHelm(t)
	m.dispatchKubePalette("kubernetes-helm-install")
	if !m.promptOpen || m.promptKind != promptKindKubeInstall {
		t.Fatal("no install prompt")
	}
	m.handleKubePromptSubmit("web --set x=1")
	if m.confirmOpen {
		t.Fatal("a bad answer opened the card")
	}
	m.handleKubePromptSubmit("web bitnami/nginx 15.1.0")
	if !m.confirmOpen || m.confirmKind != confirmKindKubeInstall || m.confirm.SafeID() != "cancel" {
		t.Fatal("no install card")
	}
	m.confirmOpen = false
	msg := m.handleConfirmSelect("install")().(kubeHelmMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if len(rec.calls) != 1 || rec.calls[0] != "install web bitnami/nginx --version 15.1.0 --kube-context dev --namespace shop" {
		t.Errorf("calls = %q", rec.calls)
	}
}

func TestKubeKustomizeNeedsAFolder(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	if cmd := m.kubeKustomize(""); cmd == nil {
		t.Error("no hint without a file")
	}
}
