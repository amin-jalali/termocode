package app

import (
	"os"
	"path/filepath"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentroll"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The live board across projects (MOB-54): board.projects lists every
// project whose termocode window runs agents, from the windows' roll-ups
// (OMX-35), with this window's project first. Reading the roll-ups is a
// few small files; nothing ticks: the page asks again while the board is
// open, and a running project's details come over the door (MOB-50).

func init() {
	webOps[webproto.OpBoardProjects] = (*Model).webBoardProjects
}

func (m *Model) webBoardProjects(call *webCall) tea.Cmd {
	call.reply(webBoardBuild(aiWorkspaceRoot(), m.agentRollupItems(), agentRollupOthers(), tildePath))
	return nil
}

// webBoardBuild is the board's project rows: home (with its own agents)
// first, then the other roll-ups (one row per workspace, the newest
// roll-up when two windows share one), needs-you first, then working,
// then by name; capped at MaxBoardProjects. where shows a folder for
// people (~ for home).
func webBoardBuild(home string, own []agentroll.Item, others []agentroll.Rollup, where func(string) string) webproto.BoardProjects {
	homeWS := session.Key(home)
	out := webproto.BoardProjects{Home: homeWS, Projects: []webproto.BoardProject{}}
	out.Projects = append(out.Projects, webBoardRow(home, homeWS, own, 0, where))
	out.Projects[0].Home = true

	byWS := map[string]agentroll.Rollup{}
	for _, r := range others {
		if r.Workspace == "" {
			continue
		}
		ws := session.Key(r.Workspace)
		if ws == homeWS {
			continue // another window of the home project: its agents are its own
		}
		if old, ok := byWS[ws]; ok && old.UpdatedMs >= r.UpdatedMs {
			continue
		}
		byWS[ws] = r
	}
	rest := make([]webproto.BoardProject, 0, len(byWS))
	for ws, r := range byWS {
		rest = append(rest, webBoardRow(r.Workspace, ws, r.Items, r.UpdatedMs, where))
	}
	rank := map[string]int{webproto.BoardNeeds: 0, webproto.BoardWorking: 1, webproto.BoardIdle: 2}
	sort.Slice(rest, func(i, j int) bool {
		a, b := rest[i], rest[j]
		if rank[a.State] != rank[b.State] {
			return rank[a.State] < rank[b.State]
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.WS < b.WS
	})
	if room := webproto.MaxBoardProjects - 1; len(rest) > room {
		out.More = len(rest) - room
		rest = rest[:room]
	}
	out.Projects = append(out.Projects, rest...)
	return out
}

// webBoardRow is one project row from its agents.
func webBoardRow(dir, ws string, items []agentroll.Item, seen int64, where func(string) string) webproto.BoardProject {
	p := webproto.BoardProject{WS: ws, Name: webName(filepath.Base(filepath.Clean(dir))), Where: webName(where(filepath.Clean(dir))), Seen: seen}
	// waiting agents first: they are what the user should see
	sorted := append([]agentroll.Item(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].State == "waiting" && sorted[j].State != "waiting" })
	for _, it := range sorted {
		if it.State != "working" && it.State != "waiting" {
			continue
		}
		if it.State == "waiting" {
			p.Waiting++
		} else {
			p.Working++
		}
		if len(p.Agents) == webproto.MaxBoardAgents {
			p.More++
			continue
		}
		p.Agents = append(p.Agents, webproto.BoardAgent{Name: webName(it.Name), State: it.State})
	}
	p.State = webproto.BoardState(p.Working, p.Waiting)
	if p.Name == "" || p.Name == "." || p.Name == string(os.PathSeparator) {
		p.Name = p.Where
	}
	return p
}
