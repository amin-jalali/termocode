package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// G-API: agents start agent runs. Each test runs scripted models only.

// aiAgentRunModel is a chat on the default profile (one step on main)
// whose main model first answers chatReplies, then the queued run's
// steps: review's Reviewer runs on main, its Fixer on quick.
func aiAgentRunModel(t *testing.T, mainReplies, quickReplies []ai.Chunk) (*Model, *recordingAIProvider) {
	t.Helper()
	main := &recordingAIProvider{replies: mainReplies}
	quick := &recordingAIProvider{replies: quickReplies}
	m, _ := aiRunModel(t, main, quick)
	m.ai.profile = "" // the chat itself: one step
	m.ai.conv.Title = "fix the parser"
	m.output = newOutputStore()
	return m, main
}

// aiAgentRunCard records the card of the first confirm and answers it.
func aiAgentRunCard(card *aiWriteCard, answer string) func(aiChatConfirmMsg) string {
	return func(c aiChatConfirmMsg) string {
		if !card.opened {
			card.opened, card.body, card.title = true, c.human, c.tool.Label()
			card.buttons = nil
		}
		return answer
	}
}

// aiDriveQueued drives the run that the end of the turn started.
func aiDriveQueued(t *testing.T, m *Model, onConfirm func(aiChatConfirmMsg) string) {
	t.Helper()
	if !m.ai.streaming {
		t.Fatalf("no queued run started; queue %+v not started %v\nmessages %+v", m.ai.runQueue, m.ai.runNotStarted, m.ai.conv.Messages)
	}
	driveAIChat(t, m, waitAIChat(m.ai.chatCh), onConfirm)
}

// aiLastMsg is the last message of the model's request i (the tool
// result after a tool call).
func aiLastMsg(p *recordingAIProvider, i int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	msgs := p.reqs[i].Messages
	return msgs[len(msgs)-1].Content
}

func aiToolJSON(t *testing.T, res string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(res), &v); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, res)
	}
	return v
}

// Spec: start and stop are mutating with no Always allow class (every
// call asks, whatever the user allowed), outside agents never get them
// and a read-only step does not either; the card puts Deny first with
// the focus and names the action.
func TestAIAgentRunToolsGates(t *testing.T) {
	m := aiWriteModel(t)
	tools := m.aiChatTools(".", nil)
	want := map[string]string{"agent_run_start": "Deny · Start run", "agent_run_stop": "Deny · Stop run"}
	for _, name := range []string{"agent_run_start", "agent_run_stop"} {
		tl, ok := ai.FindTool(tools, name)
		if !ok {
			t.Fatalf("no tool %s", name)
		}
		if !tl.Mutating || tl.Class != "" || tl.Gate != nil {
			t.Errorf("%s: mutating %v class %q", name, tl.Mutating, tl.Class)
		}
		if _, ok := mcpServeRow(name); ok {
			t.Errorf("%s is offered to outside agents", name)
		}
		ro := aiFilterTools(tools, agentdag.ToolRule{Preset: agentdag.PresetReadOnly})
		if _, ok := ai.FindTool(ro, name); ok {
			t.Errorf("a read-only step gets %s", name)
		}
		reply := make(chan bool, 1)
		m.onAIChatConfirm(aiChatConfirmMsg{gen: m.ai.chatGen, tool: tl, human: "x", reply: reply, ch: make(chan tea.Msg, 1)})
		var titles []string
		for _, b := range m.confirm.Buttons() {
			titles = append(titles, b.Title)
		}
		if got := strings.Join(titles, " · "); got != want[name] {
			t.Errorf("%s buttons %q", name, got)
		}
		m.answerAIToolConfirm("deny")
		if <-reply {
			t.Errorf("%s: Deny allowed", name)
		}
	}
}

// Spec: the AI asks for a review run; the card shows the profile, its
// steps and models, the task and who asks; Allow queues it and the
// model gets the run id; when the turn ends the run starts in the chat
// like a user message with that profile, marked as started by the AI.
func TestAIAgentRunStartQueuesAndRuns(t *testing.T) {
	m, main := aiAgentRunModel(t, []ai.Chunk{
		toolCall("agent_run_start", map[string]any{"profile": ai.ProfileReview, "task": "Review parser.go and fix the bug"}),
		{Delta: "I asked for a review run."},
		{Delta: "Problem: parser.go line 3."},
	}, []ai.Chunk{{Delta: "Fixed parser.go."}})
	var card aiWriteCard
	driveAIChat(t, m, m.sendAIChat("get this reviewed"), aiAgentRunCard(&card, "allow"))
	if !card.opened {
		t.Fatal("no card")
	}
	for _, want := range []string{"profile review (2 steps)", "Reviewer", "claude-sonnet-4-5", "Fixer", "gpt-5-mini",
		"Task:", "Review parser.go and fix the bug", `Started by AI in the chat "fix the parser"`, "plan approval"} {
		if !strings.Contains(card.body, want) {
			t.Errorf("card lacks %q:\n%s", want, card.body)
		}
	}
	res := aiLastMsg(main, 1)
	got := aiToolJSON(t, res)
	id, _ := got["run_id"].(string)
	if id == "" || got["state"] != "queued" {
		t.Fatalf("tool result %s", res)
	}
	aiDriveQueued(t, m, nil)
	rm := aiRunMsg(t, m)
	if rm.Run.ID != id || rm.Run.Profile != ai.ProfileReview || rm.Run.State != ai.RunStateDone || rm.Text != "Fixed parser.go." {
		t.Fatalf("run %+v text %q", rm.Run, rm.Text)
	}
	if by := rm.Run.By; by == nil || by.Depth != 1 || by.Chat != m.ai.conv.ID || by.ChatTitle != "fix the parser" || by.ParentRun != "" {
		t.Fatalf("by %+v", rm.Run.By)
	}
	msgs := m.ai.conv.Messages
	if u := msgs[len(msgs)-2]; u.Role != ai.StoredUser || u.Text != "Review parser.go and fix the bug" {
		t.Fatalf("task message %+v", u)
	}
	if len(m.ai.runQueue) != 0 || m.ai.streaming {
		t.Fatalf("queue %+v streaming %v", m.ai.runQueue, m.ai.streaming)
	}
}

// Spec: Deny queues nothing and tells the model.
func TestAIAgentRunStartDeny(t *testing.T) {
	m, main := aiAgentRunModel(t, []ai.Chunk{
		toolCall("agent_run_start", map[string]any{"profile": ai.ProfileReview, "task": "review it"}), {Delta: "ok"}}, nil)
	var card aiWriteCard
	driveAIChat(t, m, m.sendAIChat("go"), aiAgentRunCard(&card, "deny"))
	if !card.opened || len(m.ai.runQueue) != 0 || m.ai.streaming {
		t.Fatalf("card %v queue %+v streaming %v", card.opened, m.ai.runQueue, m.ai.streaming)
	}
	res := aiLastMsg(main, 1)
	if !strings.Contains(strings.ToLower(res), "declined") && !strings.Contains(strings.ToLower(res), "denied") {
		t.Fatalf("result %q", res)
	}
}

// Spec: every call asks, even when the user allowed every tool class
// for good ("auto" settings).
func TestAIAgentRunStartAsksDespiteAlwaysAllow(t *testing.T) {
	m, _ := aiAgentRunModel(t, []ai.Chunk{
		toolCall("agent_run_start", map[string]any{"profile": ai.ProfileReview, "task": "review it"}), {Delta: "ok"}}, nil)
	m.ai.settings.AutoApprove = ai.ToolClasses
	m.ai.settings.PlanApproval = ai.PlanApprovalAuto
	var card aiWriteCard
	driveAIChat(t, m, m.sendAIChat("go"), aiAgentRunCard(&card, "deny"))
	if !card.opened {
		t.Fatal("agent_run_start ran without a card")
	}
}

// Spec: the AI's own steps (the profile_draft step schema) run as a run
// named "AI steps" (never a profile name); steps on an unknown slot are
// refused before any card.
func TestAIAgentRunStartInlineSteps(t *testing.T) {
	m := aiWriteModel(t)
	sn := m.aiRunSnapshot(aiRunCaller{})
	bad := map[string]any{"task": "x", "steps": []any{map[string]any{"title": "A", "slot": "nope"}}}
	if _, err := sn.checkStart(bad); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown slot: %v", err)
	}
	both := map[string]any{"task": "x", "profile": "fast", "steps": []any{map[string]any{"title": "A"}}}
	if _, err := sn.checkStart(both); err == nil {
		t.Fatal("profile and steps both accepted")
	}
	if _, err := sn.checkStart(map[string]any{"task": "x"}); err == nil {
		t.Fatal("neither profile nor steps accepted")
	}
	if _, err := sn.checkStart(map[string]any{"profile": "fast"}); err == nil {
		t.Fatal("no task accepted")
	}

	mm, _ := aiAgentRunModel(t, []ai.Chunk{
		toolCall("agent_run_start", map[string]any{"task": "count the TODOs", "steps": []any{
			map[string]any{"title": "Counter", "prompt": "Count TODO lines.", "tools": "read-only"},
			map[string]any{"title": "Writer", "slot": "quick", "prompt": "Write the number."}}}),
		{Delta: "asked"}, {Delta: "12 TODOs"}}, []ai.Chunk{{Delta: "There are 12."}})
	var card aiWriteCard
	driveAIChat(t, mm, mm.sendAIChat("go"), aiAgentRunCard(&card, "allow"))
	if !strings.Contains(card.body, "steps the AI wrote (2 steps)") || !strings.Contains(card.body, "Counter") {
		t.Fatalf("card:\n%s", card.body)
	}
	aiDriveQueued(t, mm, nil)
	rm := aiRunMsg(t, mm)
	if rm.Run.Profile != aiRunInline || len(rm.Steps) != 2 || rm.Steps[0].Title != "Counter" || rm.Text != "There are 12." {
		t.Fatalf("run %+v steps %+v text %q", rm.Run, rm.Steps, rm.Text)
	}
}

// Spec: a run the AI started from a run the AI started (depth 2) cannot
// start another; the steps of a depth-1 run can, and the child records
// the run and step that asked.
func TestAIAgentRunStartDepth(t *testing.T) {
	m := aiWriteModel(t)
	args := map[string]any{"profile": "fast", "task": "x"}
	if _, err := m.aiRunSnapshot(aiRunCaller{depth: 2, parentRun: "r2"}).checkStart(args); err == nil ||
		!strings.Contains(err.Error(), "2 levels") {
		t.Fatalf("depth 2: %v", err)
	}
	if _, err := m.aiRunSnapshot(aiRunCaller{depth: 1, parentRun: "r1"}).checkStart(args); err != nil {
		t.Fatalf("depth 1: %v", err)
	}
	tl, _ := ai.FindTool(m.aiChatToolsBy(".", nil, aiRunCaller{depth: 2, parentRun: "r2"}), "agent_run_start")
	if err := tl.Validate(args); err == nil {
		t.Fatal("the depth-2 tool validates")
	}
	r, _ := m.aiRunSnapshot(aiRunCaller{depth: 1, parentRun: "r1"}).checkStart(args)
	if _, _, err := m.aiToolRunStart(context.Background(), aiRunCaller{depth: 1, parentRun: "r1", budget: 2}, r); err != nil {
		t.Fatal(err)
	}
	q := m.ai.runQueue[0]
	if q.by.Depth != 2 || q.by.ParentRun != "r1" || q.by.ParentBudget != 2 {
		t.Fatalf("by %+v", q.by)
	}
	if _, _, err := m.aiToolRunStart(context.Background(), aiRunCaller{depth: 2}, r); err == nil {
		t.Fatal("depth 2 queued")
	}
}

// Spec: at most 3 AI-started runs wait or run at once.
func TestAIAgentRunStartLimit(t *testing.T) {
	m := aiWriteModel(t)
	args := map[string]any{"profile": "fast", "task": "x"}
	r, err := m.aiRunSnapshot(aiRunCaller{}).checkStart(args)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < aiRunMaxActive; i++ {
		if _, _, err := m.aiToolRunStart(context.Background(), aiRunCaller{}, r); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	if _, _, err := m.aiToolRunStart(context.Background(), aiRunCaller{}, r); err == nil || !strings.Contains(err.Error(), "at most 3") {
		t.Fatalf("4th: %v", err)
	}
	if _, err := m.aiRunSnapshot(aiRunCaller{}).checkStart(args); err == nil {
		t.Fatal("the snapshot lets a 4th through")
	}
}

// Spec: a child of a run with a budget gets what is left of it; with
// nothing left it does not start and the chat says why.
func TestAIAgentRunBudgetFromParent(t *testing.T) {
	m, _ := aiAgentRunModel(t, []ai.Chunk{{Delta: "a"}, {Delta: "b"}, {Delta: "c"}, {Delta: "d"}}, nil)
	cost := 0.75
	m.ai.conv.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "go"},
		{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "p1", State: ai.RunStateDone},
			Steps: []ai.StoredStep{{ID: "s1", Cost: &cost, Usage: &ai.Usage{Input: 1}}}},
	}
	fast, _ := m.ai.profiles.Get("fast")
	m.ai.runQueue = []aiQueuedRun{
		{id: "c1", chat: m.ai.conv.ID, prof: fast, task: "one", by: ai.RunBy{Chat: m.ai.conv.ID, ParentRun: "p1", Depth: 1, ParentBudget: 0.5}},
		{id: "c2", chat: m.ai.conv.ID, prof: fast, task: "two", by: ai.RunBy{Chat: m.ai.conv.ID, ParentRun: "p1", Depth: 1, ParentBudget: 1}},
	}
	cmd := m.aiStartQueuedRun()
	if cmd == nil || !m.ai.streaming {
		t.Fatal("c2 did not start")
	}
	if why := m.ai.runNotStarted["c1"]; !strings.Contains(why, "budget") {
		t.Fatalf("c1 why %q", why)
	}
	var note bool
	for _, msg := range m.ai.conv.Messages {
		note = note || (msg.Role == ai.StoredError && strings.Contains(msg.Text, "c1") && strings.Contains(msg.Text, "budget"))
	}
	if !note {
		t.Fatalf("no note in %+v", m.ai.conv.Messages)
	}
	aiDriveQueued(t, m, nil)
	rm := aiRunMsg(t, m)
	if rm.Run.ID != "c2" || rm.Run.By.Budget < 0.24 || rm.Run.By.Budget > 0.26 {
		t.Fatalf("run %+v by %+v", rm.Run, rm.Run.By)
	}
	if got := aiRunBudget(fast, rm.Run.By); got < 0.24 || got > 0.26 {
		t.Fatalf("budget %v", got)
	}
}

// Spec: worktree "off" runs the steps in the user's folder even where
// agent worktrees would apply; "auto" keeps them.
func TestAIAgentRunWorktreeOff(t *testing.T) {
	for _, mode := range []string{"off", "auto"} {
		t.Run(mode, func(t *testing.T) {
			m, dir, _ := aiWorktreeRunModel(t, true)
			prof, _ := m.ai.profiles.Get("two")
			by := ai.RunBy{Chat: m.ai.conv.ID, Depth: 1}
			if mode == "off" {
				by.Worktree = "off"
			}
			m.ai.runQueue = []aiQueuedRun{{id: "w1", chat: m.ai.conv.ID, prof: prof, task: "change things", by: by}}
			if m.aiStartQueuedRun() == nil {
				t.Fatal("not started")
			}
			aiDriveQueued(t, m, nil)
			rm := aiRunMsg(t, m)
			wt := rm.Steps[0].Worktree != nil || rm.Steps[1].Worktree != nil
			_, err := os.Stat(filepath.Join(dir, "b.txt"))
			if mode == "off" && (wt || err != nil) {
				t.Fatalf("off: worktree %v, b.txt in the folder: %v", wt, err == nil)
			}
			// auto: G-WT decides; under load one step may fall back to the
			// folder (its card says so), so only ask for a worktree at all.
			if mode == "auto" && !wt {
				t.Fatalf("auto: no step got a worktree; steps %+v", rm.Steps)
			}
		})
	}
}

// Spec: Stop (Ctrl+C) also drops the runs the AI queued; the toast says
// so and the status tool will say why.
func TestAIAgentRunStopDropsQueue(t *testing.T) {
	m := aiWriteModel(t)
	m.ai.streaming = true
	fast, _ := m.ai.profiles.Get("fast")
	m.ai.runQueue = []aiQueuedRun{{id: "q1", chat: m.ai.conv.ID, prof: fast, task: "x"}}
	m.aiCancelAll()
	if len(m.ai.runQueue) != 0 || !strings.Contains(m.ai.runNotStarted["q1"], "stopped") {
		t.Fatalf("queue %+v why %q", m.ai.runQueue, m.ai.runNotStarted["q1"])
	}
	if txt := toastText(*m); !strings.Contains(txt, "1 queued run not started") {
		t.Fatalf("toast %q", txt)
	}
}

// Spec: a task or step that holds a key is refused before the card, and
// neither the card nor the result repeats it.
func TestAIAgentRunStartRefusesSecrets(t *testing.T) {
	m := aiWriteModel(t)
	sn := m.aiRunSnapshot(aiRunCaller{})
	key := "sk-" + "test" + strings.Repeat("A1b2", 8)
	for _, args := range []map[string]any{
		{"profile": "fast", "task": "use " + key + " to call the API"},
		{"task": "x", "steps": []any{map[string]any{"title": "A", "prompt": "key " + key}}},
	} {
		_, err := sn.checkStart(args)
		if err == nil || strings.Contains(err.Error(), key) {
			t.Fatalf("err %v", err)
		}
		if d := sn.describeStart(args); strings.Contains(d, key) || d != aiSecretCardText {
			t.Fatalf("card %q", d)
		}
	}
}

// aiRunToolOut runs a read tool of this file on the real model (its IDE
// call runs at once, as on the UI goroutine).
func aiRunToolOut(t *testing.T, m *Model, name, id string) (string, error) {
	t.Helper()
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(m)
		return out, err
	}
	tl, ok := ai.FindTool(m.aiAgentRunTools(call, aiRunCaller{}), name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	if tl.Mutating {
		t.Fatalf("%s is mutating", name)
	}
	return tl.Run(context.Background(), map[string]any{"run_id": id})
}

// Spec: status says queued before the run starts and done after, with
// steps, cost and who started it; result gives the final answer and
// each step's answer; an unknown id is an error.
func TestAIAgentRunStatusAndResult(t *testing.T) {
	m, _ := aiAgentRunModel(t, []ai.Chunk{{Delta: "Problem: parser.go line 3.",
		Usage: &ai.Usage{Model: "claude-sonnet-4-5", Input: 1000, Output: 100, Requests: 1}}},
		[]ai.Chunk{{Delta: "Fixed parser.go.", Usage: &ai.Usage{Model: "gpt-5-mini", Input: 1000, Output: 100, Requests: 1}}})
	review, _ := m.ai.profiles.Get(ai.ProfileReview)
	if _, _, err := m.aiToolRunStart(context.Background(), aiRunCaller{}, aiRunStartReq{prof: review, task: "review parser.go", worktree: "auto"}); err != nil {
		t.Fatal(err)
	}
	id := m.ai.runQueue[0].id
	out, err := aiRunToolOut(t, m, "agent_run_status", id)
	if err != nil {
		t.Fatal(err)
	}
	st := aiToolJSON(t, out)
	if st["state"] != "queued" || st["started_by_ai"] != true || st["profile"] != ai.ProfileReview || len(st["steps"].([]any)) != 2 {
		t.Fatalf("queued status %s", out)
	}
	if m.aiStartQueuedRun() == nil {
		t.Fatal("not started")
	}
	aiDriveQueued(t, m, nil)
	out, _ = aiRunToolOut(t, m, "agent_run_status", id)
	st = aiToolJSON(t, out)
	if st["state"] != ai.RunStateDone || st["task"] != "review parser.go" || st["cost_usd"].(float64) <= 0 || st["cost_complete"] != true {
		t.Fatalf("done status %s", out)
	}
	steps := st["steps"].([]any)
	if s0 := steps[0].(map[string]any); s0["state"] != "completed" || s0["model"] != "claude-sonnet-4-5" {
		t.Fatalf("step %v", s0)
	}
	out, err = aiRunToolOut(t, m, "agent_run_result", id)
	if err != nil {
		t.Fatal(err)
	}
	res := aiToolJSON(t, out)
	if res["answer"] != "Fixed parser.go." {
		t.Fatalf("result %s", out)
	}
	if s0 := res["steps"].([]any)[0].(map[string]any); s0["answer"] != "Problem: parser.go line 3." {
		t.Fatalf("step answer %v", s0)
	}
	if _, err := aiRunToolOut(t, m, "agent_run_status", "nope"); err == nil {
		t.Fatal("unknown id: no error")
	}
	if _, err := aiRunToolOut(t, m, "agent_run_result", "nope"); err == nil {
		t.Fatal("unknown id: no error")
	}
}

// Spec: a run that never started says why.
func TestAIAgentRunStatusNotStarted(t *testing.T) {
	m := aiWriteModel(t)
	m.ai.aiRunNotStarted("gone1", "the user stopped the AI")
	out, err := aiRunToolOut(t, m, "agent_run_status", "gone1")
	if err != nil {
		t.Fatal(err)
	}
	if st := aiToolJSON(t, out); st["state"] != "not_started" || !strings.Contains(st["note"].(string), "stopped") {
		t.Fatalf("status %s", out)
	}
}

// Spec: while the live run waits for the plan card, status says it
// waits for the user; a stored run that is not live reads interrupted.
func TestAIAgentRunStatusWaitingForUser(t *testing.T) {
	block := &blockingAIProvider{started: make(chan struct{}, 1), stopped: make(chan struct{})}
	m, _ := aiRunModel(t, block, block)
	fast, _ := m.ai.profiles.Get(ai.ProfileReview)
	m.ai.runQueue = []aiQueuedRun{{id: "live1", chat: m.ai.conv.ID, prof: fast, task: "x", by: ai.RunBy{Chat: m.ai.conv.ID, Depth: 1}}}
	cmd := m.aiStartQueuedRun()
	if cmd == nil {
		t.Fatal("not started")
	}
	<-block.started
	rm := aiRunMsg(t, m)
	rm.Run.PlanWait = true
	out, _ := aiRunToolOut(t, m, "agent_run_status", "live1")
	st := aiToolJSON(t, out)
	if st["state"] != ai.RunStateRunning || !strings.Contains(out, "plan approval") {
		t.Fatalf("status %s", out)
	}
	m.aiCancelAll()
	driveAIChat(t, m, waitAIChat(m.ai.chatCh), nil)

	m.ai.conv.Messages = append(m.ai.conv.Messages, ai.StoredMessage{Role: ai.StoredUser, Text: "old"},
		ai.StoredMessage{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "old1", State: ai.RunStateRunning}})
	out, _ = aiRunToolOut(t, m, "agent_run_status", "old1")
	if st := aiToolJSON(t, out); st["state"] != ai.RunStateInterrupted {
		t.Fatalf("stored running run %s", out)
	}
}

// Spec: the result lists the files the steps changed in their agent
// worktrees, with the branch; the user's tree stays untouched.
func TestAIAgentRunResultWorktreeFiles(t *testing.T) {
	m, _, _ := aiWorktreeRunModel(t, true)
	prof, _ := m.ai.profiles.Get("two")
	m.ai.runQueue = []aiQueuedRun{{id: "wt1", chat: m.ai.conv.ID, prof: prof, task: "change things", by: ai.RunBy{Chat: m.ai.conv.ID, Depth: 1}}}
	if m.aiStartQueuedRun() == nil {
		t.Fatal("not started")
	}
	aiDriveQueued(t, m, nil)
	out, err := aiRunToolOut(t, m, "agent_run_result", "wt1")
	if err != nil {
		t.Fatal(err)
	}
	var res aiRunResultInfo
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range res.ChangedFiles {
		if f.Branch == "" {
			t.Errorf("no branch: %+v", f)
		}
		got[f.Step] = f.Status + " " + f.Path
	}
	if got["left"] != "M a.txt" || got["right"] != "A b.txt" {
		t.Fatalf("changed files %+v\n%s", res.ChangedFiles, out)
	}
}

// Spec: the read tools never show a key: not from the task, the steps'
// answers or their errors.
func TestAIAgentRunReadToolsHideSecrets(t *testing.T) {
	m := aiWriteModel(t)
	key := "sk-" + "test" + strings.Repeat("Z9y8", 8)
	tok := "ghp_" + strings.Repeat("a1B2", 9)
	m.ai.conv.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "call it with " + key},
		{Role: ai.StoredAssistant, Text: "done, token " + tok, Run: &ai.StoredRun{ID: "s1", State: ai.RunStateDone},
			Steps: []ai.StoredStep{{ID: "a", State: "completed", Text: "I used " + key}, {ID: "b", State: "failed", Error: "401 for " + tok}}},
	}
	for _, name := range []string{"agent_run_status", "agent_run_result"} {
		out, err := aiRunToolOut(t, m, name, "s1")
		if err != nil {
			t.Fatal(err)
		}
		for _, sec := range []string{key, tok, "Z9y8Z9y8", "a1B2a1B2"} {
			if strings.Contains(out, sec) {
				t.Errorf("%s shows a secret:\n%s", name, out)
			}
		}
	}
}

// Spec: status and result are read only: a read-only step gets them,
// outside agents do not.
func TestAIAgentRunReadToolsGates(t *testing.T) {
	m := aiWriteModel(t)
	ro := aiFilterTools(m.aiChatTools(".", nil), agentdag.ToolRule{Preset: agentdag.PresetReadOnly})
	for _, name := range []string{"agent_run_status", "agent_run_result"} {
		if _, ok := ai.FindTool(ro, name); !ok {
			t.Errorf("a read-only step lacks %s", name)
		}
		if _, ok := mcpServeRow(name); ok {
			t.Errorf("%s is offered to outside agents", name)
		}
	}
}

// Spec: the AI stops a run it queued: the card says what stops, Allow
// drops it (it never starts), Deny keeps it; a run that does not wait or
// run is refused before any card.
func TestAIAgentRunStopQueued(t *testing.T) {
	for _, answer := range []string{"allow", "deny"} {
		t.Run(answer, func(t *testing.T) {
			m, _ := aiAgentRunModel(t, nil, nil)
			fast, _ := m.ai.profiles.Get("fast")
			m.ai.runQueue = []aiQueuedRun{{id: "q1", chat: m.ai.conv.ID, prof: fast, task: "count lines", by: ai.RunBy{Chat: m.ai.conv.ID, Depth: 1}}}
			main := &recordingAIProvider{replies: []ai.Chunk{toolCall("agent_run_stop", map[string]any{"run_id": "q1"}), {Delta: "ok"}, {Delta: "counted"}}}
			m.ai.provider = main
			m.ai.slotFake[ai.SlotMain] = aiSlot{p: main, cfg: ai.Config{Kind: ai.KindOllama, Model: "fake-1"}}
			var card aiWriteCard
			driveAIChat(t, m, m.sendAIChat("stop it"), aiAgentRunCard(&card, answer))
			if !strings.Contains(card.body, "Stop the agent run q1") || !strings.Contains(card.body, "count lines") {
				t.Fatalf("card:\n%s", card.body)
			}
			if answer == "allow" {
				if m.ai.streaming || len(m.ai.runQueue) != 0 || !strings.Contains(m.ai.runNotStarted["q1"], "stopped") {
					t.Fatalf("streaming %v queue %+v", m.ai.streaming, m.ai.runQueue)
				}
				return
			}
			aiDriveQueued(t, m, nil) // Deny: it starts after the turn
			if rm := aiRunMsg(t, m); rm.Run.ID != "q1" {
				t.Fatalf("run %+v", rm.Run)
			}
		})
	}
	m := aiWriteModel(t)
	if err := m.aiRunSnapshot(aiRunCaller{}).checkStop(map[string]any{"run_id": "nope"}); err == nil {
		t.Fatal("unknown run accepted")
	}
}

// Spec: a step cannot stop its own run (refused before any card).
func TestAIAgentRunStopNotSelf(t *testing.T) {
	main := &recordingAIProvider{replies: []ai.Chunk{toolCall("agent_run_stop", map[string]any{"run_id": "self1"}), {Delta: "went on"}}}
	m, _ := aiRunModel(t, main, main)
	one := ai.Profile{Name: "one", Source: ai.ProfileSourceUser, Steps: []ai.ProfileStep{{ID: "s1", Title: "Worker", Slot: ai.SlotMain}}}
	m.ai.runQueue = []aiQueuedRun{{id: "self1", chat: m.ai.conv.ID, prof: one, task: "x", by: ai.RunBy{Chat: m.ai.conv.ID, Depth: 1}}}
	if m.aiStartQueuedRun() == nil {
		t.Fatal("not started")
	}
	var card aiWriteCard
	aiDriveQueued(t, m, aiAgentRunCard(&card, "allow"))
	if card.opened {
		t.Fatal("a card asked to stop the run itself")
	}
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone {
		t.Fatalf("run %+v", rm.Run)
	}
	if res := aiLastMsg(main, 1); !strings.Contains(res, "cannot stop its own run") {
		t.Fatalf("result %q", res)
	}
}

// Spec: the chat marks a run the AI started under its task, with the
// chat and the run and step that asked.
func TestAIAgentRunChatChip(t *testing.T) {
	m, _ := aiAgentRunModel(t, []ai.Chunk{{Delta: "a"}, {Delta: "b"}}, nil)
	fast, _ := m.ai.profiles.Get("fast")
	m.ai.runQueue = []aiQueuedRun{{id: "c1", chat: m.ai.conv.ID, prof: fast, task: "count the TODOs",
		by: ai.RunBy{Chat: m.ai.conv.ID, ChatTitle: "fix the parser", ParentRun: "p1", Step: "Reviewer", Depth: 2}}}
	if m.aiStartQueuedRun() == nil {
		t.Fatal("not started")
	}
	aiDriveQueued(t, m, nil)
	got := aiTranscriptText(m, 120)
	wantAll(t, got, "count the TODOs", `started by AI in the chat "fix the parser" · from step Reviewer of run p1`)
	m.ai.conv.Messages[len(m.ai.conv.Messages)-1].Run.By = nil
	if got := aiTranscriptText(m, 120); strings.Contains(got, "started by AI") {
		t.Fatalf("a user's run is marked:\n%s", got)
	}
}

// aiMarkDemoRun marks the Runs demo run of task as started by the AI.
func aiMarkDemoRun(t *testing.T, m *Model, task string) {
	t.Helper()
	msgs := m.ai.conv.Messages
	for i := range msgs {
		if msgs[i].Run != nil && runsTask(msgs, i) == task {
			msgs[i].Run.By = &ai.RunBy{Chat: m.ai.conv.ID, ChatTitle: "plan the release", Depth: 1}
			m.ai.runGen++
			return
		}
	}
	t.Fatalf("no run %q in the open chat", task)
}

// Spec: the Runs list, a run's page and its log mark AI-started runs
// and name the chat that asked; the filter finds them by "by AI".
func TestAIAgentRunRunsViewChip(t *testing.T) {
	m := runsRig(t, "list", 200, 50)
	aiMarkDemoRun(t, m, "fix the toast flicker")
	wantAll(t, runsSideText(m), "review by AI")
	n := 0
	for _, it := range m.runsCollect(clock.Now()) {
		if aiRunByChip(&it.run) != "" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d runs marked", n)
	}
	m = runsRig(t, "detail", 200, 50)
	aiMarkDemoRun(t, m, "fix the toast flicker")
	page := runsPageText(m, 165, 48)
	wantAll(t, page, "review · Review then fix · started 11:59 · by AI", `started by AI in the chat "plan the release" · profile review`)
}

// Spec: the AGENTS view's run line says "by AI".
func TestAIAgentRunAgentsViewChip(t *testing.T) {
	agentsUnicode(t)
	msgs := agentsRunMsgs("r1", ai.RunStateDone, agentsDiamondSteps())
	msgs[1].Run.By = &ai.RunBy{Chat: "c", Depth: 1}
	m := agentsModel(t, msgs...)
	wantAll(t, agentsText(m, 64, 60), "run r1 · plan · by AI")
	msgs[1].Run.By = nil
	m = agentsModel(t, msgs...)
	wantAll(t, agentsText(m, 64, 60), "!by AI")
}

// Spec: the AI's own steps are no profile anyone can name, and a run of
// them that stopped resumes with those steps in this session.
func TestAIAgentRunInlineResume(t *testing.T) {
	if ai.ValidProfileName(aiRunInline) == nil {
		t.Fatalf("%q is a valid profile name", aiRunInline)
	}
	block := &blockingAIProvider{started: make(chan struct{}, 1), stopped: make(chan struct{})}
	m, _ := aiRunModel(t, block, block)
	steps := ai.Profile{Name: aiRunInline, Source: ai.ProfileSourceUser, Steps: []ai.ProfileStep{{ID: "step-1", Title: "Counter", Slot: ai.SlotMain}}}
	m.ai.runQueue = []aiQueuedRun{{id: "in1", chat: m.ai.conv.ID, prof: steps, task: "count", by: ai.RunBy{Chat: m.ai.conv.ID, Depth: 1}}}
	if m.aiStartQueuedRun() == nil {
		t.Fatal("not started")
	}
	<-block.started
	m.aiCancelAll()
	driveAIChat(t, m, waitAIChat(m.ai.chatCh), nil)
	idx := len(m.ai.conv.Messages) - 1
	for m.ai.conv.Messages[idx].Run == nil {
		idx--
	}
	if st := m.ai.conv.Messages[idx].Run.State; st != ai.RunStateCancelled {
		t.Fatalf("state %s", st)
	}
	again := &recordingAIProvider{replies: []ai.Chunk{{Delta: "42 lines"}}}
	m.ai.slotFake[ai.SlotMain] = aiSlot{p: again, cfg: ai.Config{Kind: ai.KindOllama, Model: "fake-1"}}
	cmd := m.aiRetryRun(idx)
	if cmd == nil || !m.ai.streaming {
		t.Fatalf("no resume; toast %q", toastText(*m))
	}
	driveAIChat(t, m, cmd, nil)
	if rm := aiRunMsg(t, m); rm.Run.State != ai.RunStateDone || rm.Text != "42 lines" || rm.Steps[0].Title != "Counter" {
		t.Fatalf("run %+v text %q", rm.Run, rm.Text)
	}
}
