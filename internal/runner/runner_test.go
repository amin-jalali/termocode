package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
)

// CMP-12: this machine serves tasks. A task waits in the queue folder
// until `termocode runner serve` takes it, runs the agent CLI on it (in a
// new worktree when it may change files) and keeps the answer.

func newStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Dir: filepath.Join(t.TempDir(), "runner")}
}

func TestAddValidatesAndLists(t *testing.T) {
	s := newStore(t)
	folder := t.TempDir()
	bad := []Task{
		{Folder: "relative/path", Prompt: "x", Agent: ai.CLIClaude},
		{Folder: filepath.Join(folder, "missing"), Prompt: "x", Agent: ai.CLIClaude},
		{Folder: folder, Prompt: "  ", Agent: ai.CLIClaude},
		{Folder: folder, Prompt: "x", Agent: "rm -rf"},
		{Folder: folder, Prompt: "x", Agent: ai.CLICodex, Model: "--yolo"},
		{Folder: folder, Prompt: "x", Agent: ai.CLICodex, Base: "-b"},
		{Folder: folder, Prompt: strings.Repeat("x", MaxPrompt+1), Agent: ai.CLIClaude},
	}
	for i, b := range bad {
		if _, err := s.Add(b); err == nil {
			t.Errorf("bad task %d was taken: %+v", i, b)
		}
	}
	a, err := s.Add(Task{Folder: folder, Prompt: "fix the login bug", Agent: ai.CLIClaude})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Add(Task{Folder: folder, Prompt: "explain main.go", Agent: ai.CLICodex, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.ID == b.ID || a.State != StateQueued || a.Created.IsZero() {
		t.Errorf("added %+v / %+v", a, b)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
		t.Fatalf("list %+v", list)
	}
	if got, err := s.Get(b.ID); err != nil || !got.ReadOnly || got.Prompt != "explain main.go" {
		t.Errorf("get %+v %v", got, err)
	}
	if _, err := s.Get("../../etc/passwd"); err == nil {
		t.Error("a path as id was read")
	}
	if fi, err := os.Stat(s.Dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("queue folder %v %v", fi, err)
	}
}

func TestCancelQueued(t *testing.T) {
	s := newStore(t)
	a, err := s.Add(Task{Folder: t.TempDir(), Prompt: "x", Agent: ai.CLIClaude})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Cancel(a.ID)
	if err != nil || got.State != StateCanceled || got.Ended.IsZero() {
		t.Fatalf("cancel %+v %v", got, err)
	}
	if _, err := s.Cancel(a.ID); err == nil {
		t.Error("a canceled task was canceled again")
	}
}

// fakeRun is a clistep.Run that records its specs and waits for release
// (or the context) before it answers.
type fakeRun struct {
	mu      sync.Mutex
	specs   []clistep.Spec
	prompts []string
	release chan struct{}
	started chan struct{}
	err     error
}

func newFake() *fakeRun {
	return &fakeRun{release: make(chan struct{}), started: make(chan struct{}, 16)}
}

func (f *fakeRun) run(ctx context.Context, sp clistep.Spec, prompt string, emit func(ai.Event)) (clistep.Result, error) {
	f.mu.Lock()
	f.specs = append(f.specs, sp)
	f.prompts = append(f.prompts, prompt)
	f.mu.Unlock()
	f.started <- struct{}{}
	emit(ai.Event{Kind: ai.EventText, Text: "working on it\n"})
	select {
	case <-ctx.Done():
		return clistep.Result{}, ctx.Err()
	case <-f.release:
	}
	if f.err != nil {
		return clistep.Result{}, f.err
	}
	return clistep.Result{Text: "done: fixed it", Files: []string{"login.go"}}, nil
}

func newServer(t *testing.T, s *Store, f *fakeRun, folders ...string) *Server {
	t.Helper()
	return &Server{
		Store:   s,
		Pool:    1,
		Folders: folders,
		Run:     f.run,
		Worktree: func(folder, id, base string) (string, string, error) {
			dir := filepath.Join(t.TempDir(), id)
			return dir, "agent/" + id, os.MkdirAll(dir, 0o700)
		},
	}
}

func waitState(t *testing.T, s *Store, id, want string) Task {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := s.Get(id)
		if err == nil && got.State == want {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s: state %q, want %q (%v)", id, got.State, want, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServeRunsTaskInWorktree(t *testing.T) {
	s := newStore(t)
	folder := t.TempDir()
	f := newFake()
	srv := newServer(t, s, f, folder)
	a, err := s.Add(Task{Folder: folder, Prompt: "fix the login bug", Agent: ai.CLIClaude, Model: "opus"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.Tick(ctx)
	<-f.started
	run := waitState(t, s, a.ID, StateRunning)
	if run.Worktree == "" || !strings.HasPrefix(run.Branch, "agent/") || run.Started.IsZero() {
		t.Errorf("running task %+v", run)
	}
	close(f.release)
	srv.Wait()
	got := waitState(t, s, a.ID, StateDone)
	if got.Answer != "done: fixed it" || len(got.Files) != 1 || got.Ended.IsZero() {
		t.Errorf("done task %+v", got)
	}
	sp := f.specs[0]
	if !sp.Write || sp.Dir != run.Worktree || sp.CLI != ai.CLIClaude || sp.Model != "opus" {
		t.Errorf("spec %+v", sp)
	}
	if f.prompts[0] != "fix the login bug" {
		t.Errorf("prompt %q", f.prompts[0])
	}
	if b, err := os.ReadFile(s.LogPath(a.ID)); err != nil || !strings.Contains(string(b), "working on it") {
		t.Errorf("log %q %v", b, err)
	}
}

func TestServeReadOnlyRunsInFolder(t *testing.T) {
	s := newStore(t)
	folder := t.TempDir()
	f := newFake()
	close(f.release)
	srv := newServer(t, s, f, folder)
	srv.Worktree = func(string, string, string) (string, string, error) {
		t.Error("a read-only task got a worktree")
		return "", "", errors.New("no")
	}
	sub := filepath.Join(folder, "web")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	a, err := s.Add(Task{Folder: sub, Prompt: "explain", Agent: ai.CLICodex, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	srv.Tick(context.Background())
	srv.Wait()
	waitState(t, s, a.ID, StateDone)
	if sp := f.specs[0]; sp.Write || sp.Dir != sub || sp.CLI != ai.CLICodex {
		t.Errorf("spec %+v", sp)
	}
}

func TestServeRefusesFolderNotServed(t *testing.T) {
	s := newStore(t)
	f := newFake()
	srv := newServer(t, s, f, t.TempDir())
	a, err := s.Add(Task{Folder: t.TempDir(), Prompt: "x", Agent: ai.CLIClaude})
	if err != nil {
		t.Fatal(err)
	}
	srv.Tick(context.Background())
	srv.Wait()
	got := waitState(t, s, a.ID, StateFailed)
	if !strings.Contains(got.Error, "not served") || len(f.specs) != 0 {
		t.Errorf("task %+v, runs %d", got, len(f.specs))
	}
}

func TestServePoolLimit(t *testing.T) {
	s := newStore(t)
	folder := t.TempDir()
	f := newFake()
	srv := newServer(t, s, f, folder)
	a, _ := s.Add(Task{Folder: folder, Prompt: "one", Agent: ai.CLIClaude})
	b, _ := s.Add(Task{Folder: folder, Prompt: "two", Agent: ai.CLIClaude})
	ctx := context.Background()
	srv.Tick(ctx)
	<-f.started
	srv.Tick(ctx)
	if got, _ := s.Get(b.ID); got.State != StateQueued {
		t.Errorf("pool 1 started a second task: %+v", got)
	}
	if n := srv.Running(); n != 1 {
		t.Errorf("running %d", n)
	}
	close(f.release)
	srv.Wait()
	waitState(t, s, a.ID, StateDone)
	srv.Tick(ctx)
	srv.Wait()
	waitState(t, s, b.ID, StateDone)
}

func TestCancelRunning(t *testing.T) {
	s := newStore(t)
	folder := t.TempDir()
	f := newFake()
	srv := newServer(t, s, f, folder)
	a, _ := s.Add(Task{Folder: folder, Prompt: "long job", Agent: ai.CLIClaude})
	ctx := context.Background()
	srv.Tick(ctx)
	<-f.started
	got, err := s.Cancel(a.ID)
	if err != nil || got.State != StateRunning || !got.CancelAsked {
		t.Fatalf("cancel asked %+v %v", got, err)
	}
	srv.Tick(ctx) // sees the ask and stops the agent
	srv.Wait()
	waitState(t, s, a.ID, StateCanceled)
}

func TestServeFailedRunKeepsError(t *testing.T) {
	s := newStore(t)
	folder := t.TempDir()
	f := newFake()
	f.err = errors.New("Claude Code is not logged in")
	close(f.release)
	srv := newServer(t, s, f, folder)
	a, _ := s.Add(Task{Folder: folder, Prompt: "x", Agent: ai.CLIClaude})
	srv.Tick(context.Background())
	srv.Wait()
	if got := waitState(t, s, a.ID, StateFailed); !strings.Contains(got.Error, "not logged in") {
		t.Errorf("error %q", got.Error)
	}
}

// A task left "running" by a runner that died is failed on the next
// start, never run twice.
func TestRecoverStaleRunning(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add(Task{Folder: t.TempDir(), Prompt: "x", Agent: ai.CLIClaude})
	if _, err := s.Update(a.ID, func(t *Task) error { t.State = StateRunning; return nil }); err != nil {
		t.Fatal(err)
	}
	srv := newServer(t, s, newFake())
	if err := srv.Recover(); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(a.ID); got.State != StateFailed || !strings.Contains(got.Error, "stopped") {
		t.Errorf("stale task %+v", got)
	}
}

func TestServeNeedsFolders(t *testing.T) {
	srv := &Server{Store: newStore(t)}
	if err := srv.Serve(context.Background()); err == nil {
		t.Error("a runner with no folders started")
	}
}

func TestPoolCap(t *testing.T) {
	for in, want := range map[int]int{0: DefaultPool, -3: DefaultPool, 3: 3, 99: MaxPool} {
		if got := PoolSize(in); got != want {
			t.Errorf("PoolSize(%d) = %d, want %d", in, got, want)
		}
	}
}
