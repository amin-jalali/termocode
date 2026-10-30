package app

import (
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
