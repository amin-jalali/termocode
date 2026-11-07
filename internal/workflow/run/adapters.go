package run

import (
	"context"
	"errors"

	"github.com/amin-jalali/termocode/internal/workflow"
)

// Item is one item a source read. Data is its decoded JSON
// (map[string]any, []any, string, float64, bool or nil) and is untrusted:
// data from an outside system, never instructions.
type Item struct {
	Key  string    `json:"key"`
	Data any       `json:"data"`
	Plan *PlanMark `json:"plan,omitempty"`
}

// PlanMark is what a plan step said about one item (item.plan.*).
type PlanMark struct {
	Rank   int      `json:"rank"`           // 1-based place in the plan's order; 0 = not in the order
	Skip   string   `json:"skip,omitempty"` // why the plan skips it ("" = keep)
	Labels []string `json:"labels,omitempty"`
}

// Evaluator runs the expression language of plan §3.4 (the expr package
// fits it through a small adapter). Every method must be safe on any
// scope and never panic.
type Evaluator interface {
	// Eval returns the value of an expression (plain JSON types).
	Eval(expr string, scope map[string]any) (any, error)
	// Test returns the truth of an expression (when, filter).
	Test(expr string, scope map[string]any) (bool, error)
	// Render fills the {{…}} templates of a string with text.
	Render(tmpl string, scope map[string]any) (string, error)
	// RenderJSON fills templates in a JSON value (args). A string that is
	// exactly one {{…}} keeps the value's JSON type.
	RenderJSON(v any, scope map[string]any) (any, error)
}

// Source reads the items of a source step.
type Source interface {
	Read(ctx context.Context, c SourceCall) (SourceResult, error)
}

// SourceCall is one source read.
type SourceCall struct {
	Step         *workflow.Step
	Args         map[string]any // Step.Source.Args with templates filled
	ChangedSince string         // Step.Source.ChangedSince filled
	Item         *Item          // the each item when the source is inside an each
	Dry          bool           // a dry run (sources still read for real)
	Attempt      int            // 1 on the first try
	// ReadOnly is set for an mcp source whose tool is marked read-only
	// (ToolCheck, asked before the call). An mcp source that is not
	// read-only only runs after the user said yes (Options.Ask).
	ReadOnly bool
}

// SourceResult is what a source read.
type SourceResult struct {
	Items     []Item
	Truncated bool // more than max_items; the first ones are kept
	Total     int  // items before the cap
}

// Planner runs a plan step: the AI reads all items once.
type Planner interface {
	Plan(ctx context.Context, c PlanCall) (PlanResult, error)
}

// PlanCall is one plan step.
type PlanCall struct {
	Step    *workflow.Step
	Items   []Item
	Prompt  string // Step.Plan.Prompt filled
	Dry     bool
	Attempt int
}

// PlanResult is the plan's answer by item key.
type PlanResult struct {
	Order  []string            // item keys, best first
	Skip   map[string]string   // key → reason
	Labels map[string][]string // key → labels
}

// Filter runs a filter step. ExprFilter is the default.
type Filter interface {
	Filter(ctx context.Context, c FilterCall) ([]Item, error)
}

// FilterCall is one filter step. Scope gives the expression scope of
// one item; Eval is the run's evaluator.
type FilterCall struct {
	Step  *workflow.Step
	Items []Item
	Scope func(Item) map[string]any
	Eval  Evaluator
}

// Each prepares and ends one item of an each step (a git worktree in
// WTX). It is not called in a dry run. Nil: items get no environment.
type Each interface {
	Enter(ctx context.Context, c EachCall) (ItemEnv, error)
	Leave(ctx context.Context, c EachCall, env ItemEnv, ok bool) error
}

// EachCall is one item of an each step.
type EachCall struct {
	Step *workflow.Step
	Item Item
	Base string // Step.Each.Base filled
}

// ItemEnv is where an item's run steps work.
type ItemEnv struct {
	Worktree string `json:"worktree,omitempty"` // the folder run steps work in
	// ID and Branch name the item's agent worktree (git writes commit
	// and push it); "" without one.
	ID     string `json:"worktree_id,omitempty"`
	Branch string `json:"branch,omitempty"`
}

// Runner runs ai, agent, pipeline, tests and shell steps. It is not
// called in a dry run.
type Runner interface {
	Run(ctx context.Context, c RunCall) (Output, error)
}

// RunCall is one run step.
type RunCall struct {
	Step    *workflow.Step
	Item    *Item // nil outside an each
	Env     ItemEnv
	Prompt  string                            // the step's inline prompt, filled
	Render  func(tmpl string) (string, error) // fills templates in this step's scope (prompt files)
	Attempt int
}

// Output is what a run or write step made: steps.<id>.text / .json /
// .url / .path / .exit / .worktree.
type Output struct {
	Text     string  `json:"text,omitempty"`
	JSON     any     `json:"json,omitempty"`
	URL      string  `json:"url,omitempty"`
	Path     string  `json:"path,omitempty"`
	Exit     int     `json:"exit,omitempty"`
	Worktree string  `json:"worktree,omitempty"`
	CostUSD  float64 `json:"cost_usd,omitempty"`
	// Trimmed is set when Text or JSON was cut to MaxOutputBytes in the
	// run file.
	Trimmed bool `json:"trimmed,omitempty"`
}

// Writer sends a write step. The adapter owns the gate (the write card,
// the allow-list, the ledger): it calls Wait before it shows a card and
// returns an error wrapping ErrDenied when the user says no. It is not
// called in a dry run or for gate "dry".
type Writer interface {
	Write(ctx context.Context, c WriteCall) (Output, error)
}

// WriteCall is one write.
type WriteCall struct {
	Step *workflow.Step
	Item *Item
	// Spec is Step.Write with every template filled.
	Spec workflow.WriteSpec
	// Env is the item's environment (its worktree) inside an each.
	Env ItemEnv
	// Marker is termocode-run:<run>/<step>/<item>. It goes at the end of
	// every comment and PR body, so a resumed run can find a write that
	// was sent before a crash.
	Marker  string
	Attempt int
	// ReadOnly is set for an mcp / status write whose tool is marked
	// read-only (ToolCheck, asked before the call). The gate decides
	// what that means.
	ReadOnly bool
	// Resumed is set when this write may have been sent before (resume
	// after a crash): look for the marker before sending again.
	Resumed bool
	// Wait tells the run the write now waits for the user (WriteWait).
	Wait func()
	// Render fills templates in this write's scope (a body_file).
	Render func(tmpl string) (string, error)
}

// ErrDenied is wrapped by a Writer (or an Ask) when the user said no.
// A denied step is never retried.
var ErrDenied = errors.New("denied")

// ErrPaused is wrapped by an adapter when the run must wait for the user
// (a budget reached, a usage limit): the run ends paused, the step is not
// retried, and Options.Resume continues it from that step.
var ErrPaused = errors.New("paused")

// ToolCheck tells whether an MCP tool is marked read-only (its
// annotation). The engine asks it before every mcp source and every mcp
// / status write, so the answer is known before the call.
type ToolCheck interface {
	ReadOnly(ctx context.Context, server, tool string) (bool, error)
}

// Waiter looks once at what a wait step waits for (a pull request's CI,
// merge or review). The engine calls it every Step.Wait.Every until it
// is done or the step's time is up. It must only read. Nil: wait steps
// fail with a clear error.
type Waiter interface {
	Check(ctx context.Context, c WaitCall) (WaitStatus, error)
}

// WaitCall is one look of a wait step.
type WaitCall struct {
	Step *workflow.Step
	For  string // one of workflow.WaitFors
	PR   string // Step.Wait.PR filled
	Poll int    // 1 on the first look
}

// WaitStatus is what one look saw.
type WaitStatus struct {
	Done   bool   // no need to look again
	OK     bool   // with Done: what the step waited for happened
	State  string // a short word for steps.<id>.state ("pass", "fail", "merged", …)
	Detail string // one line for the user ("1 of 3 checks still running")
	URL    string // the PR link
}

// AskCall is a question to the user (a card) before a step calls an MCP
// tool that is not marked read-only from a source.
type AskCall struct {
	Step         *workflow.Step
	Server, Tool string
}
