package app

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/selfwatch"
)

// fakeBuild is the program file on disk for the reload tests.
type fakeBuild struct {
	st      selfwatch.FileState
	id      string
	version string
	runErr  error
}

func (f *fakeBuild) replace(id, version string) {
	f.st = selfwatch.FileState{Dev: 1, Ino: f.st.Ino + 1, Size: f.st.Size + 1, Mod: f.st.Mod.Add(time.Second), Mode: 0o755}
	f.id, f.version, f.runErr = id, version, nil
}

// reloadRig is a model whose program file is fake and whose re-exec is
// recorded, never run.
type reloadRig struct {
	t     *testing.T
	m     *Model
	file  *fakeBuild
	execs [][]string
}

func newReloadRig(t *testing.T) *reloadRig {
	t.Helper()
	r := &reloadRig{t: t, m: &Model{w: 160, h: 40},
		file: &fakeBuild{st: selfwatch.FileState{Dev: 1, Ino: 1, Size: 10, Mod: time.Unix(1000, 0), Mode: 0o755}, id: "run", version: "termocode v0.0.0-run+dirty"}}
	old := selfReloadDepsFor
	t.Cleanup(func() { selfReloadDepsFor = old })
	selfReloadDepsFor = func() selfReloadDeps {
		return selfReloadDeps{
			exe:    func() (string, error) { return "/opt/bin/termocode", nil },
			selfID: func() string { return "run" },
			watch: selfwatch.Deps{
				Stat: func(string) (selfwatch.FileState, error) { return r.file.st, nil },
				BuildID: func(string) (string, error) {
					if r.file.id == "" {
						return "", errors.New("none")
					}
					return r.file.id, nil
				},
				Probe: func(string) (string, error) { return r.file.version, r.file.runErr },
			},
			exec: func(bin string, argv, env []string) error {
				r.execs = append(r.execs, append([]string{bin}, argv...))
				return nil
			},
			allowed: func() bool { return true },
		}
	}
	writeUserConfig(t, `{"reload_on_new_build": true}`)
	r.run(r.m.selfReloadInitCmd())
	if r.m.selfReload == nil || r.m.selfReload.w == nil {
		t.Fatal("the watcher did not start with reload_on_new_build on")
	}
	return r
}

// run plays cmd and every message it leads to that is the reload's own
// (tick, poll), at most a few rounds: ticks are run at once.
func (r *reloadRig) run(cmd tea.Cmd) {
	r.t.Helper()
	queue := []tea.Cmd{cmd}
	for n := 0; len(queue) > 0 && n < 50; n++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if b, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, b...)
			continue
		}
		if _, tick := msg.(selfReloadTickMsg); tick {
			continue // the next poll is the test's to start (poll)
		}
		if next, ok := r.m.handleSelfReloadMsg(msg); ok {
			queue = append(queue, next)
		}
	}
}

// poll is one tick of the watcher.
func (r *reloadRig) poll() {
	r.t.Helper()
	next, ok := r.m.handleSelfReloadMsg(selfReloadTickMsg{})
	if !ok {
		r.t.Fatal("the tick is not the reload's")
	}
	r.run(next)
}

// Spec: a new build on disk (the file replaced, complete, its --version
// fine) reloads termocode into it at the next idle moment: the same
// program path and the same arguments (exec, same pid); the status bar
// said so first.
func TestReloadOnNewBuildReloadsWhenIdle(t *testing.T) {
	r := newReloadRig(t)
	r.poll()
	r.poll()
	if len(r.execs) != 0 {
		t.Fatal("reloaded without a new build")
	}
	r.file.replace("next", "termocode v0.0.0-next+dirty")
	r.poll() // changed: it settles first
	if len(r.execs) != 0 {
		t.Fatal("reloaded into a file that was still changing")
	}
	r.poll()
	if len(r.execs) != 1 {
		t.Fatalf("execs %v, want one reload", r.execs)
	}
	want := append([]string{"/opt/bin/termocode", "/opt/bin/termocode"}, os.Args[1:]...)
	if !slices.Equal(r.execs[0], want) {
		t.Errorf("exec %q, want %q", r.execs[0], want)
	}
}

// Spec: while the AI answers, the reload waits: the chip and a toast say
// a new build is ready and reloads when idle; once the turn ended the
// next poll reloads.
func TestReloadOnNewBuildWaitsForTheAI(t *testing.T) {
	r := newReloadRig(t)
	r.m.ai = newAIState()
	r.m.ai.streaming = true
	r.file.replace("next", "termocode v0.0.0-next+dirty")
	r.poll()
	r.poll()
	r.poll()
	if len(r.execs) != 0 {
		t.Fatal("reloaded while the AI answered")
	}
	if got := r.m.statusState().Update; got != "new build" {
		t.Errorf("status chip %q, want \"new build\"", got)
	}
	if got := toastText(*r.m); !strings.Contains(got, "New build ready") || !strings.Contains(got, "reloading when idle") {
		t.Errorf("toast %q", got)
	}
	r.m.ai.streaming = false
	r.poll()
	if len(r.execs) != 1 {
		t.Fatalf("execs %v after the turn ended, want one", r.execs)
	}
}

// Spec: an open dialog (a card, a prompt, the unsaved-changes question),
// an agent at work or a command in a terminal tab postpones the reload
// too.
func TestReloadOnNewBuildWaitsForADialog(t *testing.T) {
	r := newReloadRig(t)
	r.m.confirmOpen = true
	r.file.replace("next", "termocode next")
	r.poll()
	r.poll()
	r.poll()
	if len(r.execs) != 0 {
		t.Fatal("reloaded under an open dialog")
	}
	if why := r.m.selfReloadBusy(); why == "" {
		t.Error("selfReloadBusy says nothing with a dialog open")
	}
	r.m.confirmOpen = false
	r.m.promptOpen = true
	r.poll()
	if len(r.execs) != 0 {
		t.Fatal("reloaded under an open prompt")
	}
	r.m.promptOpen = false
	r.m.terminalTabs = []terminalTab{{Busy: true}} // `go test` runs in a tab
	r.poll()
	if len(r.execs) != 0 {
		t.Fatal("reloaded while a command ran in a terminal tab")
	}
	r.m.terminalTabs = []terminalTab{{}}
	r.poll()
	if len(r.execs) != 1 {
		t.Fatalf("execs %v once idle, want one", r.execs)
	}
}

// Spec: a broken binary (its --version fails) is ignored: no chip, no
// toast, no reload; termocode stays on the build it runs.
func TestReloadOnNewBuildIgnoresABrokenBinary(t *testing.T) {
	r := newReloadRig(t)
	r.file.replace("broken", "")
	r.file.runErr = errors.New("exec format error")
	for range 4 {
		r.poll()
	}
	if len(r.execs) != 0 {
		t.Fatal("reloaded into a broken binary")
	}
	if got := r.m.statusState().Update; got != "" {
		t.Errorf("chip %q for a broken binary", got)
	}
	if got := toastText(*r.m); strings.Contains(got, "New build") {
		t.Errorf("toast %q for a broken binary", got)
	}
}

// Spec: Later keeps this build running (no reload when idle); Reload Now
// reloads at once. Both are palette commands, shown only while a new
// build is ready, and offered to a controller page (Reload Now asks on
// the page first).
func TestReloadOnNewBuildLaterAndNow(t *testing.T) {
	r := newReloadRig(t)
	if ids := reloadPaletteIDs(r.m.commandPaletteItems()); slices.Contains(ids, "reload-new-build") {
		t.Fatal("Reload Into New Build is in the palette with no new build")
	}
	r.m.confirmOpen = true // busy: the build waits
	r.file.replace("next", "termocode next")
	r.poll()
	r.poll()
	ids := reloadPaletteIDs(r.m.commandPaletteItems())
	if !slices.Contains(ids, "reload-new-build") || !slices.Contains(ids, "reload-new-build-later") {
		t.Fatalf("palette %v misses the reload commands", ids)
	}
	web := map[string]bool{}
	for _, it := range r.m.webPaletteEntries() {
		web[it.ID] = it.Ask
	}
	if ask, ok := web["reload-new-build"]; !ok || !ask {
		t.Errorf("web palette: reload-new-build offered %v ask %v, want offered with ask", ok, ask)
	}
	if ask, ok := web["reload-new-build-later"]; !ok || ask {
		t.Errorf("web palette: reload-new-build-later offered %v ask %v, want offered without ask", ok, ask)
	}
	r.run(r.m.dispatchPaletteAction("reload-new-build-later"))
	r.m.confirmOpen = false
	r.poll()
	r.poll()
	if len(r.execs) != 0 {
		t.Fatal("reloaded after Later")
	}
	r.run(r.m.dispatchPaletteAction("reload-new-build"))
	if len(r.execs) != 1 {
		t.Fatalf("execs %v after Reload Now, want one", r.execs)
	}
}

// Spec: the chip opens a card that asks (Later first and focused,
// Reload Now second).
func TestReloadOnNewBuildChipCard(t *testing.T) {
	r := newReloadRig(t)
	r.m.confirmOpen = true
	r.file.replace("next", "termocode v0.0.0-next+dirty")
	r.poll()
	r.poll()
	r.m.confirmOpen = false
	r.run(r.m.openSelfReloadCard())
	if !r.m.confirmOpen || buttonIDs(r.m.confirm) != "later,reload" {
		t.Fatalf("card open %v buttons %s", r.m.confirmOpen, buttonIDs(r.m.confirm))
	}
	r.m.confirmOpen = false
	r.run(r.m.handleConfirmSelect("reload"))
	if len(r.execs) != 1 {
		t.Fatalf("execs %v after the card's Reload Now", r.execs)
	}
}

// Spec: reload_on_new_build is on by default for dev builds ("dev",
// "+dirty", v0.0.0 pseudo-versions) and off for releases; config.json
// overrides it.
func TestReloadOnNewBuildDefault(t *testing.T) {
	for v, want := range map[string]bool{
		"dev": true, "v0.0.0-20261109120000-abcdef123456+dirty": true, "v0.0.0-20261109120000-abcdef123456": true,
		"v1.4.0+dirty": true, "v1.4.0": false, "v0.9.2": false,
	} {
		if got := devBuild(v); got != want {
			t.Errorf("devBuild(%q) = %v, want %v", v, got, want)
		}
	}
	old := termocodeVersion
	t.Cleanup(func() { termocodeVersion = old })
	writeUserConfig(t, `{}`)
	termocodeVersion = "v1.4.0"
	if reloadOnNewBuildOn() {
		t.Error("on for a release by default")
	}
	termocodeVersion = "v0.0.0-20261109120000-abcdef123456+dirty"
	if !reloadOnNewBuildOn() {
		t.Error("off for a dev build by default")
	}
	writeUserConfig(t, `{"reload_on_new_build": false}`)
	if reloadOnNewBuildOn() {
		t.Error("false in config.json did not turn it off")
	}
	termocodeVersion = "v1.4.0"
	writeUserConfig(t, `{"reload_on_new_build": true}`)
	if !reloadOnNewBuildOn() {
		t.Error("true in config.json did not turn it on for a release")
	}
}

// Spec: with the setting off nothing is watched.
func TestReloadOnNewBuildOff(t *testing.T) {
	writeUserConfig(t, `{"reload_on_new_build": false}`)
	m := &Model{}
	if cmd := m.selfReloadInitCmd(); cmd != nil {
		t.Fatal("a watcher starts with reload_on_new_build off")
	}
}

func reloadPaletteIDs(items []picker.Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}
