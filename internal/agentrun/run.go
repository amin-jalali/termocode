// Package agentrun runs an agent pipeline (an agentdag.Run): every ready
// node runs as its own ai.Agent in its own goroutine, at most Parallel
// at once (the nodes that are ready together form a "wave"). A node's
// input is the user message plus the final text of the nodes it depends
// on. A failed node blocks its dependents and stops the run; Cancel
// stops every node. A planner node grows the run by the nodes of its
// plan (plan.go); a budget pauses the run before the next node until
// Continue or Cancel; the nodes' confirm and review cards take turns
// through one Gate (gate.go). A run with a Loop works until done: a
// failing verify node adds the fix node and runs another round
// (verify.go). A node with a CLI runs through Options.Exec (an official
// CLI on the user's subscription); its usage limit pauses the run at
// that node.
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
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/crash"
)

// EventKind tags a run event.
type EventKind int

const (
	NodeStart        EventKind = iota // a node's agent is about to start
	NodeText                          // streamed text (AI.Text = the delta, Tail = the cleaned end so far)
	NodeTool                          // a tool call started or ended (AI = ai.EventToolStart / ai.EventToolResult)
	NodeAI                            // another agent event (AI: turn start, retry)
	NodeDone                          // the node completed (Output, Usage, Elapsed)
	NodeFailed                        // the node failed (Err, Usage, Elapsed)
	NodeCancelled                     // the node was stopped or never ran because the run stopped
	NodeBlocked                       // a node it depends on failed; it will not run
	RunDone                           // every node completed (Output = the last node's text, Usage = the sum)
	RunFailed                         // a node failed (Err names it; Usage = the sum)
	RunCancelled                      // Cancel or the parent context stopped the run
	NodeRepair                        // a planner's plan is not valid (Err); the planner is asked once more
	RunPlanned                        // a planner's plan grew the run (NodeID = the planner, Nodes = the new nodes)
	RunPaused                         // the budget is reached (Cost, Budget) or Pause was asked (Budget 0): nothing new starts until Continue
	RunResumed                        // Continue after a pause
	RunRound                          // verify failed and a new round starts (Round = the new round, Err = the verify report)
	NodeLimited                       // a CLI node hit its usage limit (Err = the CLI's words, ResetAt): it waits as pending and the run pauses
	RunAwaitApproval                  // G-PLAN: the run waits for the user's answer on its plan (Nodes = the steps left; Err = why an edit did not fit)
	RunApproved                       // the user approved the plan; the run goes on
	RunPlanEdited                     // the user's edit changed the plan (Nodes = the steps it added); RunAwaitApproval follows
	RunPlanKept                       // steps joined an approved run and need no approval (Nodes = them): it goes on without asking
	NodeStalled                       // the node's model was silent for Options.StallAfter: it was stopped and waits to start once more (Err says so)
	NodeMessage                       // a message sent to the node (Send) reached its agent (Output = the text), or not (Err = why)
)

// Verdicts of a loop's verify node (Event.Verdict).
const (
	VerdictPass    = "pass"
	VerdictFail    = "fail"
	VerdictMissing = "missing" // no verdict line (NodeRepair, or NodeDone after the repair: counted as a fail)
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
	// Nodes are the nodes a plan added, with their slots (RunPlanned).
	Nodes []agentdag.Node
	// Cost is what the run has cost so far in dollars, Budget its budget
	// (RunPaused).
	Cost, Budget float64
	// Round is the loop round of a run with a Loop (NodeStart, RunRound,
	// the final event; 0 without a loop).
	Round int
	// Verdict is the verify node's verdict (its NodeDone and NodeRepair)
	// and the run's (RunDone of a run with a Loop).
	Verdict string
	// Limit is the usage-limit message of a run a CLI's limit paused
	// (RunPaused) and ResetAt when that limit resets (zero: unknown;
	// RunPaused and NodeLimited).
	Limit   string
	ResetAt time.Time
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
	// Review (optional) is the app's gate for cards that wait for the
	// user: node contexts carry it (ReviewGate) so file reviews take
	// turns, and every node's Confirm waits for it, so the confirm cards
	// of parallel nodes come one at a time.
	Review *Gate
	// Categories map quick / deep to the model slot of the nodes a
	// planner adds.
	Categories map[string]string
	// Budget in dollars (0: none) pauses the run before the next node
	// once the nodes' cost reaches it (RunPaused); Continue lifts it for
	// the rest of the run. Cost prices one node's usage (ok=false:
	// unknown, counts as nothing).
	Budget float64
	Cost   func(n agentdag.Node, u ai.Usage) (float64, bool)
	// Exec runs a node with a CLI (agentdag.Node.CLI) instead of an
	// agent: it sends the CLI's events to emit and returns its final
	// text and usage. An *ai.LimitError puts the node back to pending
	// and pauses the run (NodeLimited, RunPaused with Limit) until
	// Continue runs it again. It runs in the node's goroutine; ctx ends
	// when the run stops.
	Exec func(ctx context.Context, n agentdag.Node, input string, emit func(ai.Event)) (string, *ai.Usage, error)
	// Finished (optional) runs in the node's goroutine once its agent or
	// CLI ended (done, failed or stopped), before the run sees the end
	// (G-WT: the app looks at the node's worktree). ctx may be done.
	Finished func(ctx context.Context, n agentdag.Node)
	// Approval (G-PLAN; ApprovalOff, ApprovalAsk, ApprovalAlways) makes
	// the run wait for the user's approval of its plan before a step
	// that may write files or run commands (approve.go). Acts (optional)
	// says what a node may do; nil: agentdag.Writes, no commands.
	Approval string
	Acts     func(n agentdag.Node) (writes, exec bool)
	// StallAfter (OMX-23; 0: off) stops an agent step whose model sent
	// nothing for this long (a running tool call does not count) and
	// starts it once more (NodeStalled); a second stall fails it.
	StallAfter time.Duration
}

// Handle is a started run.
type Handle struct {
	o      Options
	id     string // the run id (read by every goroutine; never changes)
	cancel context.CancelFunc
	done   chan struct{}
	resume chan struct{}
	// pause is set by Pause: the run pauses before its next node.
	pause atomic.Bool
	// decide carries the user's answer on a plan (approve.go); denied is
	// set by Deny.
	decide chan decision
	denied atomic.Bool

	emitMu sync.Mutex

	// run, layer and order change only on the loop goroutine (a plan
	// grows them), always under mu; the loop reads them without it.
	mu      sync.Mutex
	run     agentdag.Run
	layer   map[string]int
	order   map[string]int
	outputs map[string]string
	final   Event
	// verify and fix are the loop's node ids ("" without a loop; they
	// never change).
	verify, fix string
	// mail holds the messages Send queued for the running agents
	// (send.go).
	mail mailbox
	// stops are the running nodes' own cancels and stopped the nodes
	// the user stopped (StopNode); under mu.
	stops   map[string]context.CancelFunc
	stopped map[string]bool
}

type result struct {
	id        string
	output    string
	usage     *ai.Usage
	err       error
	cancelled bool
	elapsed   time.Duration
	// the checked plan of a planner node
	plan      []agentdag.Node
	planEdges []agentdag.Edge
	// the verdict of a loop's verify node (VerdictPass / Fail / Missing)
	// and its report
	verdict, report string
	// stalled: the watchdog stopped the node (OMX-23)
	stalled bool
	// limit: a CLI node hit its usage limit (it runs again on Continue)
	limit *ai.LimitError
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
	n = n.Clone()
	h := &Handle{o: o, id: n.ID, done: make(chan struct{}), resume: make(chan struct{}, 1), run: n,
		outputs: map[string]string{}, decide: make(chan decision, 1),
		stops: map[string]context.CancelFunc{}, stopped: map[string]bool{}}
	if l := n.Loop; l != nil {
		h.verify, h.fix = strings.TrimSpace(l.Verify), strings.TrimSpace(l.Fix.ID)
		l.Verify, l.Fix.ID = h.verify, h.fix
		l.Round = max(l.Round, 1)
	}
	h.index(ls)
	for _, nd := range n.Nodes {
		if nd.State == agentdag.StateCompleted {
			h.outputs[nd.ID] = o.Outputs[nd.ID]
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	crash.Go("an agent run", func() { h.loop(runCtx) })
	return h, nil
}

// index rebuilds the layer and order maps of h.run.
func (h *Handle) index(ls [][]string) {
	h.layer, h.order = map[string]int{}, map[string]int{}
	for i, l := range ls {
		for _, id := range l {
			h.layer[id] = i
		}
	}
	for i, nd := range h.run.Nodes {
		h.order[nd.ID] = i
	}
}

// Cancel stops every node; the run then ends with RunCancelled. Safe to
// call more than once and after the end.
func (h *Handle) Cancel() { h.cancel() }

// StoppedErr is the error of a step the user stopped (StopNode).
const StoppedErr = "stopped by the user"

// StopNode stops one running step (OMX-22): it ends as cancelled, the
// steps that need its answer are blocked, the others go on, and the run
// ends as failed. It refuses a step that does not run.
func (h *Handle) StopNode(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	stop := h.stops[id]
	if stop == nil {
		return errors.New("the step does not run now")
	}
	h.stopped[id] = true
	stop()
	return nil
}

// Continue resumes a run that its budget paused; the budget no longer
// applies to this run. A no-op when the run is not paused.
func (h *Handle) Continue() {
	select {
	case h.resume <- struct{}{}:
	default:
	}
}

// Pause asks the run to pause before its next node: the running nodes
// finish, then the run sends RunPaused (Budget 0) and waits for Continue
// or Cancel. A no-op after the end.
func (h *Handle) Pause() { h.pause.Store(true) }

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
	return h.run.Clone()
}

// round is the loop round (0 without a loop). Loop goroutine only.
func (h *Handle) round() int {
	if h.run.Loop == nil {
		return 0
	}
	return h.run.Loop.Round
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
	results := make(chan result, 2*agentdag.MaxNodes)
	running := 0
	var total *ai.Usage
	failErr := ""
	stopErr := "" // the user stopped a step (StopNode)
	stalls := map[string]int{}
	verdict := "" // the loop's verdict once verify passed or the rounds ran out
	spent, budget := 0.0, h.o.Budget
	var limited *ai.LimitError // a CLI's usage limit paused the run
	fail := func(id, msg string, usage *ai.Usage, elapsed time.Duration) {
		h.setState(id, func(n *agentdag.Node) {
			n.State = agentdag.StateFailed
			n.Error = cutRunes(agentdag.Clean(msg), agentdag.MaxErrorLen)
		})
		h.emit(Event{NodeID: id, Kind: NodeFailed, Err: msg, Usage: usage, Elapsed: elapsed})
		if failErr == "" {
			failErr = fmt.Sprintf("step %q: %s", h.title(id), msg)
		}
		stopNodes() // one failed step stops the run
		h.mu.Lock()
		blocked, ids := agentdag.Block(h.run)
		h.run = blocked
		h.mu.Unlock()
		for _, id := range ids {
			h.emit(Event{NodeID: id, Kind: NodeBlocked})
		}
	}
	for {
		if ready := agentdag.Ready(h.run); nodeCtx.Err() == nil && len(ready) > 0 {
			overBudget := budget > 0 && spent >= budget
			if h.needsApproval(ready) {
				// G-PLAN: the running nodes end first, then the plan is
				// shown and nothing starts before the user's answer.
				if running == 0 && h.awaitApproval(nodeCtx) {
					continue
				}
			} else if overBudget || h.pause.Load() {
				// Pause before the next node: the running nodes end
				// first, then the run waits for Continue or Cancel.
				if running == 0 {
					shown := budget // a Pause the user asked for says Budget 0
					if !overBudget || limited != nil {
						shown = 0
					}
					pe := Event{Kind: RunPaused, Cost: spent, Budget: shown, Usage: total}
					if limited != nil {
						pe.Limit, pe.ResetAt = limited.Msg, limited.ResetAt
						if pe.Limit == "" {
							pe.Limit = limited.Error()
						}
					}
					h.emit(pe)
					select {
					case <-h.resume:
						if overBudget && limited == nil {
							budget = 0
						}
						limited = nil
						h.pause.Store(false)
						h.emit(Event{Kind: RunResumed})
						continue
					case <-nodeCtx.Done():
					}
				}
			} else {
				for _, id := range ready {
					if running >= h.o.Parallel {
						break
					}
					input := h.input(id)
					h.setState(id, func(n *agentdag.Node) { n.State = agentdag.StateRunning; n.Attempt++; n.Error = "" })
					h.emit(Event{NodeID: id, Kind: NodeStart, Round: h.round()})
					running++
					nctx, stop := context.WithCancel(nodeCtx)
					h.mu.Lock()
					h.stops[id] = stop
					h.mu.Unlock()
					if h.o.Parallel > 1 && h.sharesWave(id) {
						nctx = context.WithValue(nctx, sharedWaveKey{}, true)
					}
					node := *h.node(id)
					crash.Go("an agent step", func() { h.runNode(nctx, node, input, results) })
				}
			}
		}
		if running == 0 {
			break
		}
		res := <-results
		running--
		h.mu.Lock()
		if stop := h.stops[res.id]; stop != nil {
			stop()
			delete(h.stops, res.id)
		}
		userStop := h.stopped[res.id]
		delete(h.stopped, res.id)
		h.mu.Unlock()
		total = total.Add(res.usage)
		if res.usage != nil && h.o.Cost != nil {
			if c, ok := h.o.Cost(*h.node(res.id), *res.usage); ok {
				spent += c
			}
		}
		switch {
		case res.stalled && nodeCtx.Err() == nil:
			quiet := StallText(h.o.StallAfter)
			if stalls[res.id]++; stalls[res.id] > 1 {
				fail(res.id, "no activity for "+quiet+", twice: stopped", res.usage, res.elapsed)
				break
			}
			h.setState(res.id, func(n *agentdag.Node) { n.State = agentdag.StatePending })
			h.emit(Event{NodeID: res.id, Kind: NodeStalled, Err: "no activity for " + quiet + ": stopped, it starts once more",
				Usage: res.usage, Elapsed: res.elapsed})
		case res.stalled:
			h.setState(res.id, func(n *agentdag.Node) { n.State = agentdag.StateCancelled })
			h.emit(Event{NodeID: res.id, Kind: NodeCancelled, Usage: res.usage, Elapsed: res.elapsed})
		case res.limit != nil && !res.cancelled:
			// The CLI's usage limit: the node waits (pending) and the run
			// pauses before anything else starts; Continue runs it again.
			h.setState(res.id, func(n *agentdag.Node) { n.State = agentdag.StatePending })
			msg := res.limit.Msg
			if msg == "" {
				msg = res.limit.Error()
			}
			h.emit(Event{NodeID: res.id, Kind: NodeLimited, Err: msg, ResetAt: res.limit.ResetAt, Usage: res.usage, Elapsed: res.elapsed})
			limited = res.limit
			h.pause.Store(true)
		case res.cancelled && userStop && nodeCtx.Err() == nil:
			// OMX-22: only this step stops; the steps after it cannot run.
			h.setState(res.id, func(n *agentdag.Node) { n.State = agentdag.StateCancelled; n.Error = StoppedErr })
			h.emit(Event{NodeID: res.id, Kind: NodeCancelled, Err: StoppedErr, Usage: res.usage, Elapsed: res.elapsed})
			if stopErr == "" {
				stopErr = fmt.Sprintf("step %q: %s", h.title(res.id), StoppedErr)
			}
			h.mu.Lock()
			blocked, ids := agentdag.Block(h.run)
			h.run = blocked
			h.mu.Unlock()
			for _, id := range ids {
				h.emit(Event{NodeID: id, Kind: NodeBlocked})
			}
		case res.cancelled:
			h.setState(res.id, func(n *agentdag.Node) { n.State = agentdag.StateCancelled })
			h.emit(Event{NodeID: res.id, Kind: NodeCancelled, Usage: res.usage, Elapsed: res.elapsed})
		case res.err != nil:
			fail(res.id, ai.Redact(res.err.Error()), res.usage, res.elapsed)
		default:
			var grown agentdag.Run
			var ls [][]string
			if len(res.plan) > 0 {
				var err error
				if grown, ls, err = h.grown(res); err != nil {
					fail(res.id, "the plan does not fit the run: "+err.Error(), res.usage, res.elapsed)
					continue
				}
			}
			h.mu.Lock()
			h.node(res.id).State = agentdag.StateCompleted
			h.outputs[res.id] = res.output
			h.mu.Unlock()
			h.emit(Event{NodeID: res.id, Kind: NodeDone, Output: res.output, Usage: res.usage, Elapsed: res.elapsed, Verdict: res.verdict})
			if res.id == h.verify && h.verify != "" {
				if verdict = h.afterVerify(res); verdict == "" {
					if err := h.nextRound(res); err != nil {
						fail(res.id, "the fix step does not fit the run: "+err.Error(), nil, 0)
					}
				}
			}
			if len(res.plan) > 0 {
				grown.Nodes[h.order[res.id]].State = agentdag.StateCompleted
				h.mu.Lock()
				h.run = grown
				h.index(ls)
				h.mu.Unlock()
				added := append([]agentdag.Node(nil), grown.Nodes[len(grown.Nodes)-len(res.plan):]...)
				h.emit(Event{NodeID: res.id, Kind: RunPlanned, Nodes: added})
				h.planKept(added)
			}
		}
	}
	// Whatever did not run is cancelled (the run stopped early).
	for _, n := range h.run.Nodes {
		if n.State == agentdag.StatePending {
			h.setState(n.ID, func(n *agentdag.Node) { n.State = agentdag.StateCancelled })
			h.emit(Event{NodeID: n.ID, Kind: NodeCancelled})
		}
	}
	final := Event{Usage: total, Elapsed: clock.Since(start), Round: h.round()}
	switch {
	case failErr != "":
		final.Kind, final.Err = RunFailed, failErr
	case runCtx.Err() == nil && stopErr != "":
		final.Kind, final.Err = RunFailed, stopErr
	case runCtx.Err() != nil:
		final.Kind = RunCancelled
		if h.denied.Load() {
			final.Err = DeniedErr
		}
	default:
		final.Kind, final.Output, final.Verdict = RunDone, h.lastOutput(), verdict
	}
	h.emit(final)
	h.mu.Lock()
	final.RunID = h.id
	h.final = final
	h.mu.Unlock()
	close(h.done)
}

// grown is the run with a planner's plan added (see grow) and its
// layers; it is checked again as a whole.
func (h *Handle) grown(res result) (agentdag.Run, [][]string, error) {
	g := grow(h.run, res.id, res.plan, res.planEdges, h.o.Categories)
	n, err := agentdag.Normalize(g)
	if err != nil {
		return agentdag.Run{}, nil, errors.New(strings.TrimPrefix(err.Error(), "agentdag: "))
	}
	ls, err := agentdag.Layers(n)
	if err != nil {
		return agentdag.Run{}, nil, err
	}
	return n, ls, nil
}

// afterVerify is the run's verdict after the verify node finished:
// VerdictPass, VerdictFail when it failed in the last round, or "" when
// it failed and another round runs.
func (h *Handle) afterVerify(res result) string {
	switch {
	case res.verdict == VerdictPass:
		return VerdictPass
	case h.run.Loop.Round >= h.run.Loop.MaxRounds:
		return VerdictFail
	}
	return ""
}

// nextRound starts the next loop round after a failing verify: the fix
// node joins the run the first time (edge fix → verify; a RunPlanned
// event tells the UI), fix and verify go back to pending, the report
// becomes the fix node's input and RunRound is sent.
func (h *Handle) nextRound(res result) error {
	g := h.run.Clone()
	if _, ok := h.order[h.fix]; !ok {
		f := g.Loop.Fix
		f.State, f.Attempt, f.Error = agentdag.StatePending, 0, ""
		if f.Title == "" {
			f.Title = f.ID
		}
		if f.Slot == "" {
			f.Slot = h.o.Categories[f.Category]
		}
		g.Nodes = append(g.Nodes, f)
		g.Edges = append(g.Edges, agentdag.Edge{From: f.ID, To: h.verify})
		for i := range g.Nodes {
			if n := &g.Nodes[i]; n.ID == h.verify && n.Input.Deps != nil {
				n.Input.Deps = append(append([]string(nil), n.Input.Deps...), f.ID)
			}
		}
	}
	n, err := agentdag.Normalize(g)
	if err != nil {
		return errors.New(strings.TrimPrefix(err.Error(), "agentdag: "))
	}
	ls, err := agentdag.Layers(n)
	if err != nil {
		return err
	}
	added := len(n.Nodes) > len(h.run.Nodes)
	for i := range n.Nodes {
		if id := n.Nodes[i].ID; id == h.fix || id == h.verify {
			n.Nodes[i].State, n.Nodes[i].Error = agentdag.StatePending, ""
		}
	}
	n.Loop.Round++
	n.Loop.Report = cutRunes(res.report, PlanPromptMaxBytes)
	h.mu.Lock()
	h.run = n
	h.index(ls)
	h.mu.Unlock()
	if added {
		h.emit(Event{NodeID: h.verify, Kind: RunPlanned, Nodes: []agentdag.Node{*h.node(h.fix)}})
	}
	h.emit(Event{Kind: RunRound, Round: n.Loop.Round, Err: n.Loop.Report, Verdict: res.verdict})
	h.planKept([]agentdag.Node{*h.node(h.fix)})
	return nil
}

// sharesWave reports whether node id may write files and another node
// that may write could run at the same time: one that is running, or
// still to run and not after id (not downstream of it). Layers are not
// enough: a node of the next layer whose deps ended early runs next to a
// slow node of this one, and both would work in the user's tree. It runs
// on the loop goroutine.
func (h *Handle) sharesWave(id string) bool {
	n := h.node(id)
	if n == nil || !agentdag.Writes(*n) {
		return false
	}
	after := map[string]bool{id: true}
	for grew := true; grew; {
		grew = false
		for _, e := range h.run.Edges {
			if after[e.From] && !after[e.To] {
				after[e.To], grew = true, true
			}
		}
	}
	for _, o := range h.run.Nodes {
		if !after[o.ID] && agentdag.Writes(o) && !o.State.Done() && !o.State.Stops() {
			return true
		}
	}
	return false
}

// runNode builds and runs one node's agent and sends its result.
func (h *Handle) runNode(ctx context.Context, n agentdag.Node, input string, out chan<- result) {
	start := clock.Now()
	res := result{id: n.ID}
	defer func() {
		if r := recover(); r != nil {
			res.err = fmt.Errorf("step crashed: %v", r)
		}
		if f := h.o.Finished; f != nil {
			f(ctx, n)
		}
		res.elapsed = clock.Since(start)
		out <- res
	}()
	ctx = context.WithValue(ctx, nodeKey{}, [2]string{n.ID, h.id})
	if g := h.o.Review; g != nil {
		ctx = context.WithValue(ctx, reviewKey{}, g)
	}
	if h.o.Parallel > 1 {
		ctx = context.WithValue(ctx, parallelKey{}, true)
	}
	if n.CLI != "" {
		h.runCLI(ctx, n, input, &res)
		return
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
	h.mail.openNode(n.ID)
	defer h.mail.closeNode(n.ID, func(text string) {
		h.emit(Event{NodeID: n.ID, Kind: NodeMessage, Output: text, Err: "the step ended before it read the message"})
	})
	agent.Inbox = h.inbox(n.ID, agent.Inbox)
	if g, ask := h.o.Review, agent.Confirm; g != nil && ask != nil {
		// The confirm queue: one card at a time over all nodes; a node
		// that waits for its turn stops with the run.
		agent.Confirm = func(ctx context.Context, t ai.Tool, human string) bool {
			if g.Acquire(ctx) != nil {
				return false
			}
			defer g.Release()
			return ask(ctx, t, human)
		}
	}
	parent := ctx
	var dog *watchdog
	if h.o.StallAfter > 0 {
		var stall context.CancelFunc
		ctx, stall = context.WithCancel(ctx)
		defer stall()
		dog = startWatchdog(h.o.StallAfter, stall)
		defer dog.stop()
	}
	var raw []byte // the end of the streamed text, for Tail
	fail := ""
	emit := func(e ai.Event) {
		if dog != nil {
			switch e.Kind {
			case ai.EventToolStart:
				dog.touch(1)
			case ai.EventToolResult:
				dog.touch(-1)
			default:
				dog.touch(0)
			}
		}
		switch e.Kind {
		case ai.EventText:
			raw = appendTail(raw, e.Text)
			h.emit(Event{NodeID: n.ID, Kind: NodeText, AI: e, Tail: tail(raw)})
		case ai.EventToolStart, ai.EventToolResult:
			h.emit(Event{NodeID: n.ID, Kind: NodeTool, AI: e})
		case ai.EventDone:
			res.usage = res.usage.Add(e.Usage)
		case ai.EventError:
			res.usage, fail = res.usage.Add(e.Usage), e.Detail
		default:
			h.emit(Event{NodeID: n.ID, Kind: NodeAI, AI: e})
		}
	}
	added := agent.Run(ctx, history, emit)
	if ctx.Err() == nil && fail == "" && n.Plan {
		taken := map[string]bool{}
		for _, x := range h.Snapshot().Nodes {
			taken[x.ID] = true
		}
		if h.fix != "" {
			taken[h.fix] = true // the loop adds it later
		}
		res.plan, res.planEdges, err = ParsePlan(finalText(added), taken)
		if err != nil { // one repair request with the error
			h.emit(Event{NodeID: n.ID, Kind: NodeRepair, Err: err.Error()})
			hist := append(append(history[:len(history):len(history)], added...),
				ai.Message{Role: ai.RoleUser, Content: RepairPrompt(err)})
			raw = nil
			added = agent.Run(ctx, hist, emit)
			if ctx.Err() == nil && fail == "" {
				res.plan, res.planEdges, err = ParsePlan(finalText(added), taken)
				if err != nil {
					fail = "the plan is not valid after one repair: " + err.Error()
				}
			}
		}
	}
	if ctx.Err() == nil && fail == "" && n.ID == h.verify && h.verify != "" {
		pass, report, ok := ParseVerdict(finalText(added))
		if !ok { // one repair request: the verdict line is missing
			h.emit(Event{NodeID: n.ID, Kind: NodeRepair, Err: "no VERDICT line", Verdict: VerdictMissing})
			hist := append(append(history[:len(history):len(history)], added...),
				ai.Message{Role: ai.RoleUser, Content: VerifyRepairPrompt})
			raw = nil
			added = agent.Run(ctx, hist, emit)
			pass, report, ok = ParseVerdict(finalText(added))
		}
		switch {
		case !ok:
			res.verdict, res.report = VerdictMissing, NoVerdictReport+"\n\n"+report
		case pass:
			res.verdict, res.report = VerdictPass, report
		default:
			res.verdict, res.report = VerdictFail, report
		}
	}
	switch {
	case dog != nil && dog.fired.Load() && parent.Err() == nil:
		res.stalled = true
	case ctx.Err() != nil:
		res.cancelled = true
	case fail != "":
		res.err = errors.New(fail)
	default:
		res.output = finalText(added)
	}
}

// runCLI runs a CLI node through Options.Exec (CS-1). A verify node's
// answer needs a verdict line; without one the CLI is asked once more
// with its answer and VerifyRepairPrompt (CS-2).
func (h *Handle) runCLI(ctx context.Context, n agentdag.Node, input string, res *result) {
	if h.o.Exec == nil {
		res.err = fmt.Errorf("no CLI runner for %s", ai.CLITitle(n.CLI))
		return
	}
	var raw []byte
	emit := func(e ai.Event) {
		switch e.Kind {
		case ai.EventText:
			raw = appendTail(raw, e.Text)
			h.emit(Event{NodeID: n.ID, Kind: NodeText, AI: e, Tail: tail(raw)})
		case ai.EventToolStart, ai.EventToolResult:
			h.emit(Event{NodeID: n.ID, Kind: NodeTool, AI: e})
		case ai.EventDone, ai.EventError:
		default:
			h.emit(Event{NodeID: n.ID, Kind: NodeAI, AI: e})
		}
	}
	exec := func(in string) (string, bool) {
		text, u, err := h.o.Exec(ctx, n, in, emit)
		res.usage = res.usage.Add(u)
		var le *ai.LimitError
		switch {
		case ctx.Err() != nil:
			res.cancelled = true
		case errors.As(err, &le):
			res.limit = le
		case err != nil:
			res.err = err
		default:
			return strings.TrimSpace(text), true
		}
		return "", false
	}
	text, ok := exec(input)
	if !ok {
		return
	}
	if n.ID == h.verify && h.verify != "" {
		pass, report, found := ParseVerdict(text)
		if !found {
			h.emit(Event{NodeID: n.ID, Kind: NodeRepair, Err: "no VERDICT line", Verdict: VerdictMissing})
			raw = nil
			again := input + "\n\n### Your answer\n" + text + "\n\n" + VerifyRepairPrompt
			if text, ok = exec(again); !ok {
				return
			}
			pass, report, found = ParseVerdict(text)
		}
		switch {
		case !found:
			res.verdict, res.report = VerdictMissing, NoVerdictReport+"\n\n"+report
		case pass:
			res.verdict, res.report = VerdictPass, report
		default:
			res.verdict, res.report = VerdictFail, report
		}
	}
	res.output = text
}

// input is node id's input: the user message (unless NoUser), the task
// of a planned node, then the output of each dependency (limited by InputRule.Deps) in layer order,
// cut at InputMaxBytes.
func (h *Handle) input(id string) string {
	n := h.node(id)
	var parts []string
	if !n.Input.NoUser && strings.TrimSpace(h.o.User) != "" {
		parts = append(parts, h.o.User)
	}
	if n.Prompt != "" {
		parts = append(parts, "### Task\n"+n.Prompt)
	}
	if l := h.run.Loop; l != nil && id == h.fix && strings.TrimSpace(l.Report) != "" {
		parts = append(parts, fmt.Sprintf("### Verify report (round %d)\n%s", max(l.Round-1, 1), l.Report))
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
