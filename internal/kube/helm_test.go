package kube_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

// helmExec is a fake services.Exec for helm: it logs each argv and
// answers by the helm subcommand.
type helmExec struct {
	calls   []string
	answers map[string]string // first two helm args ("list", "get values") -> stdout
	stderr  string            // non-empty: every call fails with it
}

func (h *helmExec) Capture(_ context.Context, _ string, argv ...string) (string, string, int, error) {
	h.calls = append(h.calls, strings.Join(argv[1:], " "))
	if h.stderr != "" {
		return "", h.stderr, 1, nil
	}
	key := argv[1]
	if len(argv) > 2 && argv[1] == "get" {
		key += " " + argv[2]
	}
	return h.answers[key], "", 0, nil
}

func (h *helmExec) CaptureInput(ctx context.Context, dir string, _ []byte, argv ...string) (string, string, int, error) {
	return h.Capture(ctx, dir, argv...)
}

func (h *helmExec) Stream(context.Context, string, ...string) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("no stream")
}

func fakeHelm(answers map[string]string) (*helmExec, kube.Helm) {
	x := &helmExec{answers: answers}
	return x, kube.Helm{Binary: "/bin/helm", Context: "dev", Namespace: "shop", X: x}
}

func TestFindHelm(t *testing.T) {
	if got := kube.FindHelm(func(n string) (string, error) { return "/usr/bin/" + n, nil }); got != "/usr/bin/helm" {
		t.Errorf("found = %q", got)
	}
	if got := kube.FindHelm(func(string) (string, error) { return "", errors.New("no") }); got != "" {
		t.Errorf("missing = %q", got)
	}
}

func TestHelmNotInstalled(t *testing.T) {
	_, err := kube.Helm{}.Releases(context.Background())
	if err == nil || err.Error() != "helm is not installed" {
		t.Errorf("err = %v", err)
	}
}

func TestHelmReleases(t *testing.T) {
	x, h := fakeHelm(map[string]string{"list": `[
	 {"name":"api","namespace":"shop","revision":"3","updated":"2026-11-01 10:00:00.1 +0000 UTC","status":"deployed","chart":"api-1.2.0","app_version":"1.2"},
	 {"name":"db","namespace":"shop","revision":"1","updated":"2026-10-01 09:00:00 +0000 UTC","status":"failed","chart":"postgres-15.0.0","app_version":"15"}]`})
	rs, err := h.Releases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].Name != "api" || rs[0].Revision != 3 || rs[0].Status != "deployed" || rs[0].Chart != "api-1.2.0" || rs[0].AppVersion != "1.2" {
		t.Errorf("releases = %+v", rs)
	}
	if rs[1].Status != "failed" || rs[1].Revision != 1 {
		t.Errorf("db = %+v", rs[1])
	}
	if len(x.calls) != 1 || x.calls[0] != "list -o json --kube-context dev --namespace shop" {
		t.Errorf("calls = %v", x.calls)
	}
}

func TestHelmReleasesAllNamespaces(t *testing.T) {
	x, h := fakeHelm(map[string]string{"list": `[]`})
	h.Namespace = ""
	if rs, err := h.Releases(context.Background()); err != nil || len(rs) != 0 {
		t.Fatalf("releases = %v %v", rs, err)
	}
	if x.calls[0] != "list -o json --kube-context dev --all-namespaces" {
		t.Errorf("calls = %v", x.calls)
	}
}

func TestHelmReleasesBadJSON(t *testing.T) {
	_, h := fakeHelm(map[string]string{"list": `not json`})
	if _, err := h.Releases(context.Background()); err == nil {
		t.Error("bad json gave no error")
	}
}

func TestHelmStatus(t *testing.T) {
	x, h := fakeHelm(map[string]string{"status": `{"name":"api","namespace":"shop","version":3,
	 "info":{"first_deployed":"2026-09-01T10:00:00Z","last_deployed":"2026-11-01T10:00:00Z","status":"deployed","description":"Upgrade complete","notes":"Visit http://api"},
	 "chart":{"metadata":{"name":"api","version":"1.2.0","appVersion":"1.2"}}}`})
	s, err := h.Status(context.Background(), "api")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "api" || s.Revision != 3 || s.Status != "deployed" || s.Description != "Upgrade complete" || s.Notes != "Visit http://api" || s.Chart != "api-1.2.0" || s.AppVersion != "1.2" || s.LastDeployed != "2026-11-01T10:00:00Z" {
		t.Errorf("status = %+v", s)
	}
	if x.calls[0] != "status api -o json --kube-context dev --namespace shop" {
		t.Errorf("calls = %v", x.calls)
	}
}

func TestHelmHistory(t *testing.T) {
	x, h := fakeHelm(map[string]string{"history": `[
	 {"revision":1,"updated":"2026-09-01T10:00:00Z","status":"superseded","chart":"api-1.0.0","app_version":"1.0","description":"Install complete"},
	 {"revision":2,"updated":"2026-10-01T10:00:00Z","status":"deployed","chart":"api-1.1.0","app_version":"1.1","description":"Upgrade complete"}]`})
	hs, err := h.History(context.Background(), "api")
	if err != nil {
		t.Fatal(err)
	}
	// Newest first, as the view shows it.
	if len(hs) != 2 || hs[0].Revision != 2 || hs[0].Status != "deployed" || hs[1].Description != "Install complete" {
		t.Errorf("history = %+v", hs)
	}
	if x.calls[0] != "history api -o json --max 50 --kube-context dev --namespace shop" {
		t.Errorf("calls = %v", x.calls)
	}
}

func TestHelmValuesMasksSecrets(t *testing.T) {
	x, h := fakeHelm(map[string]string{"get values": "USER-SUPPLIED VALUES:\nreplicas: 2\ndb:\n  password: hunter2\n  apiToken: \"abc\"\n  host: db\nsecretKey: x\n"})
	v, err := h.Values(context.Background(), "api", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"hunter2", "abc", ": x"} {
		if strings.Contains(v, leak) {
			t.Errorf("values leak %q:\n%s", leak, v)
		}
	}
	for _, keep := range []string{"replicas: 2", "host: db", "password: ••••"} {
		if !strings.Contains(v, keep) {
			t.Errorf("values lost %q:\n%s", keep, v)
		}
	}
	if x.calls[0] != "get values api -o yaml --kube-context dev --namespace shop" {
		t.Errorf("calls = %v", x.calls)
	}
	if _, err := h.Values(context.Background(), "api", 2); err != nil || x.calls[1] != "get values api -o yaml --revision 2 --kube-context dev --namespace shop" {
		t.Errorf("revision call = %v %v", x.calls, err)
	}
}

func TestHelmRollbackAndUninstall(t *testing.T) {
	x, h := fakeHelm(nil)
	if err := h.Rollback(context.Background(), "api", 2); err != nil {
		t.Fatal(err)
	}
	if err := h.Uninstall(context.Background(), "api"); err != nil {
		t.Fatal(err)
	}
	want := []string{"rollback api 2 --wait --kube-context dev --namespace shop", "uninstall api --kube-context dev --namespace shop"}
	if strings.Join(x.calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls = %v", x.calls)
	}
}

func TestHelmRejectsBadNames(t *testing.T) {
	x, h := fakeHelm(nil)
	for _, n := range []string{"", "-x", "--all", "a b"} {
		if err := h.Uninstall(context.Background(), n); err == nil {
			t.Errorf("uninstall %q ran", n)
		}
	}
	if err := h.Rollback(context.Background(), "api", 0); err == nil {
		t.Error("rollback to revision 0 ran")
	}
	if len(x.calls) != 0 {
		t.Errorf("calls = %v", x.calls)
	}
}

func TestHelmErrorIsOneLine(t *testing.T) {
	x, h := fakeHelm(nil)
	x.stderr = "Error: release: not found\n"
	_, err := h.Status(context.Background(), "nope")
	if err == nil || err.Error() != "release: not found" {
		t.Errorf("err = %v", err)
	}
	x.stderr = "Error: Kubernetes cluster unreachable: Get \"https://1.2.3.4\": dial tcp: connection refused\n"
	if _, err := h.Releases(context.Background()); kube.KindOf(err) != kube.ErrNoCluster {
		t.Errorf("unreachable = %v", err)
	}
}
