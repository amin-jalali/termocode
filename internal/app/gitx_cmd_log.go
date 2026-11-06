package app

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// The Git Command Log (GITX-33, P9): every git command termocode makes is
// kept (the last gitxCmdLogMax), so the user sees exactly what ran. The
// status reads of the panel's poll (--no-optional-locks) are left out:
// they run every few seconds and change nothing. A user or token in a
// URL is hidden and a long argument (a commit message) is cut.

const (
	gitxCmdLogMax    = 500
	gitxCmdLogArgMax = 120
)

type gitxCmdEntry struct {
	at   time.Time
	argv []string
}

var gitxCmdLog struct {
	sync.Mutex
	ring []gitxCmdEntry // oldest first
}

func init() {
	spawn.SetObserver(func(argv []string) { gitxCmdLogAdd(time.Now(), argv) })
	registerGitx(gitxFeature{
		palette: []picker.Item{{ID: "git-cmd-log", Title: "Git: Show Git Command Log"}},
		policies: map[string]aiActionPolicy{
			"git-cmd-log": aiAuto(),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			if id != "git-cmd-log" {
				return nil, false
			}
			m.ensureEditorWindowCurrent()
			if err := m.openReadOnlyPage("Git Command Log", gitxCmdLogPage(), "", "log"); err != nil {
				return m.gitxFail("Could not open the log", err), true
			}
			return nil, true
		},
	})
}

// gitxURLUser matches the user (and token) part of a URL.
var gitxURLUser = regexp.MustCompile(`://([^/@\s]+)@`)

// gitxCmdLogAdd keeps one git command (other programs and the status
// polls left out).
func gitxCmdLogAdd(at time.Time, argv []string) {
	if len(argv) == 0 || strings.TrimSuffix(filepath.Base(argv[0]), ".exe") != "git" ||
		len(argv) > 1 && argv[1] == "--no-optional-locks" {
		return
	}
	e := gitxCmdEntry{at: at, argv: make([]string, len(argv))}
	e.argv[0] = "git"
	for i := 1; i < len(argv); i++ {
		a := gitxURLUser.ReplaceAllStringFunc(argv[i], func(s string) string {
			user, _, hasPass := strings.Cut(gitxURLUser.FindStringSubmatch(s)[1], ":")
			if hasPass {
				return "://" + user + ":***@"
			}
			return "://***@"
		})
		if r := []rune(a); len(r) > gitxCmdLogArgMax {
			a = string(r[:gitxCmdLogArgMax]) + "…"
		}
		a = strings.ReplaceAll(a, "\n", "⏎")
		e.argv[i] = a
	}
	l := &gitxCmdLog
	l.Lock()
	l.ring = append(l.ring, e)
	if n := len(l.ring); n > gitxCmdLogMax {
		l.ring = append(l.ring[:0:0], l.ring[n-gitxCmdLogMax:]...)
	}
	l.Unlock()
}

func gitxCmdLogReset() {
	gitxCmdLog.Lock()
	gitxCmdLog.ring = nil
	gitxCmdLog.Unlock()
}

// gitxCmdLogPage is the log as text, newest first.
func gitxCmdLogPage() string {
	l := &gitxCmdLog
	l.Lock()
	ring := append([]gitxCmdEntry(nil), l.ring...)
	l.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "Git commands termocode ran (the last %d, newest first; the panel's status reads are not listed).\n\n", gitxCmdLogMax)
	if len(ring) == 0 {
		b.WriteString("No git command has run yet.\n")
	}
	for i := len(ring) - 1; i >= 0; i-- {
		e := ring[i]
		b.WriteString(e.at.Format("15:04:05") + "  " + strings.Join(e.argv, " ") + "\n")
	}
	return b.String()
}
