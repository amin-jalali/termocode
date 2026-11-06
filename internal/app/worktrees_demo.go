package app

import (
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/agentroll"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/worktreeform"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// wtCannedCache is a status cache filled from a table, without git (the
// demo and tests): every value is in when it returns.
func wtCannedCache(vals map[string]git.WorktreeStatus) *worktrees.StatusCache {
	c := worktrees.NewStatusCache(func(p string) (git.WorktreeStatus, error) { return vals[p], nil })
	paths := make([]string, 0, len(vals))
	for p := range vals {
		paths = append(paths, p)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	if c.Refresh(paths, wg.Done) {
		wg.Wait()
	}
	return c
}

// worktreesDemo is the WORKTREES view with a canned repository (dev
// action "worktreesdemo", for the screenshots and goldens; git is never
// called). The variant puts the cursor on a card: "running" (default),
// "waiting", "asleep", "external"; "keys" opens the ? card; "filter"
// shows the Dirty filter with the text "fix"; "jump" opens Worktree:
// Jump…; "link" the link card; "nested" a child, a linked PR with an
// agent status and an external worktree with no meta; "notes" the N
// card; W4: "sleep" the sleep card of a parent, "archive" the archive
// card of a dirty worktree, "delete" the type-the-name step, "cleanup"
// the cleanup list, "archived" the list with an archived row.
func (m *Model) worktreesDemo(variant string) {
	now := clock.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	list := []git.Worktree{
		{Path: "/src/app", Branch: "main", Main: true, Head: "4f1c2a9e0b7d"},
		{Path: "/src/app.worktrees/wt-sidebar", Branch: "feat/wt-sidebar"},
		{Path: "/src/app.worktrees/toast-race", Branch: "fix/toast-race"},
		{Path: "/src/app.worktrees/grpc", Branch: "spike/grpc"},
		{Path: "/src/app/.git/termocode/worktrees/r9-tests", Branch: "termocode/agent-r9-tests", Agent: true, AgentID: "r9-tests"},
		{Path: "/src/api-v2", Branch: "api-v2"},
		{Path: "/src/old-fix", Branch: "old-fix", Prunable: true},
	}
	if variant == "nested" { // a worktree made by hand, nothing known about it
		list = append(list[:len(list)-1], git.Worktree{Path: "/src/scratch", Branch: "scratch"}, list[len(list)-1])
	}
	metas := map[string]worktrees.Meta{
		"/src/app.worktrees/wt-sidebar": {Pinned: true, Notes: []worktrees.Note{{Text: "adding the pin row"}}, Issue: 412, LastSeen: ago(2 * time.Minute)},
		"/src/app":                      {Pinned: true},
		"/src/app.worktrees/toast-race": {LastSeen: ago(20 * time.Minute)},
		"/src/app.worktrees/grpc":       {State: worktrees.StateAsleep, LastSeen: ago(72 * time.Hour)},
		"/src/api-v2":                   {LastSeen: ago(9 * 24 * time.Hour)},
	}
	if variant == "archived" || variant == "cleanup" {
		metas["/src/app.worktrees/old-ui"] = worktrees.Meta{State: worktrees.StateArchived, Branch: "feat/old-ui", LastSeen: ago(30 * 24 * time.Hour)}
	}
	st := newWTState()
	st.off, st.loaded, st.visible = true, true, true
	st.root, st.common = "/src/app", "/src/app/.git"
	st.entries = worktrees.Build(list, metas)
	st.demoTabs = []wtTabAgent{
		{name: "Claude Code", cwd: "/src/app.worktrees/wt-sidebar", state: agents.Working},
		{name: "Codex", cwd: "/src/app.worktrees/toast-race/web", state: agents.Waiting},
	}
	st.others = []agentroll.Rollup{{Workspace: "/src/app", PID: 2, Items: []agentroll.Item{{Name: "run r9 · tests", State: "working", Worktree: "/src/app/.git/termocode/worktrees/r9-tests"}}}}
	st.cache = wtCannedCache(map[string]git.WorktreeStatus{
		"/src/app":                                   {Upstream: true},
		"/src/app.worktrees/wt-sidebar":              {Ahead: 3, Dirty: 4, Upstream: true},
		"/src/app.worktrees/toast-race":              {Ahead: 1, Dirty: 2},
		"/src/app.worktrees/grpc":                    {Ahead: 5},
		"/src/app/.git/termocode/worktrees/r9-tests": {Dirty: 7},
		"/src/api-v2":                                {Behind: 12, Upstream: true},
		"/src/scratch":                               {Dirty: 1},
	})
	st.cursorPath = map[string]string{
		"": "/src/app.worktrees/wt-sidebar", "running": "/src/app.worktrees/wt-sidebar",
		"waiting": "/src/app.worktrees/toast-race", "asleep": "/src/app.worktrees/grpc", "external": "/src/api-v2", "keys": "/src/app.worktrees/wt-sidebar",
		"filter": "/src/app.worktrees/toast-race", "jump": "/src/app.worktrees/wt-sidebar",
		"link": "/src/app.worktrees/toast-race", "nested": "/src/app/.git/termocode/worktrees/r9-tests", "notes": "/src/app.worktrees/wt-sidebar",
		"sleep": "/src/app.worktrees/wt-sidebar", "archive": "/src/app.worktrees/toast-race", "delete": "/src/app.worktrees/toast-race",
		"cleanup": "/src/app.worktrees/wt-sidebar", "archived": "/src/app.worktrees/old-ui",
	}[variant]
	m.wt = st
	m.activity.SetActive(activity.ViewWorktrees)
	if !m.showExp {
		m.showExp = true
		m.applyLayout()
	}
	m.focus = FocusExplorer
	switch variant {
	case "nested", "notes":
		for i := range st.entries {
			md := &st.entries[i].Meta
			switch st.entries[i].Path {
			case "/src/app.worktrees/toast-race":
				md.PR, md.Checkpoint = 12, worktrees.CheckBlocked
				md.Link = worktrees.LinkInfo{Title: "Fix the toast race", State: "open", CI: "pass"}
				md.AddNote("waiting on review of the lock order", ago(5*time.Minute))
			case "/src/app/.git/termocode/worktrees/r9-tests":
				md.Parent = "/src/app.worktrees/toast-race"
			case "/src/app.worktrees/wt-sidebar":
				md.Checkpoint, md.CheckpointAt = worktrees.CheckWorking, ago(time.Minute)
				md.Notes = nil
				md.AddNote("read the plan; starting with the model", ago(3*time.Hour))
				md.AddNote("pin row drawn; goldens next", ago(40*time.Minute))
				md.AddNote("adding the pin row", ago(2*time.Minute))
			}
		}
		if variant == "notes" {
			m.worktreeNotes()
		}
	case "link":
		m.worktreeLink()
		m.connect.SetInputValue("ref", "https://github.com/o/app/pull/12")
		m.connect.SetCard(m.wtLinkCard("https://github.com/o/app/pull/12"))
	case "keys":
		m.openKeyCard("Worktree keys", worktreeKeyRows(), helpLocWorktrees(), nil)
	case "filter":
		st.kind, st.filter = wtKindDirty, "fix"
	case "jump":
		m.openWorktreeJump()
	case "new", "new-error":
		m.worktreesNewDemo(variant == "new-error")
	case "setup":
		f := wtDemoForm()
		m.wtSetupAsk(f, "/src/app.worktrees/feat-pin-row")
	case "setup-running":
		st.setups = map[string]*wtSetupRun{"/src/app.worktrees/wt-sidebar": {state: wtSetupRunning}}
	case "sleep":
		for i := range st.entries {
			if st.entries[i].Path == "/src/app.worktrees/toast-race" {
				st.entries[i].Meta.Parent = "/src/app.worktrees/wt-sidebar"
			}
		}
		m.worktreeSleep()
	case "archive":
		m.onWTArchivePrep(wtArchivePrepMsg{title: "fix/toast-race",
			main: wtArchiveTarget{path: "/src/app.worktrees/toast-race", branch: "fix/toast-race", dirty: 2}})
	case "delete":
		m.wt.del = &wtDeletePending{title: "fix/toast-race", path: "/src/app.worktrees/toast-race", branch: "fix/toast-race", base: "main",
			folder: true, dirty: 2, unmerged: 3, discard: true}
		m.wtDeleteNext()
		m.connect.SetInputValue("name", "fix/toast")
	case "cleanup":
		m.onWTCleanupPrep(wtCleanupPrepMsg{c: &wtCleanup{gone: 1, prune: true, rows: []wtCleanRow{
			{path: "/src/app.worktrees/wt-sidebar", title: "feat/wt-sidebar", why: "kept: 4 changed files", dirty: 4},
			{path: "/src/app.worktrees/toast-race", title: "fix/toast-race", why: "PR #12 merged · 2 changed files", dirty: 2},
			{path: "/src/app.worktrees/grpc", title: "spike/grpc", why: "merged into main", tick: true},
			{path: "/src/api-v2", title: "api-v2", why: "kept: active 9 days ago"},
			{path: "/src/scratch", title: "scratch", why: "no activity for 21 days", tick: true},
		}}})
	}
	st.touch()
}

// wtDemoForm is a dialog state with a canned repository and config.
func wtDemoForm() *wtForm {
	return &wtForm{
		off: true, root: "/src/app", main: "/src/app", common: "/src/app/.git", base: "/src/app.worktrees",
		from: "main", refs: []string{"main", "feat/wt-sidebar", "fix/toast-race", "origin/main", "v1.4.0"},
		cfg:  worktreeform.Config{Include: []string{".env", ".env.local", "secrets.env"}, Shared: []string{"node_modules"}, Setup: []string{"pnpm install"}},
		hash: "demo",
		files: []worktreeform.File{
			{Rel: ".env", Kind: worktreeform.Copy}, {Rel: ".env.local", Kind: worktreeform.Copy},
			{Rel: "secrets.env", Kind: worktreeform.Copy, Skip: "not there"},
			{Rel: "node_modules", Kind: worktreeform.Link},
		},
		agents: []agents.Agent{agentDemo("claude"), agentDemo("codex")},
	}
}

func agentDemo(id string) agents.Agent {
	a, _ := agents.ByID(id)
	return a
}

// worktreesNewDemo opens Worktree: New… with a typed name and task
// (nothing is made); fail shows git's error on the card.
func (m *Model) worktreesNewDemo(fail bool) {
	f := wtDemoForm()
	m.wt.form = f
	m.connect = connectcard.New(m.wtFormCard())
	m.connect.SetSize(m.w, m.h)
	m.connectOpen = true
	m.connect.SetInputValue("name", "feat/pin-row")
	m.connect.SetInputValue("task", "Add the pinned row as in issue 412. Run go test.")
	if fail {
		f.fail = "the branch feat/pin-row already exists"
	}
	m.connect.SetCard(m.wtFormCard())
}
