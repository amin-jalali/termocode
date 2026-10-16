package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// The Services view against a fake `docker` (internal/services/servicestest)
// on PATH: no test talks to a real Docker daemon.

// svcTestdata is internal/services/testdata, read before a driver
// changes the working directory.
var svcTestdata = func() string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "..", "services", "testdata")
}()

const svcCompose = "services:\n  postgres:\n    image: postgres:16\n  redis:\n    image: redis:7\n  api:\n    build: .\n  mailhog:\n    image: mailhog/mailhog\n"

// svcFake installs the fake docker first on PATH with the canned config
// and ps answers (postgres healthy, redis running, api exited 1, mailhog
// never created).
func svcFake(t *testing.T) *servicestest.Fake {
	t.Helper()
	f := servicestest.New(t)
	for name, src := range map[string]string{"config.json": "config.json", "ps.json": "ps-lines.json"} {
		b, err := os.ReadFile(filepath.Join(svcTestdata, src))
		if err != nil {
			t.Fatal(err)
		}
		f.Set(t, name, string(b))
	}
	t.Setenv("PATH", f.PathEnv())
	return f
}

// svcDriver starts the app on a compose project with the fake docker.
func svcDriver(t *testing.T, extra map[string]string) (*Driver, *servicestest.Fake) {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	f := svcFake(t)
	files := map[string]string{"compose.yaml": svcCompose, "a.txt": "alpha\n"}
	for k, v := range extra {
		files[k] = v
	}
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: files})
	d.Palette("services-focus")
	if !d.WaitForText("mailhog", 5*time.Second) {
		t.Fatalf("the Services view did not list the compose services:\n%s", screen(d))
	}
	return d, f
}

// svcGoto moves the cursor to the row of name (from the top).
func svcGoto(d *Driver, name string) {
	d.T.Helper()
	d.Key("g")
	for i := 0; i < 12; i++ {
		for _, line := range strings.Split(screen(d), "\n") {
			if strings.Contains(line, "▌") && strings.Contains(line, " "+name+" ") {
				return
			}
		}
		d.Key("j")
	}
	d.T.Fatalf("cursor never reached %q:\n%s", name, screen(d))
}

// Spec (SV-4/5): the view lists every declared service with its state;
// s starts one in a visible "services: up" tab, a second s while it is
// starting does nothing; x stops it with a captured `compose stop`; l
// opens a "logs: <name>" tab.
func TestServicesViewActions(t *testing.T) {
	d, f := svcDriver(t, nil)
	s := screen(d)
	for _, want := range []string{"SERVICES", "COMPOSE", "postgres", ":5432", "exit 1", "2/4 up"} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q on screen:\n%s", want, s)
		}
	}
	if f.Count("compose -f compose.yaml ps --all --format json") < 1 || f.Count("events --format") != 1 {
		t.Errorf("calls on show: %q", f.Calls())
	}

	// Start mailhog: a visible tab runs `up -d mailhog`.
	f.Set(t, "up.sleep", "3")
	svcGoto(d, "mailhog")
	d.Key("s")
	if !d.WaitForText("services: up", 5*time.Second) {
		t.Fatalf("no services: up tab:\n%s", screen(d))
	}
	if !d.WaitFor(func() bool { return f.Count("up -d mailhog") == 1 }, 5*time.Second) {
		t.Fatalf("calls = %q", f.Calls())
	}
	// Busy: a second s while it starts runs nothing.
	d.Key("s")
	d.Settle(300 * time.Millisecond)
	if n := f.Count("up -d"); n != 1 {
		t.Errorf("second s ran up again (%d calls)", n)
	}

	// Stop postgres: captured `compose stop postgres`, then a refresh.
	svcGoto(d, "postgres")
	before := f.Count("ps --all")
	d.Key("x")
	if !d.WaitFor(func() bool { return f.Count("stop postgres") == 1 && f.Count("ps --all") > before }, 5*time.Second) {
		t.Fatalf("no stop / refresh: %q", f.Calls())
	}

	// Logs: a logs: postgres tab.
	d.Key("l")
	if !d.WaitForText("logs: postgres", 5*time.Second) {
		t.Fatalf("no logs tab:\n%s", screen(d))
	}
	if f.Count("logs -f --tail 200 postgres") != 1 {
		t.Errorf("calls = %q", f.Calls())
	}
}

// Spec (SV-10): hiding the view kills the docker events watcher; no
// process stays behind.
func TestServicesWatcherStopsWhenHidden(t *testing.T) {
	d, f := svcDriver(t, nil)
	if !servicestest.WaitFor(5*time.Second, func() bool { return f.PID("events.pid") > 0 }) {
		t.Fatal("docker events never started")
	}
	pid := f.PID("events.pid")
	if !servicestest.Alive(pid) {
		t.Fatal("watcher not alive while the view shows")
	}
	d.Action("view:files")
	d.Settle(200 * time.Millisecond)
	if !servicestest.WaitGone(pid, 3*time.Second) {
		t.Errorf("docker events (pid %d) still runs after the view hid", pid)
	}
	// An event pushed now reaches nobody: no refresh happens.
	before := f.Count("ps --all")
	f.PushEvent(t, `{"Type":"container","Action":"die"}`)
	d.Settle(500 * time.Millisecond)
	if n := f.Count("ps --all"); n != before {
		t.Errorf("hidden view refreshed: %d → %d", before, n)
	}
	// Shown again: a new watcher and a refresh.
	d.Palette("services-focus")
	if !servicestest.WaitFor(5*time.Second, func() bool { return f.Count("events --format") == 2 }) {
		t.Errorf("no new watcher: %q", f.Calls())
	}
}

// Spec (SV-10): a docker event refreshes the view (debounced: a burst
// is one `compose ps`).
func TestServicesEventRefreshes(t *testing.T) {
	d, f := svcDriver(t, nil)
	if !servicestest.WaitFor(5*time.Second, func() bool { return f.PID("events.pid") > 0 }) {
		t.Fatal("docker events never started")
	}
	time.Sleep(200 * time.Millisecond) // tail is following
	before := f.Count("ps --all")
	ps, _ := os.ReadFile(filepath.Join(svcTestdata, "ps-lines.json"))
	f.Set(t, "ps.json", strings.Replace(string(ps), `"State":"exited","Status":"Exited (1) 5 minutes ago"`, `"State":"running","Status":"Up 3 seconds"`, 1))
	for i := 0; i < 5; i++ {
		f.PushEvent(t, `{"Type":"container","Action":"start","Actor":{"Attributes":{"com.docker.compose.service":"api"}}}`)
	}
	if !d.WaitForText("3/4 up", 5*time.Second) {
		_, ups := d.Updates()
		t.Fatalf("event did not refresh: calls %q\nupdates %q\n%s", f.Calls(), ups[max(len(ups)-30, 0):], screen(d))
	}
	d.Settle(400 * time.Millisecond)
	if n := f.Count("ps --all") - before; n != 1 {
		t.Errorf("5 events → %d refreshes, want 1 (debounce)", n)
	}
}

// Spec (§1.3): Docker not running shows a note and keeps the rows (as ?).
func TestServicesDaemonDown(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	f := svcFake(t)
	f.Set(t, "all.err", "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?\n")
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: map[string]string{"compose.yaml": svcCompose}})
	d.Palette("services-focus")
	if !d.WaitForText("Docker is not running", 5*time.Second) {
		t.Fatalf("no daemon-down note:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "Install Docker") {
		t.Error("wrong hint")
	}
}

// Spec (§1.3): no compose file, Procfile or services.json → the empty
// state; o writes the commented services.json template and opens it.
func TestServicesEmptyStateConfigure(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	svcFake(t)
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: map[string]string{"a.txt": "x\n"}})
	d.Palette("services-focus")
	if !d.WaitForText("No services", 5*time.Second) {
		t.Fatalf("no empty state:\n%s", screen(d))
	}
	d.Key("o")
	if !d.WaitFor(func() bool { return strings.Contains(diskFile(d, ".termocode/services.json"), `"services"`) }, 3*time.Second) {
		t.Fatal("services.json not written")
	}
	name := func() string { return lua(d, `return vim.api.nvim_buf_get_name(0)`) }
	if !eventually(d, func() bool { return strings.HasSuffix(name(), ".termocode/services.json") }) {
		t.Errorf("services.json not opened (current buffer %q)\n%s", name(), screen(d))
	}
}
