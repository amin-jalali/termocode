package wire

import (
	"context"
	"errors"
	"path/filepath"
	"sync"

	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/aistep"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/source"
)

// Config is one run of one workflow with every adapter the app has.
type Config struct {
	Workspace string
	Root      string // ledger.Root(Workspace)
	Entry     Entry
	RunID     string
	Dry       bool
	NoPlan    bool
	Vars      map[string]any
	LastOKAt  string
	// ParallelMax is workflows_parallel_max (0: no cap).
	ParallelMax int

	Src      *source.Sources
	Tools    run.ToolCheck
	AskTool  func(ctx context.Context, c run.AskCall) (bool, error)
	AI       aistep.Env
	Exec     func(ctx context.Context, argv []string, dir string) (string, int, error)
	TestArgv []string
	Gate     func(ctx context.Context, a GateAsk) (GateDecision, error)
	Notify   func(ctx context.Context, via, text string) error
	// GitHubComment and FindComment back github.comment writes.
	GitHubComment func(ctx context.Context, pr, body string) (ledger.Result, error)
	Find          func(ctx context.Context, spec workflow.WriteSpec, marker string) (ledger.Result, bool, error)
	// PR pushes an item's branch and opens its pull request (pr writes).
	PR      func(ctx context.Context, p PRCall) (ledger.Result, error)
	OnEvent func(run.Event)
	OnAI    func(step, item string, e agentrun.Event)

	// AgentSandbox is workflows_agent_sandbox; OnAgent gets the agent
	// steps' events; AllowFlagged asks before an agent gets an item that
	// looks like an injection (nil: skipped).
	AgentSandbox string
	OnAgent      func(step, item string, e ai.Event)
	AllowFlagged func(ctx context.Context, step, item string, chip aistep.Chip) (bool, error)
}

// ErrUntrusted is returned for a repo file the user has not trusted.
var ErrUntrusted = errors.New("this workflow file is from the repo and is not trusted yet")

// Start checks the entry (not broken, trusted) and starts the run with
// every adapter wired.
func Start(ctx context.Context, cfg Config) (*run.Handle, error) {
	e := cfg.Entry
	if e.Broken() {
		return nil, errors.New("the workflow file has problems; fix them first")
	}
	if !e.Trusted {
		return nil, ErrUntrusted
	}
	f := e.File
	seen, err := ledger.OpenSeen(cfg.Root, f.Name)
	if err != nil {
		return nil, err
	}
	id := cfg.RunID
	if id == "" {
		id = NewRunID()
	}
	l, err := ledger.Open(cfg.Root, f.Name, id)
	if err != nil {
		return nil, err
	}
	ev := Eval{Seen: func(k string) (bool, error) { return seen.Has(k), nil }}
	tools := source.ToolCaller(nil)
	if cfg.Src != nil {
		tools = cfg.Src.Tools
	}
	steps := &Steps{Env: cfg.AI, RunID: id, Dir: filepath.Dir(e.Path), Workspace: cfg.Workspace, Exec: cfg.Exec, TestArgv: cfg.TestArgv, OnEvent: cfg.OnAI,
		AgentSandbox: cfg.AgentSandbox, OnAgent: cfg.OnAgent, AllowFlagged: cfg.AllowFlagged}
	w := &Writer{Root: cfg.Root, Workflow: f.Name, Hash: f.Hash, Ledger: l, Workspace: cfg.Workspace, Tools: tools,
		Ask: cfg.Gate, Notify: cfg.Notify, GitHubComment: cfg.GitHubComment, Find: cfg.Find, PR: cfg.PR, Dir: filepath.Dir(e.Path)}
	store := &seenStore{RunFiles: RunFiles{Root: cfg.Root}, seen: seen, file: f}
	opts := run.Options{
		RunID: id, Dry: cfg.Dry, NoPlan: cfg.NoPlan, Vars: cfg.Vars, LastOKAt: cfg.LastOKAt,
		Eval: ev, Source: &Sources{Src: cfg.Src, Eval: ev}, Plan: steps, Run: steps, Write: w,
		Each: &Worktrees{Workspace: cfg.Workspace, RunID: id}, ParallelMax: cfg.ParallelMax,
		Tools: cfg.Tools, Ask: cfg.AskTool, OnEvent: cfg.OnEvent, Store: store,
	}
	return run.Start(ctx, f, opts), nil
}

// seenStore saves run files; when a real run ends well it marks the
// items it handled in the seen ledger (each items that are done, else
// the items its sources read).
type seenStore struct {
	RunFiles
	seen *ledger.SeenSet
	file *workflow.File
	once sync.Once
}

func (s *seenStore) WriteRunFile(id string, data []byte) error {
	if err := s.RunFiles.WriteRunFile(id, data); err != nil {
		return err
	}
	rf, err := run.Load(data)
	if err != nil || rf.Dry || rf.State != run.RunOK {
		return nil
	}
	var markErr error
	s.once.Do(func() { markErr = s.mark(rf) })
	return markErr
}

func (s *seenStore) mark(rf *run.RunFile) error {
	hasEach := false
	workflow.Walk(s.file.Steps, func(st *workflow.Step) bool {
		if st.Each != nil {
			hasEach = true
		}
		return !hasEach
	})
	for id, st := range rf.Steps {
		if st == nil {
			continue
		}
		if hasEach && st.Each != nil {
			for _, it := range st.Each.Items {
				if it.State == run.StateDone {
					if err := s.seen.Mark(it.Key, rf.ID, "done"); err != nil {
						return err
					}
				}
			}
		}
		if !hasEach {
			if step := s.file.StepByID(id); step != nil && step.Type == workflow.TypeSource {
				for _, it := range st.Items {
					if err := s.seen.Mark(it.Key, rf.ID, "done"); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
