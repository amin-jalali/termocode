package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/devpreview"
	"github.com/amin-jalali/termocode/internal/services"
)

// withDevPreviewOpen records osOpen calls and clears the SSH variables.
func withDevPreviewOpen(t *testing.T) *[]string {
	t.Helper()
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	var opened []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { osOpen = old })
	return &opened
}

func devPreviewToastTitles(m *Model) string {
	var b []string
	for _, ts := range m.toast.Live() {
		b = append(b, ts.Title+" — "+ts.Detail)
	}
	return strings.Join(b, "\n")
}

func TestDevPreviewPaletteAndPolicy(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range paletteItems() {
		ids[it.ID] = true
	}
	for _, id := range []string{"preview-dev-servers", "preview-open-newest"} {
		if !ids[id] {
			t.Errorf("no palette row %s", id)
		}
		if p, ok := aiActionPolicies[id]; !ok || p.Gate != aiGateNever {
			t.Errorf("%s policy = %+v, want never", id, p)
		}
	}
}

func TestDevPreviewServiceTargetsOnlyUpWithPorts(t *testing.T) {
	m := &Model{services: &servicesState{
		compose: []services.Service{{Name: "web"}, {Name: "db"}},
		status: map[string]services.Status{
			"web": {Name: "web", State: services.Running, Ports: []services.Port{{Published: 8080, Target: 80}}},
			"db":  {Name: "db", State: services.Stopped, Ports: []services.Port{{Published: 5432, Target: 5432}}},
		},
		local:   []services.Service{{Name: "api", Ports: []services.Port{{Published: 3000, Target: 3000}}}},
		localSt: map[string]services.Status{"api": {Name: "api", State: services.Running}},
	}}
	got := m.devPreviewServiceTargets()
	if len(got) != 2 || got[0].URL != "http://localhost:8080/" || got[0].From != "service web" ||
		got[1].URL != "http://localhost:3000/" || got[1].From != "service api" {
		t.Errorf("targets = %+v (want web 8080 and api 3000, not the stopped db)", got)
	}
}

func TestDevPreviewNothingFoundSaysHow(t *testing.T) {
	m := &Model{}
	if cmd := m.devPreviewScan(false); cmd == nil {
		t.Fatal("no toast cmd")
	}
	if s := devPreviewToastTitles(m); !strings.Contains(s, "No dev server found") || !strings.Contains(s, "npm run dev") {
		t.Errorf("toast = %q", s)
	}
}

func TestDevPreviewScanProbesInBackground(t *testing.T) {
	m := &Model{services: &servicesState{
		compose: []services.Service{{Name: "web"}},
		status:  map[string]services.Status{"web": {Name: "web", State: services.Running, Ports: []services.Port{{Published: 8080}}}},
	}}
	var probed []string
	old := devPreviewProbeAll
	devPreviewProbeAll = func(_ context.Context, ts []devpreview.Target) []devpreview.Check {
		for _, tg := range ts {
			probed = append(probed, tg.URL)
		}
		return []devpreview.Check{{Up: true, Status: 200, Title: "Shop"}}
	}
	t.Cleanup(func() { devPreviewProbeAll = old })
	cmd := m.devPreviewScan(false)
	if cmd == nil || len(probed) != 0 {
		t.Fatalf("probed on Update: %v", probed)
	}
	msg, ok := cmd().(devPreviewMsg)
	if !ok || len(probed) != 1 || probed[0] != "http://localhost:8080/" {
		t.Fatalf("msg = %#v, probed %v", msg, probed)
	}
	if _, ok := m.handleDevPreviewMsg(msg); !ok {
		t.Fatal("message not taken")
	}
	if !m.pickerOpen || m.pickerKind != pickerKindTasks {
		t.Fatal("no Dev Servers picker")
	}
	cur, ok := m.picker.Current()
	if !ok || cur.ID != "dvp:http://localhost:8080/" || !strings.Contains(cur.Hint, "200 · Shop") || !strings.Contains(cur.Hint, "service web") {
		t.Errorf("row = %+v", cur)
	}
}

func TestDevPreviewPickOpensBrowser(t *testing.T) {
	opened := withDevPreviewOpen(t)
	m := &Model{}
	m.handleTasksPickerSelect("dvp:http://localhost:5173/")
	if len(*opened) != 1 || (*opened)[0] != "http://localhost:5173/" {
		t.Errorf("opened = %v", *opened)
	}
}

func TestDevPreviewPickRefusesNonLocal(t *testing.T) {
	opened := withDevPreviewOpen(t)
	m := &Model{}
	m.handleTasksPickerSelect("dvp:http://example.com/")
	m.handleTasksPickerSelect("dvp:file:///etc/passwd")
	if len(*opened) != 0 {
		t.Errorf("opened = %v, want nothing", *opened)
	}
}

func TestDevPreviewOverSSHCopiesWithForward(t *testing.T) {
	opened := withDevPreviewOpen(t)
	t.Setenv("SSH_CONNECTION", "10.0.0.2 5000 10.0.0.9 22")
	var copied []string
	defer SetClipboardSink(func(s string) { copied = append(copied, s) })()
	m := &Model{}
	m.devPreviewOpen("http://localhost:5173/")
	if len(*opened) != 0 {
		t.Errorf("opened over SSH: %v", *opened)
	}
	if len(copied) != 1 || copied[0] != "http://localhost:5173/" {
		t.Errorf("copied = %v", copied)
	}
	if s := devPreviewToastTitles(m); !strings.Contains(s, "ssh -L 5173:localhost:5173 10.0.0.9") {
		t.Errorf("toast = %q", s)
	}
}

func TestDevPreviewNewestOpensFirstThatAnswers(t *testing.T) {
	opened := withDevPreviewOpen(t)
	m := &Model{}
	msg := devPreviewMsg{newest: true,
		targets: []devpreview.Target{{URL: "http://localhost:3000/"}, {URL: "http://localhost:8080/"}},
		checks:  []devpreview.Check{{Err: "refused"}, {Up: true, Status: 200}},
	}
	m.handleDevPreviewMsg(msg)
	if len(*opened) != 1 || (*opened)[0] != "http://localhost:8080/" {
		t.Errorf("opened = %v", *opened)
	}
	if m.pickerOpen {
		t.Error("the newest command opened the list")
	}
}

func TestDevPreviewNewestNoneAnswers(t *testing.T) {
	opened := withDevPreviewOpen(t)
	m := &Model{}
	m.handleDevPreviewMsg(devPreviewMsg{newest: true,
		targets: []devpreview.Target{{URL: "http://localhost:3000/"}},
		checks:  []devpreview.Check{{Err: "refused"}},
	})
	if len(*opened) != 0 || !strings.Contains(devPreviewToastTitles(m), "No dev server answers") {
		t.Errorf("opened %v, toast %q", *opened, devPreviewToastTitles(m))
	}
}
