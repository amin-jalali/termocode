package app

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"

	"github.com/amin-jalali/termocode/internal/theme"
)

var kubeTestNow = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func kubeRender(t *testing.T, m *Model, w, h int) []string {
	t.Helper()
	out := m.kube.renderRaw(w, h, true, kubeTestNow, -1)
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("%d lines, want %d", len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Errorf("w=%d: line %d is %d wide: %q", w, i, got, ansi.Strip(l))
		}
	}
	return lines
}

func TestKubeViewRendersTheFourSections(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	_, m := kubeLoaded(t, 150, 42)
	plain := ansi.Strip(strings.Join(kubeRender(t, m, 29, 42), "\n"))
	for _, want := range []string{"KUBERNETES", "ctx dev · ns shop", "PODS", "DEPLOYMENTS", "SERVICES", "JOBS",
		"2/2 ↻1 2d", "0/1 ↻7", "3/3 9d", "ClusterIP", "1/1 4h", "2/5 1h"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in\n%s", want, plain)
		}
	}
	// A wide sidebar shows the status, the rollout numbers and the ports.
	wide := ansi.Strip(strings.Join(kubeRender(t, m, 60, 42), "\n"))
	for _, want := range []string{"CrashLoopBackOff 0/1 ↻7", "1/2 upd 2 av 1", "NodePort 80:30080/TCP,443/TCP"} {
		if !strings.Contains(wide, want) {
			t.Errorf("wide: missing %q in\n%s", want, wide)
		}
	}
	if last := ansi.Strip(kubeRender(t, m, 29, 42)[41]); !strings.Contains(last, "? keys") {
		t.Errorf("narrow footer lost \"? keys\": %q", last)
	}
	for _, w := range []int{12, 20, 40} { // every width keeps its size
		kubeRender(t, m, w, 24)
	}
}

func TestKubeViewFooterFollowsTheCursor(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	st := m.kube
	st.cursor = 2 // PODS, api, worker
	f := st.footer(kubeTestNow)
	if f[0] != "CrashLoopBackOff · ready 0/1 · 7 restarts" || !strings.Contains(f[1], "l logs") {
		t.Errorf("pod footer = %q", f)
	}
	st.cursor = 0 // the PODS header
	if f := st.footer(kubeTestNow); f[0] != "" || !strings.Contains(f[1], "c context") {
		t.Errorf("section footer = %q", f)
	}
}

func TestKubeViewProblemStates(t *testing.T) {
	m := kubeTestModel(t, 150, 42)
	if plain := ansi.Strip(strings.Join(kubeRender(t, m, 29, 24), "\n")); !strings.Contains(plain, "Looking for the cluster") {
		t.Errorf("before the first load:\n%s", plain)
	}
	st := m.kube
	st.loaded, st.detected, st.err = true, true, "kubectl is not installed"
	if plain := ansi.Strip(strings.Join(kubeRender(t, m, 29, 24), "\n")); !strings.Contains(plain, "kubectl not found") {
		t.Errorf("no kubectl:\n%s", plain)
	}
	st.binary, st.context, st.err = "/bin/kubectl", "prod", "Cannot reach the cluster"
	plain := ansi.Strip(strings.Join(kubeRender(t, m, 29, 24), "\n"))
	for _, want := range []string{"ctx prod", "Cannot reach the cluster", "R tries again", "c picks another context"} {
		if !strings.Contains(plain, want) {
			t.Errorf("down: missing %q in\n%s", want, plain)
		}
	}
}

func TestKubeDemoNeverCallsKubectl(t *testing.T) {
	f := kubeFake(t)
	for _, v := range []string{"", "restart", "down", "keys"} {
		m := kubeTestModel(t, 150, 42)
		m.kubeDemo(v)
		if cmd := m.kubeSync(); cmd != nil {
			t.Errorf("%q: the demo started a load", v)
		}
		if !m.kube.visible || m.activity.Active() != activity.ViewKubernetes {
			t.Errorf("%q: view not shown", v)
		}
		if v == "restart" && (!m.confirmOpen || m.confirmKind != confirmKindKubeRestart) {
			t.Error("no restart card")
		}
	}
	if n := len(f.Calls()); n != 0 {
		t.Errorf("%d kubectl calls", n)
	}
}
