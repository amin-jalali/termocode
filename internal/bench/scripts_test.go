package bench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeEditor(t *testing.T) Editor {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fakeed")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nif [ \"$1\" = --version ]; then echo fakeed 1.2.3; exit 0; fi\nprintf 'line 0000001\\n'\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return Editor{
		ID: "fake", Name: "Fake", Kind: Terminal, Bins: []string{p}, VersionArgs: []string{"--version"},
		Scripts: map[Measure]func(Env) Script{
			MOpen: func(Env) Script { return Script{ReadyText: "line 0000001"} },
			MType: func(Env) Script {
				return Script{ReadyText: "line 0000001", Timed: Step{Keys: "Q", Want: "Q"}, Repeat: 4}
			},
			MSearch: func(Env) Script {
				return Script{ReadyText: "line 0000001", Timed: Step{Keys: "x\n", Want: "x"}}
			},
		},
		Unsupported: map[Measure]string{MQuickOpen: "no picker"},
	}
}

func TestEditorsTableCoversEveryMeasureOrSaysWhy(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range Editors() {
		if e.ID == "" || e.Name == "" || len(e.Bins) == 0 || seen[e.ID] {
			t.Fatalf("bad or duplicate editor %+v", e)
		}
		seen[e.ID] = true
		for _, m := range AllMeasures {
			_, has := e.Scripts[m]
			why := e.Unsupported[m]
			if has == (why != "") {
				t.Errorf("%s/%s: need exactly one of a script or a reason (script=%v reason=%q)", e.ID, m, has, why)
			}
		}
	}
	if !seen["termocode"] {
		t.Fatal("termocode must be in the table")
	}
	tc := findEditor(t, "termocode")
	for _, m := range AllMeasures {
		if _, ok := tc.Scripts[m]; !ok {
			t.Errorf("termocode has no script for %s", m)
		}
	}
}

func findEditor(t *testing.T, id string) Editor {
	for _, e := range Editors() {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("no editor %s", id)
	return Editor{}
}

func TestDetectSkipsAMissingEditorWithAReason(t *testing.T) {
	e := Editor{ID: "ghost", Name: "Ghost", Bins: []string{"definitely-not-installed-xyz"}}
	in, skip := e.Detect()
	if in != nil || !strings.Contains(skip, "not installed") {
		t.Fatalf("install=%v skip=%q", in, skip)
	}
}

func TestDetectFindsBinaryAndVersion(t *testing.T) {
	in, skip := fakeEditor(t).Detect()
	if in == nil {
		t.Fatalf("skip: %s", skip)
	}
	if !strings.Contains(in.Version, "1.2.3") {
		t.Fatalf("version %q", in.Version)
	}
}

func TestRunOpenGivesOneSamplePerRunAfterWarmup(t *testing.T) {
	in, _ := fakeEditor(t).Detect()
	r, err := in.Run(MOpen, Env{}, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Samples) != 3 {
		t.Fatalf("samples %d, want 3 (warm-up not counted)", len(r.Samples))
	}
	for _, s := range r.Samples {
		if s <= 0 || s > 5*time.Second {
			t.Fatalf("sample %v", s)
		}
	}
}

func TestRunTypeGivesRepeatSamplesPerRun(t *testing.T) {
	in, _ := fakeEditor(t).Detect()
	r, err := in.Run(MType, Env{}, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Samples) != 8 || len(r.FrameBytes) != 8 {
		t.Fatalf("samples=%d bytes=%d, want 2 runs x 4 keys", len(r.Samples), len(r.FrameBytes))
	}
}

func TestRunUnsupportedSaysWhy(t *testing.T) {
	in, _ := fakeEditor(t).Detect()
	_, err := in.Run(MQuickOpen, Env{}, 1, 0)
	if err == nil || !strings.Contains(err.Error(), "no picker") {
		t.Fatalf("err %v", err)
	}
}

func TestRunReportsAMarkerThatNeverShows(t *testing.T) {
	e := fakeEditor(t)
	e.Scripts[MOpen] = func(Env) Script { return Script{ReadyText: "never-there", Timeout: 300 * time.Millisecond} }
	in, _ := e.Detect()
	if _, err := in.Run(MOpen, Env{}, 1, 0); err == nil {
		t.Fatal("want a timeout error, not a fake number")
	}
}

func TestIsolatedEnvKeepsTheOwnersHomeOut(t *testing.T) {
	dir := t.TempDir()
	env := IsolatedEnv(dir)
	joined := strings.Join(env, "\n")
	for _, k := range []string{"HOME=" + dir, "XDG_CONFIG_HOME=" + dir, "XDG_DATA_HOME=" + dir, "XDG_STATE_HOME=" + dir, "XDG_CACHE_HOME=" + dir} {
		if !strings.Contains(joined, k) {
			t.Errorf("missing %s in %v", k, env)
		}
	}
}

func TestDirtyScriptChangesAndRestoresTheRepo(t *testing.T) {
	d := t.TempDir()
	if err := GenRepo(d, RepoSpec{Files: 10, Seed: 1, Git: true}); err != nil {
		t.Skip("git unavailable:", err)
	}
	restore, err := makeDirty(d)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := gitOut(d, "status", "--porcelain")
	if !strings.Contains(out, "README.md") {
		t.Fatalf("not dirty: %q", out)
	}
	restore()
	out, _ = gitOut(d, "status", "--porcelain")
	if strings.TrimSpace(out) != "" {
		t.Fatalf("not restored: %q", out)
	}
}

// The quick-open target is a file of every fixture repo: f01201.go was
// only in the large one, so quick_open on the small repo never showed
// it (bench finding, PR #535).
func TestQuickOpenTargetIsInEveryRepo(t *testing.T) {
	for name, spec := range FixtureRepos {
		small := spec
		small.Git = false
		if small.Files > 400 {
			small.Files = 400 // the target sits early in every repo; 400 files is enough to see it
		}
		d := t.TempDir()
		if err := GenRepo(d, small); err != nil {
			t.Fatal(err)
		}
		found := false
		_ = filepath.Walk(d, func(p string, _ os.FileInfo, _ error) error {
			found = found || filepath.Base(p) == QuickOpenTarget+".go"
			return nil
		})
		if !found {
			t.Errorf("%s repo has no %s.go", name, QuickOpenTarget)
		}
	}
	for _, e := range Editors() {
		if mk, ok := e.Scripts[MQuickOpen]; ok {
			if sc := mk(Env{Repo: "/r"}); sc.Timed.Want != QuickOpenTarget+".go" {
				t.Errorf("%s quick_open waits for %q", e.ID, sc.Timed.Want)
			}
		}
	}
}

// A relative -fixtures path still finds the repo to make dirty: envRepo
// took only absolute arguments and fell back to ".", so git_status
// failed with "open README.md" (bench finding, PR #535).
func TestEnvRepoTakesARelativeRepo(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "fx", "small", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(base)
	got := envRepo(Script{Args: []string{"--no-session", filepath.Join("fx", "small")}})
	if want, _ := filepath.Abs(filepath.Join("fx", "small")); got != want {
		t.Fatalf("envRepo = %q, want %q", got, want)
	}
}
