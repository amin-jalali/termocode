package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

func kubeRunForward(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no forward command")
	}
	msg, ok := cmd().(kubeForwardMsg)
	if !ok {
		t.Fatal("not a forward result")
	}
	m.onKubeForward(msg)
}

func TestKubePortForwardStartsShowsAndStops(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "pf-port.txt", "41888")
	kubeCursorTo(t, m, "pods", "web-0")
	kubeRunForward(t, m, kubeKey(t, m, "f"))
	fw := m.kube.forward("web-0")
	if fw == nil || fw.Local != 41888 || fw.Remote != 80 {
		t.Fatalf("forward = %+v", fw)
	}
	if f.Count("port-forward --context dev --namespace shop pod/web-0 :80") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	pid := f.PID("pf.pid")
	if !servicestest.Alive(pid) {
		t.Fatal("kubectl port-forward is not running")
	}
	plain := ansi.Strip(m.kube.renderRaw(60, 42, true, kubeTestNow, -1))
	if !strings.Contains(plain, ":41888") || !strings.Contains(plain, "127.0.0.1:41888") {
		t.Errorf("the local port is not shown:\n%s", plain)
	}
	kubeKey(t, m, "f") // f again stops it
	if m.kube.forward("web-0") != nil {
		t.Error("forward kept")
	}
	if !servicestest.WaitGone(pid, 10*time.Second) {
		t.Errorf("pid %d still runs", pid)
	}
}

func TestKubePortForwardStopsOnQuit(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeCursorTo(t, m, "pods", "api-7d9f8c6b5-x2x4q")
	kubeKey(t, m, "f") // two ports: a picker
	if !m.pickerOpen {
		t.Fatal("no port picker")
	}
	m.pickerOpen = false
	kubeRunForward(t, m, m.handleTasksPickerSelect("k8s:pf:api-7d9f8c6b5-x2x4q/9090"))
	if fw := m.kube.forward("api-7d9f8c6b5-x2x4q"); fw == nil || fw.Remote != 9090 {
		t.Fatalf("forward = %+v", fw)
	}
	pid := f.PID("pf.pid")
	_ = m.Close() // the quit path
	if !servicestest.WaitGone(pid, 10*time.Second) {
		t.Errorf("pid %d outlived the quit", pid)
	}
	if len(m.kube.forwards) != 0 {
		t.Error("forwards kept after shutdown")
	}
}

func TestKubePortForwardNeedsAPort(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeCursorTo(t, m, "pods", "worker-5c4b-77k2p")
	if cmd := kubeKey(t, m, "f"); cmd == nil {
		t.Error("no toast")
	}
	if f.Count("port-forward") != 0 || m.pickerOpen {
		t.Error("a forward without a port")
	}
}

func TestKubePortForwardServiceListsAndStopsFromForwards(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "pf-port.txt", "41555")
	plain := ansi.Strip(m.kube.renderRaw(60, 42, true, kubeTestNow, -1))
	if strings.Contains(plain, "FORWARDS") {
		t.Errorf("a FORWARDS section with no forward:\n%s", plain)
	}
	kubeCursorTo(t, m, "services", "api") // one port: 80
	kubeRunForward(t, m, kubeKey(t, m, "f"))
	fw := m.kube.forwardOf("svc", "api")
	if fw == nil || fw.Local != 41555 || fw.Remote != 80 {
		t.Fatalf("forward = %+v", fw)
	}
	if f.Count("port-forward --context dev --namespace shop svc/api :80") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	pid := f.PID("pf.pid")
	plain = ansi.Strip(m.kube.renderRaw(60, 42, true, kubeTestNow, -1))
	if !strings.Contains(plain, "FORWARDS") || !strings.Contains(plain, "svc/api") || !strings.Contains(plain, ":41555") {
		t.Errorf("the active forward is not listed:\n%s", plain)
	}
	kubeCursorTo(t, m, "forwards", "svc/api")
	if line := m.kube.footer(kubeTestNow)[0]; !strings.Contains(line, "127.0.0.1:41555") {
		t.Errorf("footer = %q", line)
	}
	kubeKey(t, m, "f") // f on the FORWARDS row stops it
	if m.kube.forwardOf("svc", "api") != nil {
		t.Error("forward kept")
	}
	if !servicestest.WaitGone(pid, 10*time.Second) {
		t.Errorf("pid %d still runs", pid)
	}
	for _, r := range m.kube.rows() {
		if r.section == "forwards" {
			t.Fatal("the FORWARDS section stays with no forward")
		}
	}
}

func TestKubePortForwardServicePortPicker(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeCursorTo(t, m, "services", "web") // two ports: 80, 443
	kubeKey(t, m, "f")
	if !m.pickerOpen {
		t.Fatal("no port picker")
	}
	m.pickerOpen = false
	kubeRunForward(t, m, m.handleTasksPickerSelect("k8s:pf-svc:web/443"))
	if fw := m.kube.forwardOf("svc", "web"); fw == nil || fw.Remote != 443 {
		t.Fatalf("forward = %+v", fw)
	}
	if f.Count("port-forward --context dev --namespace shop svc/web :443") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
}

func TestKubePortForwardDeployment(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeCursorTo(t, m, "deployments", "api") // template port 8080
	kubeRunForward(t, m, kubeKey(t, m, "f"))
	if fw := m.kube.forwardOf("deployment", "api"); fw == nil || fw.Remote != 8080 {
		t.Fatalf("forward = %+v", fw)
	}
	if f.Count("port-forward --context dev --namespace shop deployment/api :8080") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	kubeCursorTo(t, m, "deployments", "worker") // no port
	if cmd := kubeKey(t, m, "f"); cmd == nil {
		t.Error("no toast")
	}
	if f.Count("deployment/worker") != 0 || m.pickerOpen {
		t.Error("a forward without a port")
	}
}
