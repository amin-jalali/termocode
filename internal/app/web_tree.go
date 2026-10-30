package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// The "tree" topic (WEB-16, web-ui-design §2.3): folders are listed
// lazily. A browser lists a folder with tree.list and the folder joins
// the snapshot; every later change of it (the git poll, the hidden-files
// toggle) goes out as a dir delta to every browser on the topic. The
// listing follows the laptop's explorer: dotfiles only while it shows
// them, build artefacts marked ignored, git letters on files and on
// folders with changes inside. Names are cleaned (no controls, no bidi
// overrides) like every name the hub sends.

// webTreeMaxDirs caps the listed folders over all connections; past it,
// folders no live browser lists any more are dropped first.
const webTreeMaxDirs = 4 * webproto.MaxTreeDirs

// webTree is the hub's tree state (Update only).
type webTree struct {
	dirs      map[string]webproto.TreeDir // listed folders by path
	refs      map[string]map[string]bool  // folder → ids of connections that listed it
	hidden    bool                        // dotfiles listed (as last published)
	hiddenSet bool
	reading   bool // a refresh of every folder runs
	again     bool // and another one is owed
}

func newWebTree() webTree {
	return webTree{dirs: map[string]webproto.TreeDir{}, refs: map[string]map[string]bool{}}
}

func init() {
	webTopics["tree"] = webTopicDef{snapshot: webTreeSnapshot, apply: webTreeApply}
	webOps[webproto.OpTreeList] = webOpTreeList
	webOps[webproto.OpTreeUnlist] = webOpTreeUnlist
}

// webFail answers call with err's code (internal for a plain error).
func webFail(call *webCall, err error) {
	var we *webError
	if errors.As(err, &we) {
		call.fail(we.code, we.msg)
		return
	}
	recordError("[web] " + call.op + ": " + err.Error())
	call.fail(webproto.CodeInternal, "Something went wrong.")
}

// webTreeOf is the hub's tree state for m (nil without a hub).
func (m *Model) webTreeOf() *webTree {
	if m.web == nil {
		return nil
	}
	t := &m.web.filesState().tree
	if !t.hiddenSet {
		t.hidden, t.hiddenSet = m.explorer.HiddenShown(), true
	}
	return t
}

func webTreeSnapshot(m *Model, _ string) (any, error) {
	root, err := m.rootDir()
	if err != nil {
		return nil, webErr(webproto.CodeNotFound, "No workspace is open.")
	}
	t := m.webTreeOf()
	if t == nil {
		return nil, webErr(webproto.CodeGone, "The web side is off.")
	}
	dirs := make(map[string]webproto.TreeDir, len(t.dirs))
	for k, v := range t.dirs {
		dirs[k] = v
	}
	return webproto.TreeSnap{
		Roots:  []webproto.TreeRoot{{Path: "", Label: webName(path.Base(filepath.ToSlash(root)))}},
		Hidden: t.hidden,
		Dirs:   dirs,
	}, nil
}

// webTreeApply folds one tree delta into a snapshot (the browser store
// does the same).
func webTreeApply(snap, delta json.RawMessage) (json.RawMessage, error) {
	var s webproto.TreeSnap
	if err := json.Unmarshal(snap, &s); err != nil {
		return nil, err
	}
	if s.Dirs == nil {
		s.Dirs = map[string]webproto.TreeDir{}
	}
	var k struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(delta, &k); err != nil {
		return nil, err
	}
	switch k.Kind {
	case "dir":
		var d webproto.TreeDirDelta
		if err := json.Unmarshal(delta, &d); err != nil {
			return nil, err
		}
		s.Dirs[d.Path] = webproto.TreeDir{Entries: d.Entries, Truncated: d.Truncated}
	case "remove":
		var d webproto.TreeRemoveDelta
		if err := json.Unmarshal(delta, &d); err != nil {
			return nil, err
		}
		for p := range s.Dirs {
			if webTreeUnder(d.Path, p) {
				delete(s.Dirs, p)
			}
		}
	case "hidden":
		var d webproto.TreeHiddenDelta
		if err := json.Unmarshal(delta, &d); err != nil {
			return nil, err
		}
		s.Hidden = d.On
	default:
		return nil, fmt.Errorf("tree: unknown delta kind %q", k.Kind)
	}
	return json.Marshal(s)
}

// webTreeUnder reports whether folder p is dir or inside it.
func webTreeUnder(dir, p string) bool {
	return dir == "" || p == dir || strings.HasPrefix(p, dir+"/")
}

func webOpTreeList(m *Model, call *webCall) tea.Cmd {
	raw := call.args.(*webproto.PathArgs).Path
	abs, err := m.webResolve(raw)
	if err != nil {
		webFail(call, err)
		return nil
	}
	if !m.webIsDir(abs) {
		call.fail(webproto.CodeBadRequest, "That is not a folder.")
		return nil
	}
	rel := webCleanRel(raw)
	t := m.webTreeOf()
	id := call.conn.id
	if !t.refs[rel][id] && t.countFor(id) >= webproto.MaxTreeDirs {
		call.fail(webproto.CodeLimit, "Too many open folders. Close some first.")
		return nil
	}
	if t.refs[rel] == nil {
		t.refs[rel] = map[string]bool{}
	}
	t.refs[rel][id] = true
	m.webTreeEvict(t)
	call.later()
	return m.webTreeRead([]string{rel}, call)
}

func webOpTreeUnlist(m *Model, call *webCall) tea.Cmd {
	rel := webCleanRel(call.args.(*webproto.PathArgs).Path)
	t := m.webTreeOf()
	if refs := t.refs[rel]; refs != nil {
		delete(refs, call.conn.id)
		if len(refs) == 0 {
			m.webTreeDrop(t, rel)
		}
	}
	call.reply(nil)
	return nil
}

// countFor is how many folders connection id lists.
func (t *webTree) countFor(id string) int {
	n := 0
	for _, refs := range t.refs {
		if refs[id] {
			n++
		}
	}
	return n
}

// webTreeDrop stops watching folder rel and tells the browsers.
func (m *Model) webTreeDrop(t *webTree, rel string) {
	delete(t.refs, rel)
	if _, ok := t.dirs[rel]; ok {
		delete(t.dirs, rel)
		m.webPublish("tree", webproto.TreeRemoveDelta{Kind: "remove", Path: rel})
	}
}

// webTreeEvict drops folders no connection lists while there are too
// many (oldest names first, for a stable order).
func (m *Model) webTreeEvict(t *webTree) {
	if len(t.refs) <= webTreeMaxDirs {
		return
	}
	var orphans []string
	for rel, refs := range t.refs {
		if len(refs) == 0 {
			orphans = append(orphans, rel)
		}
	}
	sort.Strings(orphans)
	for _, rel := range orphans {
		if len(t.refs) <= webTreeMaxDirs {
			return
		}
		m.webTreeDrop(t, rel)
	}
}

// webIsDir reports whether abs is a folder (through the workspace FS on
// a container).
func (m *Model) webIsDir(abs string) bool {
	if fsys, ok := m.remoteFS(); ok {
		info, err := fsys.Stat(context.Background(), abs)
		return err == nil && info.IsDir()
	}
	info, err := os.Stat(abs)
	return err == nil && info.IsDir()
}

// webTrackTree: the hidden toggle and connections that went away.
func (m *Model) webTrackTree(h *webHub, f *webFiles) tea.Cmd {
	t := &f.tree
	if !h.webListens("tree") {
		if len(t.refs) > 0 || len(t.dirs) > 0 {
			f.tree = newWebTree() // nobody follows the tree: forget it
		}
		return nil
	}
	for _, refs := range t.refs {
		for id := range refs {
			if !h.webConnKnown(id) {
				delete(refs, id) // the folder stays until evicted or the topic ends
			}
		}
	}
	if t.hiddenSet && m.explorer.HiddenShown() != t.hidden {
		t.hidden = m.explorer.HiddenShown()
		m.webPublish("tree", webproto.TreeHiddenDelta{Kind: "hidden", On: t.hidden})
		return m.webTreeRefresh()
	}
	return nil
}

// webTreeRefresh lists every watched folder again (after a git poll or
// the hidden toggle); changed folders go out as dir deltas. One refresh
// runs at a time; a call while one runs owes one more.
func (m *Model) webTreeRefresh() tea.Cmd {
	if m.web == nil || m.web.files == nil {
		return nil
	}
	t := &m.web.files.tree
	if len(t.refs) == 0 {
		return nil
	}
	if t.reading {
		t.again = true
		return nil
	}
	t.reading = true
	rels := make([]string, 0, len(t.refs))
	for rel := range t.refs {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	return m.webTreeRead(rels, nil)
}

// webRawEntry is one folder entry as read off the disk.
type webRawEntry struct {
	name        string
	dir         bool
	size, mtime int64
}

type webTreeRead struct {
	rel     string
	entries []webRawEntry
	err     error
}

// webTreeRead reads folders off Update and folds them in on Update. call
// (when not nil) gets the first folder's listing as its answer.
func (m *Model) webTreeRead(rels []string, call *webCall) tea.Cmd {
	root, err := m.rootDir()
	fsys, remote := m.remoteFS()
	return func() tea.Msg {
		reads := make([]webTreeRead, 0, len(rels))
		for _, rel := range rels {
			r := webTreeRead{rel: rel, err: err}
			if err == nil {
				if remote {
					r.entries, r.err = webReadDirFS(fsys, path.Join(root, rel))
				} else {
					r.entries, r.err = webReadDirLocal(root, rel)
				}
			}
			reads = append(reads, r)
		}
		return applyMsg(func(m *Model) tea.Cmd { return m.webTreeGot(reads, call) })
	}
}

func webReadDirLocal(root, rel string) ([]webRawEntry, error) {
	abs := filepath.Join(root, filepath.FromSlash(rel))
	// Check again off Update: the folder may have turned into a link.
	if realRoot, err := filepath.EvalSymlinks(root); err == nil {
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, err
		}
		if !webInside(realRoot, real) {
			return nil, webPathOutside()
		}
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	out := make([]webRawEntry, 0, len(ents))
	for _, e := range ents {
		r := webRawEntry{name: e.Name(), dir: e.IsDir()}
		if info, err := e.Info(); err == nil {
			r.size, r.mtime = info.Size(), info.ModTime().UnixMilli()
		}
		if r.dir {
			r.size = 0
		}
		out = append(out, r)
	}
	return out, nil
}

func webReadDirFS(fsys workspace.FS, abs string) ([]webRawEntry, error) {
	ents, err := fsys.ReadDir(context.Background(), abs)
	if err != nil {
		return nil, err
	}
	out := make([]webRawEntry, 0, len(ents))
	for _, e := range ents {
		r := webRawEntry{name: e.Name(), dir: e.IsDir()}
		if info, err := e.Info(); err == nil && !r.dir {
			r.size, r.mtime = info.Size(), info.ModTime().UnixMilli()
		}
		out = append(out, r)
	}
	return out, nil
}

// webTreeGot folds read folders into the tree on Update.
func (m *Model) webTreeGot(reads []webTreeRead, call *webCall) tea.Cmd {
	t := m.webTreeOf()
	if t == nil {
		if call != nil {
			call.fail(webproto.CodeGone, "The web side is off.")
		}
		return nil
	}
	for i, r := range reads {
		answer := call != nil && i == 0
		if r.err != nil {
			if errors.Is(r.err, fs.ErrNotExist) {
				m.webTreeDrop(t, r.rel)
			}
			if answer {
				var we *webError
				if errors.As(r.err, &we) {
					webFail(call, r.err)
				} else {
					call.fail(webproto.CodeNotFound, "That folder cannot be read.")
				}
			}
			continue
		}
		dir := m.webTreeBuild(r.rel, r.entries, t.hidden)
		if _, watched := t.refs[r.rel]; watched {
			if old, ok := t.dirs[r.rel]; !ok || !reflect.DeepEqual(old, dir) {
				t.dirs[r.rel] = dir
				m.webPublish("tree", webproto.TreeDirDelta{Kind: "dir", Path: r.rel, Entries: dir.Entries, Truncated: dir.Truncated})
			}
		}
		if answer {
			call.reply(webproto.TreeListRes{Path: r.rel, Entries: dir.Entries, Truncated: dir.Truncated})
		}
	}
	if call == nil { // a refresh
		t.reading = false
		if t.again {
			t.again = false
			return m.webTreeRefresh()
		}
	}
	return nil
}

// webTreeBuild turns raw entries into the listing the browser sees:
// dotfiles per the laptop's toggle, folders first, names cleaned, git
// letters, at most MaxTreeEntries.
func (m *Model) webTreeBuild(rel string, raw []webRawEntry, hidden bool) webproto.TreeDir {
	files, dirs := m.webGitBadges(rel)
	entries := make([]webproto.TreeEntry, 0, len(raw))
	for _, r := range raw {
		dot := strings.HasPrefix(r.name, ".")
		if dot && !hidden {
			continue
		}
		e := webproto.TreeEntry{Name: webName(r.name), Dir: r.dir, Size: r.size, Mtime: r.mtime,
			Hidden: dot, Ignored: explorer.IgnoredName(r.name, r.dir)}
		e.Git = files[r.name] // an untracked folder is one status line
		if r.dir && e.Git == "" && dirs[r.name] {
			e.Git = "M"
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		return a.Name < b.Name
	})
	out := webproto.TreeDir{Entries: entries}
	if len(entries) > webproto.MaxTreeEntries {
		out.Entries, out.Truncated = entries[:webproto.MaxTreeEntries], true
	}
	return out
}

// webGitBadges returns the git letter of each changed file directly in
// folder rel, and the child folders with a change somewhere inside.
func (m *Model) webGitBadges(rel string) (files map[string]string, dirs map[string]bool) {
	files, dirs = map[string]string{}, map[string]bool{}
	if !m.gitIsRepo {
		return files, dirs
	}
	prefix := ""
	if rel != "" {
		prefix = rel + "/"
	}
	for _, f := range m.gitFiles {
		p := strings.TrimSuffix(filepath.ToSlash(f.Path), "/")
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := p[len(prefix):]
		if child, _, nested := strings.Cut(rest, "/"); nested {
			dirs[child] = true
			continue
		}
		letter := ""
		switch {
		case f.Untracked():
			letter = "?"
		case len(f.Code) >= 1 && f.Code[0] != ' ':
			letter = string(f.Code[0])
		case len(f.Code) >= 2 && f.Code[1] != ' ':
			letter = string(f.Code[1])
		}
		if letter != "" {
			files[rest] = letter
		}
	}
	return files, dirs
}
