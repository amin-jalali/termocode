package main

import (
	"errors"
	"github.com/amin-jalali/termocode/internal/termcaps"
	"github.com/amin-jalali/termocode/internal/termimg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// run hands each subcommand its arguments; with --help every one of them
// answers at once and the TUI never starts.
func TestRunDispatchesEveryCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	old := startTUI
	startTUI = func() { t.Fatal("the TUI started for a subcommand") }
	t.Cleanup(func() { startTUI = old })
	for _, cmd := range []string{
		"agent", "acp", "ai", "worktree", "task", "tasks", "lane", "workflow", "workflows", "import",
		"review", "http", "db", "web", "service", "serve", "runner", "mcp",
	} {
		if code := run([]string{cmd, "--help"}); code != 0 && code != 2 {
			t.Errorf("%s --help: %d", cmd, code)
		}
	}
}

// `termocode ai` in a folder that has a folder called ai opens that folder.
func TestRunAIWithAFolderNamedAI(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(dir+"/ai", 0o755); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	started := false
	oldTUI, oldClient := startTUI, startClient
	startTUI = func() { started = true }
	startClient = func(string, clientOpts) int { started = true; return 0 }
	t.Cleanup(func() { startTUI, startClient = oldTUI, oldClient })
	t.Setenv("HOME", t.TempDir())
	t.Setenv(envNoSession, "1")
	run([]string{"ai"})
	if !started {
		t.Fatal("termocode ai with a folder named ai did not open the folder")
	}
}

func TestProgramOptionHelpers(t *testing.T) {
	for _, on := range []bool{true, false} {
		var _ tea.ProgramOption = syncOutputOption(on)
		var _ tea.ProgramOption = kittyKeyboardOption(on)
		var _ tea.ProgramOption = mouseOption(on)
	}
	if !keysWanted(func(string) string { return "" }) || keysWanted(func(string) string { return "legacy" }) {
		t.Fatal("keysWanted")
	}
}

// runTUI builds the program and runs it; a clean end stops the helpers, an
// error or a panic keeps the work and exits with 1.
func TestRunTUIEnds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("TERMOCODE_ICON_MODE", "ascii")
	oldRun, oldExit := runProgram, exitProcess
	t.Cleanup(func() { runProgram, exitProcess = oldRun, oldExit })
	var exited []int
	exitProcess = func(c int) { exited = append(exited, c) }

	runProgram = func(*tea.Program) (tea.Model, error) { return nil, nil }
	runTUI()
	if len(exited) != 0 {
		t.Fatalf("a clean run exited: %v", exited)
	}
	runProgram = func(*tea.Program) (tea.Model, error) { return nil, errors.New("lost the terminal") }
	runTUI()
	if len(exited) != 1 || exited[0] != 1 {
		t.Fatalf("an error must exit 1: %v", exited)
	}
	runProgram = func(*tea.Program) (tea.Model, error) { panic("boom") }
	runTUI()
	if len(exited) != 2 {
		t.Fatalf("a panic must exit 1: %v", exited)
	}
}

func envMap(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestProfilesAreWrittenWhenAsked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TERMOCODE_MEMPROFILE", filepath.Join(dir, "mem.pprof"))
	writeHeapProfile()
	if st, err := os.Stat(filepath.Join(dir, "mem.pprof")); err != nil || st.Size() == 0 {
		t.Fatalf("heap profile: %v", err)
	}
	t.Setenv("TERMOCODE_MEMPROFILE", filepath.Join(dir, "no", "such", "mem.pprof"))
	writeHeapProfile() // a path that cannot be made: quietly nothing
	t.Setenv("TERMOCODE_CPUPROFILE", filepath.Join(dir, "cpu.pprof"))
	stop := startCPUProfile()
	stop()
	stop() // safe to call twice
	if _, err := os.Stat(filepath.Join(dir, "cpu.pprof")); err != nil {
		t.Fatalf("cpu profile: %v", err)
	}
	t.Setenv("TERMOCODE_CPUPROFILE", filepath.Join(dir, "no", "cpu.pprof"))
	startCPUProfile()()
	t.Setenv("TERMOCODE_CPUPROFILE", "")
	startCPUProfile()()
}

func TestTuneGCKeepsTheUsersSettings(t *testing.T) {
	t.Setenv("GOGC", "150")
	t.Setenv("GOMEMLIMIT", "1GiB")
	tuneGC()
}

func TestFontDeltaOrder(t *testing.T) {
	five := 5
	if resolveFontDelta("-2", &five) != -2 || resolveFontDelta("x", &five) != 5 || resolveFontDelta("", nil) != 0 {
		t.Fatal("resolveFontDelta")
	}
}

func TestSyncOutputAndEscapes(t *testing.T) {
	for env, want := range map[string]bool{"on": true, "1": true, "off": false, "0": false} {
		if got := syncOutputWanted(!want, envMap(map[string]string{"TERMOCODE_SYNC": env})); got != want {
			t.Errorf("TERMOCODE_SYNC=%s: %v", env, got)
		}
	}
	if !syncOutputWanted(true, envMap(nil)) || syncOutputWanted(false, envMap(nil)) {
		t.Fatal("the terminal's answer decides without an override")
	}
	if enter, leave := hostEscapes(envMap(map[string]string{"TERM": "linux"})); enter != "" || leave == "" {
		t.Fatalf("console: %q %q", enter, leave)
	}
	if enter, leave := hostEscapes(envMap(map[string]string{"VTE_VERSION": "1"})); enter == "" || leave == "" {
		t.Fatalf("vte: %q %q", enter, leave)
	}
	if enter, _ := hostEscapes(envMap(map[string]string{"VTE_VERSION": "1", "TMUX": "x"})); enter != "" {
		t.Fatal("tmux keeps the change")
	}
	if a, b := rtlEscapes("e", "l", envMap(map[string]string{"TERM": "linux"})); a != "" || b != "" {
		t.Fatal("console has no bidi")
	}
	if a, b := rtlEscapes("e", "l", envMap(nil)); a != "e" || b != "l" {
		t.Fatal("rtl escapes")
	}
}

// Without a terminal the startup probe asks nothing: it reads the
// environment only, for every images setting, and is never cached.
func TestStartupProbeWithoutTerminal(t *testing.T) {
	state := t.TempDir()
	env := envMap(map[string]string{"XDG_STATE_HOME": state, "TERM": "xterm-kitty"})
	for _, mode := range []termimg.Mode{termimg.ModeAuto, termimg.ModeOff, termimg.ModeBlocks, termimg.ModeKitty} {
		_ = startupProbe(mode, env, time.Now())
	}
	if c := startupProbe(termimg.ModeAuto, envMap(map[string]string{"TERMOCODE_PROBE": "off"}), time.Now()); c.Protocol == termimg.None {
		t.Fatal("probe off still gives a protocol")
	}
	// A terminal that was silent last time is not asked again.
	path, key := termcaps.CachePath(env), termcaps.CacheKey(env)
	termcaps.Remember(path, key, termcaps.Entry{Answers: false}, time.Now())
	c := startupProbe(termimg.ModeAuto, env, time.Now())
	if !strings.Contains(c.Reason, "did not answer last time") {
		t.Fatalf("reason %q", c.Reason)
	}
}

func TestInputOptionWithoutTerminal(t *testing.T) {
	old := os.Stdin
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = old; f.Close() })
	if inputOption() == nil {
		t.Fatal("no option")
	}
}

func TestMainRunsAndExits(t *testing.T) {
	oldArgs, oldExit := os.Args, exitProcess
	t.Cleanup(func() { os.Args, exitProcess = oldArgs, oldExit })
	code := -1
	exitProcess = func(c int) { code = c }
	os.Args = []string{"termocode", "--version"}
	main()
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
}
