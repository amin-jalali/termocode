package bench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Measure names one thing we time.
type Measure string

const (
	MOpen        Measure = "open"          // start to the file on screen
	MType        Measure = "type"          // one key to its echo on screen
	MSearch      Measure = "search"        // search a big file for the last line
	MScrollEnd   Measure = "scroll_end"    // jump to the end of a big file
	MQuickOpen   Measure = "quick_open"    // type a file name, first result shows
	MFindInFiles Measure = "find_in_files" // search every file for the needle
	MGitStatus   Measure = "git_status"    // changed file shows after an outside change
	MLSPDiag     Measure = "lsp_diag"      // open a file, first language server diagnostic
)

// AllMeasures is the order of the report.
var AllMeasures = []Measure{MOpen, MType, MSearch, MScrollEnd, MQuickOpen, MFindInFiles, MGitStatus, MLSPDiag}

// Kind is terminal (PTY harness) or GUI (window harness).
type Kind int

const (
	Terminal Kind = iota
	GUI
)

// Env is what the fixtures give a script.
type Env struct {
	Repo    string // generated repo (small or large)
	BigFile string // BigLines file: the last line is "line 0050000 ..."
}

// Step sends keys, waits for text (if Want is set) and then pauses.
type Step struct {
	Keys  string
	Want  string
	Pause time.Duration
}

// Script is one measure for one editor.
type Script struct {
	Args      []string
	ReadyText string // when this shows, the editor is usable
	Setup     []Step // not timed
	Timed     Step   // timed from sending Keys until Want shows; none for MOpen
	Repeat    int    // timed keys per run (MType); default 1
	Dirty     bool   // change a file in Repo first and restore it after
	Timeout   time.Duration
}

// Editor is one program we compare. Bench scripts name the binaries they
// drive; the product UI and docs never do.
type Editor struct {
	ID, Name    string
	Kind        Kind
	Bins        []string
	VersionArgs []string
	Scripts     map[Measure]func(Env) Script
	Unsupported map[Measure]string // measure -> why there is no script
}

// Keys used by the scripts.
const (
	esc     = "\x1b"
	enter   = "\r"
	ctrlP   = "\x10"
	ctrlF   = "\x06"
	ctrlEnd = "\x1b[1;5F"
	f1      = "\x1bOP"
	f8      = "\x1b[19~"
)

const lastLine = "line 0050000"
const firstLine = "line 0000001"

func open(args func(Env) []string, ready string) func(Env) Script {
	return func(e Env) Script { return Script{Args: args(e), ReadyText: ready} }
}

func withFile(args ...string) func(Env) []string {
	return func(e Env) []string { return append(append([]string{}, args...), e.BigFile) }
}

func keys(args func(Env) []string, ready string, timed Step, repeat int) func(Env) Script {
	return func(e Env) Script { return Script{Args: args(e), ReadyText: ready, Timed: timed, Repeat: repeat} }
}

func inRepo(args ...string) func(Env) []string {
	return func(e Env) []string { return append(append([]string{}, args...), e.Repo) }
}

// typeKeys are rare capital letters, so an echo is not mistaken for other text.
const typeKeys = "QZXJKWVYQZXJKWVYQZXJ"

// Editors is the comparison table. Terminal editors run with no user config,
// in an isolated HOME; plugins a normal user adds are listed as unsupported
// until a pinned config exists, so the report says what was and was not run.
func Editors() []Editor {
	typeStep := Step{Keys: "Q", Want: "Q"}
	const noPlug = "needs a plugin or config that is not pinned yet"
	return []Editor{
		{
			ID: "termocode", Name: "termocode", Kind: Terminal, Bins: []string{os.Getenv("BENCH_TERMOCODE"), "termocode"}, VersionArgs: []string{"--version"},
			Scripts: map[Measure]func(Env) Script{
				MOpen:      open(withFile("--no-session"), firstLine),
				MType:      keys(withFile("--no-session"), firstLine, typeStep, 20),
				MSearch:    keys(withFile("--no-session"), firstLine, Step{Keys: ctrlF + Needle + enter, Want: lastLine}, 1),
				MScrollEnd: keys(withFile("--no-session"), firstLine, Step{Keys: ctrlEnd, Want: lastLine}, 1),
				MQuickOpen: func(e Env) Script {
					return Script{Args: []string{"--no-session", e.Repo}, ReadyText: "diag", Setup: []Step{{Keys: ctrlP, Pause: 300 * time.Millisecond}}, Timed: Step{Keys: QuickOpenTarget, Want: QuickOpenTarget + ".go"}}
				},
				MFindInFiles: func(e Env) Script {
					return Script{Args: []string{"--no-session", e.Repo}, ReadyText: "diag", Setup: []Step{{Keys: f8, Pause: 300 * time.Millisecond}}, Timed: Step{Keys: Needle + enter, Want: "README.md"}}
				},
				MGitStatus: func(e Env) Script {
					return Script{Args: []string{"--no-session", e.Repo}, ReadyText: "diag", Dirty: true, Setup: []Step{{Keys: f1, Pause: 300 * time.Millisecond}}, Timed: Step{Keys: "Source Control" + enter, Want: "README.md"}}
				},
				MLSPDiag: func(e Env) Script {
					return Script{Args: []string{"--no-session", filepath.Join(e.Repo, "diag", "bad.go")}, ReadyText: "missingName", Timed: Step{Want: "undefined: missingName"}, Timeout: 60 * time.Second}
				},
			},
		},
		{
			ID: "nvim", Name: "Neovim (--clean)", Kind: Terminal, Bins: []string{"nvim"}, VersionArgs: []string{"--version"},
			Scripts: map[Measure]func(Env) Script{
				MOpen: open(withFile("--clean"), firstLine),
				MType: func(e Env) Script {
					return Script{Args: []string{"--clean", e.BigFile}, ReadyText: firstLine, Setup: []Step{{Keys: "i", Pause: 100 * time.Millisecond}}, Timed: typeStep, Repeat: 20}
				},
				MSearch:    keys(withFile("--clean"), firstLine, Step{Keys: "/" + Needle + enter, Want: lastLine}, 1),
				MScrollEnd: keys(withFile("--clean"), firstLine, Step{Keys: "G", Want: lastLine}, 1),
				MFindInFiles: func(e Env) Script {
					return Script{Args: []string{"--clean", "+cd " + e.Repo}, ReadyText: "[No Name]", Timed: Step{Keys: ":vimgrep /" + Needle + "/j **/*" + enter + ":copen" + enter, Want: "README.md"}}
				},
			},
			Unsupported: map[Measure]string{MQuickOpen: noPlug, MGitStatus: noPlug, MLSPDiag: noPlug},
		},
		{
			ID: "helix", Name: "Helix", Kind: Terminal, Bins: []string{"hx", "helix"}, VersionArgs: []string{"--version"},
			Scripts: map[Measure]func(Env) Script{
				MOpen: open(withFile(), firstLine),
				MType: func(e Env) Script {
					return Script{Args: []string{e.BigFile}, ReadyText: firstLine, Setup: []Step{{Keys: "i", Pause: 100 * time.Millisecond}}, Timed: typeStep, Repeat: 20}
				},
				MSearch:    keys(withFile(), firstLine, Step{Keys: "/" + Needle + enter, Want: lastLine}, 1),
				MScrollEnd: keys(withFile(), firstLine, Step{Keys: "ge", Want: lastLine}, 1),
				MQuickOpen: func(e Env) Script {
					return Script{Args: []string{e.Repo}, ReadyText: "diag", Setup: []Step{{Keys: " f", Pause: 300 * time.Millisecond}}, Timed: Step{Keys: QuickOpenTarget, Want: QuickOpenTarget + ".go"}}
				},
				MFindInFiles: func(e Env) Script {
					return Script{Args: []string{e.Repo}, ReadyText: "diag", Setup: []Step{{Keys: " /", Pause: 300 * time.Millisecond}}, Timed: Step{Keys: Needle + enter, Want: "README.md"}}
				},
				MGitStatus: func(e Env) Script {
					return Script{Args: []string{e.Repo}, ReadyText: "diag", Dirty: true, Timed: Step{Keys: " g", Want: "README.md"}}
				},
				MLSPDiag: func(e Env) Script {
					return Script{Args: []string{filepath.Join(e.Repo, "diag", "bad.go")}, ReadyText: "missingName", Timed: Step{Want: "undefined: missingName"}, Timeout: 60 * time.Second}
				},
			},
		},
		{
			ID: "emacs", Name: "Emacs (-nw -Q)", Kind: Terminal, Bins: []string{"emacs"}, VersionArgs: []string{"--version"},
			Scripts: map[Measure]func(Env) Script{
				MOpen:      open(withFile("-nw", "-Q"), firstLine),
				MType:      keys(withFile("-nw", "-Q"), firstLine, typeStep, 20),
				MSearch:    keys(withFile("-nw", "-Q"), firstLine, Step{Keys: "\x13" + Needle + enter, Want: lastLine}, 1),
				MScrollEnd: keys(withFile("-nw", "-Q"), firstLine, Step{Keys: esc + ">", Want: lastLine}, 1),
			},
			Unsupported: map[Measure]string{MQuickOpen: noPlug, MFindInFiles: noPlug, MGitStatus: noPlug, MLSPDiag: noPlug},
		},
		{
			ID: "micro", Name: "micro", Kind: Terminal, Bins: []string{"micro"}, VersionArgs: []string{"-version"},
			Scripts: map[Measure]func(Env) Script{
				MOpen:      open(withFile(), firstLine),
				MType:      keys(withFile(), firstLine, typeStep, 20),
				MSearch:    keys(withFile(), firstLine, Step{Keys: ctrlF + Needle + enter, Want: lastLine}, 1),
				MScrollEnd: keys(withFile(), firstLine, Step{Keys: ctrlEnd, Want: lastLine}, 1),
			},
			Unsupported: map[Measure]string{MQuickOpen: noPlug, MFindInFiles: noPlug, MGitStatus: noPlug, MLSPDiag: noPlug},
		},
		guiEditor("vscode", "VS Code", "code", "--version"),
		guiEditor("zed", "Zed", "zed", "--version"),
		guiEditor("sublime", "Sublime Text", "subl", "--version"),
		guiEditor("jetbrains", "JetBrains IDE", "idea", "--version"),
	}
}

// guiEditor is a GUI program: the window harness (gui.go) times start and
// key latency; the other measures need an accessibility or UI-automation
// driver per program that is not written yet.
func guiEditor(id, name, bin, ver string) Editor {
	const why = "GUI measure: see gui.go (window start and screen-capture latency only)"
	u := map[Measure]string{}
	for _, m := range AllMeasures {
		u[m] = why
	}
	return Editor{ID: id, Name: name, Kind: GUI, Bins: []string{bin}, VersionArgs: []string{ver}, Unsupported: u}
}

// Install is an editor found on this machine.
type Install struct {
	Editor
	Path    string
	Version string
}

// Detect looks for the program. If it is missing the second result says why
// the editor is skipped.
func (e Editor) Detect() (*Install, string) {
	for _, b := range e.Bins {
		if b == "" {
			continue
		}
		p, err := lookPath(b)
		if err != nil {
			continue
		}
		in := &Install{Editor: e, Path: p}
		if len(e.VersionArgs) > 0 {
			out, _ := spawn.Command(p, e.VersionArgs...).CombinedOutput()
			in.Version = strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
		}
		return in, ""
	}
	return nil, fmt.Sprintf("%s: not installed (looked for %s)", e.Name, strings.Join(nonEmpty(e.Bins), ", "))
}

func nonEmpty(s []string) []string {
	var o []string
	for _, x := range s {
		if x != "" {
			o = append(o, x)
		}
	}
	return o
}

func lookPath(b string) (string, error) {
	if strings.ContainsRune(b, '/') {
		if st, err := os.Stat(b); err == nil && !st.IsDir() {
			return b, nil
		}
		return "", os.ErrNotExist
	}
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		p := filepath.Join(d, b)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", os.ErrNotExist
}

// IsolatedEnv points HOME and every XDG dir at dir, so a run never reads or
// writes the owner's real config and every run starts cold.
func IsolatedEnv(dir string) []string {
	return []string{"HOME=" + dir, "XDG_CONFIG_HOME=" + dir, "XDG_DATA_HOME=" + dir, "XDG_STATE_HOME=" + dir, "XDG_CACHE_HOME=" + dir}
}

// Result holds the raw samples of one measure of one editor.
type Result struct {
	Editor     string
	Measure    Measure
	Samples    []time.Duration
	FrameBytes []int // bytes the editor wrote for each timed step
}

func makeDirty(repo string) (restore func(), err error) {
	p := filepath.Join(repo, "README.md")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.WriteString("\nchanged by the bench\n"); err != nil {
		return nil, err
	}
	return func() { _, _ = gitOut(repo, "checkout", "--", "README.md") }, nil
}

// Run does warmup unmeasured runs, then runs measured ones. Each run is a
// fresh process in a fresh isolated HOME (a cold start).
func (in *Install) Run(m Measure, env Env, runs, warmup int) (Result, error) {
	r := Result{Editor: in.ID, Measure: m}
	mk, ok := in.Scripts[m]
	if !ok {
		why := in.Unsupported[m]
		if why == "" {
			why = "no script"
		}
		return r, fmt.Errorf("%s/%s: %s", in.ID, m, why)
	}
	if in.Kind != Terminal {
		return r, errors.New("GUI editors run through the window harness")
	}
	for i := 0; i < warmup+runs; i++ {
		sc := mk(env)
		samples, bytes, err := in.runOnce(m, sc)
		if err != nil {
			return r, fmt.Errorf("%s/%s run %d: %w", in.ID, m, i+1, err)
		}
		if i >= warmup {
			r.Samples = append(r.Samples, samples...)
			r.FrameBytes = append(r.FrameBytes, bytes...)
		}
	}
	return r, nil
}

func (in *Install) runOnce(m Measure, sc Script) ([]time.Duration, []int, error) {
	home, err := os.MkdirTemp("", "bench-home-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(home)
	timeout := sc.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	if sc.Dirty {
		restore, err := makeDirty(envRepo(sc))
		if err != nil {
			return nil, nil, err
		}
		defer restore()
	}
	start := time.Now()
	s, err := Start(Spec{Bin: in.Path, Args: sc.Args, Env: IsolatedEnv(home)})
	if err != nil {
		return nil, nil, err
	}
	defer s.Close()
	if _, err := s.WaitForText(sc.ReadyText, timeout); err != nil {
		return nil, nil, fmt.Errorf("never showed %q: %w", sc.ReadyText, err)
	}
	if m == MOpen {
		return []time.Duration{time.Since(start)}, []int{s.BytesOut()}, nil
	}
	for _, st := range sc.Setup {
		if err := doStep(s, st, timeout); err != nil {
			return nil, nil, err
		}
	}
	n := max(sc.Repeat, 1)
	var ds []time.Duration
	var bs []int
	for i := 0; i < n; i++ {
		key := sc.Timed.Keys
		want := sc.Timed.Want
		if m == MType {
			c := string(typeKeys[i%len(typeKeys)])
			key, want = c, c
		}
		mark := s.Mark()
		var d time.Duration
		if key == "" {
			_, err = s.WaitAfter(0, func(t string) bool { return strings.Contains(t, want) }, timeout)
			d = time.Since(start)
		} else {
			d, _, err = s.TimeKey([]byte(key), want, timeout)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("never showed %q: %w", want, err)
		}
		bs = append(bs, s.Settle(mark, 40*time.Millisecond, time.Second))
		ds = append(ds, d)
		time.Sleep(30 * time.Millisecond)
	}
	return ds, bs, nil
}

func doStep(s *Session, st Step, timeout time.Duration) error {
	if st.Keys != "" {
		if st.Want != "" {
			if _, _, err := s.TimeKey([]byte(st.Keys), st.Want, timeout); err != nil {
				return err
			}
		} else if err := s.Send([]byte(st.Keys)); err != nil {
			return err
		}
	}
	time.Sleep(st.Pause)
	return nil
}

// envRepo finds the repo a dirty script works in: the first absolute arg's
// repo root (the arg itself, or the directory that holds .git above it).
func envRepo(sc Script) string {
	for _, a := range sc.Args {
		if strings.HasPrefix(a, "-") || strings.HasPrefix(a, "+") {
			continue // a flag or an editor command, not a path
		}
		a, err := filepath.Abs(a) // a relative -fixtures path too
		if err != nil {
			continue
		}
		for d := a; d != "/" && d != "."; d = filepath.Dir(d) {
			if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
				return d
			}
		}
	}
	return "."
}
