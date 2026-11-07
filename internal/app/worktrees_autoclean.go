package app

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Auto clean-up (CMP-16) ───────────────────────────────────────────────
//
// Two Git settings, both off (0) by default: worktree_auto_archive_days
// archives a worktree with no activity for that many days, and
// worktree_max archives the oldest ones while more worktrees than that
// are live. The check runs when the worktree list is read (WORKTREES
// view, Jump, the web page), at most once an hour. It archives like x
// (WTX-14): the folder goes and frees its disk, the branch stays, and x
// on the row under ARCHIVED restores it in one step. It only takes clean
// worktrees termocode made or the user adopted: never the main
// checkout, the one this window is in, an agent lane, a locked, pinned
// or busy one, one with changed files, or one used in the last day
// (worktrees.AutoPicks). One toast names what went and the disk freed.

const (
	wtAutoDaysRowID = "setting-worktree-auto-archive-days"
	wtAutoMaxRowID  = "setting-worktree-max"
	wtAutoDaysKey   = "worktree_auto_archive_days"
	wtAutoMaxKey    = "worktree_max"
	wtAutoEvery     = time.Hour
	wtAutoDaysMax   = 3650
	wtAutoCapMax    = 1000
	wtAutoNoAI      = "archives worktree folders by itself; only the user turns it on"
)

func init() {
	settingsRows = append(settingsRows,
		settingsRow{ID: wtAutoDaysRowID, Label: wtAutoDaysKey, Kind: "int", Default: "0",
			Desc: "Archive a worktree with no activity for this many days by itself: its folder goes (git-ignored files like build output too), its branch stays, and x on its row under ARCHIVED brings it back. Only clean worktrees termocode made or you adopted; never pinned ones, ones with changed files or ones an agent works in. Checked when the worktree list is read, at most once an hour. 0 turns it off."},
		settingsRow{ID: wtAutoMaxRowID, Label: wtAutoMaxKey, Kind: "int", Default: "0",
			Desc: "Most worktrees to keep (the main checkout does not count). When there are more, the oldest clean ones are archived by themselves, by the same rules as worktree_auto_archive_days; one used in the last day is never taken. 0 means no limit."})
	aiSettingPolicies[wtAutoDaysKey] = aiNever(wtAutoNoAI)
	aiSettingPolicies[wtAutoMaxKey] = aiNever(wtAutoNoAI)
}

// wtAutoSettings reads the two keys from config.json; a missing or bad
// value is 0 (off).
func wtAutoSettings() (days, maxN int) {
	path, err := settingsConfigPath()
	if err != nil {
		return 0, 0
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0
	}
	var s struct {
		Days *float64 `json:"worktree_auto_archive_days"`
		Max  *float64 `json:"worktree_max"`
	}
	if json.Unmarshal(b, &s) != nil {
		return 0, 0
	}
	get := func(v *float64, hi int) int {
		if v == nil || *v < 0 || *v > float64(hi) || *v != float64(int(*v)) {
			return 0
		}
		return int(*v)
	}
	return get(s.Days, wtAutoDaysMax), get(s.Max, wtAutoCapMax)
}

// wtAutoSettingsCategories puts the rows in the Git group.
func wtAutoSettingsCategories(cats map[string]string) map[string]string {
	cats[wtAutoDaysRowID] = "Git"
	cats[wtAutoMaxRowID] = "Git"
	return cats
}

// wtAutoSettingValue renders a row's value. ok=false: not ours.
func wtAutoSettingValue(id string) (string, bool) {
	days, maxN := wtAutoSettings()
	switch id {
	case wtAutoDaysRowID:
		return strconv.Itoa(days), true
	case wtAutoMaxRowID:
		return strconv.Itoa(maxN), true
	}
	return "", false
}

// applyWTAutoSetting checks and saves a row. ok=false: not ours.
func (m *Model) applyWTAutoSetting(id, value string) (tea.Cmd, bool) {
	key, hi := wtAutoDaysKey, wtAutoDaysMax
	switch id {
	case wtAutoDaysRowID:
	case wtAutoMaxRowID:
		key, hi = wtAutoMaxKey, wtAutoCapMax
	default:
		return nil, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 || n > hi {
		return m.pushToastDetail(toast.Errr, key+": expected an integer 0…"+strconv.Itoa(hi)+" (0 = off)", ""), true
	}
	if err := saveHTTPSetting(key, n); err != nil {
		return m.pushToastDetail(toast.Errr, "Settings error", err.Error()), true
	}
	if m.wt != nil {
		m.wt.autoAt = time.Time{} // the next list read checks again
	}
	detail := "Off."
	if n > 0 {
		detail = "Checked the next time the worktree list is read, then at most once an hour."
	}
	return m.pushToastDetail(toast.Info, key+" = "+strconv.Itoa(n), detail), true
}

// wtAutoDone is one worktree the auto clean-up archived.
type wtAutoDone struct {
	path, title, why string
}

// wtAutoCleanedMsg is the end of an auto clean-up.
type wtAutoCleanedMsg struct {
	done  []wtAutoDone
	freed int64 // bytes
	err   error
}

// wtAutoClean runs the auto clean-up when it is on and due (nil: not).
func (m *Model) wtAutoClean() tea.Cmd {
	st := m.wt
	if st == nil || st.off || st.autoBusy || !st.loaded || st.err != "" || st.common == "" {
		return nil
	}
	now := clock.Now()
	if !st.autoAt.IsZero() && now.Sub(st.autoAt) < wtAutoEvery {
		return nil
	}
	st.autoAt = now // also when off: config.json is read once an hour, not on every poll
	days, maxN := wtAutoSettings()
	if days == 0 && maxN == 0 {
		return nil
	}
	type cand struct {
		c             worktrees.AutoCand
		title, branch string
	}
	var cands []cand
	live := 0
	items, _ := m.wtList()
	for _, it := range items {
		if it.e.Kind == worktrees.KindMain || it.e.Gone || it.e.Meta.State == worktrees.StateArchived {
			continue
		}
		live++
		if t, _ := wtRefuse(it); t != "" || (it.e.Kind == worktrees.KindExternal && !it.e.Meta.Adopted) {
			continue
		}
		tabs, _ := m.wtAgentTabsIn([]string{it.e.Path})
		last := it.e.Meta.LastSeen
		if it.e.Meta.Created.After(last) {
			last = it.e.Meta.Created
		}
		cands = append(cands, cand{title: wtTitle(it), branch: it.e.Branch,
			c: worktrees.AutoCand{Path: it.e.Path, Pinned: it.e.Meta.Pinned, Busy: it.agents > 0 || len(tabs) > 0, LastActive: last}})
	}
	if len(cands) == 0 {
		return nil
	}
	st.autoBusy = true
	root, common := m.wtRootDir(), st.common
	return func() tea.Msg {
		var msg wtAutoCleanedMsg
		facts := make([]worktrees.AutoCand, 0, len(cands))
		byPath := map[string]cand{}
		for _, c := range cands {
			f := c.c
			if n, err := wtDirtyRun(f.Path); err != nil {
				f.Dirty = -1
			} else {
				f.Dirty = n
			}
			if ht := git.HeadCommitTime(f.Path); ht.After(f.LastActive) {
				f.LastActive = ht
			}
			facts = append(facts, f)
			byPath[f.Path] = c
		}
		for _, p := range worktrees.AutoPicks(facts, live, days, maxN, now) {
			c := byPath[p.Path]
			size := worktrees.DirSize(p.Path)
			if err := wtRemoveRun(root, p.Path, false); err != nil { // never forced: a file changed since is kept
				msg.err = err
				return msg
			}
			msg.freed += size
			msg.done = append(msg.done, wtAutoDone{path: p.Path, title: c.title, why: p.Why})
			if err := worktrees.Update(common, func(mm map[string]worktrees.Meta) {
				md := mm[p.Path]
				md.State, md.Branch, md.LastSeen = worktrees.StateArchived, c.branch, clock.Now()
				mm[p.Path] = md
			}); err != nil {
				msg.err = err
				return msg
			}
		}
		return msg
	}
}

// onWTAutoCleaned tells what the auto clean-up did (nothing: no toast).
func (m *Model) onWTAutoCleaned(msg wtAutoCleanedMsg) tea.Cmd {
	m.wt.autoBusy = false
	var lines []string
	for _, d := range msg.done {
		lines = append(lines, d.title+" — "+d.why)
	}
	if msg.err != nil {
		return tea.Batch(m.pushToastDetail(toast.Warn, "The auto clean-up stopped", strings.Join(append(lines, msg.err.Error()), "\n")), m.worktreesReload())
	}
	if len(msg.done) == 0 {
		return nil
	}
	lines = append(lines, "Freed "+humanBytes(msg.freed)+". The branches stay; x on its row under ARCHIVED brings a folder back.")
	return tea.Batch(m.pushToastDetail(toast.Info, "Auto-archived "+wtPlural(len(msg.done), "worktree"), strings.Join(lines, "\n")), m.worktreesReload())
}
