package mcp

import (
	"context"
	"sort"
	"sync"
)

// Background calls (MCPX-19, RT-21): while an MCP tool runs in a chat
// turn, the user can move it to the background (Ctrl+B). The tool then
// answers the model at once ("still running"), the call goes on (the
// turn's end no longer stops it) and its result comes on a channel, so
// the app can add it to the chat.

// BackgroundResult is a background call's end.
type BackgroundResult struct {
	Label string // mcp.<server>.<tool>
	Text  string // the result as the model would get it
	Err   error
}

// Background holds a turn's running MCP calls that may be detached.
type Background struct {
	mu    sync.Mutex
	next  int
	calls map[int]*bgCall
}

type bgCall struct {
	label  string
	detach chan struct{}
	result chan BackgroundResult
}

// NewBackground makes an empty set.
func NewBackground() *Background { return &Background{calls: map[int]*bgCall{}} }

type backgroundKey struct{}

// WithBackground lets the MCP tools run with ctx be moved to b.
func WithBackground(ctx context.Context, b *Background) context.Context {
	return context.WithValue(ctx, backgroundKey{}, b)
}

func backgroundFrom(ctx context.Context) *Background {
	b, _ := ctx.Value(backgroundKey{}).(*Background)
	return b
}

func (b *Background) add(label string) (int, *bgCall) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	c := &bgCall{label: label, detach: make(chan struct{}), result: make(chan BackgroundResult, 1)}
	b.calls[b.next] = c
	return b.next, c
}

func (b *Background) remove(id int) {
	b.mu.Lock()
	delete(b.calls, id)
	b.mu.Unlock()
}

// Running are the labels of the calls that can move to the background.
func (b *Background) Running() []string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, c := range b.calls {
		out = append(out, c.label)
	}
	sort.Strings(out)
	return out
}

// Detach moves every running call to the background and returns the
// channels their results come on (one value each).
func (b *Background) Detach() []<-chan BackgroundResult {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []<-chan BackgroundResult
	for id, c := range b.calls {
		close(c.detach)
		out = append(out, c.result)
		delete(b.calls, id)
	}
	return out
}

// BackgroundNote is what the model gets for a call moved away.
func BackgroundNote(label string) string {
	return "The call " + label + " is still running in the background. Its result will be added to the chat when it ends. " +
		"Do not call it again now; go on with other work or tell the user."
}

type callOut struct {
	res CallResult
	err error
}

// runDetachable runs one call that b may move to the background;
// render makes the result text (ctx: the call's, or Background later).
func runDetachable(ctx context.Context, b *Background, label string, run func(ctx context.Context) (CallResult, error), render func(ctx context.Context, o callOut) (string, error)) (string, error) {
	cctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	var mu sync.Mutex
	detached := false
	if e := elicitorFrom(ctx); e != nil { // a background call asks nobody
		cctx = WithElicitor(cctx, func(c context.Context, r ElicitRequest) ElicitResult {
			mu.Lock()
			d := detached
			mu.Unlock()
			if d {
				return ElicitResult{Action: ElicitDecline}
			}
			return e(c, r)
		})
	}
	done := make(chan callOut, 1)
	go func() {
		res, err := run(cctx)
		done <- callOut{res, err}
	}()
	id, c := b.add(label)
	select {
	case o := <-done:
		b.remove(id)
		cancel()
		return render(ctx, o)
	case <-ctx.Done():
		b.remove(id)
		cancel()
		<-done
		return "", ctx.Err()
	case <-c.detach:
		mu.Lock()
		detached = true
		mu.Unlock()
		go func() {
			o := <-done
			cancel()
			text, err := render(context.Background(), o)
			c.result <- BackgroundResult{Label: label, Text: text, Err: err}
		}()
		return BackgroundNote(label), nil
	}
}
