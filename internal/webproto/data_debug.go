package webproto

import "strings"

// Run and Debug (WEBP-4): the laptop's debug session (nvim-dap, as its
// RUN AND DEBUG view shows it), its launch configurations, breakpoints,
// watches and Debug Console. The page starts a configuration by name,
// steps, sets breakpoints by workspace path and line, and evaluates in
// the paused frame; it never sends a command line, Lua or a DAP request
// of its own. The debug topic sends the whole state as a delta.

// Debug op names. Looking (and opening a variable) is open to viewers;
// everything that runs the program, changes breakpoints or evaluates an
// expression needs a controller.
const (
	OpDebugStart   = "debug.start"
	OpDebugRun     = "debug.run"
	OpDebugSelect  = "debug.select"
	OpDebugControl = "debug.control"
	OpDebugBp      = "debug.bp"
	OpDebugExpand  = "debug.expand"
	OpDebugWatch   = "debug.watch"
	OpDebugEval    = "debug.eval"
	OpDebugFrame   = "debug.frame"
	OpDebugSources = "debug.sources"
)

var opDebugRows = []Policy{
	{Op: OpDebugExpand, New: newOf[DebugExpandArgs](), Role: RoleViewer, Rate: perSec(20), MaxBytes: 4 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpDebugSources, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpDebugStart, New: newOf[DebugConfigArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDebugRun, New: newOf[DebugConfigArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDebugSelect, New: newOf[DebugConfigArgs](), Role: RoleController, Rate: perSec(5), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpDebugControl, New: newOf[DebugControlArgs](), Role: RoleController, Rate: perSec(10), MaxBytes: 1 * kib, Audit: AuditCounted, Phase: PhaseV2},
	{Op: OpDebugBp, New: newOf[DebugBpArgs](), Role: RoleController, Rate: perSec(10), MaxBytes: 16 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDebugWatch, New: newOf[DebugWatchArgs](), Role: RoleController, Rate: perSec(5), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDebugEval, New: newOf[DebugEvalArgs](), Role: RoleController, Rate: perSec(5), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDebugFrame, New: newOf[DebugFrameArgs](), Role: RoleController, Rate: perSec(10), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opDebugRows...); return true }()

func init() {
	registerTopic("debug", DebugSnap{}, DebugSnap{})
	registerResult(OpDebugSources, DebugSources{})
}

// Run and Debug limits.
const (
	MaxDebugFrames      = 200
	MaxDebugThreads     = 100
	MaxDebugVars        = 2000    // variables in the topic (scopes and opened children)
	MaxDebugValue       = 1 << 10 // bytes of one value or type
	MaxDebugConsole     = 500     // newest Debug Console lines in the topic
	MaxDebugConsoleLine = 2 << 10 // bytes of one console line
	MaxDebugBreakpoints = 1000
	MaxDebugConfigs     = 100
	MaxDebugWatches     = 100
	MaxDebugSources     = 1000
	MaxDebugExpr        = 4 << 10 // bytes of a watch or console expression
	MaxDebugKey         = 2 << 10 // bytes of a variable's tree key
	MaxDebugCondition   = 1 << 10 // bytes of a condition or log message
)

// DebugSnap is the debug topic.
type DebugSnap struct {
	// Available: the debugger (nvim-dap) is on the laptop.
	Available bool          `json:"available"`
	Active    bool          `json:"active"`
	Stopped   bool          `json:"stopped"`
	Name      string        `json:"name,omitempty"`   // the session's configuration
	Status    string        `json:"status,omitempty"` // "Paused on breakpoint", "Running" …
	Thread    int           `json:"thread,omitempty"` // the stopped thread
	Frame     int           `json:"frame,omitempty"`  // the selected frame
	Threads   []DebugThread `json:"threads"`
	Frames    []DebugFrame  `json:"frames"` // the stopped thread's stack, top first
	Scopes    []DebugScope  `json:"scopes"` // the selected frame's
	// Children are the variables of the opened rows of the tree, by key
	// ("s:Locals/user"); Open lists the keys the page opened.
	Children    []DebugChildren   `json:"children"`
	Open        []string          `json:"open"`
	Watches     []DebugWatch      `json:"watches"`
	Breakpoints []DebugBreakpoint `json:"breakpoints"`
	Configs     []DebugConfig     `json:"configs"`
	Selected    string            `json:"selected,omitempty"` // the configuration Start runs
	// ConfigError: launch.json does not parse (detected ones still list).
	ConfigError string `json:"config_error,omitempty"`
	// Hint: the debugger of the selected configuration is missing.
	Hint    string      `json:"hint,omitempty"`
	Console []DebugLine `json:"console"`
	// ConsoleCut: older console lines are not here.
	ConsoleCut bool `json:"console_cut,omitempty"`
	More       int  `json:"more,omitempty"` // variables past MaxDebugVars
}

// DebugThread is one thread of the program.
type DebugThread struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Stopped bool   `json:"stopped,omitempty"`
}

// DebugFrame is one stack frame. Path is workspace relative; a frame in
// a file outside the workspace has only its file's name (and Outside).
type DebugFrame struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	File    string `json:"file,omitempty"`
	Outside bool   `json:"outside,omitempty"`
	Line    int    `json:"line"`
	Col     int    `json:"col,omitempty"`
}

// DebugScope is one scope of the selected frame. Key is its tree key
// ("s:Locals"); Vars are there when the adapter sent them with the scope
// (Loaded), else they come as the key's Children once opened. Expensive
// scopes start closed.
type DebugScope struct {
	Key       string     `json:"key"`
	Name      string     `json:"name"`
	Expensive bool       `json:"expensive,omitempty"`
	Loaded    bool       `json:"loaded,omitempty"`
	Vars      []DebugVar `json:"vars"`
}

// DebugVar is one variable. Ref > 0: it has children (open it with
// debug.expand on its key, the parent's key + "/" + its name).
type DebugVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
	Ref   int    `json:"ref,omitempty"`
}

// DebugChildren are the children of one opened row, or why they did not
// come.
type DebugChildren struct {
	Key   string     `json:"key"`
	Vars  []DebugVar `json:"vars"`
	Error string     `json:"error,omitempty"`
}

// DebugWatch is one WATCH expression and its value in the paused frame.
type DebugWatch struct {
	Expr  string `json:"expr"`
	Value string `json:"value,omitempty"`
	Error bool   `json:"error,omitempty"` // Value is the error text
	Ref   int    `json:"ref,omitempty"`
}

// DebugBreakpoint is one breakpoint (kept on the laptop across runs).
// Path is workspace relative, Line 1-based; Text is the line's code.
// Verified: "yes" or "no" once a debugger answered, "" before.
type DebugBreakpoint struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Text      string `json:"text,omitempty"`
	Condition string `json:"condition,omitempty"`
	Hit       string `json:"hit,omitempty"`
	Log       string `json:"log,omitempty"`
	Verified  string `json:"verified,omitempty" enum:"yes|no"`
}

// DebugConfig is one launch configuration (launch.json, or found from
// the project). Debug: it starts a debugger; else it runs a command in a
// laptop terminal (Run without debugging).
type DebugConfig struct {
	Name   string `json:"name"`
	Type   string `json:"type,omitempty"`
	Debug  bool   `json:"debug,omitempty"`
	Source string `json:"source,omitempty"` // "launch.json", "auto", "test"
}

// DebugLine is one Debug Console line.
type DebugLine struct {
	Cat  string `json:"cat" enum:"stdout|stderr|console|important|input|result"`
	Text string `json:"text"`
}

// DebugSources answers debug.sources: the scripts the debugger loaded.
type DebugSources struct {
	Sources []DebugSource `json:"sources"`
	// Unsupported: this debugger does not list its loaded scripts.
	Unsupported bool `json:"unsupported,omitempty"`
	More        int  `json:"more,omitempty"`
}

// DebugSource is one loaded script: workspace relative, or only a name.
type DebugSource struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Outside bool   `json:"outside,omitempty"`
}

// DebugConfigArgs names a launch configuration ("": the selected one).
type DebugConfigArgs struct {
	Config string `json:"config,omitempty"`
}

// DebugControlArgs is one step of the session.
type DebugControlArgs struct {
	Op string `json:"op" enum:"continue|pause|step_over|step_into|step_out|restart|stop"`
}

// DebugBpArgs changes the breakpoints: toggle, set (with an optional
// condition, hit count or log message) or remove the one at Path:Line;
// clear the file's (Path) or, with no Path, every breakpoint.
type DebugBpArgs struct {
	Op        string `json:"op" enum:"toggle|set|remove|clear"`
	Path      string `json:"path,omitempty"`
	Line      int    `json:"line,omitempty"`
	Condition string `json:"condition,omitempty"`
	Hit       string `json:"hit,omitempty"`
	Log       string `json:"log,omitempty"`
}

// DebugExpandArgs opens or closes a row of the variables tree.
type DebugExpandArgs struct {
	Key  string `json:"key"`
	Open bool   `json:"open"`
}

// DebugWatchArgs adds a WATCH expression or removes one (by its text).
type DebugWatchArgs struct {
	Op   string `json:"op" enum:"add|remove"`
	Expr string `json:"expr"`
}

// DebugEvalArgs is a Debug Console line, evaluated in the paused frame.
type DebugEvalArgs struct {
	Expr string `json:"expr"`
}

// DebugFrameArgs selects a frame of the stack.
type DebugFrameArgs struct {
	ID int `json:"id"`
}

func (a *DebugConfigArgs) validate(int) error {
	if len(a.Config) > MaxLabel || hasControl(a.Config) {
		return invalid("bad configuration name")
	}
	return nil
}

func (a *DebugControlArgs) validate(int) error {
	switch a.Op {
	case "continue", "pause", "step_over", "step_into", "step_out", "restart", "stop":
		return nil
	}
	return invalid("bad debug step")
}

// validText is text of at most max bytes with no control character
// (tabs allowed).
func validText(s string, max int) bool {
	return len(s) <= max && !hasControl(strings.ReplaceAll(s, "\t", " "))
}

func (a *DebugBpArgs) validate(int) error {
	if !validText(a.Condition, MaxDebugCondition) || !validText(a.Hit, MaxDebugCondition) || !validText(a.Log, MaxDebugCondition) {
		return invalid("bad condition")
	}
	switch a.Op {
	case "toggle", "set", "remove":
		if !validPath(a.Path, false) || a.Line < 1 || a.Line > MaxLspRow {
			return invalid("bad breakpoint place")
		}
		if a.Op != "set" && (a.Condition != "" || a.Hit != "" || a.Log != "") {
			return invalid("only set takes a condition")
		}
	case "clear":
		if a.Path != "" && !validPath(a.Path, false) || a.Line != 0 || a.Condition != "" || a.Hit != "" || a.Log != "" {
			return invalid("clear takes a file at most")
		}
	default:
		return invalid("bad breakpoint op")
	}
	return nil
}

func (a *DebugExpandArgs) validate(int) error {
	if len(a.Key) > MaxDebugKey || hasControl(a.Key) || !strings.HasPrefix(a.Key, "s:") || len(a.Key) < 3 {
		return invalid("bad variable key")
	}
	return nil
}

func (a *DebugWatchArgs) validate(int) error {
	if a.Op != "add" && a.Op != "remove" {
		return invalid("bad watch op")
	}
	if strings.TrimSpace(a.Expr) == "" || !validText(a.Expr, MaxDebugExpr) {
		return invalid("bad expression")
	}
	return nil
}

func (a *DebugEvalArgs) validate(int) error {
	if strings.TrimSpace(a.Expr) == "" || !validText(a.Expr, MaxDebugExpr) {
		return invalid("bad expression")
	}
	return nil
}

func (a *DebugFrameArgs) validate(int) error {
	if a.ID < 0 || a.ID > MaxLspRow {
		return invalid("bad frame")
	}
	return nil
}
