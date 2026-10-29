// Package agentdag is the pure data model of an agent run: nodes (one
// agent step each), edges (B runs after A) and the functions that check a
// run and order it into waves. A linear pipeline is a run whose edges form
// one path (see Chain).
//
// Nothing here runs agents, does I/O or imports the ai or app packages;
// the scheduler (internal/agentrun) and the UI use these types.
package agentdag

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// State is the execution state of one node.
type State string

const (
	StatePending   State = "pending"
	StateBlocked   State = "blocked"
	StateScheduled State = "scheduled"
	StateRunning   State = "running"
	StatePaused    State = "paused"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
	StateSkipped   State = "skipped"
)

// States lists every known state.
var States = []State{StatePending, StateBlocked, StateScheduled, StateRunning,
	StatePaused, StateCompleted, StateFailed, StateCancelled, StateSkipped}

func knownState(s State) bool {
	for _, k := range States {
		if k == s {
			return true
		}
	}
	return false
}

// Done reports whether a node in state s satisfies its dependents: it
// completed, or the user skipped it.
func (s State) Done() bool { return s == StateCompleted || s == StateSkipped }

// Stops reports whether a node in state s stops its dependents.
func (s State) Stops() bool {
	return s == StateFailed || s == StateCancelled || s == StateBlocked
}

// Node categories (a profile maps them to model slots).
const (
	CategoryQuick = "quick"
	CategoryDeep  = "deep"
)

// Output modes of a node's text in the chat.
const (
	OutputShown  = "shown"
	OutputFolded = "folded"
	OutputHidden = "hidden"
)

// Tool presets of a ToolRule.
const (
	PresetNone     = "none"
	PresetReadOnly = "read-only"
	PresetAll      = "all"
)

// Limits of one run (plan §4 resource caps).
const (
	MaxNodes    = 12
	MaxLayers   = 6
	MaxErrorLen = 512
	// MaxRounds caps Loop.MaxRounds (verify runs at most this often).
	MaxRounds = 10
)

// ToolRule narrows the tools a node may use. It can only remove tools.
type ToolRule struct {
	Allow  []string `json:"allow,omitempty"`
	Deny   []string `json:"deny,omitempty"`
	Preset string   `json:"preset,omitempty"` // "" | none | read-only | all
}

// Writes reports whether node n may change files: its tool preset is
// not none or read-only.
func Writes(n Node) bool {
	return n.Tools.Preset != PresetNone && n.Tools.Preset != PresetReadOnly
}

// InputRule says what a node gets as input.
type InputRule struct {
	// NoUser leaves the user's message out of the input (default: in).
	NoUser bool `json:"no_user,omitempty"`
	// Deps limits which dependencies' outputs are passed; nil = all.
	Deps     []string `json:"deps,omitempty"`
	MaxSteps int      `json:"max_steps,omitempty"` // 0 = the ai_max_steps default
}

// Node is one agent step.
type Node struct {
	ID       string    `json:"id"`
	Title    string    `json:"title,omitempty"`
	Slot     string    `json:"slot,omitempty"`     // model slot name; "" = Category decides
	Category string    `json:"category,omitempty"` // quick | deep | ""
	System   string    `json:"system,omitempty"`   // system prompt addition
	Tools    ToolRule  `json:"tools,omitempty"`
	Input    InputRule `json:"input,omitempty"`
	Output   string    `json:"output,omitempty"` // shown | folded (default) | hidden
	// Plan marks a planner node: its answer is a plan whose nodes the
	// run grows by (agentrun.ParsePlan).
	Plan bool `json:"plan,omitempty"`
	// CLI runs the node with an official CLI ("claude", "codex") and the
	// user's subscription login instead of a model slot; the scheduler
	// hands it to its Exec function.
	CLI string `json:"cli,omitempty"`
	// Model is the model a CLI node asks its CLI for ("" = the CLI's
	// default). A model-slot node carries its model in Slot.
	Model string `json:"model,omitempty"`
	// Prompt is the task of a node a planner added (it goes into the
	// node's input after the user's message).
	Prompt  string `json:"prompt,omitempty"`
	State   State  `json:"state,omitempty"`
	Attempt int    `json:"attempt,omitempty"`
	Error   string `json:"error,omitempty"` // ≤ MaxErrorLen runes, cleaned
}

// Edge says To runs after From.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Loop makes a run "work until done": when node Verify ends with a
// failing verdict and rounds remain, node Fix joins the run (once) with
// an edge Fix → Verify, both run again and Round grows by one. The
// scheduler (agentrun) does it; agentdag only checks the shape.
type Loop struct {
	Verify    string `json:"verify"`
	Fix       Node   `json:"fix"`
	MaxRounds int    `json:"max_rounds"`
	// Round is the round the run is in (0 or 1: the first). It is state:
	// a resumed run starts in the round it stopped in.
	Round int `json:"round,omitempty"`
	// Report is the last failing verify report (the Fix node's input).
	Report string `json:"report,omitempty"`
}

// Run is one graph of nodes.
type Run struct {
	ID        string    `json:"id"`
	Profile   string    `json:"profile,omitempty"`
	Nodes     []Node    `json:"nodes"`
	Edges     []Edge    `json:"edges,omitempty"`
	Loop      *Loop     `json:"loop,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Clone is a copy of r that shares no slices or loop with it.
func (r Run) Clone() Run {
	out := r
	out.Nodes = append([]Node(nil), r.Nodes...)
	out.Edges = append([]Edge(nil), r.Edges...)
	if r.Loop != nil {
		l := *r.Loop
		out.Loop = &l
	}
	return out
}

// Chain returns the edges of a linear path through ids.
func Chain(ids ...string) []Edge {
	var out []Edge
	for i := 1; i < len(ids); i++ {
		out = append(out, Edge{From: ids[i-1], To: ids[i]})
	}
	return out
}

// Normalize checks r and returns a cleaned copy: ids and edge ends are
// trimmed, duplicate edges dropped, an empty state becomes pending, an
// empty output becomes folded, titles and errors are cleaned. Unknown
// values, duplicate ids, dangling or self edges and cycles are errors.
// r itself is not changed.
func Normalize(r Run) (Run, error) {
	if len(r.Nodes) == 0 {
		return Run{}, fmt.Errorf("agentdag: run has no nodes")
	}
	out := r
	out.Nodes = make([]Node, len(r.Nodes))
	seen := make(map[string]bool, len(r.Nodes))
	for i, n := range r.Nodes {
		n.ID = strings.TrimSpace(n.ID)
		if n.ID == "" {
			return Run{}, fmt.Errorf("agentdag: node %d has an empty id", i+1)
		}
		if seen[n.ID] {
			return Run{}, fmt.Errorf("agentdag: duplicate node id %q", n.ID)
		}
		seen[n.ID] = true
		if n.State == "" {
			n.State = StatePending
		}
		if !knownState(n.State) {
			return Run{}, fmt.Errorf("agentdag: node %q has unknown state %q", n.ID, n.State)
		}
		switch n.Category {
		case "", CategoryQuick, CategoryDeep:
		default:
			return Run{}, fmt.Errorf("agentdag: node %q has unknown category %q", n.ID, n.Category)
		}
		switch n.Output {
		case "":
			n.Output = OutputFolded
		case OutputShown, OutputFolded, OutputHidden:
		default:
			return Run{}, fmt.Errorf("agentdag: node %q has unknown output %q", n.ID, n.Output)
		}
		switch n.Tools.Preset {
		case "", PresetNone, PresetReadOnly, PresetAll:
		default:
			return Run{}, fmt.Errorf("agentdag: node %q has unknown tool preset %q", n.ID, n.Tools.Preset)
		}
		if n.Input.MaxSteps < 0 {
			return Run{}, fmt.Errorf("agentdag: node %q has negative max steps", n.ID)
		}
		n.Title = Clean(n.Title)
		n.Error = cut(Clean(n.Error), MaxErrorLen)
		out.Nodes[i] = n
	}
	out.Edges = nil
	seenEdge := make(map[Edge]bool, len(r.Edges))
	for _, e := range r.Edges {
		e = Edge{From: strings.TrimSpace(e.From), To: strings.TrimSpace(e.To)}
		for _, end := range []string{e.From, e.To} {
			if !seen[end] {
				return Run{}, fmt.Errorf("agentdag: edge %s→%s names unknown node %q", e.From, e.To, end)
			}
		}
		if e.From == e.To {
			return Run{}, fmt.Errorf("agentdag: self edge on node %q", e.From)
		}
		if seenEdge[e] {
			continue
		}
		seenEdge[e] = true
		out.Edges = append(out.Edges, e)
	}
	if _, err := layers(out); err != nil {
		return Run{}, err
	}
	return out, nil
}

// Validate normalizes r and also checks the run limits (MaxNodes,
// MaxLayers) and the loop: Verify names a node that is not a planner,
// Fix has an id of its own (it may already be in the run: a resumed
// run) and is not a planner, and 1 ≤ MaxRounds ≤ MaxRounds.
func Validate(r Run) error {
	n, err := Normalize(r)
	if err != nil {
		return err
	}
	if err := validateLoop(n); err != nil {
		return err
	}
	if len(n.Nodes) > MaxNodes {
		return fmt.Errorf("agentdag: too many nodes (%d, at most %d)", len(n.Nodes), MaxNodes)
	}
	ls, _ := layers(n)
	if len(ls) > MaxLayers {
		return fmt.Errorf("agentdag: too many layers (%d, at most %d)", len(ls), MaxLayers)
	}
	return nil
}

func validateLoop(r Run) error {
	l := r.Loop
	if l == nil {
		return nil
	}
	var verify *Node
	for i := range r.Nodes {
		if r.Nodes[i].ID == strings.TrimSpace(l.Verify) {
			verify = &r.Nodes[i]
		}
	}
	fix := strings.TrimSpace(l.Fix.ID)
	switch {
	case verify == nil:
		return fmt.Errorf("agentdag: loop: verify names unknown node %q", l.Verify)
	case verify.Plan:
		return fmt.Errorf("agentdag: loop: verify node %q is a planner", l.Verify)
	case fix == "":
		return fmt.Errorf("agentdag: loop: the fix node has an empty id")
	case fix == verify.ID:
		return fmt.Errorf("agentdag: loop: the fix node may not be the verify node %q", fix)
	case l.Fix.Plan:
		return fmt.Errorf("agentdag: loop: the fix node may not be a planner")
	case l.MaxRounds < 1 || l.MaxRounds > MaxRounds:
		return fmt.Errorf("agentdag: loop: max rounds %d (1 to %d)", l.MaxRounds, MaxRounds)
	case l.Round > l.MaxRounds:
		return fmt.Errorf("agentdag: loop: round %d is past max rounds %d", l.Round, l.MaxRounds)
	}
	return nil
}

// Layers orders the nodes into waves (Kahn layering): every node is in
// the first layer after all of its dependencies. Nodes keep their run
// order inside a layer. A cycle is an error. r is normalized first.
func Layers(r Run) ([][]string, error) {
	n, err := Normalize(r)
	if err != nil {
		return nil, err
	}
	return layers(n)
}

// layers is Layers on a run whose ids and edges are already checked.
func layers(r Run) ([][]string, error) {
	indeg := make(map[string]int, len(r.Nodes))
	next := make(map[string][]string, len(r.Nodes))
	for _, e := range r.Edges {
		indeg[e.To]++
		next[e.From] = append(next[e.From], e.To)
	}
	order := make(map[string]int, len(r.Nodes))
	var cur []string
	for i, n := range r.Nodes {
		order[n.ID] = i
		if indeg[n.ID] == 0 {
			cur = append(cur, n.ID)
		}
	}
	var out [][]string
	placed := 0
	for len(cur) > 0 {
		out = append(out, cur)
		placed += len(cur)
		var nxt []string
		for _, id := range cur {
			for _, to := range next[id] {
				indeg[to]--
				if indeg[to] == 0 {
					nxt = append(nxt, to)
				}
			}
		}
		sort.Slice(nxt, func(i, j int) bool { return order[nxt[i]] < order[nxt[j]] })
		cur = nxt
	}
	if placed < len(r.Nodes) {
		var left []string
		for _, n := range r.Nodes {
			if indeg[n.ID] > 0 {
				left = append(left, n.ID)
			}
		}
		return nil, fmt.Errorf("agentdag: cycle among nodes %s", strings.Join(left, ", "))
	}
	return out, nil
}

// Deps returns the ids of the nodes id depends on, in edge order.
func Deps(r Run, id string) []string {
	var out []string
	for _, e := range r.Edges {
		if e.To == id {
			out = append(out, e.From)
		}
	}
	return out
}

// Ready returns the pending nodes whose dependencies are all done
// (completed or skipped), in run order. r should be normalized.
func Ready(r Run) []string {
	state := states(r)
	var out []string
	for _, n := range r.Nodes {
		if state[n.ID] != StatePending {
			continue
		}
		ok := true
		for _, d := range Deps(r, n.ID) {
			if !state[d].Done() {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, n.ID)
		}
	}
	return out
}

// Block marks every pending node downstream of a failed, cancelled or
// blocked node as blocked (an empty state counts as pending and becomes
// pending in the copy). It returns the new run and the ids it changed,
// in run order. r itself is not changed.
func Block(r Run) (Run, []string) {
	out := r
	out.Nodes = append([]Node(nil), r.Nodes...)
	for i := range out.Nodes {
		if out.Nodes[i].State == "" {
			out.Nodes[i].State = StatePending
		}
	}
	state := states(out)
	changed := map[string]bool{}
	for {
		more := false
		for i, n := range out.Nodes {
			if state[n.ID] != StatePending {
				continue
			}
			for _, d := range Deps(out, n.ID) {
				if state[d].Stops() {
					out.Nodes[i].State = StateBlocked
					state[n.ID] = StateBlocked
					changed[n.ID] = true
					more = true
					break
				}
			}
		}
		if !more {
			break
		}
	}
	var ids []string
	for _, n := range out.Nodes {
		if changed[n.ID] {
			ids = append(ids, n.ID)
		}
	}
	return out, ids
}

func states(r Run) map[string]State {
	m := make(map[string]State, len(r.Nodes))
	for _, n := range r.Nodes {
		s := n.State
		if s == "" {
			s = StatePending
		}
		m[n.ID] = s
	}
	return m
}

// Clean makes s safe to draw in one terminal line: escape sequences,
// control characters and bidi overrides are removed, line breaks and tabs
// become spaces, and the ends are trimmed.
func Clean(s string) string {
	s = ansi.Strip(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case isBidi(r), unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func isBidi(r rune) bool {
	switch {
	case r == 0x061C, r == 0x200E, r == 0x200F:
		return true
	case r >= 0x202A && r <= 0x202E:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
