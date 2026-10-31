package app

import (
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/git"
)

// Work tree watch (FIX-33). The idle git poll (git_poll_idle.go) checks
// only file times under .git and the root folder, so an edit another
// program made inside an existing file showed only on the 10 s safety
// step. Now the poll also knows when a file in the work tree changed:
//
//   - gitTreeWatch: file events (inotify on Linux, Termux included) on
//     every folder that holds a tracked file or a listed new file. An
//     idle tree costs nothing; an event marks the gate and the next 2 s
//     step reads git.
//   - gitTreeScan: where events are missing (other systems, network
//     folders, too many folders) each 2 s step compares the size and
//     time of the tracked files, the listed new files and their folders
//     with the ones after the last git read; no process is started.
//   - gitTreeNone: more than gitTreeMaxScan paths: the 10 s safety step
//     only, as before.
//
// The lists are made again only when the index or the listed new files
// changed (one `git ls-files` then).

type gitTreeMode int

const (
	gitTreeNone gitTreeMode = iota
	gitTreeWatch
	gitTreeScan
)

func (m gitTreeMode) String() string {
	return [...]string{"none", "watch", "scan"}[m]
}

const (
	// gitTreeMaxDirs: more folders than this use the stat scan, so one
	// big tree cannot use up the system's file watches.
	gitTreeMaxDirs = 8192
	// gitTreeMaxScan: more paths than this are not scanned every 2 s.
	gitTreeMaxScan = 10000
)

// gitTreeNoWatch makes the gate use the stat scan (tests).
var gitTreeNoWatch bool

// workTreeList is the gate's view of the work tree (under gitPollGate.mu).
type workTreeList struct {
	key   string   // top, index stamp and listed new files the lists are for
	top   string   // the work tree top
	dirs  []string // folders to watch or stamp
	paths []string // dirs plus files, for the scan
	mode  gitTreeMode
	stamp uint64 // scan: the stamp after the last git read
}

// track brings the gate's tree lists up to date after a git read and
// takes the scan stamp. files is the status of that read.
func (g *gitPollGate) track(cwd string, ok bool, files []git.FileStatus) {
	gitDir, top := findGitDir(cwd)
	if !ok || gitDir == "" {
		g.mu.Lock()
		g.tree = workTreeList{}
		g.mu.Unlock()
		gitTreeUnwatch(g)
		return
	}
	var untracked []string
	for _, f := range files {
		if f.Untracked() {
			untracked = append(untracked, f.Path)
		}
	}
	key := top + "\x00" + statStamp(filepath.Join(gitDir, "index")) + "\x00" + strings.Join(untracked, "\x00")
	g.mu.Lock()
	t := g.tree
	g.mu.Unlock()
	if g.treeStale.Swap(false) {
		t.key = ""
	}
	if t.key != key {
		t = buildWorkTreeList(top, untracked)
		t.key = key
		t.mode = gitTreeScan
		if !gitTreeNoWatch && len(t.dirs) <= gitTreeMaxDirs && !gitTreeOnNetworkFS(top) && gitTreeWatchDirs(g, t.dirs) {
			t.mode = gitTreeWatch
		} else {
			gitTreeUnwatch(g)
			if len(t.paths) > gitTreeMaxScan {
				t.mode = gitTreeNone
			}
		}
		if t.mode != gitTreeScan {
			t.paths = nil // only the scan needs them
		}
	}
	if t.mode == gitTreeScan {
		t.stamp = stampPaths(t.paths)
	}
	g.mu.Lock()
	g.tree = t
	g.mu.Unlock()
}

// buildWorkTreeList lists the folders (top, every folder of a tracked file or
// a listed new file, and their parents) and the files of the tree. A
// folder git lists as new as a whole ("dir/") is not looked into: what
// changes inside it does not change the status.
func buildWorkTreeList(top string, untracked []string) workTreeList {
	tracked, err := git.TrackedFiles(top)
	if err != nil {
		recordError("git: list tracked files: " + err.Error())
	}
	t := workTreeList{top: top}
	seen := map[string]bool{}
	var addDir func(rel string)
	addDir = func(rel string) {
		if rel == "." || rel == "" || seen[rel] {
			return
		}
		seen[rel] = true
		addDir(filepath.Dir(rel))
		t.dirs = append(t.dirs, filepath.Join(top, rel))
	}
	t.dirs = append(t.dirs, top)
	seen["."] = true
	for _, rel := range append(tracked, untracked...) {
		rel = filepath.FromSlash(strings.TrimSuffix(rel, "/"))
		addDir(filepath.Dir(rel))
		t.paths = append(t.paths, filepath.Join(top, rel))
	}
	t.paths = append(t.paths, t.dirs...)
	return t
}

// beforeRead forgets the file events seen so far: the git read that
// starts now sees those changes.
func (g *gitPollGate) beforeRead() { g.treeDirty.Store(false) }

// treeSeen reports whether the tree changed since the last git read.
func (g *gitPollGate) treeSeen() bool {
	g.mu.Lock()
	t := g.tree
	g.mu.Unlock()
	switch t.mode {
	case gitTreeWatch:
		return g.treeDirty.Load()
	case gitTreeScan:
		return stampPaths(t.paths) != t.stamp
	}
	return false
}

// treeBackend is how the gate sees tree changes (tests).
func (g *gitPollGate) treeBackend() gitTreeMode {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.tree.mode
}

// stampPaths is one hash of the size, time and mode of every path.
func stampPaths(paths []string) uint64 {
	h := fnv.New64a()
	var b []byte
	for _, p := range paths {
		b = b[:0]
		if fi, err := os.Lstat(p); err == nil {
			b = strconv.AppendInt(b, fi.Size(), 10)
			b = append(b, ':')
			b = strconv.AppendInt(b, fi.ModTime().UnixNano(), 10)
			b = append(b, ':')
			b = strconv.AppendUint(b, uint64(fi.Mode()), 10)
		} else {
			b = append(b, '-')
		}
		b = append(b, '|')
		_, _ = h.Write(b)
	}
	return h.Sum64()
}
