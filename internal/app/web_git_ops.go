package app

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
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
	webOps[webproto.OpGitStage] = (*Model).webGitStage
	webOps[webproto.OpGitUnstage] = (*Model).webGitUnstage
	webOps[webproto.OpGitStageHunk] = (*Model).webGitStageHunk
	webOps[webproto.OpGitUnstageHunk] = (*Model).webGitUnstageHunk
	webOps[webproto.OpGitCommit] = (*Model).webGitCommit
	webAskOps[webproto.OpGitDiscard] = (*Model).webGitDiscard
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
	if !ok || (a.Path != "" && !webGitInWorkspace(call, root, a.Path)) {
		return nil
	}
	var paths []string
	if a.Path != "" {
		paths = []string{a.Path}
	}
	call.later()
	return func() tea.Msg {
		head, out, cut, err := git.ShowCapped(root, a.Hash, webproto.MaxGitDiff, paths...)
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

// webGitWrite runs a git write off the UI thread; the answer, the audit
// line and the laptop's git refresh follow on Update.
func (m *Model) webGitWrite(call *webCall, summary string, write func() error) tea.Cmd {
	call.later()
	return func() tea.Msg {
		err := write()
		return applyMsg(func(m *Model) tea.Cmd {
			switch {
			case errors.Is(err, git.ErrHunkGone):
				m.webAuditOp(call, summary, "moved")
				call.fail(webproto.CodeConflict, "That change moved. Look at the new diff.")
			case err != nil:
				m.webAuditOp(call, summary, "failed")
				webGitFail(call, err)
			default:
				m.webAuditOp(call, summary, "ok")
				call.reply(nil)
			}
			return m.pollGitCmd()
		})
	}
}

// webGitPick checks paths for stage (unstaged: false) or unstage: each
// must be listed, not a conflict, and have a change on the side it
// leaves.
func (m *Model) webGitPick(call *webCall, root string, paths []string, unstage bool) bool {
	for _, p := range paths {
		f, ok := m.webGitFind(root, p)
		switch {
		case !ok:
			call.fail(webproto.CodeNotFound, "That file has no changes now.")
			return false
		case webGitConflict(f.Code):
			call.fail(webproto.CodeConflict, "This file has a conflict. Resolve it on the laptop.")
			return false
		case unstage && !f.Staged():
			call.fail(webproto.CodeConflict, "That file has nothing staged.")
			return false
		case !unstage && !f.Unstaged() && !f.Untracked():
			call.fail(webproto.CodeConflict, "That file is staged already.")
			return false
		}
	}
	return true
}

func (m *Model) webGitStage(call *webCall) tea.Cmd   { return m.webGitStageOp(call, false) }
func (m *Model) webGitUnstage(call *webCall) tea.Cmd { return m.webGitStageOp(call, true) }

func (m *Model) webGitStageOp(call *webCall, unstage bool) tea.Cmd {
	paths := call.args.(*webproto.PathsArgs).Paths
	root, ok := m.webGitRoot(call)
	if !ok || !m.webGitPick(call, root, paths, unstage) {
		return nil
	}
	op := git.Stage
	if unstage {
		op = git.Unstage
	}
	return m.webGitWrite(call, webAuditPaths(paths), func() error {
		for _, p := range paths {
			if err := op(root, p); err != nil {
				return err
			}
		}
		return nil
	})
}

func (m *Model) webGitStageHunk(call *webCall) tea.Cmd   { return m.webGitHunkOp(call, false) }
func (m *Model) webGitUnstageHunk(call *webCall) tea.Cmd { return m.webGitHunkOp(call, true) }

func (m *Model) webGitHunkOp(call *webCall, unstage bool) tea.Cmd {
	a := call.args.(*webproto.GitHunkArgs)
	root, ok := m.webGitRoot(call)
	if !ok || !m.webGitPick(call, root, []string{a.Path}, unstage) {
		return nil
	}
	if f, _ := m.webGitFind(root, a.Path); f.Untracked() {
		call.fail(webproto.CodeConflict, "A new file is staged whole. Use Stage.")
		return nil
	}
	op := git.StageHunkHeader
	if unstage {
		op = git.UnstageHunkHeader
	}
	return m.webGitWrite(call, a.Path+" "+a.Hunk, func() error { return op(root, a.Path, a.Hunk) })
}

// webGitCommit commits what is staged with the page's message. No amend,
// no sign-off, no stage-all: the page commits exactly what the Git tab
// shows as staged. The laptop's undo (git_undo) takes it back.
func (m *Model) webGitCommit(call *webCall) tea.Cmd {
	msg := call.args.(*webproto.GitCommitArgs).Message
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	if op := m.webGitBuild().Op; op != "" {
		call.fail(webproto.CodeConflict, "A "+op+" is in progress on the laptop. Finish it there.")
		return nil
	}
	staged := 0
	for _, f := range m.gitFiles {
		if f.Staged() {
			staged++
		}
	}
	if staged == 0 {
		call.fail(webproto.CodeConflict, "Nothing is staged.")
		return nil
	}
	device := webName(call.conn.device)
	if device == "" {
		device = "web"
	}
	call.later()
	return func() tea.Msg {
		files := 0
		if st, err := git.Status(root); err == nil {
			for _, f := range st {
				if f.Staged() {
					files++
				}
			}
		}
		err := git.Commit(root, msg)
		hash := ""
		if err == nil {
			if cs, _, cerr := git.Commits(root, 0, 1); cerr == nil && len(cs) > 0 {
				hash = cs[0].Hash
			}
		}
		return applyMsg(func(m *Model) tea.Cmd {
			summary := strconv.Itoa(files) + " files"
			if err != nil {
				m.webAuditOp(call, summary, "failed")
				webGitFail(call, err)
				return m.pollGitCmd()
			}
			m.webAuditOp(call, summary, "ok "+hash)
			call.reply(webproto.GitCommitted{Hash: hash, Files: files})
			subject, _, _ := strings.Cut(msg, "\n")
			var t tea.Cmd
			m.toast, t = m.toast.PushDetail(toast.Info, "Committed on "+device, hash+" · "+webName(subject))
			return tea.Batch(m.pollGitCmd(), t)
		})
	}
}

// webGitDiscard asks the laptop to discard the work-tree changes of paths
// (a new file is deleted). Nothing happens until the laptop confirms.
func (m *Model) webGitDiscard(call *webCall) *webAsk {
	paths := call.args.(*webproto.PathsArgs).Paths
	root, ok := m.webGitRoot(call)
	if !ok || !m.webGitPick(call, root, paths, false) {
		return nil
	}
	for _, p := range paths {
		if !webGitInside(root, p) {
			call.fail(webproto.CodeForbidden, "That path leaves the workspace.")
			return nil
		}
	}
	what := paths[0]
	if len(paths) > 1 {
		what = strconv.Itoa(len(paths)) + " files: " + webAuditPaths(paths)
	}
	return &webAsk{
		summary: webAuditPaths(paths),
		title:   "Discard changes?",
		body:    "Discard changes in " + what + "? This cannot be undone.",
		action:  "Discard",
		run: func(m *Model) tea.Cmd {
			return func() tea.Msg {
				var err error
				for _, p := range paths {
					if !webGitInside(root, p) {
						err = errors.New(p + " leaves the workspace")
						break
					}
					if err = git.Discard(root, p); err != nil {
						break
					}
				}
				return applyMsg(func(m *Model) tea.Cmd {
					var t tea.Cmd
					if err != nil {
						m.toast, t = m.toast.PushDetail(toast.Errr, "Discard failed", err.Error())
					} else {
						m.toast, t = m.toast.PushDetail(toast.Info, "Discarded changes", webAuditPaths(paths))
					}
					if m.nvim != nil {
						_ = m.nvim.ExecLua(webLuaCheckTime)
					}
					return tea.Batch(m.pollGitCmd(), t)
				})
			}
		},
	}
}

// webLuaCheckTime reloads buffers whose files a web git op changed on
// disk. Web code sends nvim only constant Lua chunks (never a command
// line), so the reload is a chunk too.
const webLuaCheckTime = `pcall(vim.cmd, 'silent! checktime')`

// webGitInside: path's folder, with every link followed, is inside root
// (a new file is deleted by path; a linked folder must not lead out).
func webGitInside(root, path string) bool {
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(filepath.Join(root, strings.TrimSuffix(path, "/"))))
	if err != nil {
		return false
	}
	return dir == r || strings.HasPrefix(dir, r+string(filepath.Separator))
}
