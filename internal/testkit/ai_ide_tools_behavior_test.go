package testkit

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// toolCall is one native tool call the fake model makes.
type toolCall struct {
	name string
	args map[string]any
}

// fakeToolModel is a fake OpenAI-compatible model: it makes the given
// tool calls, one per request, then answers "fake-done".
func fakeToolModel(t *testing.T, calls ...toolCall) (*aiScript, *httptest.Server) {
	replies := make([]aiReply, 0, len(calls)+1)
	for _, c := range calls {
		replies = append(replies, aiReply{tool: c.name, args: c.args})
	}
	return newAIScript(t, append(replies, aiReply{text: "fake-done"})...)
}

// clickAIAllow clicks "Allow" in the "AI wants to" dialog.
func clickAIAllow(d *Driver) {
	d.T.Helper()
	for y, line := range strings.Split(screen(d), "\n") {
		if !strings.Contains(line, "Deny") {
			continue
		}
		if j := strings.Index(line, " Allow "); j >= 0 {
			d.Click(ansi.StringWidth(line[:j])+2, y)
			return
		}
	}
	d.T.Fatalf("no Allow button on screen:\n%s", screen(d))
}

// Spec: Given the AI chat, when the user asks "open it" and the model
// answers with an open_file tool call, then the file opens in the editor
// as the active tab, with no confirm dialog. The agent used to say it
// could not open files because no tool switched the active buffer.
func TestAIOpenFileToolOpensTab(t *testing.T) {
	_, srv := fakeToolModel(t, toolCall{"open_file", map[string]any{"path": "notes/todo.md", "line": 3}})
	d := aiDriver(t, srv.URL, nil, false)
	open(d, "a.txt")
	askAI(d, "open it")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "notes/todo.md" }, 5*time.Second) {
		got, a := tabs(d)
		t.Fatalf("todo.md not the active tab: active %q, tabs %v\n%s", a, got, screen(d))
	}
	if !d.WaitForText("fake-done", 5*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "AI wants to") {
		t.Errorf("open_file asked for confirmation:\n%s", screen(d))
	}
}

// aiToolCase is one row of TestAIToolsTable: the model makes call; when
// confirm is set the "AI wants to" dialog must show and the test clicks
// Allow, otherwise no dialog may show. check looks at the screen, the
// IDE state and the tool results sent back to the model.
type aiToolCase struct {
	name    string
	call    toolCall
	git     bool
	setup   func(d *Driver)
	confirm bool
	// review: the review card must show; the test accepts every file.
	review bool
	// noReview runs the case with ai_edit_review = auto (the old path).
	noReview bool
	check    func(t *testing.T, d *Driver, results []string)
}

// acceptAllReview clicks "Accept file" until the review card is gone.
func acceptAllReview(d *Driver) {
	d.T.Helper()
	if !d.WaitForText("Review AI changes", 5*time.Second) {
		d.T.Fatalf("no review card:\n%s", screen(d))
	}
	for i := 0; i < 10 && strings.Contains(screen(d), "Review AI changes"); i++ {
		clickTextOn(d, "Accept file")
		d.WaitFor(func() bool { return !strings.Contains(screen(d), "Accept file") || i > 8 }, 300*time.Millisecond)
	}
}

// clickTextOn clicks the first place s shows on screen.
func clickTextOn(d *Driver, s string) {
	d.T.Helper()
	for y, line := range strings.Split(screen(d), "\n") {
		if j := strings.Index(line, s); j >= 0 {
			d.Click(ansi.StringWidth(line[:j])+1, y)
			return
		}
	}
	d.T.Fatalf("%q not on screen:\n%s", s, screen(d))
}

// aiToolCases grow with every Group K step (K2–K8 add rows).
var aiToolCases = []aiToolCase{
	{
		name: "list_actions finds commands, never ids left out",
		call: toolCall{"list_actions", map[string]any{"query": "fold"}},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "fold-all: View: Fold All (View; auto)") {
				t.Errorf("results %q", r)
			}
		},
	},
	{
		name: "list_actions hides never ids",
		call: toolCall{"list_actions", map[string]any{"query": "quit"}},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || strings.Contains(r[0], "- quit:") {
				t.Errorf("results %q", r)
			}
		},
	},
	{
		name: "execute_action auto id runs without asking",
		call: toolCall{"execute_action", map[string]any{"id": "toggle-explorer"}},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || r[0] != "ran toggle-explorer" {
				t.Errorf("results %q", r)
			}
			if explorerShows(d, "b.txt") {
				t.Errorf("explorer still shown:\n%s", screen(d))
			}
		},
	},
	{
		name: "execute_action confirm id runs after Allow",
		call: toolCall{"execute_action", map[string]any{"id": "save"}},
		setup: func(d *Driver) {
			lua(d, `vim.api.nvim_buf_set_lines(0, 0, 1, false, { "changed" }); return ''`)
		},
		confirm: true,
		check: func(t *testing.T, d *Driver, r []string) {
			if !eventually(d, func() bool { return strings.HasPrefix(diskFile(d, "a.txt"), "changed\n") }) {
				t.Errorf("a.txt not saved: %q; results %q", diskFile(d, "a.txt"), r)
			}
		},
	},
	{
		name: "execute_action never id is refused without a dialog",
		call: toolCall{"execute_action", map[string]any{"id": "quit"}},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "not allowed") {
				t.Errorf("results %q", r)
			}
			if d.Quit() {
				t.Error("the app quit")
			}
		},
	},
}

// Spec: each AI tool runs through the real app (fake model, real nvim)
// with the right confirm rule and the right effect on screen.
func TestAIToolsTable(t *testing.T) {
	for _, tc := range aiToolCases {
		t.Run(tc.name, func(t *testing.T) {
			s, srv := fakeToolModel(t, tc.call)
			var d *Driver
			if tc.noReview {
				d = aiDriverConfig(t, srv.URL, nil, tc.git, `"ai_edit_review":"auto"`)
			} else {
				d = aiDriver(t, srv.URL, nil, tc.git)
			}
			open(d, "a.txt")
			if tc.setup != nil {
				tc.setup(d)
			}
			askAI(d, "do it")
			if tc.confirm {
				if !d.WaitForText("AI wants to", 5*time.Second) {
					t.Fatalf("no confirm dialog:\n%s", screen(d))
				}
				clickAIAllow(d)
			}
			if tc.review {
				acceptAllReview(d)
			}
			if !d.WaitForText("fake-done", 5*time.Second) {
				t.Fatalf("no final answer (a dialog may be waiting):\n%s", screen(d))
			}
			if !tc.confirm && strings.Contains(screen(d), "AI wants to") {
				t.Errorf("asked for confirmation:\n%s", screen(d))
			}
			tc.check(t, d, s.toolResults())
		})
	}
}
