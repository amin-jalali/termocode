package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Tags (GITX-11), the remotes manager and the upstream (GITX-13), and
// force push with lease (GITX-14). Everything that deletes or replaces
// work asks first with Cancel ready; deleting on the remote and a force
// push also want the name typed.

const (
	confirmKindGitTagDelete       confirmKindEnum = 3670
	confirmKindGitTagDeleteRemote confirmKindEnum = 3671
	confirmKindGitRemoteRemove    confirmKindEnum = 3672
	confirmKindGitForcePush       confirmKindEnum = 3673

	pickerKindGitTagDelete       pickerKindEnum = 3670
	pickerKindGitTagPush         pickerKindEnum = 3671
	pickerKindGitTagDeleteRemote pickerKindEnum = 3672
	pickerKindGitTagDetails      pickerKindEnum = 3673
	pickerKindGitRemotes         pickerKindEnum = 3674
	pickerKindGitRemoteAction    pickerKindEnum = 3675
	pickerKindGitUpstream        pickerKindEnum = 3676

	promptKindGitTagName        promptKindEnum = 3670
	promptKindGitTagMessage     promptKindEnum = 3671
	promptKindGitTagDeleteTyped promptKindEnum = 3672
	promptKindGitRemoteAddName  promptKindEnum = 3673
	promptKindGitRemoteAddURL   promptKindEnum = 3674
	promptKindGitRemoteRename   promptKindEnum = 3675
	promptKindGitRemoteURL      promptKindEnum = 3676
	promptKindGitForcePushTyped promptKindEnum = 3677
)

// gitRefOpsState is what a tag, remote or force-push card or prompt is
// about.
type gitRefOpsState struct {
	tag, tagRev string // the tag being made or deleted, and where
	remote      string // the remote of the action
	newRemote   string // a remote being added (name typed, URL next)
	branch      string // force push: the branch
	lease       string // force push: the remote commit the user saw
	page        string // the last Show Tag Details text (tests read it)
}

func (m *Model) gitRefToast(err error, fail, ok, detail string) tea.Cmd {
	var c tea.Cmd
	switch {
	case err != nil:
		m.toast, c = m.toast.PushDetail(toast.Errr, fail, err.Error())
	case detail != "":
		m.toast, c = m.toast.PushDetail(toast.Info, ok, detail)
	default:
		m.toast, c = m.toast.Push(toast.Info, ok)
	}
	return tea.Batch(fetchGitCmd(), c)
}

func (m *Model) gitRefPrompt(kind promptKindEnum, title, label, value string) {
	m.prompt = prompt.New(title, label, value)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = kind
}

func (m *Model) gitRefPicker(kind pickerKindEnum, title string, items []picker.Item) {
	m.picker = picker.NewItems(title, items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = kind
}

func (m *Model) gitRefCard(kind confirmKindEnum, title, text string, act confirm.Button) {
	m.confirm = confirm.Risky(title, text, confirm.Button{ID: "cancel", Title: "Cancel"}, act)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = kind
}

// gitPushRemote is the remote pushes go to: the branch's upstream remote,
// else origin, else the first remote ("" when there is none).
func gitPushRemote(cwd, branch string) string {
	cmd := spawn.Command("git", "config", "--get", "branch."+branch+".remote")
	cmd.Dir = cwd
	if out, err := cmd.Output(); err == nil && strings.TrimSpace(string(out)) != "" && strings.TrimSpace(string(out)) != "." {
		return strings.TrimSpace(string(out))
	}
	rs, _ := git.ListRemotes(cwd)
	for _, r := range rs {
		if r.Name == "origin" {
			return "origin"
		}
	}
	if len(rs) > 0 {
		return rs[0].Name
	}
	return ""
}

// ── tags ────────────────────────────────────────────────────────────────

// openCreateTagPrompt asks a tag name for rev ("" = HEAD).
func (m *Model) openCreateTagPrompt(rev, label string) tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	if rev == "" {
		rev, label = "HEAD", "HEAD"
	}
	m.gitRef.tagRev = rev
	m.gitRefPrompt(promptKindGitTagName, "Create Tag", "Tag name (on "+label+"):", "")
	return nil
}

// gitTagHereFromRow tags the GRAPH commit under the cursor.
func (m *Model) gitTagHereFromRow() tea.Cmd {
	if cur, ok := m.gitCurrentRow(); ok && cur.kind == gitRowCommit {
		c := m.gitCommits[cur.ci]
		return m.openCreateTagPrompt(c.FullHash, c.Hash)
	}
	return nil
}

// gitTagItems lists the tags as picker rows ("tag-<name>").
func (m *Model) gitTagItems() ([]picker.Item, tea.Cmd) {
	if !m.gitEnsureLoaded() {
		return nil, nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil, nil
	}
	tags, err := git.Tags(cwd)
	if err != nil {
		return nil, m.gitRefToast(err, "Tags failed", "", "")
	}
	if len(tags) == 0 {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, "No tags in this repository")
		return nil, c
	}
	items := make([]picker.Item, 0, len(tags))
	for _, t := range tags {
		items = append(items, picker.Item{ID: "tag-" + t, Title: t, Hint: git.TagTip(cwd, t)})
	}
	return items, nil
}

func (m *Model) openTagPickerFor(kind pickerKindEnum, title string, extra ...picker.Item) tea.Cmd {
	items, c := m.gitTagItems()
	if items == nil {
		return c
	}
	m.gitRefPicker(kind, title, append(extra, items...))
	return nil
}

// onGitTagPicked runs the action of the tag picker that was open.
func (m *Model) onGitTagPicked(kind pickerKindEnum, id string) tea.Cmd {
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	remote := gitPushRemote(cwd, pushBranch(m.gitBranch))
	if kind == pickerKindGitTagPush && id == "tags-all" {
		if remote == "" {
			return m.gitRefToast(errors.New("this repository has no remote"), "Push failed", "", "")
		}
		return m.gitRemoteOp("Pushing all tags…", func(cwd string) gitRemoteDoneMsg {
			return gitRemoteDoneMsg{failTitle: "Push tags failed", okText: "Pushed all tags to " + remote, err: git.PushAllTags(cwd, remote)}
		})
	}
	name, ok := strings.CutPrefix(id, "tag-")
	if !ok {
		return nil
	}
	m.gitRef.tag, m.gitRef.remote = name, remote
	switch kind {
	case pickerKindGitTagDelete:
		m.gitRefCard(confirmKindGitTagDelete, "Delete tag "+name+"?",
			"The tag goes from this repository only; a copy on a remote stays. The toast names its commit, so it can be made again.",
			confirm.Button{ID: "delete", Title: "Delete", Style: confirm.StyleDestructive})
	case pickerKindGitTagPush:
		if remote == "" {
			return m.gitRefToast(errors.New("this repository has no remote"), "Push failed", "", "")
		}
		return m.gitRemoteOp("Pushing "+name+"…", func(cwd string) gitRemoteDoneMsg {
			return gitRemoteDoneMsg{failTitle: "Push tag failed", okText: "Pushed " + name + " to " + remote, err: git.PushTag(cwd, remote, name)}
		})
	case pickerKindGitTagDeleteRemote:
		if remote == "" {
			return m.gitRefToast(errors.New("this repository has no remote"), "Delete failed", "", "")
		}
		m.gitRefCard(confirmKindGitTagDeleteRemote, "Delete "+name+" on "+remote+"?",
			"The tag goes for everyone who uses "+remote+". Your local tag stays. You type the name next to confirm.",
			confirm.Button{ID: "delete-remote", Title: "Delete on Remote…", Style: confirm.StyleDestructive})
	case pickerKindGitTagDetails:
		return m.showTagDetails(cwd, name)
	}
	return nil
}

// tagDetailText is the Show Tag Details page.
func tagDetailText(t git.TagInfo) string {
	var b strings.Builder
	kind := "lightweight tag"
	if t.Annotated {
		kind = "annotated tag"
	}
	fmt.Fprintf(&b, "Tag:     %s (%s)\n", t.Name, kind)
	if t.Tagger != "" {
		fmt.Fprintf(&b, "Tagger:  %s\n", t.Tagger)
	}
	if t.Date != "" {
		fmt.Fprintf(&b, "Date:    %s\n", t.Date)
	}
	fmt.Fprintf(&b, "Commit:  %s %s\n", t.Commit, t.Subject)
	if t.Message != "" {
		b.WriteString("\n" + t.Message + "\n")
	}
	return b.String()
}

func (m *Model) showTagDetails(cwd, name string) tea.Cmd {
	t, err := git.TagDetail(cwd, name)
	if err != nil {
		return m.gitRefToast(err, "Tag details failed", "", "")
	}
	m.gitRef.page = tagDetailText(t)
	m.focus = FocusEditor
	m.ensureEditorWindowCurrent()
	if err := m.openReadOnlyPage("Tag "+name, m.gitRef.page, "", "text"); err != nil {
		m.err = "tag: " + err.Error()
	}
	return nil
}

// onGitRefConfirm answers this file's cards; Cancel does nothing.
func (m *Model) onGitRefConfirm(kind confirmKindEnum, id string) tea.Cmd {
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	r := m.gitRef
	switch {
	case kind == confirmKindGitTagDelete && id == "delete":
		tip := git.TagTip(cwd, r.tag)
		return m.gitRefToast(git.DeleteTag(cwd, r.tag), "Delete tag failed", "Deleted tag "+r.tag,
			fmt.Sprintf("was at %s · git tag %s %s brings it back", tip, r.tag, tip))
	case kind == confirmKindGitTagDeleteRemote && id == "delete-remote":
		m.gitRefPrompt(promptKindGitTagDeleteTyped, "Delete "+r.tag+" on "+r.remote, "Type "+r.tag+" to delete it on "+r.remote+":", "")
	case kind == confirmKindGitRemoteRemove && id == "remove":
		return m.gitRefToast(git.RemoveRemote(cwd, r.remote), "Remove failed", "Removed remote "+r.remote, "Its remote-tracking branches went too")
	case kind == confirmKindGitForcePush && id == "force":
		m.gitRefPrompt(promptKindGitForcePushTyped, "Force push "+r.branch, "Type "+r.branch+" to replace "+r.remote+"/"+r.branch+":", "")
	}
	return nil
}

// ── remotes and upstream ───────────────────────────────────────────────

// openRemotesPicker lists the remotes and an Add Remote row.
func (m *Model) openRemotesPicker() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	rs, err := git.ListRemotes(cwd)
	if err != nil {
		return m.gitRefToast(err, "Remotes failed", "", "")
	}
	items := []picker.Item{{ID: "remote-add", Title: "+ Add Remote…"}}
	for _, r := range rs {
		hint := r.FetchURL
		if r.PushURL != "" && r.PushURL != r.FetchURL {
			hint += " (push: " + r.PushURL + ")"
		}
		items = append(items, picker.Item{ID: "remote-" + r.Name, Title: r.Name, Hint: hint})
	}
	m.gitRefPicker(pickerKindGitRemotes, " Remotes ", items)
	return nil
}

func (m *Model) onRemotePicked(id string) tea.Cmd {
	if id == "remote-add" {
		m.gitRefPrompt(promptKindGitRemoteAddName, "Add Remote", "Remote name:", "")
		return nil
	}
	name, ok := strings.CutPrefix(id, "remote-")
	if !ok {
		return nil
	}
	m.gitRef.remote = name
	m.gitRefPicker(pickerKindGitRemoteAction, " Remote "+name+" ", []picker.Item{
		{ID: "remote-rename", Title: "Rename…"},
		{ID: "remote-url", Title: "Change URL…"},
		{ID: "remote-remove", Title: "Remove…"},
	})
	return nil
}

func (m *Model) onRemoteActionPicked(id string) tea.Cmd {
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	name := m.gitRef.remote
	switch id {
	case "remote-rename":
		m.gitRefPrompt(promptKindGitRemoteRename, "Rename Remote", "New name for "+name+":", name)
	case "remote-url":
		url := ""
		if rs, err := git.ListRemotes(cwd); err == nil {
			for _, r := range rs {
				if r.Name == name {
					url = r.FetchURL
				}
			}
		}
		m.gitRefPrompt(promptKindGitRemoteURL, "Change URL", "URL of "+name+":", url)
	case "remote-remove":
		m.gitRefCard(confirmKindGitRemoteRemove, "Remove remote "+name+"?",
			"termocode forgets "+name+" and its remote-tracking branches. Nothing on the server changes; Add Remote… brings it back.",
			confirm.Button{ID: "remove", Title: "Remove", Style: confirm.StyleDestructive})
	}
	return nil
}

// openUpstreamPicker lists the remote branches the current branch can
// track.
func (m *Model) openUpstreamPicker() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	branch := pushBranch(m.gitBranch)
	if branch == "" {
		return m.aiInfo("No branch (detached HEAD)")
	}
	var items []picker.Item
	for _, u := range git.UpstreamCandidates(cwd) {
		items = append(items, picker.Item{ID: "up-" + u, Title: u})
	}
	if len(items) == 0 {
		return m.aiInfo("No remote branches yet: fetch, or push the branch first")
	}
	m.gitRefPicker(pickerKindGitUpstream, " Upstream of "+branch+" ", items)
	return nil
}

func (m *Model) onUpstreamPicked(id string) tea.Cmd {
	up, ok := strings.CutPrefix(id, "up-")
	cwd, err := m.gitRootDir()
	if !ok || err != nil {
		return nil
	}
	branch := pushBranch(m.gitBranch)
	return m.gitRefToast(git.SetUpstream(cwd, branch, up), "Set upstream failed", branch+" now tracks "+up, "")
}

// gitUnsetUpstream stops the current branch tracking its upstream.
func (m *Model) gitUnsetUpstream() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	branch := pushBranch(m.gitBranch)
	if branch == "" {
		return m.aiInfo("No branch (detached HEAD)")
	}
	return m.gitRefToast(git.UnsetUpstream(cwd, branch), "Unset upstream failed", branch+" tracks nothing now", "Git: Set Upstream Branch… sets it again")
}

// ── force push ─────────────────────────────────────────────────────────

// gitRevCount counts the commits of a range ("a..b"); -1 when unknown.
func gitRevCount(cwd, rng string) int {
	cmd := spawn.Command("git", "rev-list", "--count", rng)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return -1
	}
	return n
}

// openForcePushCard explains what a force push replaces; Cancel is
// ready.
func (m *Model) openForcePushCard() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	branch := pushBranch(m.gitBranch)
	if branch == "" {
		return m.aiInfo("No branch to push (detached HEAD)")
	}
	remote := gitPushRemote(cwd, branch)
	lease := git.RemoteTip(cwd, remote, branch)
	if remote == "" || lease == "" {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, branch+" is not on a remote yet", "Nothing to force. Git: Push sends it")
		return c
	}
	m.gitRef.branch, m.gitRef.remote, m.gitRef.lease = branch, remote, lease
	text := fmt.Sprintf("%s/%s is replaced by your %s.", remote, branch, branch)
	if n := gitRevCount(cwd, "HEAD.."+remote+"/"+branch); n > 0 {
		text += fmt.Sprintf(" %d commit%s on %s/%s that your branch does not have will be gone from the remote.", n, plural(n), remote, branch)
	}
	text += fmt.Sprintf(" With lease: if someone pushed after %s (the last commit you fetched), git refuses and nothing changes. You type the branch name next.", lease[:min(7, len(lease))])
	m.gitRefCard(confirmKindGitForcePush, "Force push "+branch+"?", text,
		confirm.Button{ID: "force", Title: "Force Push…", Style: confirm.StyleDestructive})
	return nil
}

// ── prompts ─────────────────────────────────────────────────────────────

// handleGitRefPromptSubmit answers this file's prompts.
func (m *Model) handleGitRefPromptSubmit(value string) (tea.Cmd, bool) {
	switch m.promptKind {
	case promptKindGitTagName, promptKindGitTagMessage, promptKindGitTagDeleteTyped,
		promptKindGitRemoteAddName, promptKindGitRemoteAddURL, promptKindGitRemoteRename,
		promptKindGitRemoteURL, promptKindGitForcePushTyped:
	default:
		return nil, false
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil, true
	}
	value = strings.TrimSpace(value)
	r := &m.gitRef
	noMatch := func(want string) tea.Cmd {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "Name did not match", "Nothing changed. Type "+want+" exactly.")
		return c
	}
	switch m.promptKind {
	case promptKindGitTagName:
		if value == "" {
			return nil, true
		}
		r.tag = value
		m.gitRefPrompt(promptKindGitTagMessage, "Create Tag "+value, "Message (empty: a lightweight tag):", "")
		return nil, true
	case promptKindGitTagMessage:
		kind := "lightweight"
		if value != "" {
			kind = "annotated"
		}
		return m.gitRefToast(git.CreateTag(cwd, r.tag, r.tagRev, value), "Create tag failed", "Created tag "+r.tag,
			kind+" · Git: Push Tag… shares it"), true
	case promptKindGitTagDeleteTyped:
		if value != r.tag {
			return noMatch(r.tag), true
		}
		tag, remote := r.tag, r.remote
		return m.gitRemoteOp("Deleting "+tag+" on "+remote+"…", func(cwd string) gitRemoteDoneMsg {
			return gitRemoteDoneMsg{failTitle: "Delete failed", okText: "Deleted " + tag + " on " + remote, err: git.DeleteRemoteTag(cwd, remote, tag)}
		}), true
	case promptKindGitRemoteAddName:
		if value == "" {
			return nil, true
		}
		r.newRemote = value
		m.gitRefPrompt(promptKindGitRemoteAddURL, "Add Remote "+value, "URL:", "")
		return nil, true
	case promptKindGitRemoteAddURL:
		if value == "" {
			return nil, true
		}
		return m.gitRefToast(git.AddRemote(cwd, r.newRemote, value), "Add remote failed", "Added remote "+r.newRemote, value), true
	case promptKindGitRemoteRename:
		if value == "" || value == r.remote {
			return nil, true
		}
		old := r.remote
		err := git.RenameRemote(cwd, old, value)
		if err == nil {
			r.remote = value
		}
		return m.gitRefToast(err, "Rename failed", "Renamed remote "+old+" to "+value, ""), true
	case promptKindGitRemoteURL:
		if value == "" {
			return nil, true
		}
		return m.gitRefToast(git.SetRemoteURL(cwd, r.remote, value), "Change URL failed", "Changed the URL of "+r.remote, value), true
	}
	// promptKindGitForcePushTyped
	if value != r.branch {
		return noMatch(r.branch), true
	}
	branch, remote, lease := r.branch, r.remote, r.lease
	return m.gitRemoteOp("Force pushing "+branch+"…", func(cwd string) gitRemoteDoneMsg {
		return gitRemoteDoneMsg{failTitle: "Force push refused", okText: "Force pushed " + branch + " to " + remote, err: git.PushForceWithLease(cwd, remote, branch, lease)}
	}), true
}
