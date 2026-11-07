package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/pairsync"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workspace"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// CMP-65: pair sync. Containers: Pair Sync with Workspace… keeps a
// folder in a running container and the open local workspace in step,
// both ways. termocode lists both sides first (no file is read), then
// shows ONE card with every copy and delete; Cancel is first and
// focused. A path changed on both sides is a conflict: both copies
// stay, the card lists it. The last sync is recorded in
// <config>/pairsync/<key>.json, so a delete on one side is carried to
// the other only when the file was synced before.

const (
	confirmKindPairSync      confirmKindEnum = 6565
	promptKindPairSyncFolder promptKindEnum  = 6566
)

const (
	pairSyncMaxFiles = 20000
	pairSyncMaxFile  = 64 << 20 // one copy
	pairSyncSameMax  = 8 << 20  // a "new on both sides" pair is compared up to this
	pairSyncCardList = 8        // paths listed per group on the card
)

// pairSyncState is the pairing between the prompt, the scan, the card
// and the apply.
type pairSyncState struct {
	c         containers.Container
	name      string
	root      string // folder in the container
	localRoot string
	baseFile  string
	host      *containerhost.Host
	local     workspace.FS
	ig        *pairsync.Ignore
	base      pairsync.Base
	preL      pairsync.Snapshot
	preR      pairsync.Snapshot
	plan      pairsync.Plan
	lastRoot  map[string]string // container ID → last folder (prompt default)
	busy      bool
}

// askPairSync asks for the folder in the container.
func (m *Model) askPairSync(c containers.Container) tea.Cmd {
	if !strings.EqualFold(c.State, "running") {
		return m.servicesToast(toast.Info, "Start the container first", "Pair sync needs a running container")
	}
	if _, remote := m.remoteFS(); remote {
		return m.servicesToast(toast.Info, "Open a local folder first", "Pair sync copies between a container and a folder on this machine")
	}
	st := &m.services.pair
	if st.busy {
		return m.servicesToast(toast.Info, "A pair sync is running", "Wait for it to finish")
	}
	def := st.lastRoot[c.ID]
	if def == "" {
		def = c.WorkingDir
	}
	if def == "" || def == "/" {
		def = "/workspace"
	}
	st.c, st.name = c, containerShownName(c)
	m.prompt = prompt.New("Pair sync with "+sanitizeOutputLine(st.name), "Folder in the container:", def)
	m.prompt.SetNote("Workspace: " + aiWorkspaceRoot() + "\nNothing is copied before a card lists every change.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindPairSyncFolder
	return nil
}

// pairSyncFolder checks the typed container folder.
func pairSyncFolder(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !path.IsAbs(s) {
		return "", errors.New("type a full folder like /workspace")
	}
	s = path.Clean(s)
	if s == "/" {
		return "", errors.New("the container root cannot be synced; pick the project folder")
	}
	return s, nil
}

// pairSyncBaseFile is where the last sync of one pairing is recorded.
func pairSyncBaseFile(localRoot, engineContext, container, root string) (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pairsync", pairsync.BaseKey(localRoot, engineContext, container, root)+".json"), nil
}

// pairSyncListRemote lists the container side: one find through the
// container's exec, or a walk when it has no shell.
func pairSyncListRemote(ctx context.Context, h *containerhost.Host, root string, ig *pairsync.Ignore) (pairsync.Snapshot, error) {
	if h.Caps().HasShell {
		if snap, err := pairsync.ExecList(ctx, h.Exec(), root, ig, pairSyncMaxFiles); err == nil || errors.Is(err, pairsync.ErrTooMany) {
			return snap, err
		}
	}
	return pairsync.Walk(ctx, h.FS(), ig, pairSyncMaxFiles)
}

// onPairSyncFolderPrompt connects, scans both sides and plans.
func (m *Model) onPairSyncFolderPrompt(value string) tea.Cmd {
	st := &m.services.pair
	root, err := pairSyncFolder(value)
	if err != nil {
		return m.servicesToast(toast.Info, "Not synced", err.Error())
	}
	localRoot, err := filepath.Abs(aiWorkspaceRoot())
	if err != nil {
		return m.servicesToast(toast.Errr, "Not synced", err.Error())
	}
	if st.lastRoot == nil {
		st.lastRoot = map[string]string{}
	}
	st.lastRoot[st.c.ID] = root
	st.busy = true
	e := m.services.containerEngine
	c, name := st.c, st.name
	cfg := containerhost.Config{Binary: e.Binary, Kind: e.Kind, Context: e.Context, Container: c.ID, Root: root, X: e.X}
	baseName := c.ID
	if len(c.Names) > 0 {
		baseName = strings.TrimPrefix(c.Names[0], "/")
	}
	return tea.Batch(m.servicesToast(toast.Info, "Comparing "+name, root+" ⇄ "+localRoot), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		next := pairSyncState{c: c, name: name, root: root, localRoot: localRoot}
		err := func() error {
			var err error
			if next.baseFile, err = pairSyncBaseFile(localRoot, e.Context, baseName, root); err != nil {
				return err
			}
			if next.base, err = pairsync.LoadBase(next.baseFile); err != nil {
				return err
			}
			ws, err := localhost.New(localRoot)
			if err != nil {
				return err
			}
			next.local = ws.Host.FS()
			gi, _ := os.ReadFile(filepath.Join(localRoot, ".gitignore"))
			next.ig = pairsync.NewIgnore(string(gi))
			if next.host, err = containerhost.New(ctx, cfg); err != nil {
				return err
			}
			if next.preL, err = pairsync.Walk(ctx, next.local, next.ig, pairSyncMaxFiles); err != nil {
				return fmt.Errorf("workspace: %w", err)
			}
			if next.preR, err = pairSyncListRemote(ctx, next.host, root, next.ig); err != nil {
				return fmt.Errorf("container: %w", err)
			}
			if err := pairsync.GuardMassDelete(next.base, next.preL, next.preR); err != nil {
				return err
			}
			next.plan = pairsync.ResolveSame(ctx, pairsync.Diff(next.base, next.preL, next.preR), next.local, next.host.FS(), pairSyncSameMax)
			return nil
		}()
		return containerUIDoneMsg{apply: func(m *Model) tea.Cmd { return m.onPairSyncPlanned(next, err) }}
	})
}

// pairSyncWrites is how many copies and deletes plan has.
func pairSyncWrites(p pairsync.Plan) int {
	return p.Count(pairsync.OpPush) + p.Count(pairsync.OpPull) + p.Count(pairsync.OpDeleteLocal) + p.Count(pairsync.OpDeleteRemote)
}

func (m *Model) onPairSyncPlanned(next pairSyncState, err error) tea.Cmd {
	st := &m.services.pair
	next.lastRoot = st.lastRoot
	if err != nil {
		st.busy = false
		return m.servicesToast(toast.Errr, "Cannot pair sync "+next.name, errLine(err.Error()))
	}
	next.busy = true
	*st = next
	if pairSyncWrites(st.plan) == 0 {
		if st.plan.Empty() {
			st.busy = false
			return m.servicesToast(toast.Info, "Already in sync", st.name+":"+st.root)
		}
		return m.runPairSync() // only records equal files; conflicts are reported
	}
	style := confirm.StyleDefault
	if st.plan.Count(pairsync.OpDeleteLocal)+st.plan.Count(pairsync.OpDeleteRemote) > 0 {
		style = confirm.StyleDestructive
	}
	m.confirm = confirm.New("Pair sync?", sanitizePairSyncBody(pairSyncCardBody(st.plan, st.localRoot, st.name, st.root)), []confirm.Button{
		{ID: "cancel", Title: "Cancel"}, {ID: "sync", Title: "Sync", Style: style},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindPairSync
	return nil
}

// sanitizePairSyncBody cleans each line (file names come from the
// container).
func sanitizePairSyncBody(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = sanitizeOutputLine(l)
	}
	return strings.Join(lines, "\n")
}

// pairSyncCardBody is the text of the card: the two folders, then each
// group with its count and first paths.
func pairSyncCardBody(p pairsync.Plan, localRoot, name, root string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Workspace: %s\nContainer: %s:%s\n", localRoot, name, root)
	groups := []struct {
		op    pairsync.Op
		label string
	}{
		{pairsync.OpPush, "→ Copy to the container"},
		{pairsync.OpPull, "← Copy to the workspace"},
		{pairsync.OpDeleteRemote, "✕ Delete in the container"},
		{pairsync.OpDeleteLocal, "✕ Delete in the workspace"},
		{pairsync.OpConflict, "! Conflicts (left alone)"},
	}
	for _, g := range groups {
		var items []string
		for _, a := range p.Actions {
			if a.Op != g.op {
				continue
			}
			if a.Reason != "" {
				items = append(items, a.Path+" ("+a.Reason+")")
			} else {
				items = append(items, a.Path)
			}
		}
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s: %d\n", g.label, len(items))
		for i, it := range items {
			if i == pairSyncCardList {
				fmt.Fprintf(&b, "  … and %d more\n", len(items)-pairSyncCardList)
				break
			}
			b.WriteString("  " + it + "\n")
		}
	}
	if n := p.Count(pairsync.OpAdopt); n > 0 {
		fmt.Fprintf(&b, "\n= Same on both sides: %d (only recorded)\n", n)
	}
	return strings.TrimRight(b.String(), "\n")
}

// handlePairSyncConfirm answers the pair sync card.
func (m *Model) handlePairSyncConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindPairSync {
		return nil, false
	}
	if id != "sync" {
		m.services.pair.busy = false
		return nil, true
	}
	return m.runPairSync(), true
}

// runPairSync applies the planned sync, scans again and records it.
func (m *Model) runPairSync() tea.Cmd {
	st := m.services.pair
	if st.host == nil || st.local == nil {
		m.services.pair.busy = false
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		res := pairsync.Apply(ctx, st.plan, st.local, st.host.FS(), st.preL, st.preR, pairSyncMaxFile)
		postL, errL := pairsync.Walk(ctx, st.local, st.ig, pairSyncMaxFiles)
		postR, errR := pairSyncListRemote(ctx, st.host, st.root, st.ig)
		var saveErr error
		switch {
		case errL != nil:
			saveErr = errL
		case errR != nil:
			saveErr = errR
		default:
			saveErr = pairsync.SaveBase(st.baseFile, pairsync.NextBase(st.base, st.plan, res.OK, st.preL, st.preR, postL, postR))
		}
		return containerUIDoneMsg{apply: func(m *Model) tea.Cmd {
			m.services.pair.busy = false
			m.services.pair.plan = pairsync.Plan{}
			return m.onPairSyncDone(st, res, saveErr)
		}}
	}
}

// pairSyncSummary is the one line of a finished sync.
func pairSyncSummary(r pairsync.Result) string {
	var parts []string
	add := func(n int, s string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", s, n))
		}
	}
	add(r.Pushed, "→")
	add(r.Pulled, "←")
	add(r.DeletedRemote+r.DeletedLocal, "✕")
	add(r.Adopted, "=")
	add(r.Conflicts, "conflicts")
	add(len(r.Failed), "failed")
	if len(parts) == 0 {
		return "nothing changed"
	}
	return strings.Join(parts, " · ")
}

func (m *Model) onPairSyncDone(st pairSyncState, res pairsync.Result, saveErr error) tea.Cmd {
	m.explorer.Reload()
	sum := pairSyncSummary(res)
	switch {
	case len(res.Failed) > 0:
		return m.servicesToast(toast.Errr, "Pair sync: "+sum, errLine(res.Failed[0]))
	case saveErr != nil:
		return m.servicesToast(toast.Errr, "Pair sync: "+sum, "The sync was not recorded: "+errLine(saveErr.Error()))
	case res.Conflicts > 0:
		paths := st.plan.Paths(pairsync.OpConflict)
		if len(paths) > 3 {
			paths = append(paths[:3], "…")
		}
		return m.servicesToast(toast.Warn, "Pair sync: "+sum, "Fix by hand, then sync again: "+strings.Join(paths, ", "))
	}
	return m.servicesToast(toast.Info, "Pair sync: "+sum, st.name+":"+st.root+" ⇄ "+st.localRoot)
}
