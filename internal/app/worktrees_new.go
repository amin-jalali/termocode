package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/jsonc"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktreeform"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Worktree: New… (WTX-5..8, plan 2.3) ──────────────────────────────────
//
// A form card (the shared connect card, like the slot form): Name, From
// (the ref it starts from; Start point… picks one), Branch (drafted from
// the name), Agent and Task (WTX-8), what .termocode/worktrees.json adds
// (WTX-6/7) and the folder. Cancel is first; nothing is made until
// Create. Create runs git worktree add off the UI thread (internal/spawn
// through internal/git); an error stays on the card and nothing is half
// made. Create + Start also starts the chosen agent in the new worktree
// and types the task once the agent shows its input box.

const (
	wtFormTitle                        = "New worktree"
	pickerKindWTRef     pickerKindEnum = 3601
	wtFormAgentsMax                    = 3 // agent options after "none" (one row)
	wtNewPaletteID                     = "worktree-new"
	wtSetupLogPaletteID                = "worktree-setup-log"
)

// wtForm is the open dialog.
type wtForm struct {
	root   string // where git runs (this window's folder)
	main   string // the main checkout (include / shared come from it)
	common string // the git common dir (worktrees.json)
	base   string // where user worktrees go
	from   string // the start point
	refs   []string
	cfg    worktreeform.Config
	hash   string // the config's hash (setup trust)
	cfgErr string
	files  []worktreeform.File
	agents []agents.Agent // the Agent row's options after "none"
	busy   bool
	fail   string
	gen    int
	// vals keeps the typed text while the ref picker is open.
	vals map[string]string
	pick int // the agent choice while the ref picker is open
	off  bool
	// issueNum / issueTitle: the issue read for the branch (WTX-9).
	issueNum   int
	issueTitle string
	// parent / parentTitle: a child worktree (c, WTX-11) starts from the
	// parent's branch and is stored under it; parentBranch is that branch
	// ("" detached), kept as the base its pull request targets (CMP-17).
	parent, parentTitle, parentBranch string
}

// wtFormVals are the typed values of the card.
type wtFormVals struct {
	name, from, branch, task string
	issue                    string // an issue or PR to link (WTX-9): the branch comes from its title
	agent                    int    // 0 = none
}

// wtFormDraft is what Create would do with vals: the branch and folder.
func (f *wtForm) draft(v wtFormVals) (branch, folder string) {
	branch = strings.TrimSpace(v.branch)
	if branch == "" {
		branch = worktreeform.BranchFromName(v.name)
	}
	if branch == "" && f.issueTitle != "" {
		branch = worktreeform.BranchFromIssue(f.issueNum, f.issueTitle)
	}
	if branch == "" {
		return "", ""
	}
	return branch, worktreeform.Folder(f.base, branch)
}

// wtNewPrepMsg is the repository read for a new dialog.
type wtNewPrepMsg struct {
	f   *wtForm
	err error
}

// wtNewAgents lists the installed agents for the Agent row (a var for
// tests and the demo).
var wtNewAgents = func() []agents.Agent {
	var out []agents.Agent
	for _, a := range agents.Registry {
		if agentInstalled(a) {
			out = append(out, a)
		}
	}
	return out
}

// worktreeDirSetting is config.json "worktree_dir" (file only; "" =
// <repo>.worktrees next to the repository).
func worktreeDirSetting() string {
	path, err := settingsConfigPath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg struct {
		Dir string `json:"worktree_dir"`
	}
	if json.Unmarshal(jsonc.Strip(b), &cfg) != nil {
		return ""
	}
	return cfg.Dir
}

// openWorktreeNew reads the repository off the UI thread, then opens
// the dialog (Worktree: New…, n in the view).
func (m *Model) openWorktreeNew() tea.Cmd { return m.openWorktreeNewFrom("", "", "", "") }

// openWorktreeNewChild is c: a child of the cursor's worktree, from its
// branch (or its commit when detached).
func (m *Model) openWorktreeNewChild() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	if it.e.Gone {
		return m.pushToastDetail(toast.Info, "No child of a gone worktree", "Its folder is gone: F forgets it.")
	}
	from := it.e.Branch
	if from == "" {
		from = it.e.Head
	}
	return m.openWorktreeNewFrom(it.e.Path, wtTitle(it), from, it.e.Branch)
}

func (m *Model) openWorktreeNewFrom(parent, parentTitle, from, parentBranch string) tea.Cmd {
	if m.wt == nil {
		return nil
	}
	if m.wt.off { // a demo: the dialog is set up by the demo
		return nil
	}
	root, _ := m.rootDir()
	setting := worktreeDirSetting()
	return func() tea.Msg {
		msg := wtNewPrep(root, setting)
		if msg.f != nil && parent != "" {
			msg.f.parent, msg.f.parentTitle, msg.f.parentBranch = parent, parentTitle, parentBranch
			if from != "" {
				msg.f.from = from
			}
		}
		return msg
	}
}

// wtNewPrep reads what the dialog shows: the main checkout, the refs,
// the config and its files.
func wtNewPrep(root, setting string) wtNewPrepMsg {
	list, common, err := git.ListWorktrees(root)
	if err != nil {
		return wtNewPrepMsg{err: err}
	}
	main := root
	for _, w := range list {
		if w.Main && !w.Bare {
			main = w.Path
		}
	}
	home, _ := os.UserHomeDir()
	f := &wtForm{root: root, main: main, common: common, base: worktreeform.BaseDir(main, setting, home)}
	f.refs, f.from, _ = git.WorktreeRefs(root)
	if f.from == "" {
		f.from = "HEAD"
	}
	cfg, hash, cerr := worktreeform.LoadConfig(main)
	if cerr != nil {
		f.cfgErr = cerr.Error()
	}
	f.cfg, f.hash = cfg, hash
	f.files = worktreeform.PlanFiles(main, cfg)
	return wtNewPrepMsg{f: f}
}

// onWTNewPrep opens the dialog.
func (m *Model) onWTNewPrep(msg wtNewPrepMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Info, "No worktree can be made here", "This folder is not in a git repository.")
	}
	if m.connectOpen { // another card came first
		return nil
	}
	f := msg.f
	ags := wtNewAgents()
	if len(ags) > wtFormAgentsMax {
		ags = ags[:wtFormAgentsMax]
	}
	f.agents = ags
	m.wt.form = f
	m.connect = connectcard.New(m.wtFormCard())
	m.connect.SetSize(m.w, m.h)
	m.connectOpen = true
	return nil
}

// wtFormValues reads the open card.
func (m *Model) wtFormValues() wtFormVals {
	return wtFormVals{
		name:   strings.TrimSpace(m.connect.InputValue("name")),
		from:   strings.TrimSpace(m.connect.InputValue("from")),
		branch: strings.TrimSpace(m.connect.InputValue("branch")),
		task:   strings.TrimSpace(m.connect.InputValue("task")),
		issue:  strings.TrimSpace(m.connect.InputValue("issue")),
		agent:  m.connect.Choice("agent"),
	}
}

// wtFormCard is the card for the form's state and the typed values.
func (m *Model) wtFormCard() connectcard.Card {
	f := m.wt.form
	v := wtFormVals{from: f.from}
	if m.connectOpen && m.connect.Card().Title == wtFormTitle {
		v = m.wtFormValues()
	} else if f.vals != nil {
		v = wtFormVals{name: f.vals["name"], from: f.vals["from"], branch: f.vals["branch"], task: f.vals["task"], issue: f.vals["issue"], agent: f.pick}
	}
	return wtFormCardFor(f, v, m.h < 30)
}

// wtFormCardFor builds the card (pure: tests and goldens).
func wtFormCardFor(f *wtForm, v wtFormVals, short bool) connectcard.Card {
	branch, folder := f.draft(v)
	ph := "from the name"
	switch {
	case strings.TrimSpace(v.branch) == "" && branch != "":
		ph = branch
	case strings.TrimSpace(v.issue) != "":
		ph = "from the issue title"
	}
	opts := []string{"none"}
	for _, a := range f.agents {
		opts = append(opts, a.Title)
	}
	var rows []connectcard.Row
	if f.parent != "" {
		text := "Child of " + f.parentTitle + " (starts from its branch)"
		if f.parentBranch != "" {
			text = "Child of " + f.parentTitle + " (stacked: starts from and its PR targets " + f.parentBranch + ")"
		}
		rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: text})
	}
	rows = append(rows, []connectcard.Row{
		{Kind: connectcard.Input, ID: "name", Text: "Name", Inline: true, Placeholder: "feat/my-change", Value: v.name},
		{Kind: connectcard.Input, ID: "issue", Text: "Issue", Inline: true, Placeholder: "optional: number or URL, linked to it", Value: v.issue},
		{Kind: connectcard.Input, ID: "from", Text: "From", Inline: true, Value: v.from},
		{Kind: connectcard.Input, ID: "branch", Text: "Branch", Inline: true, Placeholder: ph, Value: v.branch},
		{Kind: connectcard.Choice, ID: "agent", Text: "Agent", Options: opts, Selected: v.agent},
		{Kind: connectcard.Input, ID: "task", Text: "Task", Inline: true, Placeholder: "typed into the agent once it is ready", Value: v.task},
		{Kind: connectcard.Blank},
	}...)
	rows = append(rows, wtAlsoRows(f, short)...)
	where := "type a name"
	if folder == "" && strings.TrimSpace(v.issue) != "" {
		where = "from the issue title"
	}
	if folder != "" {
		home, _ := os.UserHomeDir()
		where = homeShort(folder, home)
	}
	rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: "Folder  " + where})
	switch {
	case f.busy:
		rows = append(rows, connectcard.Row{Kind: connectcard.Status, Text: theme.GlyphWait.String() + " Making the worktree…"})
	case f.fail != "":
		rows = append(rows, connectcard.Row{Kind: connectcard.Fail, Text: f.fail})
	}
	return connectcard.Card{Title: wtFormTitle, Rows: rows, Form: true,
		Buttons: []connectcard.Button{
			{ID: "cancel", Title: "Cancel"},
			{ID: "refs", Title: "Start point…"},
			{ID: "create", Title: "Create"},
			{ID: "start", Title: "Create + Start"},
		}}
}

// wtAlsoRows say what .termocode/worktrees.json adds: the copies, the
// links, the setup commands and what is skipped (names only, never a
// file's contents). short: one line (a low window).
func wtAlsoRows(f *wtForm, short bool) []connectcard.Row {
	dot := theme.ASCIIText(" · ")
	if f.cfgErr != "" {
		return []connectcard.Row{{Kind: connectcard.Fail, Text: worktreeform.ConfigFile + " could not be read; nothing is copied and no setup runs."}}
	}
	var copies, links, skips []string
	for _, fl := range f.files {
		switch {
		case fl.Skip != "":
			skips = append(skips, fl.Rel+" ("+fl.Skip+")")
		case fl.Kind == worktreeform.Copy:
			copies = append(copies, fl.Rel)
		default:
			links = append(links, fl.Rel)
		}
	}
	setup := ""
	if len(f.cfg.Setup) > 0 {
		setup = "asks first"
		if worktreeform.SetupTrusted(f.hash, loadSession().WorktreeSetupTrusted) {
			setup = "trusted " + theme.GlyphOK.String()
		}
	}
	if len(copies)+len(links)+len(skips) == 0 && setup == "" {
		return nil
	}
	if short {
		var parts []string
		if len(copies) > 0 {
			parts = append(parts, "copy "+strconv.Itoa(len(copies)))
		}
		if len(links) > 0 {
			parts = append(parts, "link "+strconv.Itoa(len(links)))
		}
		if setup != "" {
			parts = append(parts, "setup "+setup)
		}
		if len(skips) > 0 {
			parts = append(parts, "skip "+strconv.Itoa(len(skips)))
		}
		return []connectcard.Row{{Kind: connectcard.Text, Text: "Also: " + strings.Join(parts, dot)}, {Kind: connectcard.Blank}}
	}
	rows := []connectcard.Row{{Kind: connectcard.Dim, Text: "When it is made:"}}
	add := func(k connectcard.Kind, s string) { rows = append(rows, connectcard.Row{Kind: k, Text: "  " + s}) }
	if len(copies) > 0 {
		add(connectcard.Text, "copy "+strings.Join(copies, ", ")+" ("+wtPlural(len(copies), "file")+")")
	}
	if len(links) > 0 {
		add(connectcard.Text, "link "+strings.Join(links, ", ")+" "+theme.ASCIIText("→")+" shared (both sides see writes)")
	}
	if setup != "" {
		add(connectcard.Text, "run setup: "+strings.Join(f.cfg.Setup, " && ")+dot+setup)
	}
	if len(skips) > 0 {
		add(connectcard.Dim, "skip "+strings.Join(skips, ", "))
	}
	return append(rows, connectcard.Row{Kind: connectcard.Blank})
}

func wtPlural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// refreshWTForm rebuilds the open card after a key or a click (the
// branch placeholder and the folder follow the name).
func (m *Model) refreshWTForm() {
	if m.wt == nil || m.wt.form == nil || !m.connectOpen || m.connect.Card().Title != wtFormTitle {
		return
	}
	m.connect.SetCard(m.wtFormCard())
}

// wtFormOpen: the dialog has the shared card.
func (m Model) wtFormOpen() bool {
	return m.wt != nil && m.wt.form != nil && m.connectOpen && m.connect.Card().Title == wtFormTitle
}

// routeToWTForm sends a key to the dialog. Enter in a field goes to the
// next one: only a button makes anything.
func (m Model) routeToWTForm(msg tea.Msg) (Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.Type == tea.KeyEnter && m.connect.FocusedID() != "" {
		msg = tea.KeyMsg{Type: tea.KeyTab}
	}
	var cmd tea.Cmd
	m.connect, cmd = m.connect.Update(msg)
	m.refreshWTForm()
	return m, cmd
}

// handleWTFormMsg runs the dialog's buttons. ok=false: not ours.
func (m *Model) handleWTFormMsg(msg tea.Msg) (tea.Cmd, bool) {
	if m.wt == nil || m.wt.form == nil {
		return nil, false
	}
	switch msg := msg.(type) {
	case connectcard.ButtonMsg:
		return m.onWTFormButton(msg.ID), true
	case connectcard.SubmitMsg, connectcard.KeyMsg:
		return nil, true
	case connectcard.CloseMsg:
		return m.onWTFormButton("cancel"), true
	}
	return nil, false
}

// closeWTForm closes the dialog; nothing is made.
func (m *Model) closeWTForm() {
	m.connectOpen = false
	if m.wt != nil {
		m.wt.form = nil
	}
}

func (m *Model) onWTFormButton(id string) tea.Cmd {
	f := m.wt.form
	switch id {
	case "cancel":
		m.closeWTForm()
		return nil
	case "refs":
		if f.busy {
			return nil
		}
		return m.openWTRefPicker()
	case "create", "start":
		if f.busy {
			return nil
		}
		return m.wtCreate(id == "start")
	}
	return nil
}

// openWTRefPicker lists the start points; the card waits behind it (its
// typed text is kept).
func (m *Model) openWTRefPicker() tea.Cmd {
	f := m.wt.form
	v := m.wtFormValues()
	f.vals = map[string]string{"name": v.name, "from": v.from, "branch": v.branch, "task": v.task, "issue": v.issue}
	f.pick = v.agent
	m.connectOpen = false
	items := make([]picker.Item, 0, len(f.refs))
	for i, r := range f.refs {
		hint := ""
		if i == 0 && r == f.from {
			hint = "current branch"
		}
		items = append(items, picker.Item{ID: r, Title: r, Hint: hint})
	}
	m.picker = picker.NewItems(" Start point ", items)
	m.picker.SetNote([]string{"Pick the branch, remote branch or tag the worktree starts from. Esc goes back."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindWTRef
	return nil
}

// reopenWTForm shows the dialog again after the ref picker (ref "" =
// Esc: the start point stays).
func (m *Model) reopenWTForm(ref string) {
	f := m.wt.form
	if f == nil {
		return
	}
	if ref != "" && f.vals != nil {
		f.vals["from"] = ref
	}
	m.connect = connectcard.New(m.wtFormCard())
	m.connect.SetSize(m.w, m.h)
	m.connectOpen = true
	f.vals = nil
}

// wtCreatedMsg is the end of a Create.
type wtCreatedMsg struct {
	gen            int
	folder, branch string
	files          []worktreeform.File
	err            error
	start          bool
	agent          agents.Agent
	task           string
	from           string        // the start point it was made from
	link           *ghci.Link    // the linked issue / PR as gh read it
	linkRef        *ghci.LinkRef // linked without a read (no gh)
}

// wtCreateRun makes the worktree and copies / links the files (a var:
// tests).
var wtCreateRun = func(f *wtForm, folder, branch, ref string) ([]worktreeform.File, error) {
	if err := git.AddWorktree(f.root, folder, branch, ref); err != nil {
		return nil, err
	}
	return worktreeform.ApplyFiles(f.main, folder, f.files), nil
}

// wtCreate checks the form and makes the worktree in the background.
func (m *Model) wtCreate(start bool) tea.Cmd {
	f := m.wt.form
	v := m.wtFormValues()
	branch, folder := f.draft(v)
	f.fail = ""
	ref, hasRef, refErr := wtLinkRefOf(v.issue, f.root)
	switch {
	case refErr != nil:
		f.fail = "Issue: " + refErr.Error()
	case branch == "" && !hasRef:
		f.fail = "Type a name (or a branch)."
	case v.from == "":
		f.fail = "Type where it starts from, or pick a start point."
	}
	var ag agents.Agent
	if v.agent > 0 && v.agent <= len(f.agents) {
		ag = f.agents[v.agent-1]
	}
	if start && ag.ID == "" && f.fail == "" {
		f.fail = "Pick an agent to start, or use Create."
	}
	if f.fail != "" {
		m.refreshWTForm()
		return nil
	}
	f.busy = true
	f.gen++
	gen := f.gen
	m.refreshWTForm()
	from := v.from
	task := v.task
	run := wtGHRun()
	return func() tea.Msg {
		msg := wtCreatedMsg{gen: gen, start: start, agent: ag, task: task, from: from}
		if hasRef {
			l, err := ghci.ViewLink(context.Background(), run, f.root, ref)
			switch {
			case err == nil:
				msg.link = &l
				if branch == "" {
					branch = worktreeform.BranchFromIssue(l.Number, l.Title)
					folder = worktreeform.Folder(f.base, branch)
				}
			case branch == "":
				msg.err = errors.New("Could not read #" + strconv.Itoa(ref.Number) + " for its title (" + err.Error() + "); type a name.")
				return msg
			default:
				msg.linkRef = &ref
			}
		}
		msg.folder, msg.branch = folder, branch
		msg.files, msg.err = wtCreateRun(f, folder, branch, from)
		return msg
	}
}

// onWTCreated closes the dialog after a good Create (or shows the
// error on it), saves the name, then runs the setup (after trust) and
// starts the agent.
func (m *Model) onWTCreated(msg wtCreatedMsg) tea.Cmd {
	st := m.wt
	f := st.form
	if f == nil || msg.gen != f.gen {
		return nil
	}
	f.busy = false
	if msg.err != nil {
		f.fail = wtCreateError(msg.err)
		m.refreshWTForm()
		return nil
	}
	m.closeWTForm()
	name := strings.TrimSpace(filepath.Base(msg.folder))
	_ = worktrees.Update(f.common, func(mm map[string]worktrees.Meta) {
		md := mm[msg.folder]
		if md.Name == "" && name != worktreeform.FolderName(msg.branch) {
			md.Name = name
		}
		md.Created, md.LastSeen = clock.Now(), clock.Now()
		if f.parent != "" {
			mm[msg.folder] = md
			_ = worktrees.SetParent(mm, msg.folder, f.parent)
			md = mm[msg.folder]
			if f.parentBranch != "" && msg.from == f.parentBranch {
				md.Base = f.parentBranch // CMP-17: the PR targets the parent
			}
		}
		if msg.start {
			md.Agent = msg.agent.ID
		}
		switch {
		case msg.link != nil:
			wtLinkMeta(&md, *msg.link, clock.Now())
		case msg.linkRef != nil && msg.linkRef.Kind == ghci.LinkPR:
			md.PR = msg.linkRef.Number
		case msg.linkRef != nil:
			md.Issue = msg.linkRef.Number
		}
		mm[msg.folder] = md
	})
	st.cursorPath = msg.folder
	detail := wtShortPath(msg.folder)
	if c, l := worktreeform.Counts(msg.files); c+l > 0 {
		detail += theme.ASCIIText(" · ") + "copied " + strconv.Itoa(c) + theme.ASCIIText(" · ") + "linked " + strconv.Itoa(l)
	}
	var skipped []string
	for _, fl := range msg.files {
		if fl.Skip != "" {
			skipped = append(skipped, fl.Rel+" ("+fl.Skip+")")
		}
	}
	if len(skipped) > 0 {
		detail += ". Skipped: " + strings.Join(skipped, ", ")
	}
	cmds := []tea.Cmd{m.pushToastDetail(toast.Info, "Worktree "+msg.branch+" is ready", detail), m.worktreesReload()}
	if msg.start && msg.agent.ID != "" {
		cmds = append(cmds, m.startAgentWithTask(msg.agent, msg.folder, msg.task))
	}
	if len(f.cfg.Setup) > 0 && f.cfgErr == "" {
		cmds = append(cmds, m.wtSetupAsk(f, msg.folder))
	}
	return tea.Batch(cmds...)
}

// wtCreateError is a git error in plain words.
func wtCreateError(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "invalid reference"), strings.Contains(s, "not a valid"):
		return "That start point is not a branch, tag or commit here. " + s
	}
	return s
}

func init() {
	aiActionPolicies[wtNewPaletteID] = aiNever(aiNoPicker + " (worktree_create comes later)")
	aiActionPolicies[wtSetupLogPaletteID] = aiAuto() // opens a log file in the editor
	aiSettingPolicies["worktree_dir"] = aiNever("where new worktrees go on disk; a file-only switch the user edits in config.json")
}
