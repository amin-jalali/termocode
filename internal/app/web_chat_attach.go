package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Items the page sends back with chat.send (MOB-5, plan §3.3). The page
// sends references only (webproto.Attach); the laptop resolves each one
// here under the same rules as every op: a workspace path through
// webResolve, a secret-like file is refused ("stays on the laptop"),
// every text is redacted and cut (MaxAttachBytes each, MaxAttachTotal
// for the message). The resolved texts go to the model as the message's
// attachments (hidden context, as the laptop's own attachments); the
// message shows one chip per item (ChatMsg.Attached).
//
// Cheap lookups (topic items, path checks) run on Update; reading files,
// git and a task's terminal run in a tea.Cmd, whose webChatAttachedMsg
// sends the message on Update.

// webAttachJob is one attach on its way: what the Cmd still reads.
type webAttachJob struct {
	kind, label string
	text        string // resolved on Update already ("" when read later)
	abs         string // file, lines, ptask: the file to read
	from, to    int    // lines
	id          string // ptask: the line key
	diff        bool   // diff: Path ("" is the whole tree)
	rel         string
	root        string
	untracked   bool
	tree        []git.FileStatus       // diff of the whole tree: the changed files
	tailBuf     int                    // task: its terminal (0: none)
	read        func() (string, error) // issue, pr: read off the UI thread (MOB-55)
}

// webChatAttachedMsg carries the resolved attaches back to Update.
type webChatAttachedMsg struct {
	call *webCall
	chat string // the chat the send was for
	text string
	got  []aiAttachment
}

// webAttachSecret is the refusal of a secret-like file.
func webAttachSecret(label string) error {
	return webErr(webproto.CodeForbidden, label+" may hold secrets: it stays on the laptop.")
}

// webChatSendAttached resolves a.Attach and sends the message once they
// are read. Any attach that cannot be resolved fails the whole send:
// nothing is sent half.
func (m *Model) webChatSendAttached(call *webCall, a *webproto.ChatSendArgs) tea.Cmd {
	if err := webAttachWSCheck(a.Attach, webOwnWS()); err != nil { // MOB-55: only this project's items
		webFailErr(call, err, "That item cannot be sent.")
		return nil
	}
	jobs := make([]webAttachJob, 0, len(a.Attach))
	for i := range a.Attach {
		j, err := m.webAttachPlan(&a.Attach[i])
		if err != nil {
			webFailErr(call, err, "That item cannot be sent.")
			return nil
		}
		jobs = append(jobs, j)
	}
	chat, text, c := m.aiS().conv.ID, a.Text, m.nvim
	call.later()
	return func() tea.Msg {
		got, err := webAttachRead(jobs, c)
		if err != nil {
			webFailErr(call, err, "That item cannot be read.")
			return nil
		}
		return webChatAttachedMsg{call: call, chat: chat, text: text, got: got}
	}
}

// onWebChatAttached sends the message with its resolved attaches, unless
// the laptop moved on meanwhile (the AI started, another chat opened).
func (m *Model) onWebChatAttached(msg webChatAttachedMsg) tea.Cmd {
	call := msg.call
	if call.ctx.Err() != nil {
		return nil // the page is gone: nothing is sent for it
	}
	s := m.aiS()
	switch {
	case s.streaming:
		call.fail(webproto.CodeBusy, "The AI is still answering. Stop it or wait.")
		return nil
	case s.conv.ID != msg.chat:
		call.fail(webproto.CodeConflict, "The laptop opened another chat. Send again.")
		return nil
	}
	s.attachments = append(s.attachments, msg.got...)
	m.webAuditOp(call, fmt.Sprintf("%d attached", len(msg.got)), "ok")
	return m.webChatSendNow(call, msg.text)
}

// webAttachLabel is the chip's text: the page's label, else one made
// from the reference.
func webAttachLabel(a *webproto.Attach) string {
	if l := strings.TrimSpace(a.Label); l != "" {
		return webAttachCut(webName(l), webproto.MaxAttachLabel)
	}
	var l string
	switch a.Kind {
	case webproto.AttachFile:
		l = a.Path
	case webproto.AttachLines:
		l = fmt.Sprintf("%s:%d-%d", a.Path, a.From, a.To)
	case webproto.AttachDiff:
		l = "changes"
		if a.Path != "" {
			l = "changes of " + a.Path
		}
	case webproto.AttachIssue:
		l = "issue #" + strings.TrimPrefix(a.ID, "#")
	case webproto.AttachPR:
		l = "pull request " + a.ID
	case webproto.AttachView:
		l = "view"
		if a.ID != "" {
			l = "view " + a.ID
		}
	default:
		l = a.Kind + " " + a.ID
	}
	return webAttachCut(webName(l), webproto.MaxAttachLabel)
}

// webAttachCut cuts s at max bytes on a rune boundary.
func webAttachCut(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

// webAttachPlan checks one attach on Update and resolves what is cheap.
func (m *Model) webAttachPlan(a *webproto.Attach) (webAttachJob, error) {
	j := webAttachJob{kind: a.Kind, label: webAttachLabel(a)}
	gone := func(what string) error { return webErr(webproto.CodeNotFound, what+" is gone.") }
	switch a.Kind {
	case webproto.AttachFile, webproto.AttachLines, webproto.AttachPTask:
		abs, err := m.webAttachFile(a.Path, j.label)
		if err != nil {
			return j, err
		}
		j.abs, j.from, j.to, j.id = abs, a.From, a.To, a.ID
	case webproto.AttachDiff:
		return m.webAttachDiffPlan(a, j)
	case webproto.AttachTask:
		snap := m.webTasksBuild()
		for _, t := range snap.Tasks {
			if t.Label == a.ID {
				j.text = webAttachJSON(t)
				if t.Last != nil {
					j.tailBuf = t.Last.Buf
				}
				return j, nil
			}
		}
		return j, gone("That task")
	case webproto.AttachRun:
		for _, r := range m.webRunsBuild(clock.Now()).Runs {
			if r.Key == a.ID {
				j.text = webAttachJSON(r)
				return j, nil
			}
		}
		return j, gone("That run")
	case webproto.AttachWorktree:
		for _, w := range m.webWorktreesBuild().Worktrees {
			if w.ID == a.ID {
				j.text = webAttachJSON(w)
				return j, nil
			}
		}
		return j, gone("That worktree")
	case webproto.AttachWorkflow:
		snap := m.webWorkflowsBuild()
		for _, w := range snap.Workflows {
			if w.Name != a.ID {
				continue
			}
			out := struct {
				Workflow webproto.WorkflowItem  `json:"workflow"`
				Runs     []webproto.WorkflowRun `json:"runs,omitempty"`
			}{Workflow: w}
			for _, r := range snap.Runs {
				if r.Workflow == w.Name && len(out.Runs) < 3 {
					out.Runs = append(out.Runs, r)
				}
			}
			j.text = webAttachJSON(out)
			return j, nil
		}
		return j, gone("That workflow")
	case webproto.AttachCard:
		for _, c := range webCardList(m.webCards(&m.web.cards, false)) {
			if c.ID == a.ID {
				c.Choices = nil // the model never answers a card
				j.text = webAttachJSON(c)
				return j, nil
			}
		}
		return j, gone("That card")
	case webproto.AttachView:
		j.text = "The view the user looks at, as its spec:\n" + ai.RedactSecrets(a.Spec)
	case webproto.AttachIssue, webproto.AttachPR, webproto.AttachProject: // MOB-55 (web_chat_attach_ws.go)
		return m.webAttachProjectPlan(a, j)
	case webproto.AttachResource:
		j.text = "The MCP resource " + ai.RedactSecrets(a.ID) + ". Read it with your MCP tools when you need it."
	case webproto.AttachBlock: // MOB-10: a tool's block in the open chat
		text, ok := m.webChatBlockText(a.ID)
		if !ok {
			return j, gone("That block")
		}
		j.text = text
	case webproto.AttachRace: // AttachProject: MOB-55 above; AttachBlock: MOB-10
		j.text = "The user points at " + a.Kind + " " + a.ID + " (" + j.label + ")."
	default:
		return j, webErr(webproto.CodeBadRequest, "That item cannot be sent.")
	}
	return j, nil
}

// webAttachFile resolves a workspace file the page names: inside the
// workspace, local (not a container's), not secret-like.
func (m *Model) webAttachFile(rel, label string) (string, error) {
	abs, err := m.webResolve(rel)
	if err != nil {
		return "", err
	}
	if ai.IsSecretFileName(abs) || ai.IsSecretFileName(rel) {
		return "", webAttachSecret(label)
	}
	if _, remote := m.remoteFS(); remote {
		return "", webErr(webproto.CodeNotFound, "Files of a container workspace open on the laptop only.")
	}
	return abs, nil
}

// webAttachDiffPlan checks a diff attach: a changed file that is not
// secret-like, or the whole tree without its secret-like files.
func (m *Model) webAttachDiffPlan(a *webproto.Attach, j webAttachJob) (webAttachJob, error) {
	root, err := m.gitRootDir()
	switch {
	case err != nil:
		return j, webErr(webproto.CodeConflict, "No workspace is open on the laptop.")
	case !m.gitLoaded:
		return j, webErr(webproto.CodeBusy, "The laptop is still reading git. Try again.")
	case !m.gitIsRepo:
		return j, webErr(webproto.CodeConflict, "This workspace is not a git repository.")
	}
	j.diff, j.root, j.rel = true, root, a.Path
	if a.Path == "" {
		for _, f := range m.gitFiles {
			if !strings.HasSuffix(f.Path, "/") && !ai.IsSecretFileName(f.Path) {
				j.tree = append(j.tree, f)
			}
		}
		if len(j.tree) == 0 {
			return j, webErr(webproto.CodeNotFound, "There are no changes to send.")
		}
		return j, nil
	}
	if ai.IsSecretFileName(a.Path) {
		return j, webAttachSecret(j.label)
	}
	f, ok := m.webGitFind(root, a.Path)
	if !ok || strings.HasSuffix(a.Path, "/") {
		return j, webErr(webproto.CodeNotFound, "That file has no changes now.")
	}
	j.untracked = f.Untracked()
	return j, nil
}

// webAttachJSON is a topic item as indented JSON for the model.
func webAttachJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return ai.RedactSecrets(string(b))
}

// webAttachFloor is what an attach keeps after the total is used up.
const webAttachFloor = 256

// webAttachRead reads what the jobs still need (off the UI thread) and
// returns the attachments, each cut at MaxAttachBytes and all together at
// MaxAttachTotal.
func webAttachRead(jobs []webAttachJob, c *nvim.Client) ([]aiAttachment, error) {
	out := make([]aiAttachment, 0, len(jobs))
	left := webproto.MaxAttachTotal
	for _, j := range jobs {
		text := j.text
		var err error
		switch {
		case j.diff:
			text, err = webAttachDiff(j)
		case j.read != nil:
			text, err = j.read()
		case j.abs != "":
			text, err = webAttachFileText(j)
		case j.tailBuf > 0:
			text += webAttachTail(c, j.tailBuf)
		}
		if err != nil {
			return nil, err
		}
		text = ai.RedactSecrets(text)
		// Each attach keeps a short start even when the ones before it used
		// the total, so a reference (issue, view) after big files still
		// reaches the model: at most MaxAttach × webAttachFloor over.
		max := min(webproto.MaxAttachBytes, max(left, webAttachFloor))
		if len(text) > max {
			text = webAttachCut(text, max) + "\n… (cut)"
		}
		left -= len(text)
		if left < 0 {
			left = 0
		}
		out = append(out, aiAttachment{Kind: j.kind, Label: j.label, Text: text})
	}
	return out, nil
}

// webAttachFileText reads a file, its lines From..To, or the task line
// of a checklist with what is indented under it.
func webAttachFileText(j webAttachJob) (string, error) {
	f, err := os.Open(j.abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", webErr(webproto.CodeNotFound, j.label+" does not exist.")
		}
		return "", webErr(webproto.CodeForbidden, j.label+" cannot be read.")
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return "", webErr(webproto.CodeBadRequest, j.label+" is not a file.")
	}
	// A whole file is cut at MaxAttachBytes anyway; lines and tasks may
	// sit deeper in a big file.
	limit := int64(webproto.MaxAttachBytes + 1)
	if j.kind != webproto.AttachFile {
		limit = 8 << 20
	}
	b, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return "", webErr(webproto.CodeForbidden, j.label+" cannot be read.")
	}
	if !webAttachIsText(b) {
		return "", webErr(webproto.CodeBadRequest, j.label+" is not a text file.")
	}
	text := string(b)
	switch j.kind {
	case webproto.AttachLines:
		lines := strings.Split(text, "\n")
		if j.from > len(lines) {
			return "", webErr(webproto.CodeNotFound, "Those lines are gone.")
		}
		to := min(j.to, len(lines))
		return strings.Join(lines[j.from-1:to], "\n"), nil
	case webproto.AttachPTask:
		return webAttachTaskLines(text, j.id, j.label)
	}
	return text, nil
}

// webAttachIsText reports whether b is text: no NUL byte, valid UTF-8
// (a read cut inside its last rune still is).
func webAttachIsText(b []byte) bool {
	if bytes.IndexByte(b, 0) >= 0 {
		return false
	}
	for i := 0; i < utf8.UTFMax && len(b) > 0 && !utf8.Valid(b); i++ {
		b = b[:len(b)-1]
	}
	return utf8.Valid(b)
}

// webAttachTaskLines finds the checklist line that holds key and the
// lines indented under it (at most 40).
func webAttachTaskLines(text, key, label string) (string, error) {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if !strings.Contains(l, key) || !strings.Contains(l, "[") {
			continue
		}
		indent := len(l) - len(strings.TrimLeft(l, " \t"))
		out := []string{l}
		for _, next := range lines[i+1:] {
			if len(out) == 40 || strings.TrimSpace(next) == "" || len(next)-len(strings.TrimLeft(next, " \t")) <= indent {
				break
			}
			out = append(out, next)
		}
		return strings.Join(out, "\n"), nil
	}
	return "", webErr(webproto.CodeNotFound, label+" is not in that file now.")
}

// webAttachDiff is the diff of one file, or of the whole tree file by
// file (secret-like files were left out on Update).
func webAttachDiff(j webAttachJob) (string, error) {
	one := func(path string, untracked bool) (string, error) {
		out, _, err := git.DiffFile(j.root, path, false, untracked, webproto.MaxAttachBytes)
		if err == nil && strings.TrimSpace(out) == "" && !untracked {
			out, _, err = git.DiffFile(j.root, path, true, false, webproto.MaxAttachBytes)
		}
		return out, err
	}
	if j.rel != "" {
		out, err := one(j.rel, j.untracked)
		if err != nil {
			return "", webErr(webproto.CodeInternal, "git could not show "+j.label+".")
		}
		return out, nil
	}
	var b strings.Builder
	for _, f := range j.tree {
		if b.Len() >= webproto.MaxAttachBytes {
			b.WriteString("\n… (more files changed)")
			break
		}
		out, err := one(f.Path, f.Untracked())
		if err != nil {
			continue // one file git cannot show does not stop the others
		}
		b.WriteString(out)
	}
	return b.String(), nil
}

// webAttachTail is the end of a task's terminal for a task attach ("" when
// it cannot be read now: the task's line still goes).
func webAttachTail(c *nvim.Client, buf int) string {
	lines, err := webTaskLines(c, buf, webproto.MaxTailLines+webTailSpare)
	if err != nil {
		return ""
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > webproto.MaxTailLines {
		lines = lines[len(lines)-webproto.MaxTailLines:]
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\nThe end of its output:\n" + strings.Join(lines, "\n")
}
