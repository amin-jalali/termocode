package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Issue / PR links of a worktree (WTX-9) ───────────────────────────────
//
// i in WORKTREES (Worktree: Link Issue or PR…) opens a small card: a
// number, #N or the GitHub URL of an issue or pull request. Cancel is
// first; Link checks the text (ghci.ParseLinkRef: https on the repo's
// host only) and reads it with gh off the UI thread; the card line then
// says "PR #12 open · CI ✓". Without gh (or not signed in) the number is
// kept and a toast says why there is no title. Links are read again
// while the view shows, on the CI cadence (ghci.FastPoll while checks
// run, else ghci.SlowPoll); merged and closed ones are left alone. The
// AI reads links (worktrees_list) but never sets one.

const (
	wtLinkTitle     = "Link an issue or PR"
	wtLinkPaletteID = "worktree-link"
	wtLinkMaxReads  = 4 // links read again per load
)

// wtGHRun is the gh runner of the links (a var: tests).
var wtGHRun = func() ghci.Runner {
	if headless() {
		return ciNoGH
	}
	return ghci.DefaultRunner(ciGHTimeout)
}

// wtLinkForm is the open link card.
type wtLinkForm struct {
	path, title string
	linked      bool
	busy        bool
	fail        string
	gen         int
}

// wtLinkHosts are the hosts a link URL may name: github.com and the
// repository's remotes.
func wtLinkHosts(root string) []string {
	hosts := []string{"github.com"}
	for _, h := range git.RemoteHosts(root) {
		if h != "github.com" {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

// worktreeLink opens the link card for the cursor's worktree.
func (m *Model) worktreeLink() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	if m.connectOpen {
		return nil
	}
	m.wt.link = &wtLinkForm{path: it.e.Path, title: wtTitle(it), linked: it.e.Meta.Issue > 0 || it.e.Meta.PR > 0}
	m.connect = connectcard.New(m.wtLinkCard(""))
	m.connect.SetSize(m.w, m.h)
	m.connectOpen = true
	return nil
}

func (m *Model) wtLinkCard(value string) connectcard.Card {
	f := m.wt.link
	rows := []connectcard.Row{
		{Kind: connectcard.Text, Text: "Worktree  " + f.title},
		{Kind: connectcard.Blank},
		{Kind: connectcard.Input, ID: "ref", Text: "Link", Inline: true, Placeholder: "412, #412 or its GitHub URL", Value: value},
		{Kind: connectcard.Dim, Text: "The card shows its state and CI; gh reads it with your own sign-in."},
	}
	switch {
	case f.busy:
		rows = append(rows, connectcard.Row{Kind: connectcard.Status, Text: theme.GlyphWait.String() + " Reading it with gh…"})
	case f.fail != "":
		rows = append(rows, connectcard.Row{Kind: connectcard.Fail, Text: f.fail})
	}
	buttons := []connectcard.Button{{ID: "cancel", Title: "Cancel"}}
	if f.linked {
		buttons = append(buttons, connectcard.Button{ID: "unlink", Title: "Unlink"})
	}
	buttons = append(buttons, connectcard.Button{ID: "link", Title: "Link"})
	return connectcard.Card{Title: wtLinkTitle, Rows: rows, Form: true, Buttons: buttons}
}

func (m Model) wtLinkOpen() bool {
	return m.wt != nil && m.wt.link != nil && m.connectOpen && m.connect.Card().Title == wtLinkTitle
}

// wtLinkedMsg is the end of a gh read for the card.
type wtLinkedMsg struct {
	gen  int
	path string
	ref  ghci.LinkRef
	link ghci.Link
	err  error
}

// handleWTLinkMsg runs the card's buttons. ok=false: not ours.
func (m *Model) handleWTLinkMsg(msg tea.Msg) (tea.Cmd, bool) {
	if _, ok := msg.(wtLinksReadMsg); ok && m.wt != nil {
		return m.onWTLinksRead(msg.(wtLinksReadMsg)), true
	}
	if m.wt == nil || m.wt.link == nil {
		_, stale := msg.(wtLinkedMsg) // the card was closed meanwhile
		return nil, stale
	}
	f := m.wt.link
	switch msg := msg.(type) {
	case connectcard.ButtonMsg:
		switch msg.ID {
		case "cancel":
			m.closeWTLink()
		case "unlink":
			err := m.wtTouchMeta(f.path, func(md *worktrees.Meta) { md.Issue, md.PR, md.Link = 0, 0, worktrees.LinkInfo{} })
			m.closeWTLink()
			if err != nil {
				return m.pushToastDetail(toast.Warn, "The link could not be saved", err.Error()), true
			}
		case "link":
			return m.wtLinkStart(), true
		}
		return nil, true
	case connectcard.SubmitMsg:
		return m.wtLinkStart(), true
	case connectcard.CloseMsg:
		m.closeWTLink()
		return nil, true
	case connectcard.KeyMsg:
		return nil, true
	case wtLinkedMsg:
		return m.onWTLinked(msg), true
	}
	return nil, false
}

func (m *Model) closeWTLink() {
	if m.wtLinkOpen() {
		m.connectOpen = false
	}
	m.wt.link = nil
}

// wtLinkStart checks the text and reads it with gh in the background.
func (m *Model) wtLinkStart() tea.Cmd {
	f := m.wt.link
	if f.busy {
		return nil
	}
	value := m.connect.InputValue("ref")
	root := m.wt.root
	if root == "" {
		root, _ = m.rootDir()
	}
	ref, err := ghci.ParseLinkRef(value, wtLinkHosts(root))
	if err != nil {
		f.fail = err.Error()
		m.connect.SetCard(m.wtLinkCard(value))
		return nil
	}
	f.busy, f.fail = true, ""
	f.gen++
	gen, path := f.gen, f.path
	m.connect.SetCard(m.wtLinkCard(value))
	run := wtGHRun()
	return func() tea.Msg {
		l, err := ghci.ViewLink(context.Background(), run, root, ref)
		return wtLinkedMsg{gen: gen, path: path, ref: ref, link: l, err: err}
	}
}

// wtLinkMeta puts a read link on the metadata.
func wtLinkMeta(md *worktrees.Meta, l ghci.Link, now time.Time) {
	md.Issue, md.PR = 0, 0
	if l.Kind == ghci.LinkPR {
		md.PR = l.Number
	} else {
		md.Issue = l.Number
	}
	md.Link = worktrees.LinkInfo{Title: l.Title, State: l.State, CI: wtCIWord(l.CI), URL: l.URL, Checked: now}
}

func wtCIWord(s ghci.State) string {
	switch s {
	case ghci.StatePass:
		return "pass"
	case ghci.StateFail:
		return "fail"
	case ghci.StatePending:
		return "pending"
	}
	return ""
}

func (m *Model) onWTLinked(msg wtLinkedMsg) tea.Cmd {
	f := m.wt.link
	if f == nil || msg.gen != f.gen {
		return nil
	}
	f.busy = false
	value := m.connect.InputValue("ref")
	num := "#" + strconv.Itoa(msg.ref.Number)
	switch {
	case msg.err != nil && ghci.Quiet(msg.err):
		err := m.wtTouchMeta(msg.path, func(md *worktrees.Meta) {
			md.Issue, md.PR, md.Link = 0, 0, worktrees.LinkInfo{}
			if msg.ref.Kind == ghci.LinkPR {
				md.PR = msg.ref.Number
			} else {
				md.Issue = msg.ref.Number
			}
		})
		m.closeWTLink()
		if err != nil {
			return m.pushToastDetail(toast.Warn, "The link could not be saved", err.Error())
		}
		return m.pushToastDetail(toast.Info, "Linked "+num, wtGHWhy(msg.err)+", so the card shows the number without its title or state.")
	case msg.err != nil:
		f.fail = "Could not read " + num + ": " + msg.err.Error()
		m.connect.SetCard(m.wtLinkCard(value))
		return nil
	}
	err := m.wtTouchMeta(msg.path, func(md *worktrees.Meta) { wtLinkMeta(md, msg.link, clock.Now()) })
	m.closeWTLink()
	if err != nil {
		return m.pushToastDetail(toast.Warn, "The link could not be saved", err.Error())
	}
	return m.pushToastDetail(toast.Info, "Linked "+wtLinkLine(m.wtMetaOf(msg.path)), msg.link.Title)
}

// wtGHWhy says why gh gave nothing.
func wtGHWhy(err error) string {
	switch {
	case errors.Is(err, ghci.ErrNoGH):
		return "The gh command is not installed"
	case errors.Is(err, ghci.ErrNotLoggedIn):
		return "gh is not signed in (gh auth login)"
	}
	return "This repository has no GitHub remote"
}

func (m *Model) wtMetaOf(path string) worktrees.Meta {
	for _, e := range m.wt.entries {
		if e.Path == path {
			return e.Meta
		}
	}
	return worktrees.Meta{}
}

// wtLinkLine is the link piece of a card: "#412 open", "PR #12 merged ·
// CI ✓" ("" without a link).
func wtLinkLine(md worktrees.Meta) string {
	var s string
	switch {
	case md.PR > 0:
		s = "PR #" + strconv.Itoa(md.PR)
	case md.Issue > 0:
		s = "#" + strconv.Itoa(md.Issue)
	default:
		return ""
	}
	if md.Link.State != "" {
		s += " " + md.Link.State
	}
	if md.PR > 0 {
		switch md.Link.CI {
		case "pass":
			s += " · CI ✓"
		case "fail":
			s += " · CI ✗"
		case "pending":
			s += " · CI ●"
		}
	}
	return theme.ASCIIText(s)
}

// wtLinkDue: the link should be read again now.
func wtLinkDue(md worktrees.Meta, now time.Time) bool {
	if md.Issue <= 0 && md.PR <= 0 {
		return false
	}
	switch md.Link.State {
	case "merged", "closed":
		return false
	}
	if md.Link.Checked.IsZero() {
		return true
	}
	every := ghci.SlowPoll
	if md.PR > 0 && md.Link.CI == "pending" {
		every = ghci.FastPoll
	}
	return now.Sub(md.Link.Checked) >= every
}

// wtLinksReadMsg is the end of a background read of due links.
type wtLinksReadMsg struct {
	reads map[string]ghci.Link
}

// wtLinksRefresh reads the due links again (a few per load) in the
// background; quiet errors leave them as they are.
func (st *wtState) wtLinksRefresh(now time.Time) tea.Cmd {
	if st.off || st.linkReading || st.common == "" {
		return nil
	}
	type due struct {
		path string
		ref  ghci.LinkRef
	}
	var todo []due
	for _, e := range st.entries {
		if e.Gone || !wtLinkDue(e.Meta, now) || len(todo) == wtLinkMaxReads {
			continue
		}
		ref := ghci.LinkRef{Number: e.Meta.Issue, Kind: ghci.LinkIssue}
		if e.Meta.PR > 0 {
			ref = ghci.LinkRef{Number: e.Meta.PR, Kind: ghci.LinkPR}
		}
		todo = append(todo, due{e.Path, ref})
	}
	if len(todo) == 0 {
		return nil
	}
	st.linkReading = true
	run, root := wtGHRun(), st.root
	return func() tea.Msg {
		out := wtLinksReadMsg{reads: map[string]ghci.Link{}}
		for _, d := range todo {
			if l, err := ghci.ViewLink(context.Background(), run, root, d.ref); err == nil {
				out.reads[d.path] = l
			}
		}
		return out
	}
}

func (m *Model) onWTLinksRead(msg wtLinksReadMsg) tea.Cmd {
	st := m.wt
	st.linkReading = false
	now := clock.Now()
	for p, l := range msg.reads {
		_ = m.wtTouchMeta(p, func(md *worktrees.Meta) { wtLinkMeta(md, l, now) })
	}
	return nil
}

// wtLinkRef parses the Issue row of Worktree: New… ("" = none).
func wtLinkRefOf(value, root string) (ghci.LinkRef, bool, error) {
	if strings.TrimSpace(value) == "" {
		return ghci.LinkRef{}, false, nil
	}
	ref, err := ghci.ParseLinkRef(value, wtLinkHosts(root))
	return ref, err == nil, err
}

func init() {
	aiActionPolicies[wtLinkPaletteID] = aiNever("links are the user's choice; the AI reads them with worktrees_list")
}
