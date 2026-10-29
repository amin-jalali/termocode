package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// covSvcProject is the PROJECT part (compose + local) with the fake
// docker on PATH, a private HOME and a real project folder.
func covSvcProject(t *testing.T) (*Model, *servicestest.Fake) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_CONFIG", filepath.Join(home, "docker"))
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	f := servicestest.New(t)
	t.Setenv("PATH", f.PathEnv())
	st := svcTestState()
	st.root = t.TempDir()
	mm := svcTestModel(st, 120, 50)
	return &mm, f
}

// covSvcNvim gives m a real headless nvim (closed at the end), so
// terminal tabs really start. It skips without nvim.
func covSvcNvim(t *testing.T, m *Model) *nvim.Client {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	t.Setenv("SHELL", "/bin/sh")
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(120, 40); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	m.nvim, m.terminalRows, m.output = c, 8, newOutputStore()
	return c
}

func covSvcTab(m *Model, key string) (terminalTab, bool) {
	for _, t := range m.terminalTabs {
		if t.Service == key {
			return t, true
		}
	}
	return terminalTab{}, false
}

func covSvcConfirm(t *testing.T, m *Model, id string) tea.Cmd {
	t.Helper()
	cmd, ok := m.handleServicesConfirm(id)
	if !ok {
		t.Fatalf("confirm kind %d not handled", m.confirmKind)
	}
	return cmd
}

// Without an editor no terminal tab can start: every visible command
// says so instead of failing quietly.
func TestCovSvcProjectCommandsWithoutEditor(t *testing.T) {
	m, _ := covSvcProject(t)
	st := m.services
	note, cmd := m.servicesStart(svcSecCompose, "mailhog")
	if note != "" || cmd == nil || !strings.Contains(regToast(m), "Cannot start services") {
		t.Errorf("start: note=%q toast=%q", note, regToast(m))
	}
	if st.busy[svcKey(svcSecCompose, "mailhog")].Busy() {
		t.Error("a failed start must not mark the service busy")
	}
	if cmd := m.servicesLogs(svcSecCompose, "redis"); cmd == nil || !strings.Contains(regToast(m), "Cannot show logs") {
		t.Errorf("logs toast = %q", regToast(m))
	}
	if cmd := m.servicesShell(svcSecCompose, "redis"); cmd == nil || !strings.Contains(regToast(m), "Cannot open a shell") {
		t.Errorf("shell toast = %q", regToast(m))
	}
	if cmd := m.servicesShell(svcSecLocal, "web"); cmd == nil || !strings.Contains(regToast(m), "No shell for local services") {
		t.Errorf("local shell toast = %q", regToast(m))
	}
	st.localOK[svcCommandHash(st.local[0])] = true
	if _, cmd := m.servicesStartLocal("web", false); cmd == nil || !strings.Contains(regToast(m), "Cannot start web") {
		t.Errorf("local start toast = %q", regToast(m))
	}
	if cmd := m.servicesLogs(svcSecLocal, "web"); cmd == nil || !strings.Contains(regToast(m), `"web" has not run yet`) {
		t.Errorf("local logs toast = %q", regToast(m))
	}
	m.services.menuRow = svcRow{kind: svcRowService, section: svcSecCompose, name: "api"}
	if cmd := m.handleServicesMenuSelect("recreate"); cmd == nil || !strings.Contains(regToast(m), "Cannot recreate") {
		t.Errorf("recreate toast = %q", regToast(m))
	}
}

// The "nothing to do" answers of start, stop and restart.
func TestCovSvcProjectNotes(t *testing.T) {
	m, f := covSvcProject(t)
	st := m.services
	cases := []struct {
		note string
		run  func() (string, tea.Cmd)
	}{
		{`service "redis" is already running`, func() (string, tea.Cmd) { return m.servicesStart(svcSecCompose, "redis") }},
		{`no local service "nope"`, func() (string, tea.Cmd) { return m.servicesStart(svcSecLocal, "nope") }},
		{`service "web" is not running`, func() (string, tea.Cmd) { return m.servicesStop(svcSecLocal, "web", nil) }},
	}
	for _, c := range cases {
		if note, cmd := c.run(); note != c.note || cmd != nil {
			t.Errorf("note = %q (cmd %v), want %q", note, cmd != nil, c.note)
		}
	}
	st.busy[svcKey(svcSecCompose, "api")] = services.Starting
	if note, _ := m.servicesStart(svcSecCompose, "api"); !strings.Contains(note, `"api" is busy`) {
		t.Errorf("busy start note = %q", note)
	}
	if note, _ := m.servicesRestart(svcSecCompose, "api", nil); !strings.Contains(note, `"api" is busy`) {
		t.Errorf("busy restart note = %q", note)
	}
	st.busy[svcKey(svcSecCompose, "api")] = services.Stopping
	if note, _ := m.servicesStop(svcSecCompose, "api", nil); note != `service "api" is already stopping` {
		t.Errorf("stopping note = %q", note)
	}
	delete(st.busy, svcKey(svcSecCompose, "api"))
	// A running "up" tab blocks a second up.
	m.terminalTabs = []terminalTab{{Service: svcTabUp, Running: true}}
	if note, cmd := m.servicesStart(svcSecCompose, "mailhog"); !strings.Contains(note, "still running") || cmd != nil {
		t.Errorf("up running note = %q", note)
	}
	m.terminalTabs = nil
	// No compose file: every compose verb says so.
	st.det.Compose = false
	for _, run := range []func() (string, tea.Cmd){
		func() (string, tea.Cmd) { return m.servicesStart(svcSecCompose, "api") },
		func() (string, tea.Cmd) { return m.servicesStop(svcSecCompose, "api", nil) },
		func() (string, tea.Cmd) { return m.servicesRestart(svcSecCompose, "api", nil) },
	} {
		if note, _ := run(); note != "Docker Compose is not available here" {
			t.Errorf("no compose note = %q", note)
		}
	}
	if cmd := m.servicesLogs(svcSecCompose, "api"); cmd == nil || !strings.Contains(regToast(m), "Docker Compose is not available") {
		t.Errorf("no compose logs toast = %q", regToast(m))
	}
	if len(f.Calls()) != 0 {
		t.Errorf("notes ran docker: %q", f.Calls())
	}
}

// Stop and restart run captured compose commands; a failure is a toast
// and the done callback runs once the next snapshot is in.
func TestCovSvcProjectStopRestartRunCompose(t *testing.T) {
	m, f := covSvcProject(t)
	st := m.services
	var doneErr error
	called := 0
	note, cmd := m.servicesStop(svcSecCompose, "", func(_ *Model, err error) { called++; doneErr = err })
	if note != "" || cmd == nil {
		t.Fatalf("stop all: %q", note)
	}
	for _, k := range []string{"postgres", "redis"} {
		if st.busy[svcKey(svcSecCompose, k)] != services.Stopping {
			t.Errorf("%s not stopping: %v", k, st.busy)
		}
	}
	if st.busy[svcKey(svcSecCompose, "api")].Busy() {
		t.Error("stop all marks a stopped service busy")
	}
	msg := cmd().(servicesActionDoneMsg)
	if msg.err != nil || msg.verb != "stop" {
		t.Fatalf("stop msg = %+v", msg)
	}
	if f.Count("compose") == 0 || !strings.Contains(strings.Join(f.Calls(), "\n"), " stop") {
		t.Errorf("calls = %q", f.Calls())
	}
	m.onServicesActionDone(msg)
	if len(st.busy) != 0 || len(st.waiters) != 1 {
		t.Fatalf("after stop: busy=%v waiters=%d", st.busy, len(st.waiters))
	}
	st.waiters[0](m)
	if called != 1 || doneErr != nil {
		t.Errorf("done: %d %v", called, doneErr)
	}

	// restart fails: the error is a toast.
	f.Set(t, "restart.err", "no such service: redis")
	_, cmd = m.servicesRestart(svcSecCompose, "redis", nil)
	if st.busy[svcKey(svcSecCompose, "redis")] != services.Restarting {
		t.Errorf("redis not restarting")
	}
	msg = cmd().(servicesActionDoneMsg)
	if msg.err == nil {
		t.Fatal("restart error lost")
	}
	m.onServicesActionDone(msg)
	if !strings.Contains(regToast(m), "docker compose restart failed") {
		t.Errorf("toast = %q", regToast(m))
	}

}

// Stop all (A) and the Down card: Cancel runs nothing, Down runs
// `compose down` and marks every service stopping.
func TestCovSvcProjectDownCard(t *testing.T) {
	m, f := covSvcProject(t)
	st := m.services
	m.servicesAskDown()
	if !m.confirmOpen || m.confirmKind != confirmKindServicesDown {
		t.Fatal("no down card")
	}
	if cmd := covSvcConfirm(t, m, "cancel"); cmd != nil || len(f.Calls()) != 0 {
		t.Fatalf("cancel ran %q", f.Calls())
	}
	cmd := covSvcConfirm(t, m, "down")
	for _, s := range st.compose {
		if st.busy[svcKey(svcSecCompose, s.Name)] != services.Stopping {
			t.Errorf("%s not stopping", s.Name)
		}
	}
	msg := cmd().(servicesActionDoneMsg)
	if msg.verb != "down" || msg.err != nil || !strings.Contains(strings.Join(f.Calls(), "\n"), " down") {
		t.Fatalf("down msg=%+v calls=%q", msg, f.Calls())
	}
	st.startedByUs["redis"] = true
	m.onServicesActionDone(msg)
	if st.startedByUs["redis"] || len(st.busy) != 0 {
		t.Errorf("down left state: %v %v", st.startedByUs, st.busy)
	}
	// No compose: Down does nothing.
	st.det.Compose = false
	m.confirmKind = confirmKindServicesDown
	if cmd := covSvcConfirm(t, m, "down"); cmd != nil {
		t.Error("down without compose returned a command")
	}
}

// The local run card: Cancel runs nothing; Run remembers the command.
func TestCovSvcLocalRunCard(t *testing.T) {
	m, _ := covSvcProject(t)
	st := m.services
	note, cmd := m.servicesStart(svcSecLocal, "web")
	if cmd != nil || note != "asked the user to allow the command first" || !m.confirmOpen {
		t.Fatalf("first start: %q open=%v", note, m.confirmOpen)
	}
	if !strings.Contains(cardText(m), "npm run dev") {
		t.Errorf("card = %s", cardText(m))
	}
	if cmd := covSvcConfirm(t, m, "cancel"); cmd != nil || st.localOK[svcCommandHash(st.local[0])] {
		t.Error("cancel allowed the command")
	}
	m.servicesStart(svcSecLocal, "web")
	st.pendingLocal = "gone"
	if cmd := covSvcConfirm(t, m, "run"); cmd != nil {
		t.Error("run of a removed service did something")
	}
}

// Copy address: the published port, the probe port, nothing; over SSH
// the toast tells the tunnel command.
func TestCovSvcCopyAddress(t *testing.T) {
	m, _ := covSvcProject(t)
	var clip []string
	defer SetClipboardSink(func(s string) { clip = append(clip, s) })()
	if cmd := m.servicesCopyAddress(svcSecCompose, "postgres"); cmd == nil || !strings.Contains(regToast(m), "Copied localhost:5432") {
		t.Errorf("toast = %q", regToast(m))
	}
	m.services.local[0].Ports = nil
	if addr, port := m.servicesAddress(svcSecLocal, "web"); addr != "localhost:5173" || port != 5173 {
		t.Errorf("probe address = %q %d", addr, port)
	}
	m.services.compose[3].Ports = nil
	m.servicesCopyAddress(svcSecCompose, "mailhog")
	if !strings.Contains(regToast(m), "No published port") {
		t.Errorf("no port toast = %q", regToast(m))
	}
	t.Setenv("SSH_CONNECTION", "10.0.0.1 5555 10.0.0.9 22")
	m.servicesCopyAddress(svcSecCompose, "redis")
	if !strings.Contains(regToast(m), "ssh -L 6379:localhost:6379 10.0.0.9") {
		t.Errorf("ssh toast = %q", regToast(m))
	}
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "/dev/pts/1")
	m.servicesCopyAddress(svcSecCompose, "redis")
	if !strings.Contains(regToast(m), "<host>") {
		t.Errorf("ssh tty toast = %q", regToast(m))
	}
	if strings.Join(clip, ",") != "localhost:5432,localhost:6379,localhost:6379" {
		t.Errorf("clipboard = %q", clip)
	}
}

// o opens the compose file at the service's line, or the local
// service's source line; a missing local service opens services.json.
func TestCovSvcOpenSource(t *testing.T) {
	m, _ := covSvcProject(t)
	st := m.services
	compose := "services:\n  postgres:\n    image: postgres\n  redis:\n    image: redis\n"
	if err := os.WriteFile(filepath.Join(st.root, "compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.root, "Procfile"), []byte("api: go run .\nweb: npm run dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := covSvcNvim(t, m)
	var opened []string
	at := func() {
		out, _ := c.EvalLuaString(`return vim.fn.expand('%:t') .. ':' .. vim.fn.line('.')`)
		opened = append(opened, out)
	}
	m.servicesOpenSource(svcSecCompose, "redis")
	at()
	m.servicesOpenSource(svcSecCompose, "")
	at()
	m.servicesOpenSource(svcSecLocal, "web")
	at()
	st.det.ComposeFiles = []string{filepath.Join(st.root, "compose.yaml")}
	m.servicesOpenSource(svcSecCompose, "postgres")
	at()
	if got := strings.Join(opened, ","); got != "compose.yaml:4,compose.yaml:1,Procfile:2,compose.yaml:2" {
		t.Errorf("opened = %s", got)
	}
	// An unknown local service opens (and creates) services.json.
	m.servicesOpenSource(svcSecLocal, "nope")
	if _, err := os.Stat(filepath.Join(st.root, services.ConfigPath)); err != nil {
		t.Errorf("services.json not created: %v", err)
	}
}

// With a real editor the visible commands open terminal tabs: compose
// up marks services starting, logs and shell get their own tabs, a local
// service runs and stops through its job.
func TestCovSvcProjectTerminalTabs(t *testing.T) {
	m, _ := covSvcProject(t)
	covSvcNvim(t, m)
	st := m.services
	note, _ := m.servicesStart(svcSecCompose, "")
	if note != "" {
		t.Fatalf("start all: %q", note)
	}
	if _, ok := covSvcTab(m, svcTabUp); !ok {
		t.Fatalf("no up tab: %+v", m.terminalTabs)
	}
	if st.busy[svcKey(svcSecCompose, "api")] != services.Starting || st.busy[svcKey(svcSecCompose, "mailhog")] != services.Starting || !st.startedByUs["api"] {
		t.Errorf("start all busy = %v", st.busy)
	}
	if st.busy[svcKey(svcSecCompose, "redis")].Busy() {
		t.Error("a running service is marked starting")
	}
	if cmd := m.servicesLogs(svcSecCompose, ""); cmd != nil {
		t.Errorf("logs: %q", regToast(m))
	}
	if _, ok := covSvcTab(m, svcLogsTab("compose")); !ok {
		t.Errorf("no compose logs tab")
	}
	// A second logs on a running tab only focuses it.
	n := len(m.terminalTabs)
	m.servicesLogs(svcSecCompose, "")
	if len(m.terminalTabs) != n {
		t.Errorf("logs opened a second tab")
	}
	if cmd := m.servicesShell(svcSecCompose, "redis"); cmd != nil {
		t.Errorf("shell: %q", regToast(m))
	}
	if _, ok := covSvcTab(m, "shell:redis"); !ok {
		t.Error("no shell tab")
	}
	m.services.menuRow = svcRow{kind: svcRowService, section: svcSecCompose, name: "api"}
	m.handleServicesMenuSelect("recreate")
	// Local: allow, run, logs focus, stop.
	st.local[0].Cwd = "sub"
	if err := os.Mkdir(filepath.Join(st.root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	st.local[0].Env = map[string]string{"PORT": "5173"}
	st.localOK[svcCommandHash(st.local[0])] = true
	note, cmd := m.servicesStart(svcSecLocal, "web")
	if note != "" || cmd == nil {
		t.Fatalf("local start: %q", note)
	}
	tab, ok := covSvcTab(m, svcLocalTab("web"))
	if !ok || tab.Cwd != filepath.Join(st.root, "sub") {
		t.Fatalf("local tab = %+v toast=%q", tab, regToast(m))
	}
	if note, _ := m.servicesStart(svcSecLocal, "web"); !strings.Contains(note, "already running") {
		t.Errorf("second start: %q", note)
	}
	if cmd := m.servicesLogs(svcSecLocal, "web"); cmd != nil {
		t.Errorf("local logs: %q", regToast(m))
	}
	stopped := false
	note, _ = m.servicesStop(svcSecLocal, "web", func(*Model, error) { stopped = true })
	if note != "" || !st.localStop["web"] || st.busy[svcKey(svcSecLocal, "web")] != services.Stopping {
		t.Fatalf("local stop: %q %v", note, st.busy)
	}
	for _, w := range st.waiters {
		w(m)
	}
	if !stopped {
		t.Error("stop done not called")
	}
}
