package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Version is the file format version this termocode reads and writes.
// A file with a higher version is refused; version 1 is read forever.
const Version = 1

// Format limits (plan §3.1).
const (
	MaxFileBytes    = 256 << 10 // a workflow file
	MaxSteps        = 40        // all steps, nested ones and else lists too
	MaxEachDepth    = 3         // each inside each inside each
	MaxItemsCap     = 500       // the highest max_items
	DefaultMaxItems = 100       // max_items when not set
	MaxItemBytes    = 64 << 10  // one item (checked by the source adapters)
	MaxPromptBytes  = 32 << 10  // prompt, text, body, content
	MaxExprBytes    = 1 << 10   // an expression (when, sort, dedupe, items)
	MaxErrorChars   = 512       // ParseError.Error()
)

// Ext is the workflow file extension.
const Ext = ".jsonc"

// Step types.
const (
	TypeSource   = "source"
	TypePlan     = "plan"
	TypeFilter   = "filter"
	TypeEach     = "each"
	TypeAI       = "ai"
	TypeAgent    = "agent"
	TypePipeline = "pipeline"
	TypeTests    = "tests"
	TypeShell    = "shell"
	TypeWrite    = "write"
)

// StepTypes lists every step type, in the order of the plan.
var StepTypes = []string{TypeSource, TypePlan, TypeFilter, TypeEach, TypeAI, TypeAgent, TypePipeline, TypeTests, TypeShell, TypeWrite}

// Source kinds (SourceSpec.From).
var SourceKinds = []string{"mcp", "github", "shell", "files", "http", "db"}

// Write targets (WriteSpec.To).
var WriteTargets = []string{"pr", "commit", "mcp", "status", "github.comment", "file", "http", "message"}

// Write gates (WriteSpec.Gate). GateAsk is the default.
const (
	GateAsk   = "ask"   // the write card asks every time
	GateAllow = "allow" // no card if the allow-list outside the repo has it
	GateDry   = "dry"   // never written, only shown
)

// On-error values (File.OnError, Step.OnError).
const (
	OnErrorContinue = "continue" // the next item goes on; else runs
	OnErrorStop     = "stop"     // the run stops
)

// File is one parsed and checked workflow file. Every field has passed
// the checks of Parse; expressions and templates are kept as text (the
// expr package evaluates them).
type File struct {
	Version     int
	Name        string // [a-z][a-z0-9_-]{0,39}; matches the file name
	Title       string
	Description string
	Schedule    *Schedule      // nil: runs only by hand
	Vars        map[string]any // string, float64 or bool; CLI --var wins
	Limits      Limits
	Retries     Retries // the default of every step
	OnError     string  // OnErrorContinue (default) or OnErrorStop
	Notify      []string
	// ResumeOnStart re-runs interrupted items after a restart (default
	// true).
	ResumeOnStart bool
	Steps         []Step
	// Hash is the sha256 of the file bytes (hex). Trust and the write
	// allow-list are bound to it.
	Hash string
}

// Schedule is when a workflow runs by itself: a Preset with Time, or a
// 5-field Cron. TZ is an IANA zone name ("" = local); the scheduler
// checks it.
type Schedule struct {
	Preset   string // hourly | daily | weekdays | weekly | monthly
	Time     string // HH:MM
	Cron     string
	TZ       string
	Precheck []string // argv; a non-zero exit skips the run
}

// SchedulePresets are the allowed Schedule.Preset values.
var SchedulePresets = []string{"hourly", "daily", "weekdays", "weekly", "monthly"}

// Limits are the run's caps. A zero value means "not set" (the runtime
// then uses its default), except MaxItems, which Parse fills.
type Limits struct {
	BudgetUSD     float64
	ItemBudgetUSD float64
	Parallel      int // items at once inside one each (0: 1)
	Timeout       time.Duration
	ItemTimeout   time.Duration
	Wait          time.Duration // how long a card may wait (0: workflows_wait_hours)
	MaxItems      int           // per source; DefaultMaxItems when not set
}

// Retries is how often a failed step is tried again.
type Retries struct {
	Max     int
	Backoff time.Duration
}

// Step is one step. ID, Type and the common fields are on every step;
// exactly one of the spec pointers is set, the one for Type (Run steps:
// AI, Agent, Pipeline, Tests or Shell).
type Step struct {
	ID    string // optional; unique in the file; steps.<id> in expressions
	Type  string // one of StepTypes
	Title string
	// When is an expression. On a filter step it is the keep test, run
	// per item; on every other step the step runs only when it is true.
	When    string
	Else    []Step   // runs when When is false, or on failure with on_error continue
	Retries *Retries // nil: the file's Retries
	Timeout time.Duration
	OnError string // "" = the file's OnError
	Line    int    // 1-based line of the step's "{" in the file

	Source   *SourceSpec
	Plan     *PlanSpec
	Filter   *FilterSpec
	Each     *EachSpec
	AI       *AISpec
	Agent    *AgentSpec
	Pipeline *PipelineSpec
	Tests    *TestsSpec
	Shell    *ShellSpec
	Write    *WriteSpec
}

// SourceSpec reads items. From picks the kind; only its fields are set.
type SourceSpec struct {
	From     string // one of SourceKinds
	Items    string // a path into the result: $.a.b or $.a[*].b ("" = the whole result)
	Key      string // template for the item key (seen ledger); "" = the item's id
	MaxItems int    // ≤ MaxItemsCap; Parse fills the file's limit when not set

	Server string         // mcp
	Tool   string         // mcp
	Args   map[string]any // mcp, github; values may hold {{templates}}
	What   string         // github: issues | pulls | checks

	Argv  []string // shell; no templates
	Parse string   // shell: json (default) | jsonl | lines
	Cwd   string   // shell; relative to the workspace

	Glob         string // files
	ChangedSince string // files; a template

	Request string            // http: a .http file in the repo
	Headers map[string]string // http; ${secret:NAME} allowed
	Env     map[string]string // shell; ${secret:NAME} allowed

	Connection string // db
	SQL        string // db; SELECT only
}

// PlanSpec is a plan step: the AI reads all items once and answers JSON.
type PlanSpec struct {
	Profile    string
	Model      string // a model category ("quick")
	Prompt     string
	PromptFile string            // relative to the workflow folder
	Output     map[string]string // field → what it holds (for the prompt)
}

// FilterSpec keeps the items for which Step.When is true.
type FilterSpec struct {
	Sort   string // an expression to sort by
	Take   int    // keep the first N (0: all)
	Dedupe string // an expression; items with the same value are kept once
}

// EachSpec runs Steps once per item.
type EachSpec struct {
	Items    string // an expression for the list ("steps.ready.items")
	Parallel int    // items at once (0: 1)
	Worktree bool   // a git worktree per item
	Base     string // the worktree's base branch (template)
	Cleanup  string // keep (default) | discard
	Steps    []Step
}

// ToolRules narrow an ai step's tools. They can only remove tools.
type ToolRules struct {
	Preset string   // a tool preset name ("all")
	Deny   []string // tool names
}

// AISpec runs our AI: a named profile, or one step on a model category.
type AISpec struct {
	Profile     string
	Model       string
	Prompt      string
	PromptFile  string
	Tools       *ToolRules
	ApprovePlan bool
	BudgetUSD   float64
}

// AgentSpec runs an outside agent CLI in the item's worktree.
type AgentSpec struct {
	Agent      string // codex | claude-code
	Prompt     string
	PromptFile string
	Sandbox    string // "" = the workflows_agent_sandbox setting; worktree | read-only
	Model      string
	Effort     string
}

// Agents are the allowed AgentSpec.Agent values.
var Agents = []string{"codex", "claude-code"}

// PipelineSpec runs a named pipeline profile.
type PipelineSpec struct {
	Profile    string
	Prompt     string
	PromptFile string
}

// TestsSpec runs the project's test task, or Argv.
type TestsSpec struct {
	Task string
	Argv []string
}

// ShellSpec runs one command (argv only, never a shell string).
type ShellSpec struct {
	Argv []string
	Cwd  string
	Env  map[string]string // ${secret:NAME} allowed
}

// UndoSpec is the MCP call that undoes a write.
type UndoSpec struct {
	Tool string
	Args map[string]any
}

// WriteSpec sends a result out. To picks the target; only its fields
// are set. Every write passes the gate.
type WriteSpec struct {
	To   string // one of WriteTargets
	Gate string // GateAsk (default), GateAllow or GateDry

	Server string         // mcp, status
	Tool   string         // mcp, status
	Args   map[string]any // mcp, status
	Undo   *UndoSpec      // mcp, status

	Title    string // pr
	Body     string // pr, github.comment, http
	BodyFile string // pr, github.comment
	Base     string // pr
	Draft    bool   // pr
	WaitCI   bool   // pr
	Message  string // commit
	PR       string // github.comment: the PR number (template)

	Path    string // file: relative to the workspace (template)
	Content string // file
	Mode    string // file: create (default) | overwrite | append

	Request string            // http
	Headers map[string]string // http; ${secret:NAME} allowed

	Via  string // message: os | web | an MCP server | a webhook; ${secret:NAME} allowed
	Text string // message
}

// Walk calls fn for every step in steps, depth first: each step, then
// its Each steps, then its Else steps. fn returns false to stop.
func Walk(steps []Step, fn func(*Step) bool) bool {
	for i := range steps {
		s := &steps[i]
		if !fn(s) {
			return false
		}
		if s.Each != nil && !Walk(s.Each.Steps, fn) {
			return false
		}
		if !Walk(s.Else, fn) {
			return false
		}
	}
	return true
}

// StepByID finds a step anywhere in the file (nil when none).
func (f *File) StepByID(id string) *Step {
	var out *Step
	Walk(f.Steps, func(s *Step) bool {
		if s.ID == id {
			out = s
			return false
		}
		return true
	})
	return out
}

// NameFromPath is the workflow name a file path must hold: the base name
// without .jsonc ("" when the path has another extension).
func NameFromPath(path string) string {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, Ext) {
		return ""
	}
	return strings.TrimSuffix(base, Ext)
}

// HashBytes is the File.Hash of data.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Problem is one error or warning, at a line of the file.
type Problem struct {
	Line  int    // 1-based; 0 = the whole file
	Where string // the field: "steps[2].parallel"; "" = the top level
	Msg   string
}

func (p Problem) String() string {
	var b strings.Builder
	if p.Line > 0 {
		fmt.Fprintf(&b, "line %d: ", p.Line)
	}
	if p.Where != "" {
		b.WriteString(p.Where + ": ")
	}
	b.WriteString(p.Msg)
	return b.String()
}

// ParseError lists every problem that makes a file unusable. Error()
// shows the first ones in at most MaxErrorChars characters.
type ParseError struct {
	File     string // the path given to Parse ("" = none)
	Problems []Problem
}

func (e *ParseError) Error() string {
	prefix := "workflow"
	if e.File != "" {
		prefix = filepath.Base(e.File)
	}
	var b strings.Builder
	b.WriteString(prefix + ": ")
	for i, p := range e.Problems {
		s := p.String()
		if i > 0 {
			s = "; " + s
		}
		rest := len(e.Problems) - i - 1
		more := ""
		if rest > 0 {
			more = fmt.Sprintf(" (and %d more)", rest)
		}
		if i > 0 && b.Len()+len(s)+len(more) > MaxErrorChars {
			fmt.Fprintf(&b, " (and %d more)", rest+1)
			break
		}
		b.WriteString(s)
	}
	return capRunes(b.String(), MaxErrorChars)
}

// capRunes cuts s to at most n bytes on a rune boundary, with "…".
func capRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - len("…")
	for cut > 0 && !utf8RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }
