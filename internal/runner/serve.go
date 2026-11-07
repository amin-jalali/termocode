package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/git"
)

// Pool sizes.
const (
	DefaultPool = 2
	MaxPool     = 8
	DefaultPoll = time.Second
)

// PoolSize is n kept in 1…MaxPool (DefaultPool when n is not set).
func PoolSize(n int) int {
	switch {
	case n <= 0:
		return DefaultPool
	case n > MaxPool:
		return MaxPool
	}
	return n
}

// Server is the runner loop: it takes queued tasks of the folders it
// serves and runs at most Pool of them at once.
type Server struct {
	Store   *Store
	Pool    int      // tasks at once (PoolSize)
	Folders []string // the folders served; a task's folder must be one of them or inside one
	// Run runs one agent CLI step (nil: clistep.Run).
	Run func(ctx context.Context, sp clistep.Spec, prompt string, emit func(ai.Event)) (clistep.Result, error)
	// Worktree makes a task's agent worktree from base and returns the
	// agent's folder in it and its branch (nil: a git agent worktree).
	Worktree func(folder, id, base string) (dir, branch string, err error)
	Poll     time.Duration     // how often the queue is read (0: DefaultPoll)
	Timeout  time.Duration     // one task's longest run (0: clistep.DefaultTimeout)
	Log      func(line string) // progress lines (nil: none)
	Now      func() time.Time  // nil: time.Now

	mu      sync.Mutex
	running map[string]context.CancelFunc
	wg      sync.WaitGroup
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) logf(f string, a ...any) {
	if s.Log != nil {
		s.Log(fmt.Sprintf(f, a...))
	}
}

func gitWorktree(folder, id, base string) (string, string, error) {
	wt, err := git.CreateAgentWorktreeFrom(folder, git.AgentWorktreeID("runner", id), base)
	if err != nil {
		return "", "", err
	}
	return wt.Dir(), wt.Branch, nil
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// served reports a folder that is a served folder or inside one (links
// resolved on both sides, so a link cannot lead out).
func (s *Server) served(folder string) bool {
	f := realPath(folder)
	for _, d := range s.Folders {
		rel, err := filepath.Rel(realPath(d), f)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

// Running is the number of tasks running now.
func (s *Server) Running() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.running)
}

// Wait waits until every started task has ended.
func (s *Server) Wait() { s.wg.Wait() }

// Recover fails every task a runner left "running" when it stopped: it
// is never run twice (its worktree, if any, stays for review).
func (s *Server) Recover() error {
	list, err := s.Store.List()
	if err != nil {
		return err
	}
	for _, t := range list {
		if t.State != StateRunning {
			continue
		}
		_, err := s.Store.Update(t.ID, func(t *Task) error {
			if t.State == StateRunning {
				t.State, t.Ended = StateFailed, s.now()
				t.Error = "the runner stopped while the task ran; run it again if you want"
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Serve runs the loop until ctx ends; then the running agents are
// stopped and their tasks failed. Only one runner serves a queue.
func (s *Server) Serve(ctx context.Context) error {
	if len(s.Folders) == 0 {
		return errors.New("the runner serves no folder: give it at least one (--folder)")
	}
	for i, d := range s.Folders {
		if !filepath.IsAbs(d) {
			return fmt.Errorf("folder %q is not an absolute path", d)
		}
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			return fmt.Errorf("folder %q is not there", d)
		}
		s.Folders[i] = filepath.Clean(d)
	}
	if err := s.Store.ensure(); err != nil {
		return err
	}
	unlock, err := tryLockFile(filepath.Join(s.Store.Dir, "serve.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.Recover(); err != nil {
		return err
	}
	poll := s.Poll
	if poll <= 0 {
		poll = DefaultPoll
	}
	s.logf("runner: serving %s, %d at once", strings.Join(s.Folders, ", "), PoolSize(s.Pool))
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			s.Wait()
			return nil
		case <-tick.C:
		}
	}
}

// Tick reads the queue once: it stops the running tasks whose cancel was
// asked and starts queued ones while the pool has room.
func (s *Server) Tick(ctx context.Context) {
	list, err := s.Store.List()
	if err != nil {
		s.logf("runner: read the queue: %v", err)
		return
	}
	s.mu.Lock()
	if s.running == nil {
		s.running = map[string]context.CancelFunc{}
	}
	for _, t := range list {
		if t.State == StateRunning && t.CancelAsked {
			if stop, ok := s.running[t.ID]; ok {
				stop()
			}
		}
	}
	free := PoolSize(s.Pool) - len(s.running)
	s.mu.Unlock()
	for _, t := range list {
		if free <= 0 || ctx.Err() != nil {
			return
		}
		if t.State != StateQueued {
			continue
		}
		if !s.served(t.Folder) {
			s.finish(t.ID, func(t *Task) {
				t.State, t.Error = StateFailed, "folder "+t.Folder+" is not served by this runner"
			})
			s.logf("runner: %s refused: folder %s is not served", t.ID, t.Folder)
			continue
		}
		claimed := false
		task, err := s.Store.Update(t.ID, func(t *Task) error {
			if t.State != StateQueued {
				return nil
			}
			t.State, t.Started, claimed = StateRunning, s.now(), true
			return nil
		})
		if err != nil {
			s.logf("runner: %s: %v", t.ID, err)
			continue
		}
		if !claimed {
			continue
		}
		free--
		tctx, stop := context.WithCancel(ctx)
		s.mu.Lock()
		s.running[task.ID] = stop
		s.mu.Unlock()
		s.wg.Add(1)
		go s.work(ctx, tctx, stop, task)
	}
}

// finish writes a task's end under the queue lock.
func (s *Server) finish(id string, f func(*Task)) {
	if _, err := s.Store.Update(id, func(t *Task) error {
		f(t)
		t.Ended = s.now()
		return nil
	}); err != nil {
		s.logf("runner: %s: save: %v", id, err)
	}
}

func capText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n… (cut)"
}

// work runs one claimed task to its end.
func (s *Server) work(parent, ctx context.Context, stop context.CancelFunc, t Task) {
	defer s.wg.Done()
	defer func() {
		stop()
		s.mu.Lock()
		delete(s.running, t.ID)
		s.mu.Unlock()
	}()
	s.logf("runner: %s started (%s in %s)", t.ID, t.Agent, t.Folder)
	log := openLog(s.Store.LogPath(t.ID))
	defer log.close()
	log.line(fmt.Sprintf("== %s %s in %s", s.now().Format(time.RFC3339), t.Agent, t.Folder))

	dir := t.Folder
	if !t.ReadOnly {
		mk := s.Worktree
		if mk == nil {
			mk = gitWorktree
		}
		wdir, branch, err := mk(t.Folder, t.ID, t.Base)
		if err != nil {
			log.line("error: " + err.Error())
			s.finish(t.ID, func(t *Task) { t.State, t.Error = StateFailed, "worktree: "+err.Error() })
			s.logf("runner: %s failed: worktree: %v", t.ID, err)
			return
		}
		dir = wdir
		if _, err := s.Store.Update(t.ID, func(x *Task) error { x.Worktree, x.Branch = wdir, branch; return nil }); err != nil {
			s.logf("runner: %s: save: %v", t.ID, err)
		}
		log.line("worktree " + wdir + " (" + branch + ")")
	}

	run := s.Run
	if run == nil {
		run = clistep.Run
	}
	sp := clistep.Spec{CLI: t.Agent, Write: !t.ReadOnly, Model: t.Model, Dir: dir, Timeout: s.Timeout}
	res, err := run(ctx, sp, t.Prompt, log.event)
	if err != nil {
		log.line("error: " + err.Error())
	}
	state := ""
	s.finish(t.ID, func(x *Task) {
		x.Answer, x.Files = capText(res.Text, MaxAnswer), res.Files
		switch {
		case x.CancelAsked:
			x.State, x.Error = StateCanceled, "canceled"
		case err == nil:
			x.State, x.Error = StateDone, ""
		case parent.Err() != nil:
			x.State, x.Error = StateFailed, "the runner stopped while the task ran"
		default:
			x.State, x.Error = StateFailed, err.Error()
		}
		state = x.State
	})
	s.logf("runner: %s %s", t.ID, state)
}

// taskLog is a task's log file, capped at MaxLog. A log that cannot be
// opened is skipped: the task still runs.
type taskLog struct {
	mu sync.Mutex
	f  *os.File
	n  int
}

func openLog(path string) *taskLog {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return &taskLog{}
	}
	return &taskLog{f: f}
}

func (l *taskLog) write(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil || l.n >= MaxLog {
		return
	}
	if l.n+len(s) > MaxLog {
		s = s[:MaxLog-l.n] + "\n… (log cut)\n"
	}
	n, _ := l.f.WriteString(s)
	l.n += n
}

func (l *taskLog) line(s string) { l.write(strings.TrimRight(s, "\n") + "\n") }

func (l *taskLog) event(e ai.Event) {
	switch e.Kind {
	case ai.EventText:
		l.write(e.Text)
	case ai.EventToolStart:
		d := e.Detail
		if d == "" {
			d = e.Tool
		}
		l.line("\n> " + d)
	case ai.EventError:
		l.line("error: " + e.Detail)
	}
}

func (l *taskLog) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
}
