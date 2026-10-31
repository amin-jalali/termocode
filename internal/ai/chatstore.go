package ai

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
)

// Stored chat roles. "tool" and "error" lines are kept for display only;
// they are not replayed to the model.
const (
	StoredUser      = "user"
	StoredAssistant = "assistant"
	StoredTool      = "tool"
	StoredError     = "error"
	StoredNote      = "note" // a status line of termocode (keep going stopped …); only shown
)

// StoredMessage is one persisted chat line.
type StoredMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
	// Context is attached file / selection text sent to the model with a
	// user message but not shown in the transcript.
	Context string `json:"context,omitempty"`
	// Images are paths of image files sent with a user message. Only the
	// path is stored; the file is read again for every request.
	Images []string `json:"images,omitempty"`
	// Rules is the project rules line of a user message (OM-1), e.g.
	// "AGENTS.md (4 KB)" or "AGENTS.md (not used)"; "" when the workspace
	// had no rules file. Only shown, never sent to the model.
	Rules string `json:"rules,omitempty"`
	// Lessons is how many memory lessons (OM-2) went into the prompt of
	// this user message's turn. Only shown.
	Lessons int `json:"lessons,omitempty"`
	// Memory is how many lessons the memory extractor saved after the
	// turn that ends with this message (OM-2). Only shown.
	Memory int `json:"memory,omitempty"`
	// Run and Steps are set on the assistant message of an agent
	// pipeline run (U20): the run's summary and one card per step. A
	// plain chat turn has neither.
	Run   *StoredRun   `json:"run,omitempty"`
	Steps []StoredStep `json:"steps,omitempty"`
	// Model is the model that answered a plain chat turn (the profile
	// step's slot model); "" for older chats and pipeline runs.
	Model string `json:"model,omitempty"`
	// Summary is set on a user message when the messages before it were
	// summarised to fit the context window (OMX-3): from then on the
	// model gets Summary instead of them. Summarised is how many stored
	// messages it stands for (only shown).
	Summary    string `json:"summary,omitempty"`
	Summarised int    `json:"summarised,omitempty"`
	// KeepGoing is set on a user message the keep-going loop sent
	// (OMX-7): the push number, from 1.
	KeepGoing int `json:"keep_going,omitempty"`
}

// Run states (StoredRun.State).
const (
	RunStateRunning     = "running"
	RunStateDone        = "done"
	RunStateFailed      = "failed"
	RunStateCancelled   = "cancelled"
	RunStateInterrupted = "interrupted" // termocode stopped while it ran
	RunStatePaused      = "paused"      // the budget is reached; Continue or Stop
)

// StoredRun is the summary of one pipeline run.
type StoredRun struct {
	ID        string `json:"id"`
	Profile   string `json:"profile,omitempty"`
	State     string `json:"state"`
	ElapsedMs int64  `json:"elapsed_ms,omitempty"`
	// StartedAtMs is when the run started (Unix ms; 0 in older chats:
	// RunStarted reads the time from the run id then).
	StartedAtMs int64 `json:"started_at_ms,omitempty"`
	// Budget is the run's budget in dollars and Spent what it had cost
	// when it paused (RunStatePaused).
	Budget float64 `json:"budget,omitempty"`
	Spent  float64 `json:"spent,omitempty"`
	// Work until done (a profile with a loop): Rounds is the round the
	// run is in (or ended in) and MaxRounds its limit; Verdict is "pass"
	// or "fail" once it ended; Report is the last failing verify report
	// (the fix step's input; a resume needs it).
	Rounds    int    `json:"rounds,omitempty"`
	MaxRounds int    `json:"max_rounds,omitempty"`
	Verdict   string `json:"verdict,omitempty"`
	Report    string `json:"report,omitempty"`
	// Limit is the usage-limit message of a CLI step that paused the run
	// (CS-4) and ResumeAtMs when it resumes by itself (Unix ms; 0: the
	// CLI did not say, Resume goes on by hand).
	Limit      string `json:"limit,omitempty"`
	ResumeAtMs int64  `json:"resume_at_ms,omitempty"`
	// Approved are the ids of the steps whose plan the user approved
	// (G-PLAN): a resume does not ask for them again. PlanWait is set
	// while the run waits for the user's answer on its plan.
	Approved []string `json:"approved,omitempty"`
	PlanWait bool     `json:"plan_wait,omitempty"`
	// By is set when the AI started the run (agent_run_start, G-API).
	By *RunBy `json:"by,omitempty"`
}

// RunBy is who started a run the AI started: the chat where it asked,
// and the run and step when a step of a run asked.
type RunBy struct {
	Chat      string `json:"chat"`
	ChatTitle string `json:"chat_title,omitempty"`
	ParentRun string `json:"parent_run,omitempty"`
	Step      string `json:"step,omitempty"`
	// Depth is 1 for a run the chat's AI started, 2 for one a step of
	// such a run started (the most there may be).
	Depth int `json:"depth"`
	// Worktree is "off" when the AI asked for no agent worktrees.
	Worktree string `json:"worktree,omitempty"`
	// ParentBudget is the asking run's budget in dollars (0: none) and
	// Budget what was left of it when this run started.
	ParentBudget float64 `json:"parent_budget,omitempty"`
	Budget       float64 `json:"budget_left,omitempty"`
}

// StoredStep is one step card of a pipeline run.
type StoredStep struct {
	ID       string `json:"id"`
	Title    string `json:"title,omitempty"`
	Slot     string `json:"slot,omitempty"`
	Model    string `json:"model,omitempty"`
	Category string `json:"category,omitempty"`
	// Text is the step's final text (while it runs: the end of what it
	// wrote so far).
	Text      string   `json:"text,omitempty"`
	Tools     []string `json:"tools,omitempty"` // tool rows ("⚙ Read a.go → 10 lines")
	ElapsedMs int64    `json:"elapsed_ms,omitempty"`
	// StartedAtMs is when the step first started (Unix ms; 0: it did not
	// run yet, or an older chat).
	StartedAtMs int64  `json:"started_at_ms,omitempty"`
	Usage       *Usage `json:"usage,omitempty"`
	// Cost in dollars and context use in percent; nil when unknown.
	Cost       *float64 `json:"cost,omitempty"`
	ContextPct *float64 `json:"context_pct,omitempty"`
	Files      int      `json:"files,omitempty"` // files the step changed
	Error      string   `json:"error,omitempty"`
	State      string   `json:"state,omitempty"` // an agentdag state
	// Deps are the ids of the steps this one waits for (set for graph
	// runs; a chain leaves it empty). Planned marks a step a planner
	// added. Notes are short lines about the step (a plan repair).
	Deps    []string `json:"deps,omitempty"`
	Planned bool     `json:"planned,omitempty"`
	Notes   []string `json:"notes,omitempty"`
	// Prompt is a planned step's task text (from the plan), so the run
	// can resume after a restart. It lives in the chat file only; run
	// snapshots never hold prompts.
	Prompt string `json:"prompt,omitempty"`
	// Round is the loop round the step last ran in (0: no loop).
	Round int `json:"round,omitempty"`
	// CLI is the official CLI the step ran with on the user's
	// subscription (CLIClaude, CLICodex; "": a model slot). Its tokens
	// have no price: the cost is the subscription.
	CLI string `json:"cli,omitempty"`
	// Worktree is the git worktree the step ran in (G-WT; nil: the
	// user's tree).
	Worktree *StoredWorktree `json:"worktree,omitempty"`
}

// Conversation is one persisted chat. Port of mobocode's chat_store.dart,
// but one JSON file per chat under ai/chats/ so a write never rewrites
// the whole history.
type Conversation struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Messages    []StoredMessage `json:"messages"`
	UpdatedAtMs int64           `json:"updated_at_ms"`
	// Folded holds the user's fold choices on step cards by card id
	// ("step:<run>:<step>"; true = folded).
	Folded map[string]bool `json:"folded,omitempty"`
	// ForkedFrom is set on a chat forked from another one (G-FORK).
	ForkedFrom *ForkInfo `json:"forked_from,omitempty"`
	// PlanApproval overrides ai_plan_approval for this chat ("": the
	// setting; AI: Toggle Plan Approval sets it).
	PlanApproval string `json:"plan_approval,omitempty"`
	// Reasoning is this chat's reasoning level ("": the slot's; "auto":
	// the slot's, picked on purpose; else a level). AI: Next Reasoning
	// Level sets it.
	Reasoning string `json:"reasoning,omitempty"`
	// Todos is the AI's todo list for this chat (todo_write, OMX-7) and
	// KeepGoing whether a turn that ends with open todos sends the next
	// "keep going" message by itself (AI: Toggle Keep Going).
	Todos     []Todo `json:"todos,omitempty"`
	KeepGoing bool   `json:"keep_going,omitempty"`
}

const (
	defaultChatTitle = "New chat"
	maxChats         = 50
	titleMaxLen      = 40
)

// ChatStore persists conversations in Dir. Every method is best-effort
// about corrupt files: they are skipped, never returned as errors.
type ChatStore struct{ Dir string }

// DefaultChatStore stores chats in <config>/ai/chats.
func DefaultChatStore() ChatStore {
	d, err := ConfigDir()
	if err != nil {
		return ChatStore{}
	}
	return ChatStore{Dir: filepath.Join(d, "ai", "chats")}
}

// NewConversation returns an empty, unsaved conversation with a fresh id.
func NewConversation() Conversation {
	now := time.Now().UnixMilli()
	var random [16]byte
	rand.Read(random[:])
	return Conversation{
		ID:          fmt.Sprintf("%d-%x", now, random),
		Title:       defaultChatTitle,
		UpdatedAtMs: now,
	}
}

// DeriveTitle returns the first user line (≤ 40 chars) or "New chat".
func DeriveTitle(msgs []StoredMessage) string {
	for _, m := range msgs {
		if m.Role != StoredUser {
			continue
		}
		line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(m.Text), "\n", 2)[0])
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > titleMaxLen {
			line = strings.TrimRight(string(r[:titleMaxLen]), " ")
		}
		return line
	}
	return defaultChatTitle
}

func (s ChatStore) path(id string) string {
	safe := strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return r
		}
		return '_'
	}, id)
	return filepath.Join(s.Dir, safe+".json")
}

// Save writes c (refreshing its title and timestamp) and prunes the store
// to the newest 50 chats. Empty conversations are not written.
func (s ChatStore) Save(c *Conversation) error {
	if s.Dir == "" || c == nil || c.ID == "" || len(c.Messages) == 0 {
		return nil
	}
	if c.Title == "" || c.Title == defaultChatTitle {
		c.Title = DeriveTitle(c.Messages)
	}
	c.UpdatedAtMs = time.Now().UnixMilli()
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(c) // compact: indenting cost more than the encoding itself
	if err != nil {
		return err
	}
	tmp := s.path(c.ID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path(c.ID)); err != nil {
		return err
	}
	s.prune()
	return nil
}

// Load reads one conversation.
func (s ChatStore) Load(id string) (Conversation, error) {
	var c Conversation
	if s.Dir == "" || id == "" {
		return c, os.ErrNotExist
	}
	b, err := os.ReadFile(s.path(id))
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Conversation{}, err
	}
	return c, nil
}

// List returns every stored conversation, newest first. The files are
// read and parsed by a few goroutines at once (parsing is the slow part
// of a long history).
func (s ChatStore) List() []Conversation { return s.ListCached(nil) }

// ChatListCache keeps the chats ListCached parsed, by file: a file that
// is still the same file with the same size and time is not read again.
// The chats it returns are shared between calls, so a caller may only
// change them in ways it would repeat on every call.
type ChatListCache struct {
	files map[string]chatListEntry
}

type chatListEntry struct {
	fi os.FileInfo
	c  Conversation
}

func sameChatFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// ListCached is List, reading only the files that changed since the last
// call with lc (nil: read every file).
func (s ChatStore) ListCached(lc *ChatListCache) []Conversation {
	if s.Dir == "" {
		return nil
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil
	}
	var names []string
	var infos []os.FileInfo
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
			var fi os.FileInfo
			if lc != nil {
				fi, _ = e.Info()
			}
			infos = append(infos, fi)
		}
	}
	parsed := make([]Conversation, len(names))
	var todo []int
	for i, n := range names {
		if lc != nil {
			if old, ok := lc.files[n]; ok && sameChatFile(old.fi, infos[i]) {
				parsed[i] = old.c
				continue
			}
		}
		todo = append(todo, i)
	}
	work := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < min(runtime.GOMAXPROCS(0), len(todo)); w++ {
		wg.Add(1)
		crash.Go("reading saved chats", func() {
			defer wg.Done()
			for i := range work {
				b, err := os.ReadFile(filepath.Join(s.Dir, names[i]))
				if err != nil {
					continue
				}
				var c Conversation
				if json.Unmarshal(b, &c) == nil {
					parsed[i] = c
				}
			}
		})
	}
	for _, i := range todo {
		work <- i
	}
	close(work)
	wg.Wait()
	if lc != nil {
		lc.files = make(map[string]chatListEntry, len(names))
		for i, n := range names {
			if infos[i] != nil && parsed[i].ID != "" { // a file that failed is read again
				lc.files[n] = chatListEntry{fi: infos[i], c: parsed[i]}
			}
		}
	}
	var out []Conversation
	for _, c := range parsed { // directory order, as before the sort
		if c.ID != "" {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAtMs > out[j].UpdatedAtMs })
	return out
}

// Delete removes one conversation.
func (s ChatStore) Delete(id string) error {
	if s.Dir == "" {
		return nil
	}
	err := os.Remove(s.path(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// prune deletes all but the newest maxChats chats. It runs after every
// save, so it reads no chat file while there are not more files than
// that, and then only each chat's id and time.
func (s ChatStore) prune() {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	if n <= maxChats {
		return
	}
	type head struct {
		ID          string `json:"id"`
		UpdatedAtMs int64  `json:"updated_at_ms"`
	}
	var list []head
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			continue
		}
		var h head
		if json.Unmarshal(b, &h) != nil || h.ID == "" {
			continue
		}
		list = append(list, h)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].UpdatedAtMs > list[j].UpdatedAtMs })
	for i := maxChats; i < len(list); i++ {
		_ = s.Delete(list[i].ID)
	}
}
