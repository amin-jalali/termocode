package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentroll"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/svcinstall"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Projects for the phone (MOB-8): every project the laptop knows, one
// row per folder, from the recent workspaces, the running sessions'
// Info files, the installed services and the other windows' agent
// roll-ups, with how many agents work or wait for the user. Read only.
// The files are read off the Update goroutine, only while a browser
// listens: once when it subscribes, then again on an Update at most every
// webProjectsEvery (nothing ticks: with nothing running, nothing wakes
// up). This window's own agents show at once.

func init() {
	webTopics["projects"] = webTopicDef{snapshot: webProjectsSnapshot, apply: webApplyReplace[webproto.ProjectsSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishProjects)
}

// webProjectsEvery is how old the files' read may get before an Update
// reads them again.
const webProjectsEvery = 10 * time.Second

// projectsInput is what the laptop's files say of its projects.
type projectsInput struct {
	recent   []WorkspaceEntry
	sessions []session.Info
	services []string
	others   []agentroll.Rollup
}

// webProjectsRead reads the files (a seam for tests); it runs off the
// Update goroutine.
var webProjectsRead = func() projectsInput {
	in := projectsInput{recent: loadWorkspaces(), sessions: webProjectsSessions(), others: agentRollupOthers()}
	if home, err := os.UserHomeDir(); err == nil {
		in.services = svcinstall.Installed(runtime.GOOS, home)
	}
	return in
}

// webProjectsLoaded carries a read to Update.
type webProjectsLoaded struct{ in projectsInput }

type webProjectsPub struct {
	in      *projectsInput // the last read; nil before the first
	readAt  time.Time
	loading bool   // a read is on its way
	dirty   bool   // a read came since the last build
	sig     string // this window's agents at the last build
	last    *webproto.ProjectsSnap
}

type webProjectsDelta webproto.ProjectsSnap

func (webProjectsDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webProjectsDelta)
	return n, ok
}

// webProjectID is the page's name for the project in folder p.
func webProjectID(p string) string {
	sum := sha256.Sum256([]byte(p))
	return hex.EncodeToString(sum[:])[:webproto.ProjectIDLen]
}

// projectWhere is p for people: ~-relative, or its name outside home
// (no absolute path goes to the page).
func projectWhere(p, home string) string {
	if w := homeShort(p, home); w != p {
		return w
	}
	return filepath.Base(p)
}

func webProjectsSnapshot(m *Model, _ string) (any, error) {
	snap := m.webProjectsBuild()
	if m.web != nil {
		m.web.projects.last, m.web.projects.sig = &snap, m.agentRollSig
	}
	return snap, nil
}

// webProjectsBuild is the projects topic now.
func (m *Model) webProjectsBuild() webproto.ProjectsSnap {
	here, _ := m.rootDir()
	home, _ := os.UserHomeDir()
	var in *projectsInput
	if m.web != nil {
		in = m.web.projects.in
	}
	return projectsMerge(here, m.agentRollupItems(), in, home)
}

// webPublishProjects starts a read when the first browser listens or the
// last read is old, and sends the list when it changed.
func (m *Model) webPublishProjects(msg tea.Msg) tea.Cmd {
	h := m.web
	p := &h.projects
	if !h.webListens("projects") {
		*p = webProjectsPub{}
		return nil
	}
	if l, ok := msg.(webProjectsLoaded); ok {
		in := l.in
		p.in, p.readAt, p.loading, p.dirty = &in, webNow(), false, true
	}
	var cmd tea.Cmd
	if !p.loading && (p.in == nil || webNow().Sub(p.readAt) >= webProjectsEvery) {
		p.loading = true
		read := webProjectsRead
		cmd = func() tea.Msg { return webProjectsLoaded{in: read()} }
	}
	if p.last != nil && !p.dirty && p.sig == m.agentRollSig {
		return cmd
	}
	p.dirty, p.sig = false, m.agentRollSig
	cur := m.webProjectsBuild()
	if p.last != nil && reflect.DeepEqual(*p.last, cur) {
		return cmd
	}
	p.last = &cur
	h.publish("projects", webProjectsDelta(cur))
	return cmd
}

// projectsMerge makes the list: one row per folder (cleaned), this
// window's first, then the ones whose agents wait, the working ones, the
// running ones and the newest; at most MaxProjects. in nil: not read yet
// ("loading", this window's project only).
func projectsMerge(here string, own []agentroll.Item, in *projectsInput, home string) webproto.ProjectsSnap {
	s := webproto.ProjectsSnap{State: "loading", Projects: []webproto.ProjectItem{}}
	by := map[string]*webproto.ProjectItem{}
	var order []*webproto.ProjectItem
	get := func(p string) *webproto.ProjectItem {
		if p == "" || !filepath.IsAbs(p) {
			return nil
		}
		p = filepath.Clean(p)
		if it := by[p]; it != nil {
			return it
		}
		it := &webproto.ProjectItem{ID: webProjectID(p), Name: webName(filepath.Base(p)), Where: webName(projectWhere(p, home)), State: "asleep"}
		by[p] = it
		order = append(order, it)
		return it
	}
	run := func(it *webproto.ProjectItem) {
		if it.State != "here" {
			it.State = "running"
		}
	}
	if it := get(here); it != nil {
		it.State = "here"
		it.Working, it.Waiting = agentroll.Rollup{Items: own}.Counts()
	}
	if in != nil {
		s.State = "ready"
		for _, e := range in.recent {
			if it := get(e.Path); it != nil {
				it.Recent = true
				if !e.OpenedAt.IsZero() && e.OpenedAt.UnixMilli() > it.OpenedAt {
					it.OpenedAt = e.OpenedAt.UnixMilli()
				}
			}
		}
		for _, si := range in.sessions {
			if it := get(si.Workspace); it != nil {
				run(it)
				it.Attached = it.Attached || si.Attached
			}
		}
		for _, ws := range in.services {
			if it := get(ws); it != nil {
				it.Service = true
			}
		}
		for _, r := range in.others {
			if it := get(r.Workspace); it != nil {
				run(it)
				w, x := r.Counts()
				it.Working += w
				it.Waiting += x
			}
		}
	}
	rank := func(it *webproto.ProjectItem) int {
		switch {
		case it.State == "here":
			return 0
		case it.Waiting > 0:
			return 1
		case it.Working > 0:
			return 2
		case it.State == "running":
			return 3
		}
		return 4
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra < rb
		}
		if a.OpenedAt != b.OpenedAt {
			return a.OpenedAt > b.OpenedAt
		}
		return a.Where < b.Where
	})
	for _, it := range order {
		s.Working += it.Working
		s.Waiting += it.Waiting
		if len(s.Projects) == webproto.MaxProjects {
			s.More++
			continue
		}
		s.Projects = append(s.Projects, *it)
	}
	return s
}
