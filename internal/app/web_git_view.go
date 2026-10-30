package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop page's git views (WEBP-5): the commit graph, blame, the
// merge editor, interactive rebase, amend and clone. Reads run git off
// the UI thread with the laptop's own git package; whatever rewrites
// history or talks to a remote waits for a laptop card and then runs as
// the laptop's own command.

func init() {
	webOps[webproto.OpGitGraph] = (*Model).webGitGraph
	webOps[webproto.OpGitBlame] = (*Model).webGitBlame
	webAskOps[webproto.OpGitAmend] = (*Model).webGitAmend
	webAskOps[webproto.OpGitClone] = (*Model).webGitClone
}

var gitRefKinds = map[git.RefKind]string{git.RefLocal: "local", git.RefRemote: "remote", git.RefTag: "tag"}

func webGraphEdges(es []git.GraphEdge) []webproto.GitGraphEdge {
	out := make([]webproto.GitGraphEdge, 0, len(es))
	for _, e := range es {
		out = append(out, webproto.GitGraphEdge{F: e.From, T: e.To, C: e.Color})
	}
	return out
}

// webGitGraph answers one page of the graph. The lanes are laid out over
// every commit down to the page's end, so a later page joins the one
// above it.
func (m *Model) webGitGraph(call *webCall) tea.Cmd {
	a := call.args.(*webproto.GitGraphArgs)
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		read := git.CommitsAll
		if a.Head {
			read = git.Commits
		}
		cs, more, err := read(root, 0, a.Skip+webproto.MaxGraphPage)
		if err != nil {
			webGitFail(call, err)
			return nil
		}
		lanes := git.GraphLanes(cs)
		out := webproto.GitGraph{Skip: a.Skip, Rows: []webproto.GitGraphRow{}, More: more}
		for i := a.Skip; i < len(cs); i++ {
			c, l := cs[i], lanes[i]
			r := webproto.GitGraphRow{Hash: c.Hash, Parents: append([]string{}, c.Parents...), Subject: webName(c.Subject), Author: webName(c.Author),
				When: c.Committed * 1000, Head: c.IsHead, Unpushed: c.Unpushed, Col: l.Col, Color: l.Color, Up: webGraphEdges(l.Up), Down: webGraphEdges(l.Down)}
			for _, ref := range c.Refs {
				r.Refs = append(r.Refs, webproto.GitRef{Name: webName(ref.Name), Kind: gitRefKinds[ref.Kind]})
			}
			out.Rows = append(out.Rows, r)
		}
		call.reply(out)
		return nil
	}
}

// webGitBlame blames the saved file. A new file has no history yet; a
// file that may hold secrets stays on the laptop, as in buf.open.
func (m *Model) webGitBlame(call *webCall) tea.Cmd {
	a := call.args.(*webproto.GitBlameArgs)
	root, ok := m.webGitRoot(call)
	if !ok || !webGitInWorkspace(call, root, a.Path) {
		return nil
	}
	if ai.IsSecretFileName(a.Path) {
		webFail(call, webErrSecret())
		return nil
	}
	if f, ok := m.webGitFind(root, a.Path); ok && f.Untracked() {
		call.fail(webproto.CodeNotFound, "This file is not committed yet.")
		return nil
	}
	call.later()
	return func() tea.Msg {
		lines, err := git.BlameRange(root, a.Path, 1, webproto.MaxBlameLines+1)
		if err != nil {
			webGitFail(call, err)
			return nil
		}
		out := webproto.GitBlame{Path: a.Path, Commits: []webproto.GitBlameCommit{}, Lines: []int{}}
		if len(lines) > webproto.MaxBlameLines {
			lines, out.Cut = lines[:webproto.MaxBlameLines], true
		}
		index := map[string]int{}
		for _, l := range lines {
			i, seen := index[l.Hash]
			if !seen {
				c := webproto.GitBlameCommit{Uncommitted: true}
				if l.Hash != "uncommitted" {
					c = webproto.GitBlameCommit{Hash: l.Hash, Author: webName(l.Author), Subject: webName(l.Subject), When: l.When * 1000}
				}
				i = len(out.Commits)
				index[l.Hash] = i
				out.Commits = append(out.Commits, c)
			}
			out.Lines = append(out.Lines, i)
		}
		call.reply(out)
		return nil
	}
}

// webGitAmend replaces the last commit after a laptop card, as the
// laptop's Commit (Amend) does. A commit already on the upstream is
// named in the card: the branch would need a force push (never done for
// the web).
func (m *Model) webGitAmend(call *webCall) *webAsk {
	msg := call.args.(*webproto.GitAmendArgs).Message
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	if op := m.webGitBuild().Op; op == "merge" {
		call.fail(webproto.CodeConflict, "A merge is in progress on the laptop. Finish it there.")
		return nil
	}
	if len(m.gitCommits) == 0 {
		call.fail(webproto.CodeConflict, "There is no commit to amend yet.")
		return nil
	}
	head := m.gitCommits[0]
	staged, _ := m.gitFileCounts()
	if staged == 0 && msg == "" {
		call.fail(webproto.CodeConflict, "Nothing to change: stage files or type a new message.")
		return nil
	}
	body := "Replace the last commit " + head.Hash + " \"" + webName(head.Subject) + "\" with a new one"
	switch staged {
	case 0:
		body += " (no staged changes)"
	case 1:
		body += " with the 1 staged file"
	default:
		body += " with the " + strconv.Itoa(staged) + " staged files"
	}
	if msg == "" {
		body += "? It keeps its message."
	} else {
		subject, _, _ := strings.Cut(msg, "\n")
		body += "? New message: \"" + webName(subject) + "\"."
	}
	if m.webHeadPushed(root) {
		body += " This commit is on the remote already: you would need to force-push. termocode never force-pushes for you."
	}
	return &webAsk{summary: head.Hash, title: "Amend the last commit?", body: body, action: "Amend",
		run: webGitLaptopRun(func(m *Model) tea.Cmd {
			return m.gitRemoteOp("Amending…", func(cwd string) gitRemoteDoneMsg {
				return gitRemoteDoneMsg{failTitle: "Amend failed", okText: "Amended the last commit", err: git.CommitWith(cwd, msg, true, false), reload: true}
			})
		})}
}

// webHeadPushed says whether HEAD's commit is on the branch's upstream
// already: HEAD is not marked unpushed and an upstream exists (one quick
// git rev-parse; the op is rate-limited to one call in 5 s).
func (m *Model) webHeadPushed(root string) bool {
	if len(m.gitCommits) == 0 || m.gitCommits[0].Unpushed || pushBranch(m.gitBranch) == "" {
		return false
	}
	return git.HasUpstream(root)
}

// webGitClone clones after a laptop card, as the laptop's Git: Clone
// Repository… does: into the laptop's default folder (next to this
// repository, or inside a folder that is not one), with its progress
// toast; a sign-in it needs is asked on the laptop. When it is done the
// laptop opens the new folder (the page reconnects).
func (m *Model) webGitClone(call *webCall) *webAsk {
	url := call.args.(*webproto.GitCloneArgs).URL
	root, err := m.rootDir()
	if err != nil {
		call.fail(webproto.CodeConflict, "No workspace is open on the laptop.")
		return nil
	}
	if m.clone.running {
		call.fail(webproto.CodeBusy, "A clone is running on the laptop.")
		return nil
	}
	name := git.RepoNameFromURL(url)
	dest := git.DefaultCloneDest(root, m.gitIsRepo, name)
	if err := git.CheckCloneDest(dest); err != nil {
		call.fail(webproto.CodeConflict, "Can't clone there: "+webName(err.Error()))
		return nil
	}
	return &webAsk{summary: url, title: "Clone repository?",
		body:   "Clone " + url + " into " + dest + "? Git on the laptop signs in, never the web. When it is done, termocode opens that folder.",
		action: "Clone",
		run: func(m *Model) tea.Cmd {
			if m.clone.running {
				return m.openCloneURLPrompt() // says a clone is running
			}
			m.clone.url, m.clone.dest = url, dest
			return m.startClone("", "")
		}}
}
