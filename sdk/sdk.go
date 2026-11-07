// Package sdk is the Go SDK of termocode's agent runtime (CMP-40): a
// program builds on the agents that run in a termocode's terminal tabs.
// It lists the tabs and their agent states, types a prompt into an
// agent's tab, waits for the agent to finish, watches state changes,
// reports its own state (an agent hook) and notes on its worktree.
//
// It speaks the control socket of a running termocode: one JSON request
// per line, one JSON reply per line (see docs/guides/sdk.md, which also
// shows the same calls from Python and the shell). Only processes of the
// same user reach the socket, and termocode still asks the user before a
// prompt is typed into a tab.
//
//	c, err := sdk.FromEnv()
//	if err != nil { … }
//	tab, err := c.FindTab(ctx, func(t sdk.Tab) bool { return t.Agent == "claude" })
//	st, err := c.Run(ctx, tab.ID, "fix the failing tests")
package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/agentctl"
)

// State is an agent's state in a tab.
type State string

// The states an agent reports or a wait waits for.
const (
	StateWorking State = "working"
	StateWaiting State = "waiting" // it waits for the user (a question, a permission)
	StateDone    State = "done"
)

// Valid reports whether s is a known state.
func (s State) Valid() bool { return agentctl.ValidState(string(s)) }

// ErrNoTab is returned by FindTab when no tab matches.
var ErrNoTab = errors.New("termocode sdk: no matching tab")

// Tab is one terminal tab of the running termocode.
type Tab struct {
	ID     string // "t3"
	Name   string
	Agent  string // claude, codex, … ("" for a plain shell)
	State  State  // "" when no agent state is known
	Hooked bool   // the agent reports its own state (else termocode reads the screen)
	Active bool   // the tab the user looks at
}

// Worktree is one git worktree termocode knows.
type Worktree struct {
	Path       string
	Branch     string
	Name       string
	Kind       string // main, user, agent, external
	Gone       bool
	Parent     string
	Link       string // "#412 open", "PR #12 merged · CI ✓"
	Checkpoint string
	Note       string // the last note
	Here       bool   // the folder asked about is in it
}

// Event is a change Watch sees.
type Event struct {
	Tab   Tab
	State State // the new state
	Was   State // the state before ("" the first time the tab is seen)
	Gone  bool  // the tab was closed
}

// Client talks to one running termocode. It is safe for concurrent use:
// each call opens its own connection.
type Client struct {
	socket string
}

// Dial returns a client of the control socket at path.
func Dial(socket string) *Client { return &Client{socket: socket} }

// FromEnv returns the client of the termocode this program runs in:
// $TERMOCODE_SOCKET inside a termocode terminal, else the socket of the
// current folder's workspace.
func FromEnv() (*Client, error) {
	if p := os.Getenv(agentctl.EnvSocket); p != "" {
		return Dial(p), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return ForWorkspace(cwd)
}

// ForWorkspace returns the client of the termocode that has the
// workspace folder dir open.
func ForWorkspace(dir string) (*Client, error) {
	p, err := workspaceSocket(dir)
	if err != nil {
		return nil, err
	}
	return Dial(p), nil
}

// Socket is the control socket's path.
func (c *Client) Socket() string { return c.socket }

// InTerminal reports whether this program runs in a termocode terminal.
func InTerminal() bool { return os.Getenv(agentctl.EnvMarker) == "1" }

// MyTab is the id of the termocode tab this program runs in ("" outside).
func MyTab() string { return os.Getenv(agentctl.EnvTab) }

// defaultTimeout bounds a call whose context has no deadline.
const defaultTimeout = 3 * time.Minute

// defaultWait bounds a wait whose context has no deadline.
const defaultWait = 10 * time.Minute

func (c *Client) call(ctx context.Context, req agentctl.Request, timeout time.Duration) (agentctl.Reply, error) {
	if c.socket == "" {
		return agentctl.Reply{}, errors.New("termocode sdk: no socket: run inside a termocode terminal or its workspace")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.socket)
	if err != nil {
		return agentctl.Reply{}, fmt.Errorf("termocode sdk: termocode is not running here: %w", err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return agentctl.Reply{}, ctxErr(ctx, err)
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 4096), 4<<20)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return agentctl.Reply{}, ctxErr(ctx, err)
		}
		return agentctl.Reply{}, errors.New("termocode sdk: termocode closed the connection")
	}
	var rep agentctl.Reply
	if err := json.Unmarshal(sc.Bytes(), &rep); err != nil {
		return agentctl.Reply{}, fmt.Errorf("termocode sdk: bad reply: %w", err)
	}
	if !rep.OK {
		if rep.Error == "" {
			rep.Error = "termocode refused the request"
		}
		return rep, errors.New(rep.Error)
	}
	return rep, nil
}

// ctxErr prefers the context's error (canceled, deadline) over the
// connection error it caused.
func ctxErr(ctx context.Context, err error) error {
	if ce := ctx.Err(); ce != nil {
		return ce
	}
	return err
}

// Tabs lists the terminal tabs and their agent states.
func (c *Client) Tabs(ctx context.Context) ([]Tab, error) {
	rep, err := c.call(ctx, agentctl.Request{Op: agentctl.OpList}, 10*time.Second)
	if err != nil {
		return nil, err
	}
	out := make([]Tab, 0, len(rep.Tabs))
	for _, t := range rep.Tabs {
		out = append(out, Tab{ID: t.ID, Name: t.Name, Agent: t.Agent, State: State(t.State), Hooked: t.Source == "hook", Active: t.Active})
	}
	return out, nil
}

// FindTab returns the first tab match accepts (ErrNoTab when none).
func (c *Client) FindTab(ctx context.Context, match func(Tab) bool) (Tab, error) {
	tabs, err := c.Tabs(ctx)
	if err != nil {
		return Tab{}, err
	}
	for _, t := range tabs {
		if match(t) {
			return t, nil
		}
	}
	return Tab{}, ErrNoTab
}

// Prompt types text into the tab and presses Enter. termocode asks the
// user first, so the call can take as long as the user needs.
func (c *Client) Prompt(ctx context.Context, tab, text string) error {
	return c.send(ctx, tab, text, true)
}

// Type types text into the tab without pressing Enter.
func (c *Client) Type(ctx context.Context, tab, text string) error {
	return c.send(ctx, tab, text, false)
}

func (c *Client) send(ctx context.Context, tab, text string, enter bool) error {
	if strings.TrimSpace(tab) == "" {
		return errors.New("termocode sdk: no tab")
	}
	_, err := c.call(ctx, agentctl.Request{Op: agentctl.OpPrompt, Tab: tab, Text: text, Enter: &enter}, defaultTimeout)
	return err
}

// Wait waits until the tab's agent reaches the state and returns it. The
// context's deadline is the wait's timeout (10 minutes without one).
func (c *Client) Wait(ctx context.Context, tab string, until State) (State, error) {
	if !until.Valid() {
		return "", fmt.Errorf("termocode sdk: unknown state %q (want %s)", until, strings.Join(agentctl.States, ", "))
	}
	left := defaultWait
	if dl, ok := ctx.Deadline(); ok {
		left = time.Until(dl)
	}
	if left < time.Second {
		return "", context.DeadlineExceeded
	}
	req := agentctl.Request{Op: agentctl.OpWait, Tab: tab, Until: string(until), TimeoutS: int(left.Seconds())}
	rep, err := c.call(ctx, req, left+20*time.Second)
	if err != nil {
		return "", err
	}
	return State(rep.State), nil
}

// startGrace is how long Run waits to see the agent start working.
const startGrace = 30 * time.Second

// Run prompts the tab's agent and waits until it is done or waits for
// the user. It first waits a short time to see the agent start, so an
// agent that was already done does not end the run at once.
func (c *Client) Run(ctx context.Context, tab, text string) (State, error) {
	if err := c.Prompt(ctx, tab, text); err != nil {
		return "", err
	}
	sctx, cancel := context.WithTimeout(ctx, startGrace)
	_, _ = c.Wait(sctx, tab, StateWorking) // a timeout here is fine: a fast agent may be done already
	cancel()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return c.Wait(ctx, tab, StateDone)
}

// Report says the state of the agent in tab (an agent's hook; tab ""
// is MyTab). agent names it: claude, codex, …
func (c *Client) Report(ctx context.Context, tab, agent string, state State) error {
	if !state.Valid() {
		return fmt.Errorf("termocode sdk: unknown state %q", state)
	}
	if tab == "" {
		tab = MyTab()
	}
	_, err := c.call(ctx, agentctl.Request{Op: agentctl.OpReport, Tab: tab, Agent: agent, State: string(state)}, 5*time.Second)
	return err
}

// Watch polls the tabs every interval (1s when 0 or less) and sends
// each state change, and a Gone event when a tab closes. The channel is
// closed when ctx ends. A failed poll is skipped (termocode may restart).
func (c *Client) Watch(ctx context.Context, every time.Duration) <-chan Event {
	if every <= 0 {
		every = time.Second
	}
	ch := make(chan Event, 16)
	go func() {
		defer close(ch)
		seen := map[string]Tab{}
		send := func(ev Event) bool {
			select {
			case ch <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			if tabs, err := c.Tabs(ctx); err == nil {
				now := map[string]bool{}
				for _, tab := range tabs {
					now[tab.ID] = true
					old := seen[tab.ID]
					seen[tab.ID] = tab
					if tab.State == old.State {
						continue // no change (a new tab with no state is not news yet)
					}
					if !send(Event{Tab: tab, State: tab.State, Was: old.State}) {
						return
					}
				}
				for id, tab := range seen {
					if !now[id] {
						delete(seen, id)
						if !send(Event{Tab: tab, Was: tab.State, Gone: true}) {
							return
						}
					}
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	return ch
}

// worktreeStates are the statuses WorktreeStatus takes.
var worktreeStates = []string{"working", "blocked", "done"}

// Worktrees lists the worktrees of the repository dir is in; the one dir
// is in has Here set.
func (c *Client) Worktrees(ctx context.Context, dir string) ([]Worktree, error) {
	rep, err := c.call(ctx, agentctl.Request{Op: agentctl.OpWorktreeList, Dir: dir}, 20*time.Second)
	if err != nil {
		return nil, err
	}
	out := make([]Worktree, 0, len(rep.Worktrees))
	for _, w := range rep.Worktrees {
		out = append(out, Worktree{Path: w.Path, Branch: w.Branch, Name: w.Name, Kind: w.Kind, Gone: w.Gone,
			Parent: w.Parent, Link: w.Link, Checkpoint: w.Checkpoint, Note: w.Note, Here: w.Here})
	}
	return out, nil
}

// WorktreeNote adds a note to the worktree dir is in.
func (c *Client) WorktreeNote(ctx context.Context, dir, text string) error {
	_, err := c.call(ctx, agentctl.Request{Op: agentctl.OpWorktreeNote, Dir: dir, Text: text}, 10*time.Second)
	return err
}

// WorktreeStatus sets the status of the worktree dir is in: working,
// blocked or done.
func (c *Client) WorktreeStatus(ctx context.Context, dir, status string) error {
	ok := false
	for _, s := range worktreeStates {
		ok = ok || s == status
	}
	if !ok {
		return fmt.Errorf("termocode sdk: unknown worktree status %q (want %s)", status, strings.Join(worktreeStates, ", "))
	}
	_, err := c.call(ctx, agentctl.Request{Op: agentctl.OpWorktreeStatus, Dir: dir, State: status}, 10*time.Second)
	return err
}

// WorktreeLink links the worktree dir is in to an issue or PR (a number,
// #N or URL). termocode asks the user first.
func (c *Client) WorktreeLink(ctx context.Context, dir, ref string) error {
	_, err := c.call(ctx, agentctl.Request{Op: agentctl.OpWorktreeLink, Dir: dir, Text: ref}, defaultTimeout)
	return err
}
