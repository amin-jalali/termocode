package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// svcStartMailhog starts mailhog from the view and makes the fake's ps
// say it runs (so it is a service "we started" that is up).
func svcStartMailhog(t *testing.T, d *Driver, f *servicestest.Fake) {
	t.Helper()
	svcGoto(d, "mailhog")
	d.Key("s")
	if !d.WaitFor(func() bool { return f.Count("up -d mailhog") == 1 }, 5*time.Second) {
		t.Fatalf("no up: %q", f.Calls())
	}
	ps, _ := os.ReadFile(filepath.Join(svcTestdata, "ps-lines.json"))
	f.Set(t, "ps.json", string(ps)+`{"Service":"mailhog","Name":"termocode-mailhog-1","Image":"mailhog/mailhog","State":"running","Status":"Up 2 seconds","Publishers":[]}`+"\n")
	d.Key("R")
	if !d.WaitForText("3/4 up", 5*time.Second) {
		t.Fatalf("mailhog not up:\n%s", screen(d))
	}
}

// Spec (SV-7, §1.4): quitting with a service termocode started asks;
// "Keep running and quit" quits without docker compose stop. The
// services that were up before (postgres, redis) are never listed.
func TestServicesQuitKeep(t *testing.T) {
	d, f := svcDriver(t, nil)
	svcStartMailhog(t, d, f)
	d.Key("ctrl+q")
	if !d.WaitForText("Stop the services you started?", 3*time.Second) {
		t.Fatalf("no quit card:\n%s", screen(d))
	}
	s := screen(d)
	if !strings.Contains(s, "mailhog") || strings.Contains(s, "You started: postgres") {
		t.Errorf("card lists the wrong services:\n%s", s)
	}
	clickButton(d, "Keep running and quit")
	if !d.WaitFor(d.Quit, 3*time.Second) {
		t.Fatalf("did not quit:\n%s", screen(d))
	}
	if n := f.Count(" stop"); n != 0 {
		t.Errorf("keep ran stop: %q", f.Calls())
	}
}

// Spec (SV-7): services_on_quit = stop stops only ours, then quits.
func TestServicesQuitStop(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	f := svcFake(t)
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: map[string]string{"compose.yaml": svcCompose},
		Config: map[string]string{"config.json": `{"animations": "off", "images": "blocks", "setup_done": true, "services_on_quit": "stop"}`}})
	d.Palette("services-focus")
	if !d.WaitForText("mailhog", 5*time.Second) {
		t.Fatalf("no view:\n%s", screen(d))
	}
	svcStartMailhog(t, d, f)
	d.Key("ctrl+q")
	if !d.WaitFor(d.Quit, 5*time.Second) {
		t.Fatalf("did not quit:\n%s", screen(d))
	}
	if f.Count("stop mailhog") != 1 || f.Count("stop postgres") != 0 {
		t.Errorf("calls = %q", f.Calls())
	}
}

// Spec (SV-8): a Procfile service is in LOCAL; its first start asks with
// the command line; it runs in its own tab; a non-zero exit shows
// "exit 1" on the row. The OK is kept for the next start.
func TestServicesLocalProcfile(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	svcFake(t)
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: map[string]string{"Procfile": "web: echo web-ran; exit 1\n"}})
	d.Palette("services-focus")
	if !d.WaitForText("LOCAL", 5*time.Second) {
		t.Fatalf("no LOCAL section:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "COMPOSE") {
		t.Error("no compose file: no COMPOSE section")
	}
	svcGoto(d, "web")
	d.Key("s")
	if !d.WaitForText("Run web?", 3*time.Second) || !strings.Contains(screen(d), "echo web-ran; exit 1") {
		t.Fatalf("no first-run card with the command:\n%s", screen(d))
	}
	clickButton(d, "Run")
	if !d.WaitForText("exited with code 1", 8*time.Second) {
		t.Fatalf("no exit toast:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "web          exit 1") && !strings.Contains(screen(d), "exit 1") {
		t.Errorf("no exit 1 on the row:\n%s", screen(d))
	}
	sess := readWorkspaceSession(t, d)
	if ok, _ := sess["services_local_ok"].([]any); len(ok) != 1 {
		t.Errorf("the OK is not kept: %v", sess)
	}
	// Second start: no card.
	d.Key("s")
	d.Settle(500 * time.Millisecond)
	if strings.Contains(screen(d), "Run web?") {
		t.Error("asked twice for the same command")
	}
}
