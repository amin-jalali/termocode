// Package agentrun runs an agent pipeline (an agentdag.Run): every ready
// node runs as its own ai.Agent in its own goroutine, at most Parallel
// at once. A node's input is the user message plus the final text of the
// nodes it depends on. A failed node blocks its dependents and stops the
// run; Cancel stops every node.
//
// The package does no I/O and knows nothing about the UI: the app builds
// each node's agent (provider for its slot, tools, system prompt) in
// Options.Build and receives progress through Options.Emit.
package agentrun

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// EventKind tags a run event.
type EventKind int

const (
	NodeStart     EventKind = iota // a node's agent is about to start
	NodeText                       // streamed text (AI.Text = the delta, Tail = the cleaned end so far)
	NodeTool                       // a tool call started or ended (AI = ai.EventToolStart / ai.EventToolResult)
	NodeAI                         // another agent event (AI: turn start, retry)
	NodeDone                       // the node completed (Output, Usage, Elapsed)
	NodeFailed                     // the node failed (Err, Usage, Elapsed)
	NodeCancelled                  // the node was stopped or never ran because the run stopped
	NodeBlocked                    // a node it depends on failed; it will not run
	RunDone                        // every node completed (Output = the last node's text, Usage = the sum)
	RunFailed                      // a node failed (Err names it; Usage = the sum)
	RunCancelled                   // Cancel or the parent context stopped the run
)

// Final reports whether k ends the run (exactly one such event is sent).
func (k EventKind) Final() bool { return k == RunDone || k == RunFailed || k == RunCancelled }

// Event is one progress report of a run. Every event carries RunID;
// node events also carry NodeID.
type Event struct {
	RunID, NodeID string
	Kind          EventKind
	AI            ai.Event
	Tail          string
	Output        string
	Usage         *ai.Usage
	Err           string
	Elapsed       time.Duration
}

// TailLen is the length (in runes) of Event.Tail.
const TailLen = 200

// DefaultInputMaxBytes caps a node's input (ai_step_input_max_kb).
const DefaultInputMaxBytes = 64 << 10

// CutNote ends an input that was cut at the cap.
const CutNote = "\n\n(The input was cut here: it was longer than the step input limit.)"

// Options configure a run.
type Options struct {
	// Parallel is how many nodes may run at once (< 1 means 1).
	Parallel int
	// User is the user's message; every node gets it first unless its
	// InputRule says NoUser.
	User string
	// InputMaxBytes caps a node's input (0: DefaultInputMaxBytes).
	InputMaxBytes int
	// Build makes the agent of node n and its history (system prompt
	// and the user turn with input). It runs in the node's goroutine;
	// ctx ends when the run stops. Required.
	Build func(ctx context.Context, n agentdag.Node, input string) (*ai.Agent, []ai.Message, error)
	// Emit receives the events, one call at a time (never concurrently),
	// from the run's goroutines. It must not block for long.
	Emit func(Event)
	// Outputs are the saved outputs of nodes that are already completed
	// (retry from a step): those nodes do not run again and their
	// dependents get this text as input.
	Outputs map[string]string
	// Review (optional) is the app's review locker: node contexts carry
	// it (ReviewLocker), so file reviews of nodes take turns.
	Review sync.Locker
}

type reviewKey struct{}

// ReviewLocker returns the review locker of the run ctx belongs to (nil
// outside a run or when the run has none).
func ReviewLocker(ctx context.Context) sync.Locker {
	l, _ := ctx.Value(reviewKey{}).(sync.Locker)
	return l
}

// Handle is a started run.
type Handle struct {
	o      Options
	id     string // the run id (read by every goroutine; never changes)
	cancel context.CancelFunc
	done   chan struct{}
	layer  map[string]int
	order  map[string]int

	emitMu sync.Mutex

	mu      sync.Mutex
	run     agentdag.Run
	outputs map[string]string
	final   Event
}

type result struct {
	id        string
	output    string
	usage     *ai.Usage
	err       error
	cancelled bool
	elapsed   time.Duration
}

// Start checks r and starts it. It returns at once; the run goes on in
// its own goroutines until it ends (see Done / Wait) or is cancelled.
func Start(ctx context.Context, r agentdag.Run, o Options) (*Handle, error) {
	if o.Build == nil {
		return nil, errors.New("agentrun: Options.Build is required")
	}
	if err := agentdag.Validate(r); err != nil {
		return nil, err
	}
	n, _ := agentdag.Normalize(r)
	ls, _ := agentdag.Layers(n)
	if o.Parallel < 1 {
		o.Parallel = 1
	}
	if o.InputMaxBytes <= 0 {
		o.InputMaxBytes = DefaultInputMaxBytes
	}
	h := &Handle{o: o, id: n.ID, done: make(chan struct{}), run: n, outputs: map[string]string{},
		layer: map[string]int{}, order: map[string]int{}}
	for i, l := range ls {
		for _, id := range l {
			h.layer[id] = i
		}
	}
	for i, nd := range n.Nodes {
		h.order[nd.ID] = i
		if nd.State == agentdag.StateCompleted {
			h.outputs[nd.ID] = o.Outputs[nd.ID]
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	go h.loop(runCtx)
	return h, nil
}

// Cancel stops every node; the run then ends with RunCancelled. Safe to
// call more than once and after the end.
func (h *Handle) Cancel() { h.cancel() }

// Done is closed when the run has ended (after its final event).
func (h *Handle) Done() <-chan struct{} { return h.done }

// Wait blocks until the run ends and returns its final event.
func (h *Handle) Wait() Event {
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.final
}

// Snapshot returns a copy of the run with the current node states and
// errors.
func (h *Handle) Snapshot() agentdag.Run {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.run
	out.Nodes = append([]agentdag.Node(nil), h.run.Nodes...)
	out.Edges = append([]agentdag.Edge(nil), h.run.Edges...)
	return out
}

// Output is the final text of a completed node ("" otherwise).
func (h *Handle) Output(id string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.outputs[id]
}

func (h *Handle) emit(e Event) {
	if h.o.Emit == nil {
		return
	}
	e.RunID = h.id
	h.emitMu.Lock()
	defer h.emitMu.Unlock()
	h.o.Emit(e)
}

func (h *Handle) node(id string) *agentdag.Node {
	return &h.run.Nodes[h.order[id]]
}

// setState changes a node under the lock (only the loop goroutine writes
// h.run, so the loop may read it without the lock).
func (h *Handle) setState(id string, f func(n *agentdag.Node)) {
	h.mu.Lock()
	f(h.node(id))
	h.mu.Unlock()
}

func (h *Handle) loop(runCtx context.Context) {
	defer h.cancel()
	nodeCtx, stopNodes := context.WithCancel(runCtx)
	defer stopNodes()
	start := clock.Now()
	results := make(chan result, len(h.run.Nodes))
	running := 0
	var total *ai.Usage
	failErr := ""
	for {
		if nodeCtx.Err() == nil {
			for _, id := range agentdag.Ready(h.run) {
				if running >= h.o.Parallel {
					break
				}
				input := h.input(id)
				h.setState(id, func(n *agentdag.Node) { n.State = agentdag.StateRunning; n.Attempt++; n.Error = "" })
				h.emit(Event{NodeID: id, Kind: NodeStart})
				running++
				go h.runNode(nodeCtx, *h.node(id), input, results)
			}
		}
		if running == 0 {
			break
		}
		res := <-results
		running--
		total = total.Add(res.usage)
		switch {
		case res.cancelled:
			h.setState(res.id, func(n *agentdag.Node) { n.State = agentdag.StateCancelled })
			h.emit(Event{NodeID: res.id, Kind: NodeCancelled, Usage: res.usage, Elapsed: res.elapsed})
		case res.err != nil:
			msg := ai.Redact(res.err.Error())
			h.setState(res.id, func(n *agentdag.Node) {
				n.State = agentdag.StateFailed
				n.Error = cutRunes(agentdag.Clean(msg), agentdag.MaxErrorLen)
			})
			h.emit(Event{NodeID: res.id, Kind: NodeFailed, Err: msg, Usage: res.usage, Elapsed: res.elapsed})
			if failErr == "" {
				failErr = fmt.Sprintf("step %q: %s", h.title(res.id), msg)
			}
			stopNodes() // v1: one failed step stops the run
			h.mu.Lock()
			blocked, ids := agentdag.Block(h.run)
			h.run = blocked
			h.mu.Unlock()
			for _, id := range ids {
				h.emit(Event{NodeID: id, Kind: NodeBlocked})
			}
		default:
			h.mu.Lock()
			h.node(res.id).State = agentdag.StateCompleted
			h.outputs[res.id] = res.output
			h.mu.Unlock()
			h.emit(Event{NodeID: res.id, Kind: NodeDone, Output: res.output, Usage: res.usage, Elapsed: res.elapsed})
		}
	}
	// Whatever did not run is cancelled (the run stopped early).
	for _, n := range h.run.Nodes {
		if n.State == agentdag.StatePending {
			h.setState(n.ID, func(n *agentdag.Node) { n.State = agentdag.StateCancelled })
			h.emit(Event{NodeID: n.ID, Kind: NodeCancelled})
		}
	}
	final := Event{Usage: total, Elapsed: clock.Since(start)}
	switch {
	case failErr != "":
		final.Kind, final.Err = RunFailed, failErr
	case runCtx.Err() != nil:
		final.Kind = RunCancelled
	default:
		final.Kind, final.Output = RunDone, h.lastOutput()
	}
	h.emit(final)
	h.mu.Lock()
	final.RunID = h.id
	h.final = final
	h.mu.Unlock()
	close(h.done)
}

// runNode builds and runs one node's agent and sends its result.
func (h *Handle) runNode(ctx context.Context, n agentdag.Node, input string, out chan<- result) {
	start := clock.Now()
	res := result{id: n.ID}
	defer func() {
		if r := recover(); r != nil {
			res.err = fmt.Errorf("step crashed: %v", r)
		}
		res.elapsed = clock.Since(start)
		out <- res
	}()
	if h.o.Review != nil {
		ctx = context.WithValue(ctx, reviewKey{}, h.o.Review)
	}
	agent, history, err := h.o.Build(ctx, n, input)
	switch {
	case ctx.Err() != nil:
		res.cancelled = true
		return
	case err != nil:
		res.err = err
		return
	case agent == nil:
		res.err = errors.New("no agent")
		return
	}
	if n.Input.MaxSteps > 0 {
		agent.MaxSteps = n.Input.MaxSteps
	}
	var raw []byte // the end of the streamed text, for Tail
	fail := ""
	added := agent.Run(ctx, history, func(e ai.Event) {
		switch e.Kind {
		case ai.EventText:
			raw = appendTail(raw, e.Text)
			h.emit(Event{NodeID: n.ID, Kind: NodeText, AI: e, Tail: tail(raw)})
		case ai.EventToolStart, ai.EventToolResult:
			h.emit(Event{NodeID: n.ID, Kind: NodeTool, AI: e})
		case ai.EventDone:
			res.usage = e.Usage
		case ai.EventError:
			res.usage, fail = e.Usage, e.Detail
		default:
			h.emit(Event{NodeID: n.ID, Kind: NodeAI, AI: e})
		}
	})
	switch {
	case ctx.Err() != nil:
		res.cancelled = true
	case fail != "":
		res.err = errors.New(fail)
	default:
		res.output = finalText(added)
	}
}

// input is node id's input: the user message (unless NoUser), then the
// output of each dependency (limited by InputRule.Deps) in layer order,
// cut at InputMaxBytes.
func (h *Handle) input(id string) string {
	n := h.node(id)
	var parts []string
	if !n.Input.NoUser && strings.TrimSpace(h.o.User) != "" {
		parts = append(parts, h.o.User)
	}
	deps := agentdag.Deps(h.run, id)
	sort.SliceStable(deps, func(i, j int) bool {
		a, b := deps[i], deps[j]
		if h.layer[a] != h.layer[b] {
			return h.layer[a] < h.layer[b]
		}
		return h.order[a] < h.order[b]
	})
	h.mu.Lock()
	for _, d := range deps {
		if n.Input.Deps != nil && !contains(n.Input.Deps, d) {
			continue
		}
		text := h.outputs[d]
		if strings.TrimSpace(text) == "" {
			text = "(no output)"
		}
		parts = append(parts, "### Output of step \""+h.title(d)+"\"\n"+text)
	}
	h.mu.Unlock()
	return capInput(strings.Join(parts, "\n\n"), h.o.InputMaxBytes)
}

func (h *Handle) title(id string) string {
	if t := h.node(id).Title; t != "" {
		return t
	}
	return id
}

// lastOutput is the text of the last sink node (no dependents) in run
// order: the pipeline's answer.
func (h *Handle) lastOutput() string {
	from := map[string]bool{}
	for _, e := range h.run.Edges {
		from[e.From] = true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.run.Nodes) - 1; i >= 0; i-- {
		if id := h.run.Nodes[i].ID; !from[id] {
			return h.outputs[id]
		}
	}
	return ""
}

// finalText is the content of the turn's last assistant message.
func finalText(added []ai.Message) string {
	for i := len(added) - 1; i >= 0; i-- {
		if added[i].Role == ai.RoleAssistant {
			return strings.TrimSpace(added[i].Content)
		}
	}
	return ""
}

func capInput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + CutNote
}

// tailWindow is how many raw bytes are kept to compute Tail (escape
// sequences are removed from them first).
const tailWindow = 8 * TailLen

func appendTail(raw []byte, s string) []byte {
	raw = append(raw, s...)
	if len(raw) > tailWindow {
		cut := len(raw) - tailWindow
		for cut < len(raw) && !utf8.RuneStart(raw[cut]) {
			cut++
		}
		raw = append(raw[:0:0], raw[cut:]...)
	}
	return raw
}

func tail(raw []byte) string {
	r := []rune(agentdag.Clean(string(raw)))
	if len(r) > TailLen {
		r = r[len(r)-TailLen:]
	}
	return string(r)
}

func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
