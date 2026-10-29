package testkit

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// aiScript is a fake OpenAI-compatible server that answers the n-th chat
// request with replies[n]: a tool call ({"tool": name, "args": {...}}) or
// plain text. A request listed in hold waits until the test releases it,
// so the test can look at the screen in the middle of an agent turn.
// The editor's own requests (inline completion, next edit) are not chat
// requests: they get an empty answer, go to side and take no reply.
type aiScript struct {
	mu      sync.Mutex
	replies []aiReply
	n       int
	hold    map[int]chan struct{}
	reached map[int]chan struct{}
	bodies  []string // chat request bodies, in order
	side    []string // inline completion and next edit request bodies
}

// editorAIRequest reports whether body is an inline completion or next
// edit request (by its system prompt), not a chat request.
func editorAIRequest(body string) bool {
	return strings.Contains(body, "You predict the NEXT edit") ||
		strings.Contains(body, "You are an inline code completion engine")
}

// toolResults returns the tool results the app sent back to the model, in
// order (from the role "tool" messages of the last request).
func (s *aiScript) toolResults() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bodies) == 0 {
		return nil
	}
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal([]byte(s.bodies[len(s.bodies)-1]), &req)
	var out []string
	for _, m := range req.Messages {
		if m.Role == "tool" {
			out = append(out, m.Content)
		}
	}
	return out
}

type aiReply struct {
	tool string
	args map[string]any
	text string
}

func newAIScript(t *testing.T, replies ...aiReply) (*aiScript, *httptest.Server) {
	s := &aiScript{replies: replies, hold: map[int]chan struct{}{}, reached: map[int]chan struct{}{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if editorAIRequest(string(body)) {
			s.mu.Lock()
			s.side = append(s.side, string(body))
			s.mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": `{"none": true}`}}}})
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
			return
		}
		s.mu.Lock()
		s.bodies = append(s.bodies, string(body))
		i := s.n
		s.n++
		hold, reached := s.hold[i], s.reached[i]
		s.mu.Unlock()
		if reached != nil {
			close(reached)
		}
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		rep := aiReply{text: "fake-done"}
		if i < len(replies) {
			rep = replies[i]
		}
		if rep.tool != "" {
			args, _ := json.Marshal(rep.args)
			call := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
				map[string]any{"index": 0, "id": fmt.Sprintf("call_%d", i), "function": map[string]any{
					"name": rep.tool, "arguments": string(args)}}}}}}}
			b, _ := json.Marshal(call)
			fmt.Fprintf(w, "data: %s\n\n", b)
		} else {
			b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": rep.text}}}})
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(func() {
		s.mu.Lock()
		for _, c := range s.hold {
			select {
			case <-c:
			default:
				close(c)
			}
		}
		s.mu.Unlock()
		srv.Close()
	})
	return s, srv
}

// holdAt makes request i wait; the returned func releases it, the channel
// closes when the request arrived.
func (s *aiScript) holdAt(i int) (reached <-chan struct{}, release func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, r := make(chan struct{}), make(chan struct{})
	s.hold[i], s.reached[i] = h, r
	return r, func() { close(h) }
}

// aiDriver starts the app with nvim, the fake AI server and the given
// tool classes auto-approved, and opens the AI chat panel.
func aiDriver(t *testing.T, srvURL string, files map[string]string, git bool, auto ...string) *Driver {
	t.Helper()
	return aiDriverConfig(t, srvURL, files, git, "", auto...)
}

// aiDriverConfig is aiDriver with extra config.json keys (`"k":v`).
func aiDriverConfig(t *testing.T, srvURL string, files map[string]string, git bool, extra string, auto ...string) *Driver {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	if files == nil {
		files = textFiles()
	}
	approve, _ := json.Marshal(auto)
	d := New(t, Options{W: 160, H: 40, Nvim: true, Git: git, Files: files,
		Config: map[string]string{"config.json": `{` + extra + comma(extra) + `"ai_auto_approve":` + string(approve) + `}`},
		Setup: func(t testing.TB, _ string) {
			t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			t.Setenv("TERMOCODE_AI_MODEL", "fake")
			t.Setenv("TERMOCODE_AI_BASE_URL", srvURL+"/v1")
		}})
	return d
}

func askAI(d *Driver, text string) {
	d.T.Helper()
	if !strings.Contains(screen(d), "AI CHAT") {
		d.Key("alt+a")
		if !d.WaitForText("AI CHAT", 2*time.Second) {
			d.T.Fatalf("AI panel did not open:\n%s", screen(d))
		}
	}
	d.Type(text)
	d.Key("enter")
}

// explorerLine returns the explorer part of the row that shows exactly
// name (so "test.md" does not match "test_001.md"), "" when none does.
func explorerLine(d *Driver, name string) string {
	for _, line := range strings.Split(screen(d), "\n") {
		if i := strings.Index(line, "▕"); i >= 0 { // the explorer's right edge
			line = line[:i]
		}
		if slices.Contains(strings.Fields(line), name) {
			return line
		}
	}
	return ""
}

func explorerShowsExact(d *Driver, name string) bool { return explorerLine(d, name) != "" }

// waitReached waits until the server got request ch was made for.
func waitReached(d *Driver, ch <-chan struct{}, what string) {
	d.T.Helper()
	if !d.WaitFor(func() bool {
		select {
		case <-ch:
			return true
		default:
			return false
		}
	}, 5*time.Second) {
		d.T.Fatalf("agent did not get past %s:\n%s", what, screen(d))
	}
}

// Spec: Given ai_max_steps 2, when the agent keeps calling tools, then
// the chat shows a clear "paused at the step limit" note that offers to
// continue.
func TestAIStepLimitNoteInChat(t *testing.T) {
	loop := aiReply{tool: "list_dir", args: map[string]any{"path": "."}}
	_, srv := newAIScript(t, loop, loop, loop)
	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := New(t, Options{W: 160, H: 40, Nvim: true, Files: textFiles(),
		Config: map[string]string{"config.json": `{"ai_max_steps": 2}`},
		Setup: func(t testing.TB, _ string) {
			t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			t.Setenv("TERMOCODE_AI_MODEL", "fake")
			t.Setenv("TERMOCODE_AI_BASE_URL", srv.URL+"/v1")
		}})
	askAI(d, "list")
	if !d.WaitForText("step limit (2 tool steps)", 5*time.Second) {
		t.Fatalf("no step-limit note:\n%s", screen(d))
	}
	sc := screen(d)
	if !strings.Contains(sc, "Paused") || !strings.Contains(sc, `"continue"`) || strings.Contains(sc, "**") {
		t.Errorf("step-limit note not shown cleanly:\n%s", sc)
	}
}

// Spec: Given b.txt open in a tab, when the agent deletes it, then its
// tab closes and the explorer no longer lists it, during the turn.
func TestAIDeleteClosesTabAndExplorerRow(t *testing.T) {
	s, srv := newAIScript(t,
		aiReply{tool: "delete", args: map[string]any{"path": "b.txt"}},
		aiReply{text: "fake-deleted-it"},
	)
	after, release := s.holdAt(1)
	d := aiDriver(t, srv.URL, nil, false, "fileWrite")
	open(d, "a.txt")
	open(d, "b.txt")
	askAI(d, "delete b")
	waitReached(d, after, "delete")
	if !d.WaitFor(func() bool {
		got, _ := tabs(d)
		return !slices.Contains(got, "b.txt") && slices.Contains(got, "a.txt") && !explorerShowsExact(d, "b.txt")
	}, 3*time.Second) {
		got, a := tabs(d)
		t.Fatalf("tabs %v active %q\n%s", got, a, screen(d))
	}
	release()
	if !d.WaitForText("fake-deleted-it", 5*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
}

// Spec: Given a git workspace, when the agent creates a branch, then the
// status bar shows the new branch at once; when it runs a shell command
// that makes a file, then the explorer lists it with its git marker.
func TestAIGitAndShellRefreshStatusAndExplorer(t *testing.T) {
	s, srv := newAIScript(t,
		aiReply{tool: "git_checkout", args: map[string]any{"branch": "ai-topic", "create": true}},
		aiReply{tool: "run_command", args: map[string]any{"command": "printf 'x\\n' > shellmade.txt"}},
		aiReply{text: "fake-git-done"},
	)
	afterCheckout, releaseCheckout := s.holdAt(1)
	afterRun, releaseRun := s.holdAt(2)
	d := aiDriver(t, srv.URL, nil, true, "git", "run")
	askAI(d, "branch and run")
	waitReached(d, afterCheckout, "git_checkout")
	if !d.WaitFor(func() bool { return strings.Contains(screen(d), "ai-topic") }, 3*time.Second) {
		t.Fatalf("branch ai-topic not shown:\n%s", screen(d))
	}
	releaseCheckout()
	waitReached(d, afterRun, "run_command")
	if !d.WaitFor(func() bool {
		l := explorerLine(d, "shellmade.txt")
		return l != "" && strings.Contains(strings.SplitN(l, "shellmade.txt", 2)[1], "U")
	}, 3*time.Second) {
		t.Fatalf("explorer does not list shellmade.txt with its U marker: %q\n%s", explorerLine(d, "shellmade.txt"), screen(d))
	}
	releaseRun()
	if !d.WaitForText("fake-git-done", 5*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
}

// Spec (user report): Given the AI chat, when the agent creates test.md,
// opens it and renames it to test_001.md, then right after create_file
// the explorer lists test.md (in the middle of the turn), and at the end
// the active tab is test_001.md, the explorer lists test_001.md, test.md
// is shown nowhere, and the buffer still holds the content.
func TestAICreateOpenRenameFollowsInIDE(t *testing.T) {
	s, srv := newAIScript(t,
		aiReply{tool: "create_file", args: map[string]any{"path": "test.md", "content": "# hello\nworld\n"}},
		aiReply{tool: "open_file", args: map[string]any{"path": "test.md"}},
		aiReply{tool: "rename", args: map[string]any{"from": "test.md", "to": "test_001.md"}},
		aiReply{text: "fake-renamed-it"},
	)
	afterCreate, releaseCreate := s.holdAt(1)
	afterRename, releaseRename := s.holdAt(3)
	d := aiDriver(t, srv.URL, nil, false, "fileWrite")
	askAI(d, "make a file")

	if !d.WaitFor(func() bool {
		select {
		case <-afterCreate:
			return true
		default:
			return false
		}
	}, 5*time.Second) {
		t.Fatalf("agent did not get past create_file:\n%s", screen(d))
	}
	if !d.WaitFor(func() bool { return explorerShowsExact(d, "test.md") }, 3*time.Second) {
		t.Fatalf("explorer does not show test.md right after create_file:\n%s", screen(d))
	}
	releaseCreate()

	if !d.WaitFor(func() bool {
		select {
		case <-afterRename:
			return true
		default:
			return false
		}
	}, 5*time.Second) {
		t.Fatalf("agent did not get past rename:\n%s", screen(d))
	}
	check := func(when string) {
		t.Helper()
		if !d.WaitFor(func() bool {
			got, a := tabs(d)
			return a == "test_001.md" && !slices.Contains(got, "test.md") && explorerShowsExact(d, "test_001.md") && !explorerShowsExact(d, "test.md")
		}, 3*time.Second) {
			got, a := tabs(d)
			t.Fatalf("%s: tabs %v active %q\n%s", when, got, a, screen(d))
		}
		if b := bufLines(d); b != "# hello\nworld" {
			t.Errorf("%s: buffer %q", when, b)
		}
		if strings.Contains(lua(d, `local t = {} for _, b in ipairs(vim.api.nvim_list_bufs()) do t[#t+1] = vim.api.nvim_buf_get_name(b) end return table.concat(t, '\n')`), "/test.md") {
			t.Errorf("%s: nvim still has a test.md buffer", when)
		}
		if exists(d, "test.md") || diskFile(d, "test_001.md") != "# hello\nworld\n" {
			t.Errorf("%s: disk: test.md exists=%v test_001.md=%q", when, exists(d, "test.md"), diskFile(d, "test_001.md"))
		}
	}
	check("right after rename")
	releaseRename()
	if !d.WaitForText("fake-renamed-it", 5*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	check("after the turn")
	// Saving writes test_001.md, never test.md again.
	d.Key("ctrl+s")
	d.Settle(200 * time.Millisecond)
	if exists(d, "test.md") {
		t.Errorf("saving re-created test.md")
	}
}

func comma(s string) string {
	if s == "" {
		return ""
	}
	return ","
}
