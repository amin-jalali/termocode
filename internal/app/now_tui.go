package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/now"
	"github.com/amin-jalali/termocode/internal/nowview"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Now and Review in the TUI (Now + Review): AI: Now (What Needs You)…
// opens a full page with the web's list and keys (internal/nowview); e,
// h and m put an item away in the per-user store, u takes the last step
// back, enter opens it (a pull request opens the Review page). The
// Review page approves or comments on the commit it shows, after a 4 s
// undo; the laptop reads the head again first and refuses when new code
// came in. Nothing here acts on a source by itself.

const nowPaletteID = "now-open"

// nowUndoFor is how long an approval waits for u before it is sent.
const nowUndoFor = 4 * time.Second

func init() {
	aiActionPolicies[nowPaletteID] = aiAuto() // a view: nothing changes
}

// nowTUI is the open Now page and, over it, the Review page.
type nowTUI struct {
	page    *nowview.Page
	review  *nowview.ReviewPage
	queue   []int
	qi      int
	pending *nowPending
	gen     int
}

// nowPending is an approval that waits for its undo.
type nowPending struct {
	n    int
	head string
	gen  int
}

// nowTickMsg: the undo wait of approval gen is over.
type nowTickMsg struct{ gen int }

// nowPageOpen reports whether the Now page shows.
func (m Model) nowPageOpen() bool { return m.nowT != nil }

// openNowPage shows the Now page as it is now.
func (m *Model) openNowPage() tea.Cmd {
	m.nowT = &nowTUI{page: nowview.New(m.nowData())}
	m.nowT.page.SetSize(m.w, m.h)
	return nil
}

// nowData is the Now list from the inbox's items.
func (m *Model) nowData() nowview.Data {
	root, _ := m.rootDir()
	home, _ := os.UserHomeDir()
	items, _ := m.nowPlace(m.webInboxItems(webInboxPaths{root: root, home: home}))
	at := webNow()
	rules := map[string]string{}
	for _, r := range m.nowStore().State().Rules {
		rules[r.ID] = r.Name
	}
	byID := map[string]webproto.InboxItem{}
	xs := make([]now.Item, 0, len(items))
	d := nowview.Data{Project: webName(filepath.Base(m.ws.Root)), Branch: m.nowBranch()}
	for _, it := range items {
		byID[it.ID] = it
		xs = append(xs, nowItem(it))
		if it.State == webproto.InboxWorking {
			d.Running++
		}
	}
	row := func(x now.Item) nowview.Row {
		it := byID[x.ID]
		r := nowview.Row{ID: x.ID, Kind: it.Kind, Title: it.Title, Sub: nowSub(it), Why: it.Why, Rule: rules[it.Rule], Fix: hasWord(it.Can, "fix")}
		if when := it.WhyAt; when > 0 {
			r.Age = nowAge(at.Sub(time.UnixMilli(when)))
			r.WhyLong = strings.ToLower(it.Why[:min(1, len(it.Why))]) + it.Why[min(1, len(it.Why)):] + " since " + time.UnixMilli(when).Format("15:04")
		}
		return r
	}
	v := now.Split(xs, at)
	for _, x := range v.Turn {
		d.Turn = append(d.Turn, row(x))
	}
	for _, x := range v.Overdue {
		d.Overdue = append(d.Overdue, row(x))
	}
	for _, x := range v.Waiting {
		d.Waiting = append(d.Waiting, row(x))
	}
	for _, x := range v.Other {
		d.Other = append(d.Other, row(x))
	}
	return d
}

func (m *Model) nowBranch() string {
	if m.ci != nil {
		return webName(m.ci.branch)
	}
	return ""
}

func hasWord(xs []string, w string) bool {
	for _, x := range xs {
		if x == w {
			return true
		}
	}
	return false
}

var nowKindWords = map[string]string{webproto.InboxCard: "agent", webproto.InboxRun: "agent run", webproto.InboxLane: "agent lane",
	webproto.InboxWorkflow: "workflow", webproto.InboxTask: "task", webproto.InboxCI: "CI", webproto.InboxRace: "race", webproto.InboxPR: "PR"}

func nowSub(it webproto.InboxItem) string {
	w := nowKindWords[it.Kind]
	if it.Chat != "" {
		w += " · " + it.Chat
	}
	return w
}

// nowAge is a short age: now, 12m, 3h, 1d.
func nowAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}

// nowLayer draws the Now page (or the Review page over it) on the whole
// screen.
func (m Model) nowLayer(string) string {
	t := m.nowT
	if t == nil {
		return ""
	}
	if t.review != nil {
		t.review.SetSize(m.w, m.h)
		return t.review.View()
	}
	t.page.SetSize(m.w, m.h)
	return t.page.View()
}

// nowRefresh shows the list again with a note.
func (m *Model) nowRefresh(note string, undo bool) {
	t := m.nowT
	t.page.SetData(m.nowData())
	t.page.Note(note, undo)
}

// routeToNowPage sends keys and resizes to the open page.
func (m Model) routeToNowPage(msg tea.Msg) (Model, tea.Cmd) {
	t := m.nowT
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		return m, nil
	case tea.KeyMsg:
		if t.review != nil {
			return m, m.nowReviewKey(t.review.Key(msg))
		}
		return m, m.nowPageKey(t.page.Key(msg))
	}
	return m, nil
}

// nowPageKey runs one action of the Now page.
func (m *Model) nowPageKey(a nowview.Action) tea.Cmd {
	st := m.nowStore()
	at := webNow()
	find := func() (now.Item, bool) { return m.nowFind(a.ID) }
	switch a.Do {
	case "close":
		m.nowT = nil
		return nil
	case "undo":
		if _, ok := st.Undo(); ok {
			m.nowRefresh("↶ undone", false)
		}
	case "done", "later", "mute":
		it, ok := find()
		if !ok {
			return nil
		}
		var err error
		note := "✓ done: " + it.Title
		switch a.Do {
		case "done":
			err = st.Done(now.DoneKey(it), at)
		case "later":
			until, perr := now.ParseLater(a.Text, at)
			if perr != nil {
				m.nowT.page.Note(perr.Error(), false)
				return nil
			}
			err, note = st.Later(it.ID, until), "◷ later: "+it.Title+" · back "+until.Format("Mon 15:04")
		case "mute":
			err, note = st.Mute(now.MuteKey(it, a.Repo), at), "muted: "+it.Title
		}
		if err != nil {
			m.nowT.page.Note("Could not save: "+err.Error(), false)
			return nil
		}
		m.nowRefresh(note, true)
	case "tomorrow":
		for _, r := range m.nowData().Overdue {
			if until, err := now.ParseLater("tomorrow", at); err == nil {
				_ = st.Later(r.ID, until)
			}
		}
		m.nowRefresh("◷ overdue moved to tomorrow 9:00", true)
	case "fix":
		m.nowT = nil
		return m.ciFixWithAI()
	case "open":
		return m.nowOpen(a.ID)
	}
	return nil
}

// nowOpen opens an item where its work lives; a pull request opens the
// Review page.
func (m *Model) nowOpen(id string) tea.Cmd {
	kind, ref, _ := strings.Cut(id, ":")
	switch kind {
	case webproto.InboxPR:
		n, _ := strconv.Atoi(ref)
		var q []int
		for _, a := range m.nowAsks {
			if !a.bot && !a.draft {
				q = append(q, a.number)
			}
		}
		at := 0
		for i, x := range q {
			if x == n {
				at = i
			}
		}
		if len(q) == 0 {
			q = []int{n}
		}
		m.nowT.queue, m.nowT.qi = q, at
		return m.nowLoadReview()
	case webproto.InboxRun:
		m.nowT = nil
		return m.agentBoardShowRun(ref)
	}
	m.nowT = nil
	return nil
}

// nowLoadReview reads the queue's current pull request off the UI thread.
func (m *Model) nowLoadReview() tea.Cmd {
	t := m.nowT
	if t.qi >= len(t.queue) {
		t.review = nil
		m.nowRefresh("✓ Review queue clear.", false)
		return nil
	}
	n, i, total := t.queue[t.qi], t.qi+1, len(t.queue)
	root, err := m.rootDir()
	if err != nil {
		return nil
	}
	run := m.ciW().runner()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewActTimeout)
		defer cancel()
		d, err := ghci.ViewPR(ctx, run, root, n)
		if err != nil {
			return applyMsg(func(m *Model) tea.Cmd {
				if m.nowT != nil {
					m.nowT.page.Note("Could not read #"+strconv.Itoa(n)+": "+webName(err.Error()), false)
				}
				return nil
			})
		}
		runs, _ := ghci.BranchRuns(ctx, run, root, d.Branch)
		s := webReviewSnap(d, runs)
		return applyMsg(func(m *Model) tea.Cmd {
			if m.nowT == nil {
				return nil
			}
			r := nowReviewOf(s, i, total)
			if m.nowT.review == nil {
				m.nowT.review = nowview.NewReview(r)
			} else {
				m.nowT.review.SetReview(r)
			}
			return nil
		})
	}
}

// nowReviewOf is the review as the TUI page shows it.
func nowReviewOf(s webproto.ReviewSnap, i, n int) nowview.Review {
	r := nowview.Review{Number: s.Number, Title: s.Title, Author: s.Author, Head: now.ShortSHA(s.Head), Files: s.Files, Add: s.Add, Del: s.Del,
		I: i, N: n, Risk: s.Risk.Level, Reasons: s.Risk.Reasons, Question: s.Question, Task: s.Task, Solution: s.Solution,
		Suggestion: s.Suggestion.Text, Notes: s.Suggestion.Notes, URL: s.URL}
	for _, g := range s.Groups {
		detail := ""
		if len(g.Files) == 1 {
			detail = g.Files[0]
		} else {
			detail = strconv.Itoa(len(g.Files)) + " files"
		}
		r.Groups = append(r.Groups, nowview.Group{Title: g.Title, Detail: detail, Add: g.Add, Del: g.Del, Main: g.Main})
	}
	for _, p := range s.Proof {
		r.Proof = append(r.Proof, nowview.Proof{Name: p.Name, State: p.State, SHA: p.SHA, Fix: p.Fix, Again: p.Again})
	}
	return r
}

// nowReviewKey runs one action of the Review page.
func (m *Model) nowReviewKey(a nowview.Action) tea.Cmd {
	t := m.nowT
	switch a.Do {
	case "close":
		t.review = nil
		return nil
	case "skip":
		t.qi++
		return m.nowLoadReview()
	case "approve":
		var send tea.Cmd
		if p := t.pending; p != nil {
			send = m.nowSendApprove(*p)
		}
		t.gen++
		t.pending = &nowPending{n: a.Number, head: a.Head, gen: t.gen}
		t.review.Note("✓ approved #"+strconv.Itoa(a.Number)+" on "+a.Head, true)
		gen := t.gen
		t.qi++
		return tea.Batch(send, tea.Tick(nowUndoFor, func(time.Time) tea.Msg { return nowTickMsg{gen: gen} }), m.nowLoadReview())
	case "undo":
		if p := t.pending; p != nil {
			t.pending = nil
			t.review.Note("↶ approval of #"+strconv.Itoa(p.n)+" not sent", false)
			for i, n := range t.queue {
				if n == p.n {
					t.qi = i
				}
			}
			return m.nowLoadReview()
		}
	case "comment":
		cmd := m.nowSendReview(a.Number, a.Head, ghci.ReviewComment, a.Text)
		t.qi++
		return tea.Batch(cmd, m.nowLoadReview())
	case "openfull":
		if a.Text == "" {
			return nil
		}
		if err := osOpen(a.Text); err != nil {
			t.review.Note("Open failed: "+err.Error(), false)
		}
		return nil
	case "fix", "again", "log":
		m.nowT = nil
		return m.ciFixWithAI()
	}
	return nil
}

// handleNowMsg ends an approval's undo wait.
func (m *Model) handleNowMsg(msg tea.Msg) (tea.Cmd, bool) {
	tick, ok := msg.(nowTickMsg)
	if !ok {
		return nil, false
	}
	t := m.nowT
	if t == nil || t.pending == nil || t.pending.gen != tick.gen {
		return nil, true
	}
	p := *t.pending
	t.pending = nil
	return m.nowSendApprove(p), true
}

func (m *Model) nowSendApprove(p nowPending) tea.Cmd {
	return m.nowSendReview(p.n, p.head, ghci.ReviewApprove, "")
}

// nowSendReview sends a review for the commit the user saw: the head is
// read again first, and a moved head is refused.
func (m *Model) nowSendReview(n int, head string, kind ghci.ReviewKind, text string) tea.Cmd {
	root, err := m.rootDir()
	if err != nil {
		return nil
	}
	run := m.ciW().runner()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewActTimeout)
		defer cancel()
		note := ""
		d, err := ghci.ViewPR(ctx, run, root, n)
		switch {
		case err != nil:
			note = "Not sent: " + webName(err.Error())
		case now.CanApprove(head, d.HeadSHA) != nil:
			note = now.CanApprove(head, d.HeadSHA).Error()
		default:
			if err := ghci.Review(ctx, run, root, n, kind, text); err != nil {
				note = "Not sent: " + webName(err.Error())
			}
		}
		return applyMsg(func(m *Model) tea.Cmd {
			if note != "" {
				if m.nowT != nil && m.nowT.review != nil {
					m.nowT.review.Note(note, false)
					return nil
				}
				return m.pushToastDetail(toast.Warn, "Review not sent", note)
			}
			return nil
		})
	}
}
