package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/raceview"
)

// ── The race compare page (AGX-5, plan 2.5) ─────────────────────────────

// raceAct runs a key of the compare page.
func (m *Model) raceAct(a raceview.ActionMsg) tea.Cmd {
	s := m.raceS()
	r := s.current()
	switch a.Action {
	case raceview.ActClose:
		s.pageOn = false
		return nil
	case raceview.ActStop:
		if r == nil {
			return nil
		}
		return m.raceStop(r)
	}
	return nil
}

// raceStatsMsg carries lanes' changed files read off the UI goroutine.
type raceStatsMsg struct {
	race  string
	files map[string][]agentrace.FileStat
	errs  map[string]string
}

// raceStatsCmd reads the changed files of lanes of race.
func raceStatsCmd(root, race string, lanes []string) tea.Cmd {
	return func() tea.Msg {
		msg := raceStatsMsg{race: race, files: map[string][]agentrace.FileStat{}, errs: map[string]string{}}
		for _, id := range lanes {
			st, err := git.AgentNumstat(root, id)
			if err != nil {
				msg.errs[id] = aiOneLine(err.Error())
				continue
			}
			fs := make([]agentrace.FileStat, len(st))
			for i, f := range st {
				fs[i] = agentrace.FileStat{Status: f.Status, Path: f.Path, Add: f.Add, Del: f.Del, Binary: f.Binary}
			}
			msg.files[id] = fs
		}
		return msg
	}
}

func (m *Model) handleRaceCompareMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case raceStatsMsg:
		r := m.raceS().find(msg.race)
		if r == nil {
			return nil, true
		}
		for id, fs := range msg.files {
			if l := r.Lane(id); l != nil {
				l.Files, l.Stats = fs, true
			}
		}
		m.raceRefreshPage()
		return m.raceSave(), true
	}
	return nil, false
}
