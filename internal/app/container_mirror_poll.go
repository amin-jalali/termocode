package app

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/lspmirror"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// RC-6: the mirror sees termocode's own saves at once (noteMirrorWrite),
// and every containerMirrorPollEvery the files changed in the container
// in the last minute by anything else (a git checkout, a build or an
// editor in the container's terminal). One `find -mmin -1` in the image;
// nothing is written there. A failing poll (an image without find)
// turns polling off for the session; LSP: Refresh Container Mirror
// still copies everything.

// containerMirrorPollEvery is the time between two polls.
var containerMirrorPollEvery = 5 * time.Second

// containerMirrorPollMax is the most files one poll copies.
const containerMirrorPollMax = 200

type (
	containerMirrorPollMsg   struct{}
	containerMirrorPolledMsg struct {
		n   int
		err error
	}
)

// pollContainerMirror is pollMirrorChanges (tests swap it).
var pollContainerMirror = pollMirrorChanges

// mirrorFindArgs is the find argv: left-out folders pruned, regular
// files changed in the last minute, relative to the root.
func mirrorFindArgs() []string {
	args := []string{"find", ".", "("}
	for i, x := range lspmirror.Excluded {
		if i > 0 {
			args = append(args, "-o")
		}
		args = append(args, "-name", x)
	}
	args = append(args, "-o", "-path", "./.git/objects", ")", "-prune", "-o", "-type", "f", "-mmin", "-1", "-print")
	return args
}

// pollMirrorChanges copies the files of root changed in the last minute
// into mr; n is how many it copied.
func pollMirrorChanges(ctx context.Context, x services.Exec, fsys workspace.FS, root string, mr *lspmirror.Mirror) (int, error) {
	out, stderr, code, err := x.Capture(ctx, root, mirrorFindArgs()...)
	if err != nil {
		return 0, err
	}
	if code != 0 {
		return 0, errors.New(strings.TrimSpace(stderr))
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		rel := strings.TrimPrefix(strings.TrimSpace(line), "./")
		if rel == "" || rel == "." || lspmirror.SkipPath(rel) {
			continue
		}
		if n >= containerMirrorPollMax {
			break
		}
		p := strings.TrimSuffix(root, "/") + "/" + rel
		data, err := fsys.ReadFile(ctx, p)
		if err != nil {
			continue // gone again, or unreadable: the next poll or a refresh
		}
		if err := mr.Put(p, data); err != nil {
			if errors.Is(err, lspmirror.ErrCap) {
				break
			}
			continue
		}
		n++
	}
	return n, nil
}

// mirrorPollTick schedules the next poll.
func mirrorPollTick() tea.Cmd {
	return tea.Tick(containerMirrorPollEvery, func(time.Time) tea.Msg { return containerMirrorPollMsg{} })
}

// mirrorPollable reports whether a poll would help now: a filled mirror
// of a workspace without a bind mount, polling not turned off.
func (m *Model) mirrorPollable() bool {
	st := m.containerLSP
	if st == nil || !st.mirrored || st.mirror == nil || st.pollOff {
		return false
	}
	h, ok := m.containerLSPHostOf()
	return ok && h.Caps().MountOf == ""
}

// startMirrorPoll starts polling after a fill (once: a poll already
// waiting or running goes on).
func (m *Model) startMirrorPoll() tea.Cmd {
	if !m.mirrorPollable() || m.containerLSP.pollTicking {
		return nil
	}
	m.containerLSP.pollTicking = true
	return mirrorPollTick()
}

// handleMirrorPollMsg runs a poll off the UI thread, then the next.
func (m *Model) handleMirrorPollMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case containerMirrorPollMsg:
		if !m.mirrorPollable() || m.containerLSP.polling {
			if m.containerLSP != nil && !m.containerLSP.polling {
				m.containerLSP.pollTicking = false
			}
			return nil, true
		}
		m.containerLSP.pollTicking = true
		st, ws := m.containerLSP, m.workspace()
		st.polling = true
		mr, root := st.mirror, ws.Root
		var x services.Exec
		var fsys workspace.FS
		if ws.Host != nil {
			x, fsys = ws.Host.Exec(), ws.Host.FS()
		}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), containerLSPTimeout)
			defer cancel()
			n, err := pollContainerMirror(ctx, x, fsys, root, mr)
			return containerMirrorPolledMsg{n: n, err: err}
		}, true
	case containerMirrorPolledMsg:
		st := m.containerLSP
		if st == nil {
			return nil, true
		}
		st.polling = false
		if msg.err != nil {
			st.pollOff, st.pollTicking = true, false
			recordError("[lsp-mirror] poll off: " + msg.err.Error())
			return nil, true
		}
		if !m.mirrorPollable() {
			st.pollTicking = false
			return nil, true
		}
		return mirrorPollTick(), true
	}
	return nil, false
}
