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
