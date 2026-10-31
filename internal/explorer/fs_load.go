package explorer

import (
	"context"
	"os"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Folder listings of a container workspace run off the UI loop (FIX-2).
// Each listing is a call to the container engine, and a hung engine
// answers only when its 10 s timeout ends: done inside Update that froze
// the whole UI. Now a folder of a workspace FS is never read in place:
// Load asks the root's fsLoader for it and shows the folder as loading
// ("…"), ScheduleLoads (called after every Update, like SchedulePreview)
// starts the asked listings as commands, and their DirLoadedMsg fills
// the folder in Update. A reveal or a session restore that needs a
// folder not yet read goes on when it arrives. Local folders are read in
// place as before.

// fsLoader is the queue of listings of one workspace FS root, shared by
// every node of that root (and every Model copy).
type fsLoader struct {
	want     map[string]bool // asked, not started
	inflight map[string]bool // started, no answer yet
	order    []string        // want in the order asked

	// What waits for a listing: a reveal and a session restore.
	reveal string
	expand []string
}

func newFSLoader() *fsLoader {
	return &fsLoader{want: map[string]bool{}, inflight: map[string]bool{}}
}

func (l *fsLoader) ask(p string) {
	if l.want[p] {
		return
	}
	l.want[p] = true
	l.order = append(l.order, p)
}

// busy: a listing is asked or running.
func (l *fsLoader) busy() bool { return len(l.want) > 0 || len(l.inflight) > 0 }

// DirLoadedMsg is the listing of one folder of a workspace FS root.
type DirLoadedMsg struct {
	Path    string
	Err     error
	entries []os.DirEntry
	ld      *fsLoader
	// Root: the folder is the root itself (an error means the workspace
	// cannot be listed at all).
	Root bool
}

// ScheduleLoads starts the asked listings of the workspace FS roots;
// nil when none is waiting.
func (m *Model) ScheduleLoads() tea.Cmd {
	var cmds []tea.Cmd
	for _, r := range m.allRoots() {
		l := r.ld
		if r.fsys == nil || l == nil {
			continue
		}
		for _, p := range l.order {
			if !l.want[p] || l.inflight[p] {
				continue // a listing of p runs: p is asked again after it
			}
			delete(l.want, p)
			l.inflight[p] = true
			fsys, p, isRoot := r.fsys, p, p == r.Path
			cmds = append(cmds, func() tea.Msg {
				entries, err := fsys.ReadDir(context.Background(), p)
				return DirLoadedMsg{Path: p, Err: err, entries: entries, ld: l, Root: isRoot}
			})
		}
		kept := l.order[:0]
		for _, p := range l.order {
			if l.want[p] {
				kept = append(kept, p)
			}
		}
		l.order = kept
	}
	return tea.Batch(cmds...)
}

// ApplyDirLoaded fills the folder of msg. A listing of a root that is
// gone (another workspace) or of a folder no longer in the tree is
// dropped. A failed root listing is the explorer's error; a failed
// folder closes again (the caller tells the user msg.Err).
func (m *Model) ApplyDirLoaded(msg DirLoadedMsg) {
	var root *Node
	for _, r := range m.allRoots() {
		if r.ld != nil && r.ld == msg.ld {
			root = r
		}
	}
	if root == nil {
		return
	}
	l := root.ld
	delete(l.inflight, msg.Path)
	n := findNode(root, msg.Path)
	if n == nil {
		return
	}
	cursorPath := m.CursorPath()
	touch()
	n.loading = l.want[msg.Path] || l.inflight[msg.Path]
	if msg.Err != nil {
		if n == root {
			m.err = msg.Err.Error()
		} else {
			n.Expanded = false
		}
	} else {
		if n == root {
			m.err = ""
		}
		n.mergeChildren(msg.entries)
	}
	m.keepCursorOn(cursorPath)
	// What waited for this listing goes on (and waits again if it needs
	// another folder).
	if len(l.expand) > 0 {
		paths := l.expand
		l.expand = nil
		m.ExpandPaths(paths)
	}
	if l.reveal != "" {
		p := l.reveal
		l.reveal = ""
		m.RevealPath(p)
	}
}

// findNode is the loaded node at p under root (slash paths).
func findNode(root *Node, p string) *Node {
	n := root
	for n.Path != p {
		var next *Node
		for _, c := range n.Children {
			if c.Path == p || (c.IsDir && strings.HasPrefix(p, c.Path+"/")) {
				next = c
				break
			}
		}
		if next == nil {
			return nil
		}
		n = next
	}
	return n
}

// mergeChildren sets n's children to entries. A folder that was there
// before keeps its node (open, loaded children), so a reload does not
// fold the tree.
func (n *Node) mergeChildren(entries []os.DirEntry) {
	old := make(map[string]*Node, len(n.Children))
	for _, c := range n.Children {
		old[c.Name] = c
	}
	dir := n.Path
	if !strings.HasSuffix(dir, "/") {
		dir += "/"
	}
	kids := make([]*Node, 0, len(entries))
	for _, e := range entries {
		if o := old[e.Name()]; o != nil && o.IsDir == e.IsDir() {
			kids = append(kids, o)
			continue
		}
		kids = append(kids, &Node{Name: e.Name(), Path: dir + e.Name(), IsDir: e.IsDir(), fsys: n.fsys, ld: n.ld})
	}
	sortNodes(kids)
	n.Children = kids
	n.Loaded = true
}

// sortNodes orders folders first, then by name.
func sortNodes(kids []*Node) {
	slices.SortFunc(kids, func(a, b *Node) int {
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
}

// refreshFS asks again for every open folder of a workspace FS root (a
// Reload). A closed folder forgets its listing and is read when opened.
func (n *Node) refreshFS() {
	if !n.IsDir || !n.Loaded {
		return
	}
	if !n.Expanded && n.ld != nil && !n.isRootOf() {
		touch()
		n.Loaded, n.Children = false, nil
		return
	}
	n.ld.ask(n.Path)
	n.loading = true
	touch()
	for _, c := range n.Children {
		c.refreshFS()
	}
}

// isRootOf: n is the root its loader belongs to (roots are never
// forgotten).
func (n *Node) isRootOf() bool { return n.root }

// keepCursorOn puts the cursor back on the row at path when rows above
// it came or went.
func (m *Model) keepCursorOn(path string) {
	if path != "" {
		for i, e := range m.visible() {
			if e.Node.Path == path {
				m.cursor = i
				break
			}
		}
	}
	if c := len(m.visible()); m.cursor >= c {
		m.cursor = max(c-1, 0)
	}
}
