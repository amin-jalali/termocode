package run

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// tools answers ReadOnly from a map and logs each check into calls.
type tools struct {
	c   *calls
	ro  map[string]bool
	err error
}

func (t tools) ReadOnly(ctx context.Context, server, tool string) (bool, error) {
	t.c.add("readonly %s %s", server, tool)
	return t.ro[server+"/"+tool], t.err
}

// roFakes logs ReadOnly into the source and write calls.
type roFakes struct{ *fakes }

func (f roFakes) Read(ctx context.Context, c SourceCall) (SourceResult, error) {
	f.add("source %s ro=%v", stepKey(c.Step), c.ReadOnly)
	return SourceResult{Items: f.items}, nil
}

func (f roFakes) Write(ctx context.Context, c WriteCall) (Output, error) {
	f.add("write %s ro=%v", stepKey(c.Step), c.ReadOnly)
	return Output{}, nil
}

const mcpFlow = `{"version": 1, "name": "w", "retries": {"max": 2}, "steps": [
  {"id": "bugs", "type": "source", "from": "mcp", "server": "linear", "tool": "list_issues"},
  {"id": "note", "type": "write", "to": "mcp", "server": "linear", "tool": "create_comment", "args": {"body": "hi"}}
]}`

func TestReadOnlyIsKnownBeforeTheCall(t *testing.T) {
	cases := []struct {
		name   string
		ro     map[string]bool
		noTool bool // Options.Tools nil
		err    error
		ask    *bool // nil: Options.Ask nil
		dry    bool
		calls  []string
		state  string // of the source step
	}{
		{name: "read-only: no card", ro: map[string]bool{"linear/list_issues": true}, state: StateDone,
			calls: []string{"readonly linear list_issues", "source bugs ro=true", "readonly linear create_comment", "write note ro=false"}},
		{name: "not read-only: asked, yes", ask: ptr(true), state: StateDone,
			calls: []string{"readonly linear list_issues", "ask bugs linear list_issues", "source bugs ro=false", "readonly linear create_comment", "write note ro=false"}},
		{name: "not read-only: asked, no", ask: ptr(false), state: StateDenied,
			calls: []string{"readonly linear list_issues", "ask bugs linear list_issues"}},
		{name: "no answer is no", state: StateDenied, calls: []string{"readonly linear list_issues"}},
		{name: "no tool check: asked", noTool: true, ask: ptr(true), state: StateDone,
			calls: []string{"ask bugs linear list_issues", "source bugs ro=false", "write note ro=false"}},
		{name: "check fails: not called", err: errors.New("not connected"), ask: ptr(true), state: StateFailed,
			calls: []string{"readonly linear list_issues", "readonly linear list_issues", "readonly linear list_issues"}},
		{name: "dry asks too, writes not checked", dry: true, ask: ptr(true), state: StateDone,
			calls: []string{"readonly linear list_issues", "ask bugs linear list_issues", "source bugs ro=false"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fk := roFakes{newFakes(items("a")...)}
			opts := fk.options()
			opts.Source, opts.Write, opts.Dry = fk, fk, tc.dry
			if !tc.noTool {
				opts.Tools = tools{c: &fk.calls, ro: tc.ro, err: tc.err}
			}
			if tc.ask != nil {
				opts.Ask = func(ctx context.Context, a AskCall) (bool, error) {
					fk.add("ask %s %s %s", stepKey(a.Step), a.Server, a.Tool)
					return *tc.ask, nil
				}
			}
			rf := wait(t, Start(context.Background(), mustParse(t, mcpFlow), opts))
			if got := fk.all(); !slices.Equal(got, tc.calls) {
				t.Fatalf("calls %q, want %q", got, tc.calls)
			}
			if st := rf.Steps["bugs"].State; st != tc.state {
				t.Fatalf("source state %q, want %q (%s)", st, tc.state, rf.Steps["bugs"].Error)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
