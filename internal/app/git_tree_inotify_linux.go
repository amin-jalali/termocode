//go:build linux

package app

import (
	"encoding/binary"
	"errors"
	"sync"

	"golang.org/x/sys/unix"
)

// File events for the work tree watch (git_tree_watch.go): one inotify
// instance for the whole process, one watch per folder, shared by the
// gates that want it. Its reader never ends; an event only marks the
// gates of its folder.

const gitTreeWatchSupported = true

const gitTreeInotifyMask = unix.IN_MODIFY | unix.IN_ATTRIB | unix.IN_CREATE | unix.IN_DELETE |
	unix.IN_MOVED_FROM | unix.IN_MOVED_TO | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF | unix.IN_ONLYDIR

type gitTreeWatchDir struct {
	wd    int32
	gates map[*gitPollGate]bool
}

var gitTreeInotify struct {
	once  sync.Once
	fd    int
	err   error
	mu    sync.Mutex
	dirs  map[string]*gitTreeWatchDir
	byWD  map[int32]string
	mine  map[*gitPollGate][]string
	ready bool
}

func gitTreeInotifyStart() bool {
	w := &gitTreeInotify
	w.once.Do(func() {
		w.fd, w.err = unix.InotifyInit1(unix.IN_CLOEXEC)
		if w.err != nil {
			return
		}
		w.dirs = map[string]*gitTreeWatchDir{}
		w.byWD = map[int32]string{}
		w.mine = map[*gitPollGate][]string{}
		w.ready = true
		go gitTreeInotifyRead(w.fd)
	})
	return w.ready
}

// gitTreeWatchDirs makes g's watched folders exactly dirs. false (and
// no watches for g) when the system has no file events or no more
// watches; the gate then uses the stat scan.
func gitTreeWatchDirs(g *gitPollGate, dirs []string) bool {
	if !gitTreeInotifyStart() {
		return false
	}
	w := &gitTreeInotify
	w.mu.Lock()
	defer w.mu.Unlock()
	want := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		want[d] = true
	}
	for _, d := range w.mine[g] {
		if !want[d] {
			gitTreeInotifyDrop(g, d)
		}
	}
	w.mine[g] = nil
	for _, d := range dirs {
		e := w.dirs[d]
		if e == nil {
			wd, err := unix.InotifyAddWatch(w.fd, d, gitTreeInotifyMask)
			if err != nil {
				if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR) {
					continue // gone since git listed it
				}
				// Out of watches (ENOSPC) or not allowed: no events.
				for _, d := range w.mine[g] {
					gitTreeInotifyDrop(g, d)
				}
				delete(w.mine, g)
				return false
			}
			e = &gitTreeWatchDir{wd: int32(wd), gates: map[*gitPollGate]bool{}}
			w.dirs[d] = e
			w.byWD[e.wd] = d
		}
		e.gates[g] = true
		w.mine[g] = append(w.mine[g], d)
	}
	return true
}

// gitTreeUnwatch drops all of g's watches.
func gitTreeUnwatch(g *gitPollGate) {
	w := &gitTreeInotify
	if !w.ready {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, d := range w.mine[g] {
		gitTreeInotifyDrop(g, d)
	}
	delete(w.mine, g)
}

// gitTreeInotifyDrop takes g off folder d; the last gate removes the
// watch. Under the lock.
func gitTreeInotifyDrop(g *gitPollGate, d string) {
	w := &gitTreeInotify
	e := w.dirs[d]
	if e == nil {
		return
	}
	delete(e.gates, g)
	if len(e.gates) == 0 {
		_, _ = unix.InotifyRmWatch(w.fd, uint32(e.wd))
		delete(w.dirs, d)
		delete(w.byWD, e.wd)
	}
}

// gitTreeInotifyRead reads the events for ever and marks the gates.
func gitTreeInotifyRead(fd int) {
	buf := make([]byte, 64*1024)
	for {
		n, err := unix.Read(fd, buf)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return
		}
		gitTreeInotifyEvents(buf[:n])
	}
}

// gitTreeInotifyEvents marks the gates of each event's folder. An event
// about the ".git" folder itself is not a work tree change. A full
// queue marks every gate.
func gitTreeInotifyEvents(b []byte) {
	w := &gitTreeInotify
	w.mu.Lock()
	defer w.mu.Unlock()
	for len(b) >= unix.SizeofInotifyEvent {
		wd := int32(binary.NativeEndian.Uint32(b[0:]))
		mask := binary.NativeEndian.Uint32(b[4:])
		nameLen := int(binary.NativeEndian.Uint32(b[12:]))
		end := unix.SizeofInotifyEvent + nameLen
		if end > len(b) {
			return
		}
		name := b[unix.SizeofInotifyEvent:end]
		for len(name) > 0 && name[len(name)-1] == 0 {
			name = name[:len(name)-1]
		}
		b = b[end:]
		if mask&unix.IN_Q_OVERFLOW != 0 {
			for g := range w.mine {
				g.treeDirty.Store(true)
			}
			continue
		}
		d, ok := w.byWD[wd]
		if !ok {
			continue
		}
		e := w.dirs[d]
		if mask&unix.IN_IGNORED != 0 { // the folder is gone: the kernel dropped the watch
			delete(w.dirs, d)
			delete(w.byWD, wd)
			for g := range e.gates {
				g.treeStale.Store(true) // watch it again when it comes back
			}
		}
		if string(name) == ".git" {
			continue
		}
		for g := range e.gates {
			g.treeDirty.Store(true)
		}
	}
}

// gitTreeOnNetworkFS: a folder on a network or FUSE file system, where
// file events miss changes made on other machines.
func gitTreeOnNetworkFS(dir string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return false
	}
	switch uint32(st.Type) {
	case unix.NFS_SUPER_MAGIC, unix.SMB_SUPER_MAGIC, unix.SMB2_SUPER_MAGIC, unix.CIFS_SUPER_MAGIC,
		unix.FUSE_SUPER_MAGIC, unix.V9FS_MAGIC, unix.AFS_SUPER_MAGIC, unix.CEPH_SUPER_MAGIC:
		return true
	}
	return false
}
