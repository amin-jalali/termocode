package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentctl"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Agent notes and status checkpoints (WTX-12) ──────────────────────────
//
// `termocode worktree note|status|list|link` (cmd/termocode) and the AI
// tools worktrees_list / worktree_note let an agent say what it does in
// the worktree it works in. The worktree is the one the caller's folder
// is in (the control request's Dir, else its terminal tab's folder).
// note and status only write short text to worktrees.json, so they need
// no card; a note is one clean, redacted line (worktrees.CleanNote),
// drawn as text only. link asks the user first (Deny first and focused).
// The card's line 3 shows the last note, line 4 the status; N lists the
// notes with their times.

const (
	confirmKindWTCtlLink confirmKindEnum = 3603
	wtNotesPaletteID                     = "worktree-notes"
)

// wtRepoAt reads the worktrees of the repository dir is in.
func wtRepoAt(dir string) (entries []worktrees.Entry, common string, err error) {
	list, common, err := git.ListWorktrees(dir)
	if err != nil {
		return nil, "", fmt.Errorf("%s is not in a git repository", dir)
	}
	metas, err := worktrees.Load(common)
	if err != nil {
		return nil, "", err
	}
	return worktrees.Build(list, metas), common, nil
}

// wtRealDir is dir with its symlinks resolved (git lists real paths).
// A folder that is not there (yet) is read as its nearest parent.
func wtRealDir(dir string) string {
	d := filepath.Clean(dir)
	for {
		if real, err := filepath.EvalSymlinks(d); err == nil {
			if d == filepath.Clean(dir) {
				return real
			}
			rel, _ := filepath.Rel(d, filepath.Clean(dir))
			return filepath.Join(real, rel)
		}
		up := filepath.Dir(d)
		if up == d {
			return filepath.Clean(dir)
		}
		d = up
	}
}

// wtExisting is dir or its nearest parent that is there (git runs in it).
func wtExisting(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
		if filepath.Dir(d) == d {
			return dir
		}
	}
}

// wtFind is the worktree arg names: a path in (or of) one, its branch or
// its name.
func wtFind(entries []worktrees.Entry, arg string) (worktrees.Entry, bool) {
	if p := wtOwner(entries, wtRealDir(arg)); p != "" && filepath.IsAbs(arg) {
		arg = p
	}
	for _, e := range entries {
		if !e.Gone && (e.Path == arg || e.Branch == arg || (e.Meta.Name != "" && e.Meta.Name == arg)) {
			return e, true
		}
	}
	return worktrees.Entry{}, false
}

// wtAddNote checks and stores a note and / or a status on the worktree
// at path.
func wtAddNote(common, path, text, status string) error {
	clean := ""
	if text != "" || status == "" {
		var err error
		if clean, err = worktrees.CleanNote(text, ai.Redact); err != nil {
			return err
		}
	}
	if status != "" && !worktrees.ValidCheckpoint(status) {
		return fmt.Errorf("status must be one of %s", strings.Join(worktrees.Checkpoints, ", "))
	}
	now := clock.Now()
	return worktrees.Update(common, func(mm map[string]worktrees.Meta) {
		md := mm[path]
		if clean != "" {
			md.AddNote(clean, now)
		}
		if status != "" {
			_ = md.SetCheckpoint(status, now)
		}
		md.LastSeen = now
		mm[path] = md
	})
}

// wtCtlList is the reply of worktree-list.
func wtCtlList(entries []worktrees.Entry, dir string) []agentctl.Worktree {
	here := wtOwner(entries, dir)
	out := make([]agentctl.Worktree, 0, len(entries))
	for _, e := range entries {
		w := agentctl.Worktree{Path: e.Path, Branch: e.Branch, Name: e.Meta.Name, Kind: e.Kind.String(), Gone: e.Gone,
			Parent: e.Meta.Parent, Link: wtLinkLine(e.Meta), Checkpoint: e.Meta.Checkpoint, Here: e.Path == here, Agent: e.Meta.Agent}
		if n, ok := e.Meta.LastNote(); ok {
			w.Note = n.Text
		}
		out = append(out, w)
	}
	return out
}

// wtCtlDoneMsg: a control request changed worktrees.json.
type wtCtlDoneMsg struct{}

// wtCtlLinkAskMsg: a CLI link waits for the user's answer.
type wtCtlLinkAskMsg struct {
	call         agentCtlCall
	path, title  string
	common, root string
	ref          ghci.LinkRef
}

// wtCtlPending is the open CLI link card.
type wtCtlPending struct{ ask wtCtlLinkAskMsg }

// onWTCtl answers a worktree op of the control socket off the UI thread.
func (m *Model) onWTCtl(call agentCtlCall) tea.Cmd {
	r := call.req
	dir := r.Dir
	if dir == "" {
		if t, ok := m.agentCtlFind(r.Tab); ok {
			dir = t.t.Cwd
			if t.t.Worktree != "" {
				dir = t.t.Worktree
			}
		}
	}
	if dir == "" {
		call.reply <- agentctl.Reply{Error: "no folder: run it in a worktree"}
		return nil
	}
	return func() tea.Msg {
		fail := func(err error) tea.Msg { call.reply <- agentctl.Reply{Error: err.Error()}; return nil }
		dir := wtRealDir(dir)
		entries, common, err := wtRepoAt(wtExisting(dir))
		if err != nil {
			return fail(err)
		}
		if r.Op == agentctl.OpWorktreeList {
			call.reply <- agentctl.Reply{OK: true, Worktrees: wtCtlList(entries, dir)}
			return nil
		}
		path := wtOwner(entries, dir)
		if path == "" {
			return fail(fmt.Errorf("%s is not in a worktree of this repository", dir))
		}
		switch r.Op {
		case agentctl.OpWorktreeNote:
			if err := wtAddNote(common, path, r.Text, ""); err != nil {
				return fail(err)
			}
		case agentctl.OpWorktreeStatus:
			if !worktrees.ValidCheckpoint(r.State) {
				return fail(fmt.Errorf("status must be one of %s", strings.Join(worktrees.Checkpoints, ", ")))
			}
			if err := wtAddNote(common, path, "", r.State); err != nil {
				return fail(err)
			}
		case agentctl.OpWorktreeLink:
			ref, err := ghci.ParseLinkRef(r.Text, wtLinkHosts(dir))
			if err != nil {
				return fail(err)
			}
			title := filepath.Base(path)
			for _, e := range entries {
				if e.Path == path {
					title = e.Title()
				}
			}
			return wtCtlLinkAskMsg{call: call, path: path, title: title, common: common, root: dir, ref: ref}
		}
		call.reply <- agentctl.Reply{OK: true}
		return wtCtlDoneMsg{}
	}
}

// askWTCtlLink shows the CLI link card (Deny first).
func (m *Model) askWTCtlLink(ask wtCtlLinkAskMsg) tea.Cmd {
	if m.confirmOpen || m.wt.ctlLink != nil {
		ask.call.reply <- agentctl.Reply{Error: "termocode is asking something else: try again"}
		return nil
	}
	m.wt.ctlLink = &wtCtlPending{ask: ask}
	what := "#" + strconv.Itoa(ask.ref.Number)
	if ask.ref.Repo != "" {
		what += " of " + ask.ref.Repo
	}
	body := "A program on this computer (termocode worktree link) wants to link " + what + " to the worktree " + ask.title +
		" (" + wtShortPath(ask.path) + ").\n\ntermocode then reads its title, state and CI with gh."
	m.confirm = confirm.Risky("Link an issue or PR to a worktree?", body,
		confirm.Button{ID: "deny", Title: "Deny"},
		confirm.Button{ID: "link", Title: "Link", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWTCtlLink
	return nil
}

// answerWTCtlLink answers the card ("" = Esc = Deny).
func (m *Model) answerWTCtlLink(id string) tea.Cmd {
	p := m.wt.ctlLink
	m.wt.ctlLink = nil
	if p == nil {
		return nil
	}
	ask := p.ask
	if id != "link" {
		ask.call.reply <- agentctl.Reply{Error: "the user did not allow the link"}
		return nil
	}
	run := wtGHRun()
	return func() tea.Msg {
		l, err := ghci.ViewLink(context.Background(), run, ask.root, ask.ref)
		if err != nil && !ghci.Quiet(err) {
			ask.call.reply <- agentctl.Reply{Error: "could not read #" + strconv.Itoa(ask.ref.Number) + ": " + err.Error()}
			return nil
		}
		uerr := worktrees.Update(ask.common, func(mm map[string]worktrees.Meta) {
			md := mm[ask.path]
			if err == nil {
				wtLinkMeta(&md, l, clock.Now())
			} else {
				md.Issue, md.PR, md.Link = 0, 0, worktrees.LinkInfo{}
				if ask.ref.Kind == ghci.LinkPR {
					md.PR = ask.ref.Number
				} else {
					md.Issue = ask.ref.Number
				}
			}
			mm[ask.path] = md
		})
		if uerr != nil {
			ask.call.reply <- agentctl.Reply{Error: uerr.Error()}
			return nil
		}
		ask.call.reply <- agentctl.Reply{OK: true}
		return wtCtlDoneMsg{}
	}
}

// handleWTCtlMsg routes the control messages of the view.
func (m *Model) handleWTCtlMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case wtCtlDoneMsg:
		if m.wt != nil && m.wt.visible {
			return m.worktreesReload(), true
		}
		return nil, true
	case wtCtlLinkAskMsg:
		return m.askWTCtlLink(msg), true
	}
	return nil, false
}

// worktreeNotes lists the cursor's notes, newest first (N).
func (m *Model) worktreeNotes() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	notes := it.e.Meta.Notes
	if len(notes) == 0 && it.e.Meta.Checkpoint == "" {
		return m.pushToastDetail(toast.Info, "No notes on "+wtTitle(it), "Agents leave them with termocode worktree note or the worktree_note tool.")
	}
	now := clock.Now()
	var rows [][]string
	if c := it.e.Meta.Checkpoint; c != "" {
		rows = append(rows, []string{"Status: " + c + " (" + formatRecentTime(it.e.Meta.CheckpointAt, now) + ")"})
	}
	rows = append(rows, []string{"Notes, the latest on top"})
	for i := len(notes) - 1; i >= 0; i-- {
		at := "-"
		if !notes[i].At.IsZero() {
			at = formatRecentTime(notes[i].At, now)
		}
		rows = append(rows, []string{at, aiCleanText(notes[i].Text)})
	}
	return m.openKeyCard("Notes · "+wtTitle(it), keyRows(rows...), helpLocWorktrees(), nil)
}

// ── AI tools ─────────────────────────────────────────────────────────────

// aiWorktreeTools are worktrees_list and worktree_note for the chat and
// outside agents (both read or write short text only: no card).
func aiWorktreeTools(root string) []ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	return []ai.Tool{
		{
			Name: "worktrees_list",
			Description: "List the git worktrees of this repository: path, branch, kind (main, user, agent, external), " +
				"parent, the linked issue or PR with its state and CI, the agent status (working, blocked, done) and the last note.",
			Params:   map[string]any{},
			Describe: func(map[string]any) string { return "List worktrees" },
			Run: func(context.Context, map[string]any) (string, error) {
				entries, _, err := wtRepoAt(root)
				if err != nil {
					return "", err
				}
				var b strings.Builder
				for _, w := range wtCtlList(entries, wtRealDir(root)) {
					fmt.Fprintf(&b, "- %s (%s", w.Path, w.Kind)
					for _, kv := range [][2]string{{"branch", w.Branch}, {"name", w.Name}, {"parent", w.Parent}, {"link", w.Link}, {"status", w.Checkpoint}} {
						if kv[1] != "" {
							fmt.Fprintf(&b, ", %s %s", kv[0], kv[1])
						}
					}
					if w.Gone {
						b.WriteString(", folder gone")
					}
					if w.Here {
						b.WriteString(", this workspace")
					}
					b.WriteString(")")
					if w.Note != "" {
						fmt.Fprintf(&b, " last note: %q", w.Note)
					}
					b.WriteString("\n")
				}
				return b.String(), nil
			},
		},
		{
			Name: "worktree_note",
			Description: "Leave a short note (one line, at most 500 characters) and / or a status on a worktree, so the user " +
				"sees what you do there in the WORKTREES view. Text only; it is shown as plain text.",
			Params: map[string]any{
				"worktree": str("optional: a path or branch from worktrees_list; the workspace's worktree when left out"),
				"text":     str("the note"),
				"status":   map[string]any{"type": "string", "enum": worktrees.Checkpoints, "description": "optional: your status in it"},
			},
			Describe: func(a map[string]any) string { return "Note on a worktree" },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				entries, common, err := wtRepoAt(root)
				if err != nil {
					return "", err
				}
				arg := strings.TrimSpace(ai.ArgString(a, "worktree"))
				if arg == "" {
					arg = wtRealDir(root)
				}
				e, ok := wtFind(entries, arg)
				if !ok {
					return "", fmt.Errorf("worktree %q is not a worktree of this repository (see worktrees_list)", arg)
				}
				text, status := ai.ArgString(a, "text"), strings.TrimSpace(ai.ArgString(a, "status"))
				if strings.TrimSpace(text) == "" && status == "" {
					return "", errors.New("pass text, status or both")
				}
				if err := wtAddNote(common, e.Path, text, status); err != nil {
					return "", err
				}
				return "Noted on " + e.Title() + ".", nil
			},
		},
	}
}

func init() {
	aiActionPolicies[wtNotesPaletteID] = aiAuto() // shows the notes card; read only
}
