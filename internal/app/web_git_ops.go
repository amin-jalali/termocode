package app

import (
	"errors"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The git requests (WEB-23, web design 2.6). Every path must be one the
// laptop's git state lists as changed (and inside the workspace): the web
// acts only on what the Git tab shows. git runs off the UI thread; reads
// answer from the background command, writes answer on Update (applyMsg)
// so the laptop's toast and git refresh follow at once.

func init() {
	webOps[webproto.OpGitDiff] = (*Model).webGitDiff
	webOps[webproto.OpGitShow] = (*Model).webGitShow
}

// webGitRoot is the workspace folder when it is a git repository.
func (m *Model) webGitRoot(call *webCall) (string, bool) {
	root, err := m.rootDir()
	switch {
	case err != nil:
		call.fail(webproto.CodeConflict, "No workspace is open on the laptop.")
		return "", false
	case !m.gitLoaded:
		call.fail(webproto.CodeBusy, "The laptop is still reading git. Try again.")
		return "", false
	case !m.gitIsRepo:
		call.fail(webproto.CodeConflict, "This workspace is not a git repository.")
		return "", false
	}
	return root, true
}

// webGitFind returns the laptop's git entry for path, which must also be
// inside the workspace.
func (m *Model) webGitFind(root, path string) (git.FileStatus, bool) {
	if _, err := (ai.Workspace{Root: root}).Resolve(path); err != nil {
		return git.FileStatus{}, false
	}
	for _, f := range m.gitFiles {
		if f.Path == path {
			return f, true
		}
	}
	return git.FileStatus{}, false
}

// webGitFail answers with git's reason, one line.
func webGitFail(call *webCall, err error) {
	line := err.Error()
	var ce *git.CmdError
	if errors.As(err, &ce) {
		line = ce.Reason
	}
	line, _, _ = strings.Cut(strings.TrimSpace(line), "\n")
	call.fail(webproto.CodeInternal, "git said: "+line)
}

// webGitDiff answers one page of a changed file's diff, staged or not.
func (m *Model) webGitDiff(call *webCall) tea.Cmd {
	a := call.args.(*webproto.GitDiffArgs)
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	f, ok := m.webGitFind(root, a.Path)
	if !ok {
		call.fail(webproto.CodeNotFound, "That file has no changes now.")
		return nil
	}
	if strings.HasSuffix(a.Path, "/") {
		call.fail(webproto.CodeBadRequest, "This is a folder of new files. Open it on the laptop.")
		return nil
	}
	untracked := f.Untracked() && !a.Staged
	call.later()
	return func() tea.Msg {
		out, cut, err := git.DiffFile(root, a.Path, a.Staged, untracked, webproto.MaxGitDiff)
		if err != nil {
			webGitFail(call, err)
			return nil
		}
		d := webGitPage(webGitRows(out, false), a.From)
		d.Path, d.Staged, d.Truncated = a.Path, a.Staged, cut
		call.reply(d)
		return nil
	}
}

// webGitShow answers one page of a commit.
func (m *Model) webGitShow(call *webCall) tea.Cmd {
	a := call.args.(*webproto.GitShowArgs)
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		head, out, cut, err := git.ShowCapped(root, a.Hash, webproto.MaxGitDiff)
		if err != nil {
			call.fail(webproto.CodeNotFound, "That commit is not here.")
			return nil
		}
		d := webGitPage(webGitRows(out, true), a.From)
		d.Truncated = cut
		d.Commit = &webproto.GitCommitInfo{Hash: head.Hash, Subject: webName(head.Subject), Body: head.Body,
			Author: webName(head.Author), When: head.When * 1000}
		call.reply(d)
		return nil
	}
}

// webGitParsed is a whole diff as rows, with its counts.
type webGitParsed struct {
	rows           []webproto.DiffRow
	added, removed int
	binary         bool
}

// webGitRows turns git's unified diff into rows: a hunk row per "@@"
// header (its text is the exact header line), then ctx / add / del rows
// with their line numbers. files: a file row starts each file (git show);
// else the file headers are dropped.
func webGitRows(diff string, files bool) webGitParsed {
	var p webGitParsed
	p.rows = []webproto.DiffRow{}
	oldN, newN, inHunk := 0, 0, false
	for _, l := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			inHunk = false
			if files {
				name := l[len("diff --git "):]
				if i := strings.LastIndex(name, " b/"); i >= 0 {
					name = name[i+3:]
				}
				p.rows = append(p.rows, webproto.DiffRow{K: "file", Text: name})
			}
		case strings.HasPrefix(l, "@@ "):
			inHunk = true
			oldN, newN = webHunkStarts(l)
			p.rows = append(p.rows, webproto.DiffRow{K: "hunk", Text: l, Old: oldN, New: newN})
		case !inHunk:
			if strings.HasPrefix(l, "Binary files ") || l == "GIT binary patch" {
				p.binary = true
			}
		case strings.HasPrefix(l, "+"):
			p.rows = append(p.rows, webproto.DiffRow{K: "add", Text: l[1:], New: newN})
			newN++
			p.added++
		case strings.HasPrefix(l, "-"):
			p.rows = append(p.rows, webproto.DiffRow{K: "del", Text: l[1:], Old: oldN})
			oldN++
			p.removed++
		case strings.HasPrefix(l, " "):
			p.rows = append(p.rows, webproto.DiffRow{K: "ctx", Text: l[1:], Old: oldN, New: newN})
			oldN++
			newN++
		case strings.HasPrefix(l, `\`): // "\ No newline at end of file"
			p.rows = append(p.rows, webproto.DiffRow{K: "ctx", Text: l})
		}
	}
	return p
}

// webHunkStarts reads a and c of "@@ -a,b +c,d @@".
func webHunkStarts(h string) (oldStart, newStart int) {
	f := strings.Fields(h)
	num := func(s string) int {
		s, _, _ = strings.Cut(s[1:], ",")
		n, _ := strconv.Atoi(s)
		return n
	}
	if len(f) >= 3 && strings.HasPrefix(f[1], "-") && strings.HasPrefix(f[2], "+") {
		return num(f[1]), num(f[2])
	}
	return 0, 0
}

// webGitPage cuts the page that starts at row from: at most
// MaxGitDiffRows rows and MaxGitDiffPage bytes of text (at least one row).
func webGitPage(p webGitParsed, from int) webproto.GitDiff {
	d := webproto.GitDiff{Rows: []webproto.DiffRow{}, From: from, Added: p.added, Removed: p.removed, Binary: p.binary}
	if from >= len(p.rows) {
		return d
	}
	size, end := 0, from
	for end < len(p.rows) && end-from < webproto.MaxGitDiffRows {
		size += len(p.rows[end].Text)
		if size > webproto.MaxGitDiffPage && end > from {
			break
		}
		end++
	}
	d.Rows = p.rows[from:end]
	if end < len(p.rows) {
		d.Next = end
	}
	return d
}
