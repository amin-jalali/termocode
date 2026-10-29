package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/theme"
)

// G-WT: agent steps that run at the same time as another writing step
// work in their own git worktree (internal/git agent_worktree.go), so
// they never change the same files. The step card shows the worktree's
// branch; when the step ends it shows how many files changed, with
// Review, Merge and Discard (ai_worktree_actions.go).
//
//	▸ 2 · Left · gpt-5-mini · 14 s · 2 tools
//	    ⑂ termocode/agent-r1-left · 3 files changed
//	    [ Review ] [ Merge ] [ Discard ]

// aiRunWorktrees are the worktrees of one launched run, by node id. The
// node goroutines read and add to it.
type aiRunWorktrees struct {
	root  string // the user's tree
	runID string
	on    bool // ai_worktrees is not off

	mu     sync.Mutex
	byNode map[string]git.AgentWorktree
}

// newAIRunWorktrees starts the run's worktree list; steps of rm that
// still have an open worktree (a retry) keep working in it.
func newAIRunWorktrees(root, runID string, on bool, rm *ai.StoredMessage) *aiRunWorktrees {
	w := &aiRunWorktrees{root: root, runID: runID, on: on, byNode: map[string]git.AgentWorktree{}}
	if rm != nil {
		for _, st := range rm.Steps {
			if wt := st.Worktree; wt.Open() {
				w.byNode[st.ID] = git.AgentWorktree{ID: wt.ID, Path: wt.Path, Branch: wt.Branch}
			}
		}
	}
	return w
}

// forNode is node n's worktree. A node that shares its wave with
// another writing node gets a new one (in the node's goroutine); note
// says why it works in the user's tree when that failed.
func (w *aiRunWorktrees) forNode(ctx context.Context, n agentdag.Node) (wt git.AgentWorktree, ok bool, note string) {
	w.mu.Lock()
	wt, ok = w.byNode[n.ID]
	w.mu.Unlock()
	id := git.AgentWorktreeID(w.runID, n.ID)
	switch {
	case ok: // a retry: the same tree again
		id = wt.ID
	case !w.on || !agentrun.SharedWave(ctx):
		return wt, false, ""
	case !git.IsRepo(w.root):
		return wt, false, "" // not a git repository: every step works in your tree
	}
	wt, err := git.CreateAgentWorktree(w.root, id)
	if err != nil {
		return wt, false, "⚠ no own worktree (" + aiOneLine(err.Error()) + "): works in your tree"
	}
	w.mu.Lock()
	w.byNode[n.ID] = wt
	w.mu.Unlock()
	return wt, true, ""
}

// get is node id's worktree (ok=false: it has none).
func (w *aiRunWorktrees) get(id string) (git.AgentWorktree, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	wt, ok := w.byNode[id]
	return wt, ok
}

// othersNote tells a node which other steps work in their own worktree:
// their changes are not in the user's tree until merged.
func (w *aiRunWorktrees) othersNote(id string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var names []string
	for node, wt := range w.byNode {
		if node != id {
			names = append(names, node+" ("+wt.Dir()+")")
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return "\n\n(Note: these steps work in their own git worktree; their file changes are not in the workspace until the user merges them: " +
		strings.Join(names, ", ") + ".)"
}

// aiWorktreePrompt is the system prompt line of a step in its own
// worktree: the workspace root is the worktree.
func aiWorktreePrompt(base, root string, wt git.AgentWorktree) string {
	line := "\nWorkspace root: " + wt.Dir() + " (your own git worktree of " + root + " on branch " + wt.Branch +
		"; other steps work at the same time in their own trees. Change files only here; the user reviews and merges your changes.)"
	if strings.Contains(base, "\nWorkspace root: "+root) {
		return strings.Replace(base, "\nWorkspace root: "+root, line, 1)
	}
	return base + line
}

// aiWorktreeMsg updates a step's worktree from a node goroutine.
type aiWorktreeMsg struct {
	gen  uint64
	ch   chan tea.Msg
	step string
	wt   ai.StoredWorktree
	note string
}

// aiWorktreeFinished runs in the node goroutine when node n ended: it
// counts the files the node changed in its worktree; with none the
// worktree goes (nothing to lose).
func (w *aiRunWorktrees) finished(n agentdag.Node) (aiWorktreeMsg, bool) {
	wt, ok := w.get(n.ID)
	if !ok {
		return aiWorktreeMsg{}, false
	}
	st := ai.StoredWorktree{ID: wt.ID, Path: wt.Path, Branch: wt.Branch, State: ai.WorktreeReady}
	ch, err := git.AgentChanges(w.root, wt.ID)
	switch {
	case err != nil:
		return aiWorktreeMsg{step: n.ID, wt: st, note: "⚠ could not read the worktree: " + aiOneLine(err.Error())}, true
	case len(ch) == 0:
		if err := git.RemoveAgentWorktree(w.root, wt.ID, true); err == nil {
			w.mu.Lock()
			delete(w.byNode, n.ID)
			w.mu.Unlock()
			st.State = ai.WorktreeDiscarded
			return aiWorktreeMsg{step: n.ID, wt: st, note: theme.IconBranch.String() + " no file changes: worktree removed"}, true
		}
	}
	st.Files = len(ch)
	return aiWorktreeMsg{step: n.ID, wt: st}, true
}

// onAIWorktree records a step's worktree on its card.
func (m *Model) onAIWorktree(msg aiWorktreeMsg) tea.Cmd {
	s := m.aiS()
	if msg.gen == s.chatGen {
		m.aiSetStepWorktree(msg.step, msg.wt, msg.note)
	}
	return waitAIChat(msg.ch)
}

// aiSetStepWorktree sets the worktree (and a note) of step id of the
// running run.
func (m *Model) aiSetStepWorktree(id string, wt ai.StoredWorktree, note string) {
	s := m.aiS()
	st := aiFindStep(s.aiRunMessage(), id)
	if st == nil {
		return
	}
	if wt.ID != "" {
		c := wt
		st.Worktree = &c
	}
	if note != "" {
		st.Notes = append(st.Notes, note)
	}
	s.runGen++
}

// aiWorktreeStatus is the card line of a step's worktree.
func aiWorktreeStatus(wt *ai.StoredWorktree) string {
	return theme.IconBranch.String() + " " + wt.Branch + " · " + aiWorktreeState(wt)
}

// aiWorktreeShort is the worktree line of a narrow AGENTS card (the
// node details name the branch).
func aiWorktreeShort(wt *ai.StoredWorktree) string {
	return theme.IconBranch.String() + " " + aiWorktreeState(wt)
}

// aiWorktreeState says what a step's worktree waits for.
func aiWorktreeState(wt *ai.StoredWorktree) string {
	head := ""
	switch wt.State {
	case ai.WorktreeRunning:
		return head + "working"
	case ai.WorktreeConflict:
		return head + "conflict: " + strings.Join(wt.Conflicts, ", ")
	case ai.WorktreeMerged:
		return head + "merged into your tree"
	case ai.WorktreeKept:
		return head + "merged with conflict markers · branch kept"
	case ai.WorktreeDiscarded:
		return head + "discarded"
	case ai.WorktreeOnBranch:
		return head + "not merged · work kept on the branch"
	}
	return head + fmt.Sprintf("%d file%s changed", wt.Files, plural(wt.Files))
}
