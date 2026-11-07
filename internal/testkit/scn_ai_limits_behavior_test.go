package testkit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// SCN-21: the AI provider fails in the middle of an answer, a token or
// budget limit is hit, and an agent step's own git worktree is merged
// or discarded, all through the driver (real app, real nvim, a fake
// OpenAI-compatible server).

// scn21Reply is how the fake server answers one chat request.
type scn21Reply struct {
	text   string         // a plain answer
	tool   string         // a native tool call (args below)
	args   map[string]any //
	status int            // an HTTP error answer with body
	body   string         //
	cut    bool           // send text, then drop the connection (no [DONE])
	usage  bool           // add a usage chunk (1200 prompt, 300 output tokens)
}

// scn21Server is a fake OpenAI-compatible server. pick chooses the reply
// of chat request i (body is the request); the editor's own requests,
// the summary request of a compaction and the memory extractor are
// answered on their own and are not chat requests.
type scn21Server struct {
	mu        sync.Mutex
	bodies    []string // chat request bodies, in order
	summaries []string // compaction request bodies
	pick      func(i int, body string) scn21Reply
}

const scn21Summary = "SCN21 summary of the earlier chat"

func newSCN21Server(t *testing.T, pick func(i int, body string) scn21Reply) (*scn21Server, *httptest.Server) {
	t.Helper()
	s := &scn21Server{pick: pick}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		var rep scn21Reply
		switch {
		case editorAIRequest(body):
			rep = scn21Reply{text: `{"none": true}`}
		case strings.Contains(body, "You summarise the earlier part of a coding chat"):
			s.mu.Lock()
			s.summaries = append(s.summaries, body)
			s.mu.Unlock()
			rep = scn21Reply{text: scn21Summary}
		case strings.Contains(body, "You keep the memory of a coding assistant"):
			rep = scn21Reply{text: "```json\n{\"lessons\":[]}\n```"}
		default:
			s.mu.Lock()
			i := len(s.bodies)
			s.bodies = append(s.bodies, body)
			s.mu.Unlock()
			rep = s.pick(i, body)
		}
		scn21Write(w, rep)
	}))
	t.Cleanup(srv.Close)
	return s, srv
}

// scn21Write sends rep as an SSE stream (or an HTTP error, or a stream
// that breaks off).
func scn21Write(w http.ResponseWriter, rep scn21Reply) {
	if rep.status != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rep.status)
		fmt.Fprint(w, rep.body)
		return
	}
	var events []string
	if rep.tool != "" {
		args, _ := json.Marshal(rep.args)
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
			map[string]any{"index": 0, "id": "call_" + rep.tool, "function": map[string]any{"name": rep.tool, "arguments": string(args)}}}}}}})
		events = append(events, string(b))
	} else {
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": rep.text}}}})
		events = append(events, string(b))
	}
	if rep.usage {
		b, _ := json.Marshal(map[string]any{"choices": []any{}, "model": "fake",
			"usage": map[string]any{"prompt_tokens": 1200, "completion_tokens": 300}})
		events = append(events, string(b))
	}
	if rep.cut {
		scn21Cut(w, events)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		fmt.Fprintf(w, "data: %s\n\n", e)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// scn21Cut writes a chunked SSE answer with events and then closes the
// connection in the middle of the body: the provider went away.
func scn21Cut(w http.ResponseWriter, events []string) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		panic("no hijacker")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	bw := bufio.NewWriter(rw)
	fmt.Fprint(bw, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n")
	for _, e := range events {
		data := "data: " + e + "\n\n"
		fmt.Fprintf(bw, "%x\r\n%s\r\n", len(data), data)
	}
	_ = bw.Flush() // no final 0-size chunk: the body ends in the middle
}

func (s *scn21Server) chats() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

func (s *scn21Server) compactions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.summaries)
}

// scn21Driver starts the app with nvim on the fake server. kind is the
// provider ("ollama" is free; "openai" is priced by ai_prices).
func scn21Driver(t *testing.T, srvURL, kind string, git bool, files map[string]string, config map[string]string, extra string, auto ...string) *Driver {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	if files == nil {
		files = textFiles()
	}
	approve, _ := json.Marshal(auto)
	cfg := map[string]string{"config.json": `{"ai_inline":false,` + extra + comma(extra) + `"ai_auto_approve":` + string(approve) + `}`}
	for k, v := range config {
		cfg[k] = v
	}
	return New(t, Options{W: 160, H: 48, Nvim: true, Git: git, Files: files, Config: cfg,
		Setup: func(t testing.TB, _ string) {
			t.Setenv("TERMOCODE_AI_PROVIDER", kind)
			t.Setenv("TERMOCODE_AI_MODEL", "fake")
			t.Setenv("TERMOCODE_AI_BASE_URL", srvURL+"/v1")
			if kind == "openai" {
				t.Setenv("TERMOCODE_AI_API_KEY", "sk-"+"scn21-fake-key")
			}
		}})
}

// scn21Ask sends text in the AI chat and waits until want is shown.
func scn21Ask(d *Driver, text, want string) {
	d.T.Helper()
	askAI(d, text)
	if !d.WaitForText(want, 10*time.Second) {
		d.T.Fatalf("%q not on screen after %q:\n%s", want, text, screen(d))
	}
}

// Spec: Given a chat, when the provider sends part of an answer and then
// the connection breaks (no end of stream), then the text that came stays
// in the chat, an error line says the answer stopped in the middle and is
// not complete, the request is not sent again behind the user's back,
// and the chat is not stuck: the next message gets its answer.
func TestSCN21ProviderOutageMidStream(t *testing.T) {
	s, srv := newSCN21Server(t, func(i int, _ string) scn21Reply {
		if i == 0 {
			return scn21Reply{text: "partial-answer-start", cut: true}
		}
		return scn21Reply{text: "fake-done"}
	})
	d := scn21Driver(t, srv.URL, "ollama", false, nil, nil, "")
	scn21Ask(d, "explain it", "partial-answer-start")
	if !d.WaitForText("stopped in the middle", 10*time.Second) {
		t.Fatalf("no error line for the broken answer:\n%s", screen(d))
	}
	if got := screen(d); !strings.Contains(got, "not complete") {
		t.Errorf("the error does not say the text is incomplete:\n%s", got)
	}
	d.Settle(300 * time.Millisecond)
	if n := len(s.chats()); n != 1 {
		t.Errorf("%d chat requests after a break mid-answer, want 1 (no silent retry)", n)
	}
	scn21Ask(d, "again", "fake-done")
	if !strings.Contains(screen(d), "partial-answer-start") {
		t.Errorf("the partial text left the chat:\n%s", screen(d))
	}
	if n := len(s.chats()); n != 2 {
		t.Errorf("%d chat requests, want 2", n)
	}
}

// Spec: Given a chat with one earlier turn, when the provider refuses
// the next request because the prompt is over the model's token limit
// (HTTP 400 "prompt is too long"), then the older turns are summarised
// (one summary request), the request is sent again with the summary, the
// answer arrives, and the chat shows the summary marker.
func TestSCN21TokenLimitCompactsAndRetries(t *testing.T) {
	tooLong := scn21Reply{status: 400, body: `{"error":{"message":"prompt is too long: 300000 tokens > 200000 maximum"}}`}
	s, srv := newSCN21Server(t, func(i int, _ string) scn21Reply {
		switch i {
		case 0:
			return scn21Reply{text: "first-answer"}
		case 1:
			return tooLong
		}
		return scn21Reply{text: "second-answer"}
	})
	d := scn21Driver(t, srv.URL, "ollama", false, nil, nil, "")
	scn21Ask(d, "first question", "first-answer")
	scn21Ask(d, "second question", "second-answer")
	if n := s.compactions(); n != 1 {
		t.Errorf("%d summary requests, want 1", n)
	}
	chats := s.chats()
	if len(chats) != 3 || !strings.Contains(chats[2], scn21Summary) {
		t.Fatalf("the retry does not carry the summary (%d chat requests)", len(chats))
	}
	if strings.Contains(chats[2], "first-answer") {
		t.Errorf("the retry still sends the old turns in full")
	}
	if got := screen(d); !strings.Contains(got, "Earlier chat summarised") {
		t.Errorf("no summary marker:\n%s", got)
	}
	if strings.Contains(screen(d), "prompt is too long") {
		t.Errorf("the refusal that was handled shows as an error:\n%s", screen(d))
	}
}

// Spec: when the prompt is still too long after the summary, the turn
// ends with the provider's error (no loop of summaries), and the chat
// takes the next message.
func TestSCN21TokenLimitStillTooLong(t *testing.T) {
	tooLong := scn21Reply{status: 400, body: `{"error":{"message":"prompt is too long: 300000 tokens > 200000 maximum"}}`}
	s, srv := newSCN21Server(t, func(i int, _ string) scn21Reply {
		switch i {
		case 0:
			return scn21Reply{text: "first-answer"}
		case 1, 2:
			return tooLong
		}
		return scn21Reply{text: "third-answer"}
	})
	d := scn21Driver(t, srv.URL, "ollama", false, nil, nil, "")
	scn21Ask(d, "first question", "first-answer")
	scn21Ask(d, "second question", "prompt is too long")
	if n := s.compactions(); n != 1 {
		t.Errorf("%d summary requests, want 1 (once per turn)", n)
	}
	d.Settle(300 * time.Millisecond)
	if n := len(s.chats()); n != 3 {
		t.Errorf("%d chat requests, want 3", n)
	}
	scn21Ask(d, "third question", "third-answer")
}

// scn21BudgetProfile: two steps one after the other; the run's budget
// ($0.01) is below what step 1 costs (1500 tokens at $10 per million =
// $0.015).
const scn21BudgetProfile = `{"profiles":{"tight":{"title":"Tight","budget_usd":0.01,"steps":[
	{"id":"one","title":"One","tools":{"preset":"none"}},
	{"id":"two","title":"Two","tools":{"preset":"none"}}]}}}`

func scn21BudgetDriver(t *testing.T) (*Driver, *scn21Server) {
	t.Helper()
	s, srv := newSCN21Server(t, func(_ int, body string) scn21Reply {
		if strings.Contains(body, "You are step 2 of 2") {
			return scn21Reply{text: "SCN21-STEP2-DONE", usage: true}
		}
		return scn21Reply{text: "SCN21-STEP1-DONE", usage: true}
	})
	d := scn21Driver(t, srv.URL, "openai", false, nil,
		map[string]string{"ai/profiles.json": scn21BudgetProfile},
		`"ai_prices":{"fake":{"in":10,"out":10}}`)
	d.Palette("ai-profile-switch")
	if !d.WaitForText("AI Profile", 3*time.Second) {
		t.Fatalf("no profile picker:\n%s", screen(d))
	}
	d.Type("tight")
	d.Key("enter")
	d.Settle(200 * time.Millisecond)
	askAI(d, "run it")
	if !d.WaitForText("Budget $0.01 reached", 15*time.Second) {
		t.Fatalf("no budget card after step 1:\n%s", screen(d))
	}
	return d, s
}

// scn21ClickCardButton clicks label on the row that also shows other
// (a card's button row; the budget card has no Cancel).
func scn21ClickCardButton(d *Driver, label, other string) {
	d.T.Helper()
	for y, line := range strings.Split(screen(d), "\n") {
		if !strings.Contains(line, other) {
			continue
		}
		if j := strings.Index(line, " "+label+" "); j >= 0 {
			d.Click(ansi.StringWidth(line[:j])+2, y)
			return
		}
	}
	d.T.Fatalf("no %q button next to %q:\n%s", label, other, screen(d))
}

// Spec: Given a profile with a $0.01 budget, when step 1 costs more,
// then the run pauses before step 2 with the budget card (cost so far,
// steps left); Stop ends the run and step 2 never reaches the model.
func TestSCN21BudgetReachedStop(t *testing.T) {
	d, s := scn21BudgetDriver(t)
	got := screen(d)
	if !strings.Contains(got, "1 step did not run yet") || !strings.Contains(got, "$0.015") {
		t.Errorf("budget card does not say what was spent and what is left:\n%s", got)
	}
	scn21ClickCardButton(d, "Stop", "Continue")
	d.Settle(500 * time.Millisecond)
	if strings.Contains(screen(d), "Budget $0.01 reached") {
		t.Fatalf("the card is still open after Stop:\n%s", screen(d))
	}
	for _, b := range s.chats() {
		if strings.Contains(b, "You are step 2 of 2") {
			t.Fatal("step 2 reached the model after Stop")
		}
	}
	if strings.Contains(screen(d), "SCN21-STEP2-DONE") {
		t.Errorf("step 2 ran:\n%s", screen(d))
	}
}

// Spec: the same, Continue runs step 2 without another budget stop.
func TestSCN21BudgetReachedContinue(t *testing.T) {
	d, s := scn21BudgetDriver(t)
	scn21ClickCardButton(d, "Continue", "Stop")
	if !d.WaitForText("SCN21-STEP2-DONE", 15*time.Second) {
		t.Fatalf("step 2 did not run after Continue:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "Budget $0.01 reached") {
		t.Errorf("a second budget stop:\n%s", screen(d))
	}
	n := 0
	for _, b := range s.chats() {
		if strings.Contains(b, "You are step 2 of 2") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("step 2 sent %d requests, want 1", n)
	}
}

// scn21WorktreeProfile: Left and Right run at the same time (no deps,
// ai_parallel_max 2), so each works in its own git worktree.
const scn21WorktreeProfile = `{"profiles":{"pair":{"title":"Pair","steps":[
	{"id":"left","title":"Left","system":"SCN21-LEFT"},
	{"id":"right","title":"Right","system":"SCN21-RIGHT"}]}}}`

func scn21WorktreeDriver(t *testing.T) *Driver {
	t.Helper()
	_, srv := newSCN21Server(t, func(_ int, body string) scn21Reply {
		answered := strings.Contains(body, `"role":"tool"`)
		switch {
		case strings.Contains(body, "SCN21-LEFT") && !answered:
			return scn21Reply{tool: "write_file", args: map[string]any{"path": "left.txt", "content": "left\n"}}
		case strings.Contains(body, "SCN21-LEFT"):
			return scn21Reply{text: "left done"}
		case strings.Contains(body, "SCN21-RIGHT") && !answered:
			return scn21Reply{tool: "create_file", args: map[string]any{"path": "right.txt", "content": "right\n"}}
		case strings.Contains(body, "SCN21-RIGHT"):
			return scn21Reply{text: "right done"}
		}
		return scn21Reply{text: "fake-done"}
	})
	files := map[string]string{"left.txt": "mine\n", "notes.md": "# notes\n"}
	d := scn21Driver(t, srv.URL, "ollama", true, files,
		map[string]string{"ai/profiles.json": scn21WorktreeProfile},
		`"ai_parallel_max":2`, "fileWrite", "git", "run", "ide", "settings", "services")
	d.Palette("ai-profile-switch")
	if !d.WaitForText("AI Profile", 3*time.Second) {
		t.Fatalf("no profile picker:\n%s", screen(d))
	}
	d.Type("pair")
	d.Key("enter")
	d.Settle(200 * time.Millisecond)
	askAI(d, "change things")
	if !d.WaitFor(func() bool { return strings.Count(screen(d), "Review   Merge   Discard") == 2 }, 20*time.Second) {
		t.Fatalf("no Review / Merge / Discard chips for both steps:\n%s", screen(d))
	}
	return d
}

// scn21AgentBranches lists the agent branches of the workspace repo.
func scn21AgentBranches(d *Driver) string {
	d.T.Helper()
	out, err := exec.Command("git", "-C", d.Dir, "branch", "--list", "termocode/agent-*").Output()
	if err != nil {
		d.T.Fatalf("git branch: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// Spec: Given two steps that ran at the same time in their own
// worktrees, the user's files are untouched until Merge; Merge on Left
// writes left.txt into the user's tree and removes Left's branch;
// Discard on Right asks first with Cancel first (Cancel keeps it), and
// Discard then removes Right's worktree and branch and writes nothing.
func TestSCN21WorktreeMergeAndDiscard(t *testing.T) {
	d := scn21WorktreeDriver(t)
	if got := diskFile(d, "left.txt"); got != "mine\n" {
		t.Fatalf("user's left.txt changed before Merge: %q", got)
	}
	if exists(d, "right.txt") {
		t.Fatal("right.txt written into the user's tree before Merge")
	}
	if n := len(strings.Fields(scn21AgentBranches(d))); n != 2 {
		t.Fatalf("%d agent branches, want 2:\n%s", n, scn21AgentBranches(d))
	}

	clickText(d, " Merge ") // the first chip row is Left's
	if !eventually(d, func() bool { return diskFile(d, "left.txt") == "left\n" }) {
		t.Fatalf("Merge did not write left.txt: %q\n%s", diskFile(d, "left.txt"), screen(d))
	}
	if !d.WaitFor(func() bool { return strings.Count(screen(d), "Review   Merge   Discard") == 1 }, 5*time.Second) {
		t.Errorf("Left still offers its chips after Merge:\n%s", screen(d))
	}
	if n := len(strings.Fields(scn21AgentBranches(d))); n != 1 {
		t.Errorf("%d agent branches after Merge, want 1 (Right's)", n)
	}

	clickText(d, " Discard ")
	if !d.WaitForText("Discard the changes of Right?", 3*time.Second) {
		t.Fatalf("no discard card:\n%s", screen(d))
	}
	clickButton(d, "Cancel")
	d.Settle(300 * time.Millisecond)
	if n := len(strings.Fields(scn21AgentBranches(d))); n != 1 {
		t.Errorf("Cancel removed Right's branch")
	}
	clickText(d, " Discard ")
	if !d.WaitForText("Discard the changes of Right?", 3*time.Second) {
		t.Fatalf("no discard card the second time:\n%s", screen(d))
	}
	clickButton(d, "Discard")
	if !d.WaitFor(func() bool { return scn21AgentBranches(d) == "" }, 5*time.Second) {
		t.Errorf("Right's branch still there after Discard:\n%s", scn21AgentBranches(d))
	}
	if exists(d, "right.txt") {
		t.Error("Discard wrote right.txt into the user's tree")
	}
	if got := diskFile(d, "left.txt"); got != "left\n" {
		t.Errorf("Discard of Right changed the merged left.txt: %q", got)
	}
	if strings.Contains(screen(d), "Review   Merge   Discard") {
		t.Errorf("chips left after both worktrees are done:\n%s", screen(d))
	}
}
