package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentpacks"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// Agent: Agent Packs… (OMX-46): install a pack of agents, skills, rules
// and commands (a local folder, or a git address pinned to a tag or
// commit) into Claude Code or Codex. The user picks the agent and the
// parts; the card lists every file it adds or replaces and the token
// cost, with Cancel first. The install stays pinned and one Enter on it
// opens the undo card, which puts the old files back.

const (
	pickerKindAgentPacks         pickerKindEnum  = 3787
	pickerKindAgentPacksTarget   pickerKindEnum  = 3788
	pickerKindAgentPacksParts    pickerKindEnum  = 3789
	promptKindAgentPacksSource   promptKindEnum  = 3790
	confirmKindAgentPacksInstall confirmKindEnum = 3791
	confirmKindAgentPacksUndo    confirmKindEnum = 3792
)

// agentPacksCardLines caps the file list on a card.
const agentPacksCardLines = 300

// agentPacksState is the open flow.
type agentPacksState struct {
	pack   *agentpacks.Pack
	target string
	plan   *agentpacks.Plan
	undo   string // the install the open undo card is about
}

func (m *Model) agentPacksS() *agentPacksState {
	s := m.aiS()
	if s.packs == nil {
		s.packs = &agentPacksState{}
	}
	return s.packs
}

// agentPacksStateDir holds the undo records (termocode's config folder,
// never inside a repo or an agent's folder).
func agentPacksStateDir() string {
	d, err := ai.ConfigDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "termocode")
	}
	return d
}

// agentPacksCacheDir holds git checkouts of packs.
func agentPacksCacheDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "termocode")
}

func agentPackTargetTitle(id string) string {
	if t, ok := agentpacks.TargetFor(id); ok {
		return t.Title
	}
	return id
}

// openAgentPacks lists Install a pack… and the installed packs.
func (m *Model) openAgentPacks() tea.Cmd {
	items := []picker.Item{{ID: "install", Title: "Install a pack…", Hint: "a folder, or a git address @version"}}
	recs, err := agentpacks.Installed(agentPacksStateDir())
	if err != nil {
		return m.aiInfo("Could not read the installed packs: " + agentCtlShort(err.Error()))
	}
	for _, r := range recs {
		hint := "pinned"
		if r.Commit != "" {
			hint += " at " + shortSHA(r.Commit)
		}
		hint += fmt.Sprintf(" · %d files · Enter to undo", len(r.Files))
		items = append(items, picker.Item{ID: "undo:" + r.ID, Title: r.Name + " " + r.Version + " · " + agentPackTargetTitle(r.Target), Hint: hint})
	}
	m.picker = picker.NewItems(" Agent Packs ", items)
	m.picker.SetNote([]string{"Ready agents, skills and rules for Claude Code and Codex. You see every file first; undo puts the old files back."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAgentPacks
	return nil
}

// onAgentPacksPicked asks for the source, or opens the undo card.
func (m *Model) onAgentPacksPicked(id string) tea.Cmd {
	if rid, ok := strings.CutPrefix(id, "undo:"); ok {
		return m.openAgentPackUndo(rid)
	}
	if id != "install" {
		return nil
	}
	m.prompt = prompt.New(" Install an agent pack ", "Pack folder, or git address with @tag or @commit:", "")
	m.prompt.SetNote("Read on this computer. Nothing is written before you see the list of files.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAgentPacksSource
	return nil
}

// onAgentPackSource reads the pack off the UI thread.
func (m *Model) onAgentPackSource(src string) tea.Cmd {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil
	}
	if strings.HasPrefix(src, "~/") {
		src = filepath.Join(agentHooksHome(), src[2:])
	}
	cache := agentPacksCacheDir()
	return func() tea.Msg {
		p, err := agentpacks.Open(context.Background(), src, cache)
		return applyMsg(func(m *Model) tea.Cmd {
			if err != nil {
				return m.aiInfo("Agent pack: " + agentCtlShort(err.Error()))
			}
			m.agentPacksS().pack = &p
			return m.openAgentPackTargets()
		})
	}
}

// openAgentPackTargets asks which agent gets the pack.
func (m *Model) openAgentPackTargets() tea.Cmd {
	p := m.agentPacksS().pack
	if p == nil {
		return nil
	}
	var items []picker.Item
	for _, t := range agentpacks.Targets() {
		hint := "~/" + t.Base
		if t.ID == "codex" {
			hint += " · skills and commands only"
		}
		items = append(items, picker.Item{ID: t.ID, Title: t.Title, Hint: hint})
	}
	m.picker = picker.NewItems(" "+p.Name+" "+p.Version+": install for ", items)
	m.picker.SetNote([]string{agentPackCounts(p.Count())})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAgentPacksTarget
	return nil
}

var agentPackKindTitle = map[agentpacks.Kind]string{
	agentpacks.KindAgent: "Agents", agentpacks.KindSkill: "Skills", agentpacks.KindRule: "Rules", agentpacks.KindCommand: "Commands",
}

func agentPackCounts(c map[agentpacks.Kind]int) string {
	var parts []string
	for _, k := range agentpacks.Kinds {
		if c[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c[k], strings.ToLower(agentPackKindTitle[k])))
		}
	}
	return "The pack has " + strings.Join(parts, ", ") + "."
}

// onAgentPacksTargetPicked asks which parts to install, with the cost.
func (m *Model) onAgentPacksTargetPicked(id string) tea.Cmd {
	s := m.agentPacksS()
	if s.pack == nil {
		return nil
	}
	s.target = id
	home, state := agentHooksHome(), agentPacksStateDir()
	all, err := agentpacks.PlanInstall(*s.pack, id, home, state, nil)
	if err != nil {
		return m.aiInfo("Agent pack: " + agentCtlShort(err.Error()))
	}
	items := []picker.Item{{ID: "all", Title: "Everything", Hint: agentPackCost(all)}}
	for _, k := range agentpacks.Kinds {
		pl, err := agentpacks.PlanInstall(*s.pack, id, home, state, []agentpacks.Kind{k})
		if err != nil {
			continue // no such part, or no place for it in this agent
		}
		items = append(items, picker.Item{ID: string(k), Title: fmt.Sprintf("%s (%d)", agentPackKindTitle[k], s.pack.Count()[k]), Hint: agentPackCost(pl)})
	}
	note := []string{"Tokens: what every chat of " + agentPackTargetTitle(id) + " loads, about 4 letters each."}
	if len(all.Skipped) > 0 {
		note = append(note, agentPackTargetTitle(id)+" has no agent or rule files: those parts are skipped.")
	}
	m.picker = picker.NewItems(" "+s.pack.Name+" → "+agentPackTargetTitle(id)+": what to install ", items)
	m.picker.SetNote(note)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAgentPacksParts
	return nil
}

func agentPackCost(pl agentpacks.Plan) string {
	return fmt.Sprintf("%d files · ~%s tokens in every chat", pl.Writes()+pl.Same(), agentPackNum(pl.Always))
}

func agentPackNum(n int) string {
	if n >= 10000 {
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
	return fmt.Sprint(n)
}

// onAgentPacksPartsPicked shows the install card.
func (m *Model) onAgentPacksPartsPicked(id string) tea.Cmd {
	s := m.agentPacksS()
	if s.pack == nil || s.target == "" {
		return nil
	}
	var kinds []agentpacks.Kind
	if id != "all" {
		kinds = []agentpacks.Kind{agentpacks.Kind(id)}
	}
	pl, err := agentpacks.PlanInstall(*s.pack, s.target, agentHooksHome(), agentPacksStateDir(), kinds)
	if err != nil {
		return m.aiInfo("Agent pack: " + agentCtlShort(err.Error()))
	}
	if pl.Writes() == 0 {
		return m.aiInfo(fmt.Sprintf("Nothing to change: the %d files are already the same", pl.Same()))
	}
	s.plan = &pl
	tt := agentPackTargetTitle(pl.Target)
	var b strings.Builder
	fmt.Fprintf(&b, "From %s", pl.Source)
	if pl.Commit != "" {
		fmt.Fprintf(&b, " (pinned at commit %s)", shortSHA(pl.Commit))
	}
	b.WriteString(".\n\n")
	replaced := 0
	for _, c := range pl.Changes {
		if c.Op == agentpacks.OpChange {
			replaced++
		}
	}
	fmt.Fprintf(&b, "Writes %d files", pl.Writes())
	if replaced > 0 {
		fmt.Fprintf(&b, ", %d of them replace your own files", replaced)
	}
	if pl.Same() > 0 {
		fmt.Fprintf(&b, " (%d are already the same)", pl.Same())
	}
	b.WriteString(":\n\n    ")
	b.WriteString(strings.Join(agentPackCap(pl.Lines()), "\n    "))
	fmt.Fprintf(&b, "\n\nToken cost: about %s tokens in every %s chat, and up to %s more when its agents, skills or commands are used.",
		agentPackNum(pl.Always), tt, agentPackNum(pl.OnUse))
	b.WriteString("\nUndo: Agent Packs… puts your old files back and removes the added ones.")
	m.confirm = confirm.Risky("Install "+pl.Name+" "+pl.Version+" for "+tt+"?", b.String(),
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "install", Title: "Install", Style: confirm.StylePrimary}).WithWideText().WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentPacksInstall
	return nil
}

func agentPackCap(lines []string) []string {
	if len(lines) <= agentPacksCardLines {
		return lines
	}
	out := append([]string(nil), lines[:agentPacksCardLines]...)
	return append(out, fmt.Sprintf("… and %d more files", len(lines)-agentPacksCardLines))
}

// answerAgentPackInstall writes the shown plan on Install.
func (m *Model) answerAgentPackInstall(id string) tea.Cmd {
	s := m.agentPacksS()
	pl := s.plan
	s.plan, s.pack, s.target = nil, nil, ""
	if pl == nil || id != "install" {
		return nil
	}
	if _, err := agentpacks.Apply(*pl, agentPacksStateDir()); err != nil {
		return m.aiInfo("Could not install " + pl.Name + ": " + agentCtlShort(err.Error()))
	}
	return m.aiInfo(fmt.Sprintf("%s %s installed for %s (%d files): start it again to load them", pl.Name, pl.Version, agentPackTargetTitle(pl.Target), pl.Writes()))
}

// openAgentPackUndo shows what the undo of install id does.
func (m *Model) openAgentPackUndo(id string) tea.Cmd {
	recs, err := agentpacks.Installed(agentPacksStateDir())
	if err != nil {
		return m.aiInfo("Could not read the installed packs: " + agentCtlShort(err.Error()))
	}
	for _, r := range recs {
		if r.ID != id {
			continue
		}
		m.agentPacksS().undo = id
		body := "Puts back your old files and removes the files the pack added:\n\n    " +
			strings.Join(agentPackCap(r.UndoLines()), "\n    ") +
			"\n\nA file you changed after the install is kept."
		m.confirm = confirm.Risky("Undo "+r.Name+" "+r.Version+" in "+agentPackTargetTitle(r.Target)+"?", body,
			confirm.Button{ID: "cancel", Title: "Cancel"},
			confirm.Button{ID: "undo", Title: "Undo", Style: confirm.StylePrimary}).WithWideText().WithScrollText()
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindAgentPacksUndo
		return nil
	}
	return m.aiInfo("That pack is not installed any more")
}

// answerAgentPackUndo runs the undo on Undo.
func (m *Model) answerAgentPackUndo(id string) tea.Cmd {
	s := m.agentPacksS()
	rid := s.undo
	s.undo = ""
	if rid == "" || id != "undo" {
		return nil
	}
	res, err := agentpacks.Undo(agentPacksStateDir(), rid)
	if err != nil {
		return m.aiInfo("Could not undo the pack: " + agentCtlShort(err.Error()))
	}
	msg := fmt.Sprintf("Pack undone: %d files back, %d removed", res.Restored, res.Removed)
	if len(res.Kept) > 0 {
		msg += fmt.Sprintf(", %d you changed kept", len(res.Kept))
	}
	return m.aiInfo(msg)
}
