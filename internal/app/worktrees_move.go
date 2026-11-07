package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/lanemove"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Move a lane to another machine (CMP-6) ──────────────────────────────
//
// M in WORKTREES (Worktree: Move Selected to Another Machine…) packs the
// cursor's worktree into one .tlane file (internal/lanemove): the commits
// the remotes lack, the changes not committed, the new files, its notes
// and — while agent transcripts are allowed — its Claude Code chats. The
// card asks first (Cancel first and focused; Esc cancels) and can also
// download the file to this computer (over ssh: through the terminal).
// Nothing in the worktree changes; the card says to archive it here once
// it runs there.
//
// U (Worktree: Unpack a Moved Lane…) lists the packs in the lanes folder
// and ~/Downloads; its first row pulls one from a host over ssh
// (Worktree: Pull a Lane from a Host…: host:folder, then a card). Unpack
// always makes a NEW worktree next to the others: a new folder, a free
// branch name, never a file written over. A card says what comes and
// where it goes, Cancel first.

const (
	confirmKindWTMove   confirmKindEnum = 6611
	confirmKindWTUnpack confirmKindEnum = 6612
	confirmKindWTPull   confirmKindEnum = 6613
	pickerKindWTUnpack  pickerKindEnum  = 6614
	promptKindWTPull    promptKindEnum  = 6615

	wtMovePaletteID   = "worktree-move"
	wtUnpackPaletteID = "worktree-unpack"
	wtPullPaletteID   = "worktree-pull"

	wtPullRowID  = "pull"
	wtPackPfx    = "pack:"
	wtPullWait   = 10 * time.Minute
	wtMoveNoteAI = "acts on the worktree the user selected in the WORKTREES view"
)

// wtMovePending is the open move, unpack or pull card.
type wtMovePending struct {
	// move
	path, title string
	meta        worktrees.Meta
	// unpack
	file   string
	man    lanemove.Manifest
	branch string
	// pull
	host, dir string
}

// Tests swap these: the pack, unpack and pull steps.
var (
	wtLanePack   = lanemove.PackFile
	wtLaneUnpack = lanemove.UnpackFile
	wtLaneFetch  = lanemove.Fetch
	wtLaneHome   = os.UserHomeDir
)

// wtLanesDir is the folder packs are kept in.
func wtLanesDir() string {
	home, _ := wtLaneHome()
	return lanemove.Dir(os.Getenv, home)
}

// wtLaneClaudeHome is ~/.claude while agent transcripts are allowed,
// else "" (the chats stay out).
func wtLaneClaudeHome() string {
	if !agentTranscriptsOn() {
		return ""
	}
	return agentlogHomes().Claude
}

// ── Move (pack) ──

// worktreeMove is M: ask, then pack the cursor's worktree.
func (m *Model) worktreeMove() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	switch {
	case it.e.Gone:
		return m.pushToastDetail(toast.Info, "Its folder is gone", "F forgets it.")
	case it.e.Meta.State == worktrees.StateArchived:
		return m.pushToastDetail(toast.Info, "This worktree is archived", "x restores its folder first; then M moves it.")
	}
	if m.confirmOpen {
		return nil
	}
	title := wtTitle(it)
	var b strings.Builder
	b.WriteString("Packs " + wtShortPath(it.e.Path) + " into one file, to go on with it on another machine:\n" +
		"the commits the remotes lack, the changes not committed, the new files and its notes")
	if agentTranscriptsOn() {
		b.WriteString(", and the Claude Code chats of the folder")
	}
	b.WriteString(".\n\nNothing here changes. Once it runs there, archive it here (x).")
	if _, names := m.wtAgentTabsIn([]string{it.e.Path}); len(names) > 0 {
		b.WriteString("\n\nIts agent tabs keep running here (" + strings.Join(names, ", ") +
			"). s puts them to sleep first, so nothing changes while it moves.")
	}
	if !agentTranscriptsOn() {
		b.WriteString("\n\nAgent chats stay out: reading them is off in Settings (agent transcripts).")
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "<this host>"
	}
	b.WriteString("\n\nThere: termocode lane pull " + host + ":" + it.e.Path + ", or Worktree: Unpack a Moved Lane… with the file.")
	m.wt.move = &wtMovePending{path: it.e.Path, title: title, meta: it.e.Meta}
	m.confirm = confirm.Risky("Move "+title+" to another machine?", b.String(),
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "pack", Title: "Pack lane", Style: confirm.StylePrimary},
		confirm.Button{ID: "download", Title: "Pack and download"}).WithWideText().WithScrollText() // paths and the pull command stay readable
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWTMove
	return nil
}

// answerWTMove answers the move card ("" = Esc = Cancel).
func (m *Model) answerWTMove(id string) tea.Cmd {
	p := m.wt.move
	m.wt.move = nil
	if p == nil || (id != "pack" && id != "download") {
		return nil
	}
	download := id == "download"
	dir, opt := p.path, lanemove.PackOptions{Meta: p.meta, ClaudeHome: wtLaneClaudeHome()}
	lanes := wtLanesDir()
	return func() tea.Msg {
		file, man, err := wtLanePack(dir, "", lanes, opt)
		return applyMsg(func(m *Model) tea.Cmd {
			if err != nil {
				return m.pushToastDetail(toast.Warn, "The lane could not be packed", err.Error())
			}
			host := man.Host
			if host == "" {
				host = "this-host"
			}
			detail := wtShortPath(file) + " · " + wtLaneWhat(man) + ". There: termocode lane pull " + host + ":" + man.Path +
				", or Worktree: Unpack a Moved Lane… with the file."
			cmds := []tea.Cmd{m.pushToastDetail(toast.Info, "Packed "+man.Title(), detail)}
			if download {
				cmds = append(cmds, m.downloadPath(file))
			}
			return tea.Batch(cmds...)
		})
	}
}

// wtLaneWhat is what a pack holds, in a few words.
func wtLaneWhat(man lanemove.Manifest) string {
	parts := []string{wtPlural(man.Commits, "commit")}
	if man.Changed > 0 {
		parts = append(parts, wtPlural(man.Changed, "changed file"))
	}
	if n := len(man.Files); n > 0 {
		parts = append(parts, wtPlural(n, "new file"))
	}
	if n := len(man.Chats); n > 0 {
		parts = append(parts, wtPlural(n, "agent chat"))
	}
	if n := len(man.Notes); n > 0 {
		parts = append(parts, wtPlural(n, "note"))
	}
	return strings.Join(parts, " · ")
}

// ── Unpack ──

// worktreeUnpack is U: the packs here, and a row to pull one.
func (m *Model) worktreeUnpack() tea.Cmd {
	if m.wt == nil {
		return nil
	}
	home, _ := wtLaneHome()
	items := []picker.Item{{ID: wtPullRowID, Title: "Pull a lane from a host…", Hint: "over ssh: host:folder"}}
	files := lanemove.Packs(wtLanesDir(), filepath.Join(home, "Downloads"))
	if len(files) > 0 {
		items = append(items, picker.Item{Header: true, Title: "Packs", Group: "packs"})
	}
	for _, f := range files {
		hint := ""
		if info, err := os.Stat(f); err == nil {
			hint = humanBytes(info.Size()) + " · " + info.ModTime().Format("Jan 2 15:04")
		}
		items = append(items, picker.Item{ID: wtPackPfx + f, Title: wtShortPath(f), Hint: hint, Group: "packs", File: true})
	}
	m.picker = picker.NewItems(" Unpack a Moved Lane ", items)
	if len(files) == 0 {
		m.picker.SetNote([]string{"No packs yet: M on a worktree packs one; copy it to " + wtShortPath(wtLanesDir())})
	}
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindWTUnpack
	return nil
}

// onWTUnpackPicked reads the picked pack, then asks.
func (m *Model) onWTUnpackPicked(id string) tea.Cmd {
	if id == wtPullRowID {
		return m.worktreePull()
	}
	file, ok := strings.CutPrefix(id, wtPackPfx)
	if !ok {
		return nil
	}
	return m.wtUnpackAsk(file)
}

// wtUnpackAsk reads the pack and where it would go (off the UI thread),
// then opens the card.
func (m *Model) wtUnpackAsk(file string) tea.Cmd {
	root, _ := m.rootDir()
	setting := worktreeDirSetting()
	home, _ := wtLaneHome()
	return func() tea.Msg {
		man, err := lanemove.InspectFile(file)
		var pl lanemove.Place
		if err == nil {
			pl, err = lanemove.PlaceFor(root, man, "", setting, home)
		}
		return applyMsg(func(m *Model) tea.Cmd {
			if err != nil {
				return m.pushToastDetail(toast.Warn, "This pack cannot be unpacked here", err.Error())
			}
			return m.openWTUnpackCard(file, man, pl)
		})
	}
}

// openWTUnpackCard asks before a new worktree is made from a pack.
func (m *Model) openWTUnpackCard(file string, man lanemove.Manifest, pl lanemove.Place) tea.Cmd {
	if m.confirmOpen || m.wt == nil {
		return nil
	}
	from := man.Path
	if man.Host != "" {
		from = man.Host + ":" + from
	}
	var b strings.Builder
	b.WriteString("From " + from + ", packed " + man.PackedAt.Local().Format("Jan 2 15:04") + ".\n\n" +
		"A new worktree is made here:\n  folder " + wtShortPath(pl.Folder) + "\n  branch " + pl.Branch +
		"\n\nWith " + wtLaneWhat(man) + ". Nothing here is written over.")
	if n := len(man.Skipped); n > 0 {
		b.WriteString("\n\n" + wtPlural(n, "new file") + " stayed on the other machine (too big, or not plain files).")
	}
	if len(man.Chats) > 0 && !agentTranscriptsOn() {
		b.WriteString("\n\nIts agent chats stay out: reading them is off in Settings (agent transcripts).")
	}
	m.wt.move = &wtMovePending{file: file, man: man, branch: pl.Branch}
	m.confirm = confirm.Risky("Unpack "+man.Title()+"?", b.String(),
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "unpack", Title: "Unpack as a new worktree", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWTUnpack
	return nil
}

// answerWTUnpack answers the unpack card ("" = Esc = Cancel).
func (m *Model) answerWTUnpack(id string) tea.Cmd {
	p := m.wt.move
	m.wt.move = nil
	if p == nil || id != "unpack" {
		return nil
	}
	return m.wtUnpackRun(p.file, p.branch)
}

// wtUnpackRun unpacks file off the UI thread and shows the new worktree.
func (m *Model) wtUnpackRun(file, branch string) tea.Cmd {
	root, _ := m.rootDir()
	home, _ := wtLaneHome()
	in := lanemove.Into{Dir: root, Name: branch, Setting: worktreeDirSetting(), Home: home, ClaudeHome: wtLaneClaudeHome()}
	return func() tea.Msg {
		res, err := wtLaneUnpack(file, in)
		return applyMsg(func(m *Model) tea.Cmd { return m.onWTUnpacked(res, err) })
	}
}

// onWTUnpacked says what was made and puts the cursor on it.
func (m *Model) onWTUnpacked(res lanemove.Result, err error) tea.Cmd {
	if err != nil && res.Folder == "" {
		return m.pushToastDetail(toast.Warn, "The lane could not be unpacked", err.Error())
	}
	if m.wt != nil {
		m.wt.cursorPath = res.Folder
	}
	cmds := []tea.Cmd{m.worktreesReload()}
	switch {
	case err != nil:
		cmds = append(cmds, m.pushToastDetail(toast.Warn, "Unpacked "+res.Manifest.Title()+", not all of it", err.Error()))
	case res.PatchAt != "":
		cmds = append(cmds, m.pushToastDetail(toast.Warn, "Unpacked "+res.Manifest.Title()+"; its changes did not apply",
			"The worktree is on its commit. The changes are in "+wtShortPath(res.PatchAt)+" (git apply them by hand)."))
	default:
		detail := wtShortPath(res.Folder) + " · " + res.Summary() + ". Enter opens it."
		if res.Chats > 0 {
			detail += " Agent: Sessions… resumes its chats."
		}
		cmds = append(cmds, m.pushToastDetail(toast.Info, "Unpacked "+res.Manifest.Title(), detail))
	}
	return tea.Batch(cmds...)
}

// ── Pull over ssh ──

// worktreePull asks for host:folder.
func (m *Model) worktreePull() tea.Cmd {
	if m.wt == nil {
		return nil
	}
	m.prompt = prompt.New(" Pull a lane from a host ", "The lane's folder on the other machine, as host:folder:", "")
	m.prompt.SetNote("Over your own ssh (keys, ~/.ssh/config); termocode must be on that machine too.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindWTPull
	return nil
}

// onWTPullEntered checks host:folder, then asks.
func (m *Model) onWTPullEntered(value string) tea.Cmd {
	value = strings.TrimSpace(value)
	if value == "" || m.wt == nil {
		return nil
	}
	host, dir, err := lanemove.ParseRemote(value)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Not host:folder", err.Error())
	}
	if m.confirmOpen {
		return nil
	}
	body := "termocode on " + host + " packs the lane in " + dir + " over ssh, and a new worktree is made here from it" +
		" (the commits, the changes not committed, the new files and its notes"
	if agentTranscriptsOn() {
		body += ", and its Claude Code chats"
	}
	body += ").\n\nssh asks no password here: it uses your keys and agent. Nothing on " + host +
		" changes; archive the lane there once it runs here. A card shows the new folder and branch before anything is made."
	m.wt.move = &wtMovePending{host: host, dir: dir}
	m.confirm = confirm.Risky("Pull a lane from "+host+"?", body,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "pull", Title: "Pull lane", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWTPull
	return nil
}

// answerWTPull answers the pull card: the pack comes into the lanes
// folder, then the unpack card opens on it.
func (m *Model) answerWTPull(id string) tea.Cmd {
	p := m.wt.move
	m.wt.move = nil
	if p == nil || id != "pull" {
		return nil
	}
	host, dir, chats, lanes := p.host, p.dir, agentTranscriptsOn(), wtLanesDir()
	pull := m.pushToastDetail(toast.Info, "Pulling the lane from "+host+"…", "termocode there packs it over ssh.")
	work := func() tea.Msg {
		file, err := wtPullFile(host, dir, chats, lanes)
		return applyMsg(func(m *Model) tea.Cmd {
			if err != nil {
				return m.pushToastDetail(toast.Warn, "The lane could not be pulled from "+host, err.Error())
			}
			return m.wtUnpackAsk(file)
		})
	}
	return tea.Batch(work, pull) // the work first (tests run only it)
}

// wtPullFile fetches the pack of host:dir into the lanes folder.
func wtPullFile(host, dir string, chats bool, lanes string) (string, error) {
	if err := os.MkdirAll(lanes, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(lanes, ".pull-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	ctx, cancel := context.WithTimeout(context.Background(), wtPullWait)
	defer cancel()
	err = wtLaneFetch(ctx, host, dir, chats, nil, true, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	var man lanemove.Manifest
	if err == nil {
		man, err = lanemove.InspectFile(tmp)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	file := filepath.Join(lanes, lanemove.FileName(man))
	for i := 2; ; i++ {
		if _, err := os.Lstat(file); err != nil {
			break
		}
		file = filepath.Join(lanes, strings.TrimSuffix(lanemove.FileName(man), lanemove.Ext)+"-"+strconv.Itoa(i)+lanemove.Ext)
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return file, nil
}

func init() {
	aiActionPolicies[wtMovePaletteID] = aiNever(wtMoveNoteAI)
	aiActionPolicies[wtUnpackPaletteID] = aiNever(aiNoPicker)
	aiActionPolicies[wtPullPaletteID] = aiNever("runs ssh to a host the user types")
}
