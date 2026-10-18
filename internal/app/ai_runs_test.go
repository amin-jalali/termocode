package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// U20 AP-4: agent pipeline runs in the chat.

// blockingAIProvider answers nothing until its request is stopped.
type blockingAIProvider struct {
	started chan struct{}
	stopped chan struct{}
}

func (*blockingAIProvider) Name() string        { return "block" }
func (*blockingAIProvider) Configured() bool    { return true }
func (*blockingAIProvider) SupportsTools() bool { return true }
func (p *blockingAIProvider) Stream(ctx context.Context, _ ai.Request) <-chan ai.Chunk {
	ch := make(chan ai.Chunk, 1)
	go func() {
		defer close(ch)
		p.started <- struct{}{}
		<-ctx.Done()
		close(p.stopped)
		ch <- ai.Chunk{Done: true, Err: ctx.Err()}
	}()
	return ch
}

// aiRunModel is aiIDETestModel with the built-in "review" profile active
// and its two slots (main for the reviewer, quick for the fixer) faked.
func aiRunModel(t *testing.T, reviewer, fixer ai.Provider) (*Model, string) {
	t.Helper()
	t.Cleanup(clock.Set(func() time.Time { return time.Date(2026, 10, 18, 9, 5, 0, 0, time.UTC) }))
	m, dir := aiIDETestModel(t, reviewer)
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain:  {p: reviewer, cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5"}},
		ai.SlotQuick: {p: fixer, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
	m.ai.profile = ai.ProfileReview
	return m, dir
}

func aiTranscriptText(m *Model, width int) string {
	prev := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	defer func() { theme.CurrentIconMode = prev }()
	m.ai.runGen++ // the icon mode is not part of the transcript cache key
	var b strings.Builder
	for _, l := range m.buildAITranscript(width) {
		b.WriteString(strings.TrimRight(ansi.Strip(renderAILine(l, width)), " ") + "\n")
	}
	return b.String()
}

func aiRunMsg(t *testing.T, m *Model) *ai.StoredMessage {
	t.Helper()
	for i := len(m.ai.conv.Messages) - 1; i >= 0; i-- {
		if m.ai.conv.Messages[i].Run != nil {
			return &m.ai.conv.Messages[i]
		}
	}
	t.Fatalf("no run message in %+v", m.ai.conv.Messages)
	return nil
}

func lastUserContent(req ai.Request) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == ai.RoleUser {
			return req.Messages[i].Content
		}
	}
	return ""
}

func toolNames(req ai.Request) map[string]bool {
	out := map[string]bool{}
	for _, tl := range req.Tools {
		out[tl.Name] = true
	}
	return out
}

// Spec: a two-step profile runs both steps in order; step 2 gets step
// 1's answer as data; the last step's text is the assistant message;
// the cards fold when done; the run row sums time, tokens and cost.
func TestAIRunTwoSteps(t *testing.T) {
	rev := &recordingAIProvider{replies: []ai.Chunk{{Delta: "Problem: a.txt line 1 is wrong.",
		Usage: &ai.Usage{Model: "claude-sonnet-4-5", Input: 4000, Output: 100, Requests: 1, PeakContext: 4000}}}}
	fix := &recordingAIProvider{replies: []ai.Chunk{{Delta: "I fixed `a.txt`.",
		Usage: &ai.Usage{Model: "gpt-5-mini", Input: 9000, Output: 800, Requests: 1, PeakContext: 9000}}}}
	m, _ := aiRunModel(t, rev, fix)
	m.output = newOutputStore()
	driveAIChat(t, m, m.sendAIChat("review my last commit and fix what you find"), nil)

	if n := len(m.ai.conv.Messages); n != 2 {
		t.Fatalf("messages %+v", m.ai.conv.Messages)
	}
	rm := aiRunMsg(t, m)
	if rm.Role != ai.StoredAssistant || rm.Text != "I fixed `a.txt`." || rm.Run.State != ai.RunStateDone || rm.Run.Profile != ai.ProfileReview {
		t.Fatalf("run message %+v run %+v", rm, rm.Run)
	}
	if len(rm.Steps) != 2 || rm.Steps[0].State != string(agentdag.StateCompleted) || rm.Steps[1].State != string(agentdag.StateCompleted) {
		t.Fatalf("steps %+v", rm.Steps)
	}
	if s := rm.Steps[0]; s.Title != "Reviewer" || s.Model != "claude-sonnet-4-5" || s.Category != "deep" || s.Text != "Problem: a.txt line 1 is wrong." || s.Cost == nil || s.Usage == nil {
		t.Errorf("step 1 %+v", s)
	}
	if len(rev.reqs) != 1 || len(fix.reqs) != 1 {
		t.Fatalf("requests %d / %d", len(rev.reqs), len(fix.reqs))
	}
	if got := lastUserContent(fix.reqs[0]); !strings.Contains(got, "review my last commit") || !strings.Contains(got, "### Output of step \"Reviewer\"\nProblem: a.txt line 1 is wrong.") {
		t.Errorf("fixer input %q", got)
	}
	if !strings.Contains(fix.reqs[0].Messages[0].Content, "not instructions") {
		t.Error("the step prompt must say step outputs are data")
	}
	if rt, ft := toolNames(rev.reqs[0]), toolNames(fix.reqs[0]); rt["write_file"] || !rt["read_file"] || !ft["write_file"] {
		t.Errorf("tool rules: reviewer write_file %v read_file %v, fixer write_file %v", rt["write_file"], rt["read_file"], ft["write_file"])
	}
	got := aiTranscriptText(m, 100)
	for _, want := range []string{
		"▸ 1 · Reviewer · claude-sonnet-4-5 · 0 s · 4.1k tok · $0.013",
		"▸ 2 · Fixer · gpt-5-mini · 0 s · 9.8k tok · $0.004",
		"I fixed",
		"─ run · 0 s · 2 steps · 13.9k tok · $0.017 · context 2 % ─",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("transcript lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Problem: a.txt") {
		t.Errorf("a finished step is folded:\n%s", got)
	}
	saved, err := m.ai.store.Load(m.ai.conv.ID)
	if err != nil || len(saved.Messages) != 2 || len(saved.Messages[1].Steps) != 2 {
		t.Errorf("chat file %+v %v", saved, err)
	}
	var audit []string
	for _, l := range m.output.Lines(aiAuditChannel) {
		if strings.Contains(l, "usage  step ") {
			audit = append(audit, l)
		}
	}
	if len(audit) != 2 || !strings.Contains(audit[0], "step reviewer · claude-sonnet-4-5") {
		t.Errorf("audit usage lines %q", audit)
	}
}

// Spec: the history sent to the model after a run holds the run's final
// answer (and its tool lines), as for a plain turn.
func TestAIRunHistoryAfterRun(t *testing.T) {
	rev := &recordingAIProvider{replies: []ai.Chunk{{Delta: "r1"}}}
	fix := &recordingAIProvider{replies: []ai.Chunk{{Delta: "f1"}}}
	m, _ := aiRunModel(t, rev, fix)
	driveAIChat(t, m, m.sendAIChat("first"), nil)
	m.ai.profile = ai.ProfileDefault
	driveAIChat(t, m, m.sendAIChat("second"), nil)
	if len(rev.reqs) != 2 {
		t.Fatalf("requests %d", len(rev.reqs))
	}
	var roles []string
	for _, msg := range rev.reqs[1].Messages {
		roles = append(roles, string(msg.Role)+":"+msg.Content)
	}
	got := strings.Join(roles[1:], "|")
	if got != "user:first|assistant:f1|user:second" {
		t.Errorf("history %q", got)
	}
}

// Spec (B5): a running card is expanded, a finished one folded; a click
// toggles a card and the choice stays after later updates and is saved
// with the chat.
func TestAIRunCardFoldRules(t *testing.T) {
	m, _ := aiRunModel(t, &recordingAIProvider{}, &recordingAIProvider{})
	s := m.ai
	s.conv.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "go"},
		{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r1", Profile: "review", State: ai.RunStateRunning},
			Steps: []ai.StoredStep{
				{ID: "reviewer", Title: "Reviewer", Model: "claude-sonnet-4-5", State: "completed", Text: "two problems found", Tools: []string{"⚙ Read a.go → 3 lines"}},
				{ID: "fixer", Title: "Fixer", Model: "gpt-5-mini", Category: "quick", State: "running", Text: "so I will also update", Tools: []string{"⚙ Write a.go"}},
			}},
	}
	s.streaming, s.turnMsg = true, 0
	got := aiTranscriptText(m, 100)
	for _, want := range []string{"▸ 1 · Reviewer", "▾ 2 · Fixer · gpt-5-mini (quick)", "⚙ Write a.go", "✎ now · \"…so I will also update\""} {
		if !strings.Contains(got, want) {
			t.Errorf("running: lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Read a.go") {
		t.Errorf("finished card not folded:\n%s", got)
	}
	m.onAIButton("step:r1:reviewer")
	s.conv.Messages[1].Steps[1].Text += " the test"
	s.runGen++
	got = aiTranscriptText(m, 100)
	if !strings.Contains(got, "▾ 1 · Reviewer") || !strings.Contains(got, "⚙ Read a.go → 3 lines") || !strings.Contains(got, "two problems found") {
		t.Errorf("toggled card not expanded after an update:\n%s", got)
	}
	m.onAIButton("step:r1:fixer")
	if got = aiTranscriptText(m, 100); !strings.Contains(got, "▸ 2 · Fixer") || strings.Contains(got, "⚙ Write a.go") {
		t.Errorf("running card toggled closed:\n%s", got)
	}
	if !s.conv.Folded["step:r1:fixer"] || s.conv.Folded["step:r1:reviewer"] {
		t.Errorf("folded %v", s.conv.Folded)
	}
	// The keyboard: Tab selects a card, Enter (empty input) toggles it.
	s.focused = true
	m.actionsOpen, m.actionsPinned = true, true
	m.routeAIPanelKey(tea.KeyMsg{Type: tea.KeyTab})
	if s.cardSel != "step:r1:reviewer" {
		t.Fatalf("tab selected %q", s.cardSel)
	}
	m.routeAIPanelKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.conv.Folded["step:r1:reviewer"] {
		t.Errorf("enter did not toggle: %v", s.conv.Folded)
	}
}

// Spec: the run row hides what is unknown: no price → no $, no window
// → no context.
func TestAIRunSummaryRowHidesUnknown(t *testing.T) {
	steps := []ai.StoredStep{
		{State: "completed", ElapsedMs: 14000, Usage: &ai.Usage{Input: 4000, Output: 100}},
		{State: "completed", ElapsedMs: 22000, Usage: &ai.Usage{Input: 9000, Output: 800}},
	}
	run := &ai.StoredRun{State: ai.RunStateDone, ElapsedMs: 36000}
	if got := aiRunSummaryText(run, steps); got != "run · 36 s · 2 steps · 13.9k tok" {
		t.Errorf("no price, no window: %q", got)
	}
	c1, c2, p1, p2 := 0.013, 0.004, 31.0, 12.0
	steps[0].Cost, steps[1].Cost, steps[0].ContextPct, steps[1].ContextPct = &c1, &c2, &p1, &p2
	if got := aiRunSummaryText(run, steps); got != "run · 36 s · 2 steps · 13.9k tok · $0.017 · context 31 %" {
		t.Errorf("full: %q", got)
	}
	steps[1].Cost = nil
	if got := aiRunSummaryText(run, steps); strings.Contains(got, "$") {
		t.Errorf("one step without a price hides the run cost: %q", got)
	}
	if got := aiRunSummaryText(run, []ai.StoredStep{{State: "completed"}, {State: "completed"}}); got != "run · 36 s · 2 steps" {
		t.Errorf("no usage: %q", got)
	}
	run.State, run.ElapsedMs = ai.RunStateFailed, 65000
	if got := aiRunSummaryText(run, []ai.StoredStep{{State: "completed"}, {State: "failed"}}); got != "run failed · 1m 05s · 1/2 steps" {
		t.Errorf("failed: %q", got)
	}
}

// Spec: Ctrl+C stops the whole run: the running step and the waiting
// one are cancelled and the chat is saved.
func TestAIRunCtrlCCancelsWholeRun(t *testing.T) {
	rev := &blockingAIProvider{started: make(chan struct{}, 1), stopped: make(chan struct{})}
	m, _ := aiRunModel(t, rev, &recordingAIProvider{})
	cmd := m.sendAIChat("go")
	cancelled := false
	for i := 0; cmd != nil && i < 200; i++ {
		msg := nextAIMsg(t, cmd)
		if msg == nil {
			break
		}
		m.handleAIMsg(msg)
		if _, done := msg.(aiChatDoneMsg); done {
			break
		}
		if !cancelled && m.ai.conv.Messages[1].Steps[0].State == string(agentdag.StateRunning) {
			select { // the step's request is out
			case <-rev.started:
			case <-time.After(5 * time.Second):
				t.Fatal("the first step sent no request")
			}
			m.focusAIForTest()
			m.routeAIPanelKey(tea.KeyMsg{Type: tea.KeyCtrlC})
			cancelled = true
		}
		cmd = waitAIChat(m.ai.chatCh)
	}
	select {
	case <-rev.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the running step was not stopped")
	}
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateCancelled || rm.Steps[0].State != "cancelled" || rm.Steps[1].State != "cancelled" {
		t.Errorf("run %+v steps %+v", rm.Run, rm.Steps)
	}
	if m.ai.streaming || m.ai.run != nil {
		t.Error("the run is still marked running")
	}
	if got := aiTranscriptText(m, 100); !strings.Contains(got, "run cancelled") || !strings.Contains(got, "Retry from step 1") {
		t.Errorf("transcript:\n%s", got)
	}
}

func (m *Model) focusAIForTest() {
	m.ai.focused = true
	m.actionsOpen, m.actionsPinned = true, true
}

// Spec: one restore turn per run: "Undo last turn" puts back the files
// of every step.
func TestAIRunUndoRestoresEveryStep(t *testing.T) {
	writeA := ai.Chunk{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "write_file", Args: map[string]any{"path": "a.txt", "content": "agent\n"}}}}
	createB := ai.Chunk{ToolCalls: []ai.ToolCall{{ID: "c2", Name: "create_file", Args: map[string]any{"path": "new.txt", "content": "fresh\n"}}}}
	one := &recordingAIProvider{replies: []ai.Chunk{writeA, {Delta: "wrote a"}}}
	two := &recordingAIProvider{replies: []ai.Chunk{createB, {Delta: "created new"}}}
	m, dir := aiRunModel(t, one, two)
	m.ai.settings.AutoApprove = ai.ToolClasses // no review card
	writeProfiles(t, `{"profiles":{"both":{"steps":[{"id":"a","slot":"main"},{"id":"b","slot":"quick"}]}}}`)
	m.ai.loadProfiles(ai.Credentials{})
	m.ai.profile = "both"
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	driveAIChat(t, m, m.sendAIChat("change things"), nil)
	if readFile(filepath.Join(dir, "a.txt")) != "agent\n" || readFile(filepath.Join(dir, "new.txt")) != "fresh\n" {
		t.Fatalf("the steps did not write: %+v", aiRunMsg(t, m).Steps)
	}
	if n := m.aiRestoreFiles(0); n != 2 {
		t.Fatalf("restore point has %d files", n)
	}
	if rm := aiRunMsg(t, m); rm.Steps[0].Files != 1 || rm.Steps[1].Files != 1 {
		t.Errorf("files per step %d %d", rm.Steps[0].Files, rm.Steps[1].Files)
	}
	m.aiUndoTurn(-1)
	if got := readFile(filepath.Join(dir, "a.txt")); got != "mine\n" {
		t.Errorf("a.txt %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err == nil {
		t.Error("new.txt still there")
	}
}

func writeProfiles(t *testing.T, body string) {
	t.Helper()
	p, err := ai.ProfilesPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Spec: a failed step stops the run; "Retry from step N" runs again from
// that step with the saved output of the steps before it, in the same
// message and the same restore turn.
func TestAIRunRetryFromStep(t *testing.T) {
	rev := &recordingAIProvider{replies: []ai.Chunk{{Delta: "found it"}}}
	m, _ := aiRunModel(t, rev, failingAIProvider{})
	driveAIChat(t, m, m.sendAIChat("go"), nil)
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateFailed || rm.Steps[1].State != "failed" || !strings.Contains(rm.Steps[1].Error, "boom") {
		t.Fatalf("run %+v steps %+v", rm.Run, rm.Steps)
	}
	got := aiTranscriptText(m, 100)
	if !strings.Contains(got, "✘ ") || !strings.Contains(got, "Retry from step 2") {
		t.Fatalf("transcript:\n%s", got)
	}
	fix := &recordingAIProvider{replies: []ai.Chunk{{Delta: "fixed now"}}}
	m.ai.slotFake[ai.SlotQuick] = aiSlot{p: fix, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}}
	driveAIChat(t, m, m.onAIButton("run-retry:1"), nil)
	if len(rev.reqs) != 1 {
		t.Errorf("step 1 ran again (%d requests)", len(rev.reqs))
	}
	if len(fix.reqs) != 1 || !strings.Contains(lastUserContent(fix.reqs[0]), "found it") {
		t.Fatalf("fixer input: %+v", fix.reqs)
	}
	if n := len(m.ai.conv.Messages); n != 2 {
		t.Errorf("messages %d", n)
	}
	rm = aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || rm.Text != "fixed now" || rm.Steps[0].Text != "found it" {
		t.Errorf("after retry %+v %+v", rm.Run, rm.Steps)
	}
}

// failingAIProvider fails every request.
type failingAIProvider struct{}

func (failingAIProvider) Name() string        { return "fail" }
func (failingAIProvider) Configured() bool    { return true }
func (failingAIProvider) SupportsTools() bool { return true }
func (failingAIProvider) Stream(context.Context, ai.Request) <-chan ai.Chunk {
	ch := make(chan ai.Chunk, 1)
	ch <- ai.Chunk{Done: true, Err: errBoom}
	close(ch)
	return ch
}

var errBoom = &aiTestErr{"boom: bad request"}

type aiTestErr struct{ s string }

func (e *aiTestErr) Error() string { return e.s }

// Spec: a chat saved while a run was going (termocode stopped) loads as
// "run interrupted" with a retry button for the first unfinished step.
func TestAIRunInterruptedOnLoad(t *testing.T) {
	m, _ := aiRunModel(t, &recordingAIProvider{}, &recordingAIProvider{})
	c := ai.NewConversation()
	c.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "go"},
		{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r9", Profile: "review", State: ai.RunStateRunning},
			Steps: []ai.StoredStep{{ID: "reviewer", Title: "Reviewer", State: "completed", Text: "x"}, {ID: "fixer", Title: "Fixer", State: "running"}}},
	}
	if err := m.ai.store.Save(&c); err != nil {
		t.Fatal(err)
	}
	m.onAIHistoryPicked(c.ID)
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateInterrupted || rm.Steps[1].State != "cancelled" {
		t.Errorf("run %+v steps %+v", rm.Run, rm.Steps)
	}
	got := aiTranscriptText(m, 100)
	if !strings.Contains(got, "✘ run interrupted (termocode restarted)") || !strings.Contains(got, "Retry from step 2") {
		t.Errorf("transcript:\n%s", got)
	}
}

// Spec: run snapshots hold ids and states, never prompts or outputs;
// old ones are pruned at startup.
func TestAIRunSnapshotAndPrune(t *testing.T) {
	rev := &recordingAIProvider{replies: []ai.Chunk{{Delta: "secret review text"}}}
	fix := &recordingAIProvider{replies: []ai.Chunk{{Delta: "secret fix text"}}}
	m, _ := aiRunModel(t, rev, fix)
	driveAIChat(t, m, m.sendAIChat("my private prompt"), nil)
	dir, err := aiRunsDir()
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 1 {
		t.Fatalf("snapshots %v", files)
	}
	b, _ := os.ReadFile(files[0])
	for _, bad := range []string{"secret", "private prompt", "You review code"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("snapshot holds %q:\n%s", bad, b)
		}
	}
	for _, want := range []string{`"reviewer"`, `"completed"`, `"from"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("snapshot lacks %s:\n%s", want, b)
		}
	}
	old := filepath.Join(dir, "old.json")
	_ = os.WriteFile(old, []byte("{}"), 0o600)
	past := clock.Now().Add(-15 * 24 * time.Hour)
	_ = os.Chtimes(old, past, past)
	pruneAIRuns(clock.Now())
	if _, err := os.Stat(old); err == nil {
		t.Error("an old snapshot was not pruned")
	}
	if _, err := os.Stat(files[0]); err != nil {
		t.Error("a new snapshot was pruned")
	}
}

// Spec: a step's file edits go through the review card while the run's
// review locker is held.
func TestAIRunReviewLockerHeldDuringReview(t *testing.T) {
	write := ai.Chunk{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "write_file", Args: map[string]any{"path": "a.txt", "content": reviewNew}}}}
	rev := &recordingAIProvider{replies: []ai.Chunk{{Delta: "change line 2"}}}
	fix := &recordingAIProvider{replies: []ai.Chunk{write, {Delta: "done"}}}
	m, _, dir := reviewModel(t, rev, map[string]string{"a.txt": reviewOld})
	t.Cleanup(clock.Set(func() time.Time { return time.Date(2026, 10, 18, 9, 5, 0, 0, time.UTC) }))
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain:  {p: rev, cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5"}},
		ai.SlotQuick: {p: fix, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
	m.ai.profile = ai.ProfileReview
	held := false
	driveAIReview(t, m, m.sendAIChat("go"), func(m *Model) {
		if !m.reviewOpen {
			t.Fatal("review card not open")
		}
		if m.ai.reviewMu.TryLock() {
			m.ai.reviewMu.Unlock()
		} else {
			held = true
		}
		reviewKey(t, m, "a")
		reviewKey(t, m, "a")
	})
	if !held {
		t.Error("the review locker was free during the review")
	}
	if got := readFile(filepath.Join(dir, "a.txt")); got != reviewNew {
		t.Errorf("a.txt %q", got)
	}
	if !m.ai.reviewMu.TryLock() {
		t.Error("the review locker stays held after the review")
	}
}

// Spec: a slot that cannot be used stops the run before anything runs.
func TestAIRunPreflight(t *testing.T) {
	m, _ := aiRunModel(t, &recordingAIProvider{}, &recordingAIProvider{})
	delete(m.ai.slotFake, ai.SlotQuick)
	m.ai.slots = ai.NewSlotPool(map[string]ai.Config{}, nil)
	cmd := m.sendAIChat("go")
	if len(m.ai.conv.Messages) != 0 || m.ai.streaming {
		t.Fatalf("something ran: %+v", m.ai.conv.Messages)
	}
	_ = cmd
	if top := ansi.Strip(strings.Join(m.toast.View(120, 40), "\n")); !strings.Contains(top, "review") || !strings.Contains(top, "quick") {
		t.Errorf("toast %q", top)
	}
}

// Spec: problems in profiles.json are shown once as a toast at startup.
func TestAIProfileErrorsToast(t *testing.T) {
	m := aiTestModel(t)
	writeProfiles(t, `{"profiles": {"x": {"steps": [{"id": "a", "slot": "nope"}]}}}`)
	m.ai.loadProfiles(ai.Credentials{})
	cmd := m.aiProfileErrorsCmd()
	if cmd == nil {
		t.Fatal("no toast cmd")
	}
	msg, ok := cmd().(ToastMsg)
	if !ok || msg.Level != toast.Warn || !strings.Contains(msg.Body, "nope") {
		t.Errorf("toast %+v", msg)
	}
	m.ai.loadProfiles(ai.Credentials{})
	if m.aiProfileErrorsCmd() != nil {
		t.Error("the same errors are shown again")
	}
}

// Spec: the read-only tool rule never keeps a tool that changes things.
func TestAIStepToolRule(t *testing.T) {
	m, dir := aiIDETestModel(t, &recordingAIProvider{})
	all := m.aiChatTools(dir, nil)
	ro := aiFilterTools(all, agentdag.ToolRule{Preset: agentdag.PresetReadOnly})
	if len(ro) == 0 || len(ro) >= len(all) {
		t.Fatalf("read-only kept %d of %d", len(ro), len(all))
	}
	for _, tl := range ro {
		if tl.Mutating || tl.Gate != nil || tl.Class == ai.ClassReview {
			t.Errorf("read-only kept %s", tl.Name)
		}
	}
	if got := aiFilterTools(all, agentdag.ToolRule{Preset: agentdag.PresetNone}); len(got) != 0 {
		t.Errorf("none kept %d", len(got))
	}
	if got := aiFilterTools(all, agentdag.ToolRule{}); len(got) != len(all) {
		t.Errorf("default kept %d of %d", len(got), len(all))
	}
	got := aiFilterTools(all, agentdag.ToolRule{Allow: []string{"read_file", "write_file"}, Deny: []string{"write_file"}})
	if len(got) != 1 || got[0].Name != "read_file" {
		t.Errorf("allow/deny: %v", got)
	}
}
