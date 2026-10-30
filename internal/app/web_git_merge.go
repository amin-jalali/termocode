package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop page's merge editor (WEBP-5): a conflicted file's incoming
// and current sides and the merged result, as the laptop's merge view
// has them. The page only splices the texts the laptop gives it; saving
// the result goes through the laptop's own checks (git.MarkResolved).

func init() {
	webAskOps[webproto.OpGitResolveSide] = (*Model).webGitResolveSide
	webOps[webproto.OpGitConflict] = (*Model).webGitConflictRead
	webOps[webproto.OpGitResolve] = (*Model).webGitResolve
}

// webGitConflicted is the laptop's entry for a conflicted path inside
// the workspace (not one that may hold secrets).
func (m *Model) webGitConflicted(call *webCall, path string) (string, bool) {
	root, ok := m.webGitRoot(call)
	if !ok || !webGitInWorkspace(call, root, path) {
		return "", false
	}
	if ai.IsSecretFileName(path) {
		webFail(call, webErrSecret())
		return "", false
	}
	if f, ok := m.webGitFind(root, path); !ok || !webGitConflict(f.Code) {
		call.fail(webproto.CodeNotFound, "That file has no merge conflict now.")
		return "", false
	}
	return root, true
}

// webGitResolveSide keeps one side of the whole file after a laptop
// card; the laptop's merge code runs it (a side that deleted the file
// deletes it).
func (m *Model) webGitResolveSide(call *webCall) *webAsk {
	a := call.args.(*webproto.GitResolveSideArgs)
	if _, ok := m.webGitConflicted(call, a.Path); !ok {
		return nil
	}
	side := map[string]string{"ours": "current (ours)", "theirs": "incoming (theirs)"}[a.Side]
	return &webAsk{summary: a.Path + " " + a.Side, title: "Keep one side?",
		body:   "Settle the conflict in " + a.Path + " with the " + side + " file? Your other changes to it are lost. If that side deleted the file, it is deleted.",
		action: "Keep " + a.Side,
		run: func(m *Model) tea.Cmd {
			cwd, err := m.rootDir()
			if err != nil {
				return nil
			}
			path, side := a.Path, a.Side
			return func() tea.Msg {
				return mergeWholeDoneMsg{rel: path, verb: "keep-" + side, err: git.ResolveWithSide(cwd, path, side)}
			}
		}}
}

// webSHA256 names a file's bytes (git.resolve's precondition).
func webSHA256(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// webConflictSide is one index stage for the page, with "\n" line ends.
func webConflictSide(s git.ConflictSide) webproto.GitConflictSide {
	out := webproto.GitConflictSide{Present: s.Present, Binary: s.Binary}
	if s.Present && !s.Binary {
		out.Text = strings.ReplaceAll(s.Text, "\r\n", "\n")
	}
	return out
}

// webConflictParts are text's conflicts: each one's exact text and the
// text each choice puts in its place (the laptop's Replacement).
func webConflictParts(text string) []webproto.GitConflictPart {
	lines := git.SplitLines(text)
	out := []webproto.GitConflictPart{}
	for _, b := range git.ParseConflictLines(lines) {
		eol := "\n"
		if b.End == len(lines)-1 && !strings.HasSuffix(text, "\n") {
			eol = ""
		}
		join := func(ls []string) string {
			if len(ls) == 0 {
				return ""
			}
			return strings.Join(ls, "\n") + eol
		}
		out = append(out, webproto.GitConflictPart{Line: b.Start + 1, Raw: join(lines[b.Start : b.End+1]),
			OursLabel: webName(b.OursLabel), TheirsLabel: webName(b.TheirsLabel),
			Ours: join(b.Replacement(git.AcceptOurs)), Theirs: join(b.Replacement(git.AcceptTheirs)), Both: join(b.Replacement(git.AcceptBoth))})
	}
	return out
}

// webGitConflictRead answers the conflicted file and its sides.
func (m *Model) webGitConflictRead(call *webCall) tea.Cmd {
	a := call.args.(*webproto.GitConflictArgs)
	root, ok := m.webGitConflicted(call, a.Path)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		full, err := (ai.Workspace{Root: root}).Resolve(a.Path)
		var raw []byte
		if err == nil {
			raw, err = os.ReadFile(full)
		}
		if err != nil {
			webFail(call, err)
			return nil
		}
		st, err := git.ReadConflictStages(root, a.Path)
		if err != nil {
			webGitFail(call, err)
			return nil
		}
		out := webproto.GitConflict{Path: a.Path, Version: webSHA256(raw), Ours: webConflictSide(st.Ours), Theirs: webConflictSide(st.Theirs), Base: webConflictSide(st.Base),
			Blocks: []webproto.GitConflictPart{}}
		if len(raw) <= webproto.MaxResolveText && bytes.IndexByte(raw, 0) < 0 {
			out.Text = strings.ReplaceAll(string(raw), "\r\n", "\n")
			out.Blocks = webConflictParts(out.Text)
		} else {
			out.Ours.Binary, out.Theirs.Binary = true, true // too big or binary: one side or the other
			out.Ours.Text, out.Theirs.Text, out.Base.Text = "", "", ""
		}
		call.reply(out)
		return nil
	}
}

// errWebFileChanged: the saved file is not the version the page read.
var errWebFileChanged = errors.New("file changed")

// webGitResolve saves the merged text and marks the file resolved, as
// the laptop's merge view does (git.MarkResolved refuses markers).
func (m *Model) webGitResolve(call *webCall) tea.Cmd {
	a := call.args.(*webproto.GitResolveArgs)
	root, ok := m.webGitConflicted(call, a.Path)
	if !ok {
		return nil
	}
	full, err := (ai.Workspace{Root: root}).Resolve(a.Path)
	if err != nil {
		call.fail(webproto.CodeBadRequest, "That path is outside the workspace.")
		return nil
	}
	for _, b := range m.bufs {
		if b.Modified && (b.Path == full || b.Path == filepath.Join(root, a.Path)) {
			call.fail(webproto.CodeConflict, "The file has unsaved changes on the laptop. Save it there first.")
			return nil
		}
	}
	if git.HasConflicts(a.Text) {
		call.fail(webproto.CodeConflict, "Conflict markers are still in the result.")
		return nil
	}
	call.later()
	return func() tea.Msg {
		err := webWriteMerged(full, a.Text, a.Version)
		if err == nil {
			err = git.MarkResolved(root, a.Path)
		}
		return applyMsg(func(m *Model) tea.Cmd {
			switch {
			case errors.Is(err, errWebFileChanged):
				m.webAuditOp(call, a.Path, "changed")
				call.fail(webproto.CodeConflict, "The file changed on the laptop. Open it again.")
				return nil
			case errors.Is(err, git.ErrMarkersLeft):
				m.webAuditOp(call, a.Path, "markers")
				call.fail(webproto.CodeConflict, "Conflict markers are still in the saved file.")
				return nil
			case err != nil:
				m.webAuditOp(call, a.Path, "failed")
				webGitFail(call, err)
				return nil
			}
			m.webAuditOp(call, a.Path, "ok")
			left := 0
			for _, f := range m.gitFiles {
				if f.Conflicted() && f.Path != a.Path {
					left++
				}
			}
			call.reply(webproto.GitResolved{Path: a.Path, Left: left})
			if m.mergeViewShown() && m.merge.rel == a.Path {
				m.closeMergeView()
			}
			var cmd tea.Cmd
			m.toast, cmd = m.toast.PushDetail(toast.Info, "Marked as resolved: "+a.Path, m.mergeNextStep(a.Path))
			return tea.Batch(cmd, m.pollGitCmd())
		})
	}
}

// webWriteMerged writes text over full if full is still version; a file
// with CRLF line ends keeps them.
func webWriteMerged(full, text, version string) error {
	old, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	if webSHA256(old) != version {
		return errWebFileChanged
	}
	if bytes.Contains(old, []byte("\r\n")) {
		text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(full); err == nil {
		mode = fi.Mode().Perm()
	}
	return os.WriteFile(full, []byte(text), mode)
}
