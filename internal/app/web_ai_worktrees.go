package app

import (
	"bytes"
	"sort"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The agent worktrees view of the desktop page (WEBP-3): the laptop's
// "AI: Agent Worktrees…" list for the page. ai.worktrees lists every
// agent worktree of the repository with the files it changed;
// ai.worktree_text reads one of them before and after the agent (a file
// that may hold secrets stays on the laptop). Merge and Discard wait for
// a laptop card and then run the laptop's own Merge and Discard: a
// conflict asks on the laptop, as there. git runs off the UI thread.

func init() {
	webOps[webproto.OpAIWorktrees] = (*Model).webAIWorktrees
	webOps[webproto.OpAIWorktreeText] = (*Model).webAIWorktreeText
	webAskOps[webproto.OpAIWorktreeMerge] = (*Model).webAIWorktreeMerge
	webAskOps[webproto.OpAIWorktreeDiscard] = (*Model).webAIWorktreeDiscard
}

// webAIWorktreeStep is what this chat knows of a worktree: the step that
// owns it and whether that step runs now.
type webAIWorktreeStep struct {
	key, title string
	running    bool
}

// webAIWorktreeSteps are the open worktrees of this chat's steps, by id.
func (s *aiState) webAIWorktreeSteps() map[string]webAIWorktreeStep {
	out := map[string]webAIWorktreeStep{}
	for _, msg := range s.conv.Messages {
		if msg.Run == nil {
			continue
		}
		for _, st := range msg.Steps {
			if st.Worktree == nil || !st.Worktree.Open() {
				continue
			}
			out[st.Worktree.ID] = webAIWorktreeStep{key: msg.Run.ID + ":" + st.ID, title: aiCleanText(st.Title),
				running: s.run != nil && agentdag.State(st.State) == agentdag.StateRunning}
		}
	}
	return out
}

func (m *Model) webAIWorktrees(call *webCall) tea.Cmd {
	s := m.aiS()
	steps := s.webAIWorktreeSteps()
	off := !s.settings.UseWorktrees()
	root := aiWorkspaceRoot()
	call.later()
	return func() tea.Msg {
		out := webproto.AIWorktrees{Off: off, Items: []webproto.AIWorktree{}}
		left, err := git.LeftoverAgentWorktrees(root)
		if err != nil {
			out.NoRepo = true
			call.reply(out)
			return nil
		}
		for _, wt := range left {
			if len(out.Items) == webproto.MaxAIWorktrees {
				break
			}
			st := steps[wt.ID]
			it := webproto.AIWorktree{ID: wt.ID, Branch: webName(wt.Branch), Owner: webName(st.title), Running: st.running, Files: []webproto.AIWorktreeChange{}}
			if !st.running {
				ch, err := git.AgentChanges(root, wt.ID)
				if err == nil {
					sort.Slice(ch, func(i, j int) bool { return ch[i].Path < ch[j].Path })
					for _, c := range ch {
						if len(it.Files) == webproto.MaxAIWorktreeFiles {
							it.Cut = true
							break
						}
						it.Files = append(it.Files, webproto.AIWorktreeChange{Path: c.Path, Status: webAIChangeStatus(c.Status)})
					}
				}
			}
			out.Items = append(out.Items, it)
		}
		call.reply(out)
		return nil
	}
}

// webAIChangeStatus maps git's status letter to A, M or D.
func webAIChangeStatus(st string) string {
	switch st {
	case "A", "D":
		return st
	}
	return "M"
}

func (m *Model) webAIWorktreeText(call *webCall) tea.Cmd {
	a := call.args.(*webproto.AIWorktreeTextArgs)
	if st, ok := m.aiS().webAIWorktreeSteps()[a.ID]; ok && st.running {
		call.fail(webproto.CodeBusy, "Its step still runs. Look again when it ends.")
		return nil
	}
	root := aiWorkspaceRoot()
	call.later()
	return func() tea.Msg {
		fs, err := git.AgentFiles(root, a.ID)
		if err != nil {
			call.fail(webproto.CodeNotFound, "That worktree is gone.")
			return nil
		}
		for _, f := range fs {
			if f.Path != a.Path {
				continue
			}
			// Only a file the agent changed is named back, and one that may
			// hold secrets stays on the laptop.
			if ai.IsSecretFileName(a.Path) {
				call.reply(webproto.AIWorktreeText{ID: a.ID, Path: a.Path, Secret: true})
				return nil
			}
			out := webproto.AIWorktreeText{ID: a.ID, Path: a.Path}
			if webAIBinary(f.Before) || webAIBinary(f.After) {
				out.Binary = true
			} else {
				out.Before, out.After = webAICap(f.Before, &out.Truncated), webAICap(f.After, &out.Truncated)
			}
			call.reply(out)
			return nil
		}
		call.fail(webproto.CodeNotFound, "The agent did not change that file.")
		return nil
	}
}

// webAIBinary: not text (a NUL near the start, or not UTF-8).
func webAIBinary(s string) bool {
	head := s[:min(len(s), 8000)]
	return bytes.IndexByte([]byte(head), 0) >= 0 || !utf8.ValidString(s)
}

// webAICap cuts s to webproto.MaxGitText at a line end.
func webAICap(s string, cut *bool) string {
	if len(s) <= webproto.MaxGitText {
		return s
	}
	*cut = true
	s = s[:webproto.MaxGitText]
	if i := bytes.LastIndexByte([]byte(s), '\n'); i > 0 {
		s = s[:i+1]
	}
	return s
}

// webAIWorktreeRef is the laptop's reference to worktree id: a step of
// this chat owns it, or it is left over (the laptop's list keeps those
// in wtLeft). false: its step still runs.
func (m *Model) webAIWorktreeRef(call *webCall, id string) (string, bool) {
	s := m.aiS()
	if st, ok := s.webAIWorktreeSteps()[id]; ok {
		if st.running {
			call.fail(webproto.CodeBusy, "Its step still runs. Wait until it ends.")
			return "", false
		}
		return st.key, true
	}
	return "left:" + id, true
}

// webAIWorktreeLeft makes a left-over worktree known to the laptop's
// Merge and Discard (as its list does) and returns its reference.
func (m *Model) webAIWorktreeLeft(key, id string) {
	s := m.aiS()
	if key != "left:"+id {
		return
	}
	if s.wtLeft == nil {
		s.wtLeft = map[string]*ai.StoredWorktree{}
	}
	if wt := s.wtLeft[id]; wt == nil || !wt.Open() {
		s.wtLeft[id] = &ai.StoredWorktree{ID: id, Branch: git.AgentBranchPrefix + id, State: ai.WorktreeReady}
	}
}

// webAIWorktreeMerge asks the laptop to merge the agent's work into the
// user's files (the laptop's Merge: a conflict writes nothing and asks
// on the laptop).
func (m *Model) webAIWorktreeMerge(call *webCall) *webAsk {
	id := call.args.(*webproto.AIWorktreeArgs).ID
	key, ok := m.webAIWorktreeRef(call, id)
	if !ok {
		return nil
	}
	branch := git.AgentBranchPrefix + id
	return &webAsk{
		summary: branch,
		title:   "Merge the agent's work?",
		body:    "Merge writes the files the agent changed on branch " + branch + " into your tree. If you changed one of them too, nothing is written and the laptop asks what to do.",
		action:  "Merge",
		run: func(m *Model) tea.Cmd {
			m.webAIWorktreeLeft(key, id)
			return m.onAIWorktreeButton("wt-merge:" + key)
		},
	}
}

// webAIWorktreeDiscard asks the laptop to remove the agent's worktree and
// branch (the user's files do not change).
func (m *Model) webAIWorktreeDiscard(call *webCall) *webAsk {
	id := call.args.(*webproto.AIWorktreeArgs).ID
	key, ok := m.webAIWorktreeRef(call, id)
	if !ok {
		return nil
	}
	branch := git.AgentBranchPrefix + id
	return &webAsk{
		summary: branch,
		title:   "Discard the agent's work?",
		body:    "Discard removes the agent's worktree and the branch " + branch + ". Your files do not change. This cannot be undone.",
		action:  "Discard",
		run: func(m *Model) tea.Cmd {
			m.webAIWorktreeLeft(key, id)
			root := aiWorkspaceRoot()
			return func() tea.Msg {
				return aiWorktreeDiscardedMsg{key: key, err: git.RemoveAgentWorktree(root, id, true)}
			}
		},
	}
}
