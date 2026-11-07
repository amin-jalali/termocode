package kube_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

func TestPortForwardReportsLocalPortAndStops(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "pf-port.txt", "41999")
	fw, err := c.PortForward(context.Background(), "api-1", 8080)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fw.Stop)
	if fw.Local != 41999 || fw.Remote != 8080 || fw.Pod != "api-1" {
		t.Errorf("forward = %+v", fw)
	}
	got := f.Calls()
	if len(got) != 1 || got[0] != "port-forward --context dev --namespace shop pod/api-1 :8080" {
		t.Errorf("calls = %v", got)
	}
	pid := f.PID("pf.pid")
	if !servicestest.Alive(pid) {
		t.Fatalf("kubectl port-forward (pid %d) is not running", pid)
	}
	select {
	case <-fw.Done():
		t.Fatal("done before Stop")
	default:
	}
	fw.Stop()
	if !servicestest.WaitGone(pid, 10*time.Second) {
		t.Errorf("pid %d still runs after Stop", pid)
	}
	select {
	case <-fw.Done():
	case <-time.After(10 * time.Second):
		t.Error("Done not closed after Stop")
	}
	fw.Stop() // a second Stop is harmless
}

func TestPortForwardFailureIsOneLine(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "pf.err", "error: unable to forward port because pod is not running. Current status=Pending\n")
	_, err := c.PortForward(context.Background(), "cache-0", 6379)
	if err == nil {
		t.Fatal("no error")
	}
	if !strings.Contains(err.Error(), "pod is not running") || strings.Contains(err.Error(), "\n") {
		t.Errorf("err = %q", err)
	}
}

func TestPortForwardNeedsKubectl(t *testing.T) {
	if _, err := (kube.Client{}).PortForward(context.Background(), "p", 80); kube.KindOf(err) != kube.ErrNoKubectl {
		t.Errorf("err = %v", err)
	}
}

func TestPortForwardToServiceAndDeployment(t *testing.T) {
	for _, tc := range []struct {
		kind, name string
		remote     int
		call       string
	}{
		{"svc", "web", 443, "port-forward --context dev --namespace shop svc/web :443"},
		{"deployment", "api", 8080, "port-forward --context dev --namespace shop deployment/api :8080"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			f, c := fakeClient(t)
			f.Set(t, "pf-port.txt", "41777")
			fw, err := c.PortForwardTo(context.Background(), tc.kind, tc.name, tc.remote)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(fw.Stop)
			if fw.Kind != tc.kind || fw.Pod != tc.name || fw.Local != 41777 || fw.Remote != tc.remote {
				t.Errorf("forward = %+v", fw)
			}
			if fw.Target() != tc.kind+"/"+tc.name {
				t.Errorf("target = %q", fw.Target())
			}
			if got := f.Calls(); len(got) != 1 || got[0] != tc.call {
				t.Errorf("calls = %v", got)
			}
		})
	}
}

func TestPortForwardPodTargetAndUnknownKind(t *testing.T) {
	f, c := fakeClient(t)
	fw, err := c.PortForward(context.Background(), "api-1", 8080)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fw.Stop)
	if fw.Kind != "pod" || fw.Target() != "pod/api-1" {
		t.Errorf("forward = %+v", fw)
	}
	if _, err := c.PortForwardTo(context.Background(), "secret", "x", 80); err == nil {
		t.Error("a forward to a kind kubectl can not forward")
	}
	if f.Count("port-forward") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
}
