package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/transfer"
)

// Local transfers: termocode runs on the user's own computer (no SSH,
// transfer.IsRemote), so the files are already there. Download copies
// into the Downloads folder; Upload opens the desktop's file chooser
// (or asks for a path) and copies the chosen files into the folder.

// LocalTransferHooks stand for the desktop in tests.
type LocalTransferHooks struct {
	Downloads string                   // the Downloads folder
	Choose    func() ([]string, error) // the file chooser; nil: there is none
	Reveal    func(path string) error  // "Show in folder"
}

var (
	xferLocalMu    sync.Mutex
	xferLocalHooks *LocalTransferHooks
)

// SetLocalTransfer replaces the desktop (Downloads folder, file chooser,
// file manager) for tests. It returns a restore func.
func SetLocalTransfer(h LocalTransferHooks) (restore func()) {
	xferLocalMu.Lock()
	old := xferLocalHooks
	xferLocalHooks = &h
	xferLocalMu.Unlock()
	return func() {
		xferLocalMu.Lock()
		xferLocalHooks = old
		xferLocalMu.Unlock()
	}
}

func localHooks() *LocalTransferHooks {
	xferLocalMu.Lock()
	defer xferLocalMu.Unlock()
	return xferLocalHooks
}

// xferProgressAbove is the size from which a local copy shows progress.
var xferProgressAbove int64 = 32 << 20

type xferRevealMsg struct{ path string }

// xferChosenMsg is the file chooser's answer: paths, none (canceled), or
// an error (it did not run).
type xferChosenMsg struct {
	dir   string
	paths []string
	err   error
}

// downloadsDir is the user's Downloads folder (created when missing).
func downloadsDir() (string, error) {
	var d string
	if h := localHooks(); h != nil {
		d = h.Downloads
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		d = transfer.DownloadsDir(os.Getenv, home, runtime.GOOS, xdgUserDir)
	}
	return d, os.MkdirAll(d, 0o755)
}

// xdgUserDir runs `xdg-user-dir DOWNLOAD` ("" when it is missing).
func xdgUserDir() string {
	if _, err := xferLookPath("xdg-user-dir"); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xdg-user-dir", "DOWNLOAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// tildePath shows p with the home folder as ~.
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// ── Download ──

// startLocalDownload copies path into the Downloads folder in the
// background. No size question (it is a local copy); a big one shows its
// progress and File: Cancel Download stops it.
func (m *Model) startLocalDownload(path string, size int64) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	name := filepath.Base(path)
	run := &xferRun{name: name, cancel: cancel, ch: make(chan tea.Msg, 16)}
	m.xfer = run
	go func() {
		var last time.Time
		progress := func(n int64) {
			if size < xferProgressAbove || (time.Since(last) < 150*time.Millisecond && n < size) {
				return
			}
			last = time.Now()
			select {
			case run.ch <- xferProgressMsg{done: n, total: size}:
			default:
			}
		}
		msg := xferDoneMsg{name: name, method: transfer.Local}
		dir, err := downloadsDir()
		if err == nil {
			msg.name, err = transfer.CopyInto(ctx, path, dir, progress)
			msg.saved = filepath.Join(dir, msg.name)
		}
		msg.err = err
		run.ch <- msg
	}()
	var c tea.Cmd
	if size >= xferProgressAbove {
		m.toast, c = m.toast.PushKeyed(xferToastKey, toast.Info, "Copying "+name+"…", "to your Downloads folder")
	}
	return tea.Batch(c, waitXfer(run.ch))
}

// revealCmd shows path in the file manager ("Show in folder").
func revealCmd(path string) tea.Cmd {
	return func() tea.Msg {
		if err := reveal(path); err != nil {
			return ToastMsg{Level: toast.Errr, Title: "Could not open the folder", Body: err.Error()}
		}
		return nil
	}
}

// reveal opens the folder of path with path selected when the file
// manager can (macOS: open -R; Linux: the FileManager1 D-Bus service of
// Files / Dolphin / Nemo), else just the folder (xdg-open).
func reveal(path string) error {
	if h := localHooks(); h != nil {
		if h.Reveal == nil {
			return nil
		}
		return h.Reveal(path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if runtime.GOOS == "darwin" {
		return exec.CommandContext(ctx, "open", "-R", path).Run()
	}
	if _, err := xferLookPath("gdbus"); err == nil {
		uri := (&url.URL{Scheme: "file", Path: path}).String()
		err := exec.CommandContext(ctx, "gdbus", "call", "--session",
			"--dest", "org.freedesktop.FileManager1", "--object-path", "/org/freedesktop/FileManager1",
			"--method", "org.freedesktop.FileManager1.ShowItems", "['"+strings.ReplaceAll(uri, "'", "%27")+"']", "").Run()
		if err == nil {
			return nil
		}
	}
	// xdg-open may stay running (it waits for the app on some desktops).
	c := exec.Command("xdg-open", filepath.Dir(path))
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}

// revealLast is File: Show Last Download in Folder.
func (m *Model) revealLast() tea.Cmd {
	if m.extras.xferLast == "" {
		return m.pushToastDetail(toast.Info, "No download yet", "Download a file first (right-click it in the explorer).")
	}
	return revealCmd(m.extras.xferLast)
}

// ── Upload ──

// localUpload opens the desktop's file chooser for files to copy into
// dir; without one it asks for a path in the app.
func (m *Model) localUpload(dir string) tea.Cmd {
	title := "Upload to " + filepath.Base(dir)
	if h := localHooks(); h != nil {
		if h.Choose == nil {
			return m.openLocalUploadPrompt(dir)
		}
		choose := h.Choose
		return func() tea.Msg {
			paths, err := choose()
			return xferChosenMsg{dir: dir, paths: paths, err: err}
		}
	}
	home, _ := os.UserHomeDir()
	ch, ok := transfer.PickChooser(runtime.GOOS, os.Getenv, xferLookPath, title, home)
	if !ok {
		return m.openLocalUploadPrompt(dir)
	}
	return func() tea.Msg {
		paths, err := runChooser(ch)
		return xferChosenMsg{dir: dir, paths: paths, err: err}
	}
}

// runChooser runs the file chooser (a window of its own: the app keeps
// running) and returns the chosen paths; none when the user canceled.
func runChooser(ch transfer.Chooser) ([]string, error) {
	c := exec.Command(ch.Argv[0], ch.Argv[1:]...)
	var stderr strings.Builder
	c.Stderr = &stderr
	out, err := c.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 && !strings.Contains(strings.ToLower(stderr.String()), "display") {
		return nil, nil // canceled
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s: %s", ch.Name, msg)
	}
	return transfer.ParseChosen(out), nil
}

// openLocalUploadPrompt asks for a file or folder on this computer (Tab
// completes the path).
func (m *Model) openLocalUploadPrompt(dir string) tea.Cmd {
	m.extras.xfer = xferPending{path: dir, upload: true, method: transfer.Local}
	m.prompt = prompt.New("Upload to "+filepath.Base(dir), "File or folder to copy here (Tab completes):", "~/")
	m.prompt.SetCompleter(prompt.CompletePath)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindUploadSource
	return nil
}

// localUploadCmd copies srcs into dir in the background. Nothing is
// replaced (a taken name gets " (1)"); a folder is never copied into
// itself.
func localUploadCmd(dir string, srcs []string) tea.Cmd {
	return func() tea.Msg {
		var names []string
		for _, src := range srcs {
			if transfer.Inside(src, dir) {
				return xferUploadDoneMsg{dir: dir, names: names, err: fmt.Errorf("%s is the folder itself or holds it", src)}
			}
			name, err := transfer.CopyInto(context.Background(), src, dir, nil)
			if err != nil {
				return xferUploadDoneMsg{dir: dir, names: names, err: err}
			}
			names = append(names, name)
		}
		return xferUploadDoneMsg{dir: dir, names: names}
	}
}
