package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktreeform"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Run a worktree's dev server on its own port (CMP-5) ──────────────────
//
// .termocode/worktrees.json "run" is one shell line that starts the
// repository's dev server and reads its port from $PORT ("ports" is the
// range, "url" the address to open). r in the WORKTREES view (Worktree:
// Run Dev Server of Selected) gives the worktree its own port — the
// lowest of the range that no other worktree holds and nothing listens
// on, kept in worktrees.json — and runs the line in a terminal tab of
// the worktree, so parallel worktrees never fight for one port. The
// line is the repository's: it runs only after the same trust card as
// the setup (Not now first; Enter and Esc never run it; the answer is
// bound to the file's hash), with the user's environment minus
// termocode's secrets. The card shows the address while it runs; o
// opens it; r again shows the tab. Closing the tab stops the server.

const (
	confirmKindWTRunTrust confirmKindEnum = 3695

	wtRunPaletteID     = "worktree-run"
	wtRunOpenPaletteID = "worktree-run-open"
)

// wtRun is a dev server running in this window.
type wtRun struct {
	port int
	url  string
	buf  int // its terminal tab
}

// wtRunPending is the open trust card.
type wtRunPending struct {
	folder, name, hash string
	line, url          string
	port               int
}

// Seams (tests): the repository file, the port check and the tab.
var (
	wtRunConfig   = worktreeform.LoadConfig
	wtRunPortFree = worktrees.PortFree
	wtRunSpawn    func(m *Model, j terminalJob) (int, error) // nil = the panel's spawnTerminalJob
)

func init() {
	aiActionPolicies[wtRunPaletteID] = aiNever("runs the repository's dev server in the worktree the user selected; the user starts it")
	aiActionPolicies[wtRunOpenPaletteID] = aiNever("acts on the worktree the user selected in the WORKTREES view")
}

// wtMainCheckout is the main checkout of the list (the config's home).
func (st *wtState) wtMainCheckout() string {
	for _, e := range st.entries {
		if e.Kind == worktrees.KindMain {
			return e.Path
		}
	}
	return st.root
}

// wtRunLine is the run piece of a card ("" = no port yet).
func wtRunLine(port int, url string) string {
	switch {
	case url != "":
		return "▶ " + strings.TrimPrefix(strings.TrimPrefix(url, "http://"), "https://") + " (o opens)"
	case port > 0:
		return "port " + strconv.Itoa(port) + " · r runs"
	}
	return ""
}

// wtRunTab is the index of buf's terminal tab (-1 = closed).
func (m *Model) wtRunTab(buf int) int {
	for i := range m.terminalTabs {
		if m.terminalTabs[i].BufID == buf {
			return i
		}
	}
	return -1
}

// wtRunLive is the running server of folder (nil = none; a closed tab
// is forgotten).
func (m *Model) wtRunLive(folder string) *wtRun {
	r := m.wt.runs[folder]
	if r != nil && r.buf > 0 && m.wtRunTab(r.buf) < 0 {
		delete(m.wt.runs, folder)
		m.wt.touch()
		return nil
	}
	return r
}

// worktreeRun runs the dev server of the cursor's worktree, or shows its
// tab when it runs (r, worktree-run).
func (m *Model) worktreeRun() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	if why := wtUsable(it); why != "" || it.state == wtStateArchived {
		if why == "" {
			why = "Restore it first (x)."
		}
		return m.pushToastDetail(toast.Info, "This worktree cannot run", why)
	}
	st := m.wt
	if r := m.wtRunLive(it.e.Path); r != nil {
		m.wtRunShowTab(r.buf)
		return m.pushToastDetail(toast.Info, "The dev server runs", r.url+theme.ASCIIText(" · ")+"o opens it; close its tab to stop it.")
	}
	cfg, hash, err := wtRunConfig(st.wtMainCheckout())
	if err != nil {
		return m.pushToastDetail(toast.Warn, "The run command could not be read", err.Error())
	}
	line := strings.TrimSpace(cfg.Run)
	if line == "" {
		return m.pushToastDetail(toast.Info, "No run command in "+worktreeform.ConfigFile,
			`Add "run": "npm run dev -- --port $PORT" (and "ports": "3000-3099") to give each worktree its own dev server.`)
	}
	rng, err := worktrees.ParsePortRange(cfg.Ports)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "The ports in "+worktreeform.ConfigFile+" are wrong", err.Error())
	}
	port, err := m.wtRunPort(it.e, rng)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "No free port", err.Error())
	}
	p := &wtRunPending{folder: it.e.Path, name: wtTitle(it), hash: hash, line: line, url: cfg.RunURL(port), port: port}
	if worktreeform.SetupTrusted(hash, loadSession().WorktreeSetupTrusted) {
		return m.wtRunStart(p)
	}
	st.runAsk = p
	var b strings.Builder
	b.WriteString(worktreeform.ConfigFile + " in this repository asks to run, in " + wtShortPath(p.folder) + " on port " + strconv.Itoa(port) + ":\n")
	b.WriteString("\n    " + aiCleanText(line))
	b.WriteString("\n\nIt runs in a terminal tab with $PORT=" + strconv.Itoa(port) + " and your environment minus termocode's secrets. Run it only if you trust this repository; a changed file asks again.")
	m.confirm = confirm.Risky("Run this repository's dev server?", b.String(),
		confirm.Button{ID: "deny", Title: "Not now"},
		confirm.Button{ID: "run", Title: "Trust and run", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWTRunTrust
	return nil
}

// wtRunPort is the worktree's kept port, or a new one from rng (kept in
// worktrees.json). A kept port outside the range, held by another
// worktree, or taken by another program while not ours is picked again.
func (m *Model) wtRunPort(e worktrees.Entry, rng worktrees.PortRange) (int, error) {
	st := m.wt
	metas := map[string]worktrees.Meta{}
	for _, x := range st.entries {
		metas[x.Path] = x.Meta
	}
	if st.common != "" && !st.off {
		if disk, err := worktrees.Load(st.common); err == nil {
			for p, md := range disk {
				metas[p] = md
			}
		}
	}
	taken := worktrees.TakenPorts(metas, e.Path)
	for path, r := range st.runs {
		if path != e.Path {
			taken[r.port] = true
		}
	}
	if p := e.Meta.Port; p >= rng.Lo && p <= rng.Hi && !taken[p] && wtRunPortFree(p) {
		return p, nil
	}
	p, ok := worktrees.PickPort(rng, taken, wtRunPortFree)
	if !ok {
		return 0, &wtRunPortError{rng: rng}
	}
	if err := m.wtTouchMeta(e.Path, func(md *worktrees.Meta) { md.Port = p }); err != nil {
		return 0, err
	}
	return p, nil
}

type wtRunPortError struct{ rng worktrees.PortRange }

func (e *wtRunPortError) Error() string {
	return "Every port of " + e.rng.String() + " is held by another worktree or in use. Stop a dev server, or widen \"ports\" in " + worktreeform.ConfigFile + "."
}

// answerWTRunTrust stores the answer of the card ("" = Esc: no answer).
func (m *Model) answerWTRunTrust(id string) tea.Cmd {
	p := m.wt.runAsk
	m.wt.runAsk = nil
	if p == nil || id != "run" {
		if p != nil && id == "deny" {
			return m.pushToastDetail(toast.Info, "The dev server did not start", "Run it yourself in "+wtShortPath(p.folder)+" with PORT="+strconv.Itoa(p.port)+".")
		}
		return nil
	}
	sess := loadSession()
	sess.WorktreeSetupTrusted = p.hash
	saveSession(sess)
	return m.wtRunStart(p)
}

// wtRunStart opens the dev server's terminal tab.
func (m *Model) wtRunStart(p *wtRunPending) tea.Cmd {
	st := m.wt
	spawnTab := wtRunSpawn
	if spawnTab == nil && m.nvim != nil {
		spawnTab = (*Model).spawnTerminalJob
	}
	if spawnTab == nil {
		return m.pushToastDetail(toast.Info, "The dev server needs a terminal tab", "Open the editor, then press r again; or run it yourself with PORT="+strconv.Itoa(p.port)+".")
	}
	env := map[string]any{}
	for _, kv := range clistep.CleanEnv(os.Environ()) {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	env["PORT"] = strconv.Itoa(p.port)
	j := terminalJob{Label: "run: " + filepath.Base(p.folder) + " :" + strconv.Itoa(p.port), Dir: p.folder, Env: env, ClearEnv: true,
		Cmdline: "/bin/sh -c " + wtShellQuote(p.line)}
	buf, err := spawnTab(m, j)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "The dev server could not start", err.Error())
	}
	if st.runs == nil {
		st.runs = map[string]*wtRun{}
	}
	st.runs[p.folder] = &wtRun{port: p.port, url: p.url, buf: buf}
	st.touch()
	return m.pushToastDetail(toast.Info, "Dev server of "+p.name+" starting", p.url+theme.ASCIIText(" · ")+"o in WORKTREES opens it")
}

// wtRunShowTab shows the terminal tab buf.
func (m *Model) wtRunShowTab(buf int) {
	i := m.wtRunTab(buf)
	if i < 0 {
		return
	}
	m.terminalActiveTab = i
	m.panelActive = panelKindTerminal
	if m.nvim != nil {
		m.applyLayout()
		m.resizeTerminalSplit()
	}
}

// wtRunExit is the end of a dev server tab (ok=false: not one).
func (m *Model) wtRunExit(buf, code int) (tea.Cmd, bool) {
	if m.wt == nil || buf <= 0 {
		return nil, false
	}
	for folder, r := range m.wt.runs {
		if r.buf != buf {
			continue
		}
		delete(m.wt.runs, folder)
		m.wt.touch()
		why := wtShortPath(folder) + theme.ASCIIText(" · ") + "port " + strconv.Itoa(r.port) + " stays this worktree's; r starts it again."
		if code != 0 {
			return m.pushToastDetail(toast.Warn, "Dev server stopped (exit "+strconv.Itoa(code)+")", why), true
		}
		return m.pushToastDetail(toast.Info, "Dev server stopped", why), true
	}
	return nil, false
}

// worktreeRunOpen opens the address of the cursor's dev server in the
// browser (o, worktree-run-open).
func (m *Model) worktreeRunOpen() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	r := m.wtRunLive(it.e.Path)
	if r == nil {
		return m.pushToastDetail(toast.Info, "No dev server runs here", "r runs this worktree's dev server on its own port.")
	}
	if err := osOpen(r.url); err != nil {
		return m.pushToastDetail(toast.Errr, "Open failed", err.Error())
	}
	return m.pushToastDetail(toast.Info, "Opened", r.url)
}
