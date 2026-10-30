//go:build unix

package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/creack/pty"

	"github.com/amin-jalali/termocode/internal/session"
)

// covRun is one run() call with its output and what it started.
type covRun struct {
	code     int
	stdout   string
	stderr   string
	clients  []string // dirs startClient got
	opts     []clientOpts
	servers  []string
	tuiStart int
}

// covMainEnv isolates run(): temp HOME / XDG dirs and session dir, no
// terminal on stdin / stdout, and fakes for the parts that take over
// the terminal. It restores os.Args, the folder and the env.
func covMainEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	sessionEnv(t)
	t.Setenv(envNoSession, "")
	t.Setenv(session.EnvSession, "")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(wd)
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	oldC, oldS, oldT := startClient, startServer, startTUI
	t.Cleanup(func() { startClient, startServer, startTUI = oldC, oldS, oldT })
}

// covCall runs run(args) with stdout / stderr captured. in replaces
// os.Stdin and out os.Stdout's terminal check (nil: plain pipes).
func covCall(t *testing.T, args []string, in *os.File) covRun {
	t.Helper()
	var r covRun
	startClient = func(dir string, o clientOpts) int {
		r.clients = append(r.clients, dir)
		r.opts = append(r.opts, o)
		return 7
	}
	startServer = func(dir string) int { r.servers = append(r.servers, dir); return 8 }
	startTUI = func() { r.tuiStart++ }

	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	defer func() { os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr }()
	if in == nil {
		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer pr.Close()
		defer pw.Close()
		in = pr
	}
	os.Stdin = in
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdout, os.Stderr = outW, errW
	outC, errC := make(chan string), make(chan string)
	go func() { b, _ := io.ReadAll(outR); outC <- string(b) }()
	go func() { b, _ := io.ReadAll(errR); errC <- string(b) }()
	r.code = run(args)
	_ = outW.Close()
	_ = errW.Close()
	r.stdout, r.stderr = <-outC, <-errC
	return r
}

func TestCovCoreRunVersion(t *testing.T) {
	covMainEnv(t)
	for _, a := range []string{"--version", "-v", "version"} {
		r := covCall(t, []string{a}, nil)
		if r.code != 0 || r.stdout != "termocode "+versionString()+"\n" || r.tuiStart != 0 {
			t.Errorf("%s: %+v", a, r)
		}
	}
}

func TestCovCoreRunHelp(t *testing.T) {
	covMainEnv(t)
	for _, a := range []string{"-h", "--help", "help"} {
		r := covCall(t, []string{a}, nil)
		if r.code != 0 || r.tuiStart != 0 || len(r.clients) != 0 {
			t.Errorf("%s: code %d, tui %d, clients %q", a, r.code, r.tuiStart, r.clients)
		}
		for _, w := range []string{"Usage:", "termocode attach [dir]", "termocode ssh [--dir d]", "termocode --version", "persistent_session: auto"} {
			if !strings.Contains(r.stdout, w) {
				t.Errorf("%s: usage misses %q:\n%s", a, w, r.stdout)
			}
		}
	}
}

func TestCovCoreRunMissingFolder(t *testing.T) {
	covMainEnv(t)
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "gone")
	for _, args := range [][]string{
		{"attach", missing},
		{"kill", missing},
		{"attach", file},
		{"--link", missing},
		{"--link", file},
	} {
		r := covCall(t, args, nil)
		bad := args[len(args)-1]
		if r.code != 1 || r.stderr != "termocode: "+bad+" is not a folder\n" || len(r.clients) != 0 {
			t.Errorf("%q: %+v", args, r)
		}
	}
}

func TestCovCoreRunAttachAndLink(t *testing.T) {
	covMainEnv(t)
	dir := t.TempDir()
	r := covCall(t, []string{"attach", dir}, nil)
	if r.code != 7 || len(r.clients) != 1 || r.clients[0] != dir || r.opts[0] != (clientOpts{}) {
		t.Errorf("attach: %+v", r)
	}
	// attach with no folder is the current one.
	t.Chdir(dir)
	r = covCall(t, []string{"attach"}, nil)
	if len(r.clients) != 1 || r.clients[0] != "." {
		t.Errorf("attach without dir: %+v", r)
	}
	// The far end of `termocode ssh`: a linked client that makes the
	// session, with the reconnect note.
	r = covCall(t, []string{"--link", "--reconnected=12s,dropped", dir}, nil)
	want := clientOpts{create: true, link: true, note: session.ReconnectNote(12e9, true)}
	if r.code != 7 || len(r.opts) != 1 || r.opts[0] != want || r.clients[0] != dir {
		t.Errorf("link: %+v, want %+v", r, want)
	}
	r = covCall(t, []string{"--link"}, nil)
	if len(r.clients) != 1 || r.clients[0] != "." {
		t.Errorf("link without dir: %+v", r)
	}
	// termocode ssh --web: the port of the forward, never a folder.
	r = covCall(t, []string{"--link", "--web=41237", dir}, nil)
	want = clientOpts{create: true, link: true, webPort: 41237}
	if len(r.opts) != 1 || r.opts[0] != want || r.clients[0] != dir {
		t.Errorf("link --web: %+v, want %+v", r, want)
	}
}

func TestCovCoreRunServer(t *testing.T) {
	covMainEnv(t)
	r := covCall(t, []string{serverArg, "/w"}, nil)
	if r.code != 8 || len(r.servers) != 1 || r.servers[0] != "/w" || r.tuiStart != 0 {
		t.Errorf("server: %+v", r)
	}
}

func TestCovCoreRunSessionsAndKill(t *testing.T) {
	covMainEnv(t)
	r := covCall(t, []string{"sessions"}, nil)
	if r.code != 0 || r.stdout != "no running sessions\n" {
		t.Errorf("sessions: %+v", r)
	}
	dir := t.TempDir()
	r = covCall(t, []string{"kill", dir}, nil)
	if r.code != 1 || !strings.HasPrefix(r.stderr, "termocode: no session for ") || !strings.Contains(r.stderr, filepath.Base(dir)) {
		t.Errorf("kill: %+v", r)
	}
}

func TestCovCoreRunSSHErrors(t *testing.T) {
	covMainEnv(t)
	r := covCall(t, []string{"ssh"}, nil)
	if r.code != 2 || !strings.Contains(r.stderr, "usage: termocode ssh [--dir DIR] [--web] [ssh options] destination") {
		t.Errorf("ssh without host: %+v", r)
	}
	r = covCall(t, []string{"ssh", "--dir", "/x"}, nil)
	if r.code != 2 {
		t.Errorf("ssh --dir without host: %+v", r)
	}
	r = covCall(t, []string{"ssh", "host"}, nil)
	if r.code != 1 || r.stderr != "termocode: ssh needs a terminal\n" {
		t.Errorf("ssh without a terminal: %+v", r)
	}
}

func TestCovCoreRunMCPBadFlag(t *testing.T) {
	covMainEnv(t)
	r := covCall(t, []string{"mcp", "--bogus"}, nil)
	if r.code != 2 || r.stderr == "" {
		t.Errorf("mcp --bogus: %+v", r)
	}
}

func TestCovCoreRunSetupRoute(t *testing.T) {
	covMainEnv(t)
	r := covCall(t, []string{"setup", "--install"}, nil)
	if r.code != 2 || !strings.Contains(r.stdout, "usage: termocode setup --install <name>") {
		t.Errorf("setup --install: %+v", r)
	}
}

func TestCovCoreRunOpensFolder(t *testing.T) {
	covMainEnv(t)
	dir := t.TempDir()
	r := covCall(t, []string{"--no-session", dir}, nil)
	if r.code != 0 || r.tuiStart != 1 || len(r.clients) != 0 {
		t.Errorf("open folder: %+v", r)
	}
	wd, _ := os.Getwd()
	want, _ := filepath.EvalSymlinks(dir)
	if got, _ := filepath.EvalSymlinks(wd); got != want {
		t.Errorf("cwd = %q, want %q", got, want)
	}
	if os.Getenv(envNoSession) != "1" {
		t.Error("--no-session was not kept for a reload")
	}
	if len(os.Args) != 1 {
		t.Errorf("os.Args still holds the folder: %q", os.Args)
	}
	// A folder that is gone (moved or deleted) is an error: the UI does
	// not open the current folder instead, and the test process lives on.
	r = covCall(t, []string{"no-such-folder"}, nil)
	if r.code != 1 || r.tuiStart != 0 || !strings.Contains(r.stderr, "folder no-such-folder not found") {
		t.Errorf("gone folder arg: %+v", r)
	}
}

// A terminal with persistent_session on starts the UI in a session.
func TestCovCoreRunInSession(t *testing.T) {
	covMainEnv(t)
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"persistent_session":"on"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ptm, tty, err := pty.Open()
	if err != nil {
		t.Skip("no pty:", err)
	}
	defer ptm.Close()
	defer tty.Close()
	dir := t.TempDir()
	t.Chdir(dir)
	r := covCallTTY(t, tty)
	if r.code != 7 || r.tuiStart != 0 || len(r.opts) != 1 || !r.opts[0].create {
		t.Fatalf("session start: %+v", r)
	}
	got, _ := filepath.EvalSymlinks(r.clients[0])
	want, _ := filepath.EvalSymlinks(dir)
	if got != want {
		t.Errorf("session for %q, want %q", got, want)
	}
	// --no-session wins over the setting.
	r = covCallTTY(t, tty, "--no-session")
	if r.tuiStart != 1 || len(r.clients) != 0 {
		t.Errorf("--no-session in a terminal: %+v", r)
	}
}

// covCallTTY is covCall with both stdin and stdout on tty.
func covCallTTY(t *testing.T, tty *os.File, args ...string) covRun {
	t.Helper()
	var r covRun
	startClient = func(dir string, o clientOpts) int {
		r.clients = append(r.clients, dir)
		r.opts = append(r.opts, o)
		return 7
	}
	startTUI = func() { r.tuiStart++ }
	oldIn, oldOut := os.Stdin, os.Stdout
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	os.Stdin, os.Stdout = tty, tty
	r.code = run(args)
	return r
}

func TestCovCoreRunClientRefusesAndMissingSession(t *testing.T) {
	covMainEnv(t)
	dir := t.TempDir()
	// No session and none to make.
	code, errOut := covStderr(t, func() int { return runClient(dir, clientOpts{}) })
	if code != 1 || !strings.HasPrefix(errOut, "termocode: no session for ") {
		t.Errorf("no session: %d %q", code, errOut)
	}
	// Inside this very session (its terminal panel).
	p, err := session.PathsFor(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(session.EnvSession, p.Key)
	code, errOut = covStderr(t, func() int { return runClient(dir, clientOpts{create: true}) })
	if code != 1 || !strings.Contains(errOut, "already inside this session") {
		t.Errorf("same session: %d %q", code, errOut)
	}
	t.Setenv(session.EnvSession, "other-key")
	code, errOut = covStderr(t, func() int { return runClient(dir, clientOpts{create: true}) })
	if code != 1 || !strings.Contains(errOut, "cannot attach from inside another session") {
		t.Errorf("other session: %d %q", code, errOut)
	}
}

func covStderr(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	c := make(chan string)
	go func() { b, _ := io.ReadAll(r); c <- string(b) }()
	code := fn()
	_ = w.Close()
	os.Stderr = old
	return code, <-c
}

func TestCovCoreProfiles(t *testing.T) {
	dir := t.TempDir()
	// Off by default.
	t.Setenv("TERMOCODE_CPUPROFILE", "")
	t.Setenv("TERMOCODE_MEMPROFILE", "")
	startCPUProfile()()
	writeHeapProfile()
	// Paths that cannot be made.
	t.Setenv("TERMOCODE_CPUPROFILE", filepath.Join(dir, "no", "cpu.out"))
	t.Setenv("TERMOCODE_MEMPROFILE", filepath.Join(dir, "no", "mem.out"))
	startCPUProfile()()
	writeHeapProfile()
	// Real files.
	cpu, mem := filepath.Join(dir, "cpu.out"), filepath.Join(dir, "mem.out")
	t.Setenv("TERMOCODE_CPUPROFILE", cpu)
	t.Setenv("TERMOCODE_MEMPROFILE", mem)
	stop := startCPUProfile()
	// A second profile while one runs: refused, a no-op stop.
	t.Setenv("TERMOCODE_CPUPROFILE", filepath.Join(dir, "cpu2.out"))
	startCPUProfile()()
	stop()
	stop() // safe twice
	writeHeapProfile()
	for _, f := range []string{cpu, mem} {
		if fi, err := os.Stat(f); err != nil || fi.Size() == 0 {
			t.Errorf("profile %s not written: %v", f, err)
		}
	}
}

func TestCovCoreTuneGC(t *testing.T) {
	old := debug.SetGCPercent(100)
	oldLimit := debug.SetMemoryLimit(-1)
	defer func() { debug.SetGCPercent(old); debug.SetMemoryLimit(oldLimit) }()
	t.Setenv("GOGC", "")
	t.Setenv("GOMEMLIMIT", "")
	tuneGC()
	if got := debug.SetGCPercent(100); got != 200 {
		t.Errorf("GOGC = %d, want 200", got)
	}
	if got := debug.SetMemoryLimit(-1); got != 128<<20 {
		t.Errorf("memory limit = %d", got)
	}
	// The user's GOGC / GOMEMLIMIT win.
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(1 << 30)
	t.Setenv("GOGC", "50")
	t.Setenv("GOMEMLIMIT", "1GiB")
	tuneGC()
	if got := debug.SetGCPercent(-1); got != 50 {
		t.Errorf("GOGC overridden: %d", got)
	}
	if got := debug.SetMemoryLimit(-1); got != 1<<30 {
		t.Errorf("GOMEMLIMIT overridden: %d", got)
	}
}

func TestCovCoreInputOptionNoTerminal(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	old := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = old }()
	if inputOption() == nil {
		t.Error("nil option")
	}
}
