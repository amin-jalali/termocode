package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/muesli/cancelreader"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/transfer"
)

// Download / Upload between the workspace and the user's own computer,
// through the terminal (Group L3). See internal/transfer for the
// protocols. A download runs in the background: its escape sequences go
// to the terminal in whole writes (os.File serialises them with Bubble
// Tea's frame writes, so a frame never lands inside a sequence), and
// kitty's answers come back through transfer.Input. An upload needs the
// terminal's input for itself: the app hands the terminal over
// (tea.Exec) while it runs.

// Size limits. A download above xferWarnBytes asks first. iTerm2 gets the
// whole file in one sequence (held in memory), so it has a lower cap.
var (
	xferWarnBytes  int64 = 50 << 20
	xferMaxITerm   int64 = 256 << 20
	xferMaxKitty   int64 = 8 << 30
	xferMaxUpload  int64 = 2 << 30
	xferToastKey         = "transfer"
	xferKittyDest        = "~/Downloads/"
	xferStopListen       = 2 * time.Second // keep taking late kitty answers
)

// Test hooks: the fake terminal of internal/testkit.
var (
	xferHookMu  sync.Mutex
	xferHookOut io.Writer
	xferHookIn  *transfer.Input
)

// SetTransferTerminal routes transfer output to w and takes kitty answers
// from in (in.Inject), as a fake terminal does. It returns a restore
// func. For tests.
func SetTransferTerminal(w io.Writer, in *transfer.Input) (restore func()) {
	xferHookMu.Lock()
	ow, oi := xferHookOut, xferHookIn
	xferHookOut, xferHookIn = w, in
	xferHookMu.Unlock()
	return func() {
		xferHookMu.Lock()
		xferHookOut, xferHookIn = ow, oi
		xferHookMu.Unlock()
	}
}

// SetTransferWarnBytes changes the "large download" size. For tests.
func SetTransferWarnBytes(n int64) (restore func()) {
	old := xferWarnBytes
	xferWarnBytes = n
	return func() { xferWarnBytes = old }
}

// xferWrite writes one whole sequence to the terminal.
func xferWrite(s string) error {
	xferHookMu.Lock()
	w := xferHookOut
	xferHookMu.Unlock()
	if w != nil {
		_, err := io.WriteString(w, s)
		return err
	}
	if headless() {
		return errHeadless
	}
	_, err := os.Stdout.WriteString(s)
	return err
}

// xferInput is where kitty answers arrive, or nil.
func xferInput() *transfer.Input {
	xferHookMu.Lock()
	in := xferHookIn
	xferHookMu.Unlock()
	if in != nil {
		return in
	}
	return transfer.Current()
}

// xferSupport is transfer.Detect, with kitty downloads off when no input
// filter takes the answers.
func xferSupport() transfer.Support {
	s := transfer.Detect(os.Getenv)
	if s.Download == transfer.Kitty && xferInput() == nil {
		s.Download = transfer.None
		s.Why = "kitty's answers can not be read here"
	}
	return s
}

// xferRun is the running download.
type xferRun struct {
	name   string
	cancel context.CancelFunc
	ch     chan tea.Msg
}

// xferPending is a request waiting on a dialog.
type xferPending struct {
	path   string
	dir    bool
	upload bool
	method transfer.Method
	scp    string
}

type xferProgressMsg struct{ done, total int64 }

type xferDoneMsg struct {
	name   string
	method transfer.Method
	saved  string // Local: the full path of the copy
	err    error
}

type xferUploadDoneMsg struct {
	dir   string
	names []string
	err   error
}

func waitXfer(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func methodLabel(m transfer.Method) string {
	switch m {
	case transfer.Kitty:
		return "kitty"
	case transfer.ITerm2:
		return "iTerm2"
	}
	return string(m)
}

// dirSize sums the regular files below dir.
func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// insideWorkspace reports whether p (symlinks resolved) is in a root.
func insideWorkspace(p string) bool {
	rp, err := filepath.EvalSymlinks(p)
	if err != nil {
		return false
	}
	for _, r := range workspaceRoots() {
		if rr, err := filepath.EvalSymlinks(r); err == nil && transfer.Inside(rr, rp) {
			return true
		}
	}
	return false
}

var (
	sshHostOnce sync.Once
	sshHostName string
)

// sshHost is this machine's name as the user's computer may reach it
// (`hostname -f`, else the short name). The scp command uses the server
// address of SSH_CONNECTION instead when it is set.
func sshHost() string {
	sshHostOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "hostname", "-f").Output(); err == nil {
			if h := strings.TrimSpace(string(out)); h != "" && !strings.ContainsAny(h, " \t") && h != "localhost" {
				sshHostName = h
			}
		}
		if sshHostName == "" {
			sshHostName, _ = os.Hostname()
		}
		if sshHostName == "" {
			sshHostName = "HOST"
		}
	})
	return sshHostName
}

func randomID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "tc" + hex.EncodeToString(b)
}

// ── Targets for the palette ──

// xferDownloadTarget is the explorer row when the explorer has focus,
// else the open file.
func (m *Model) xferDownloadTarget() string {
	if m.focus == FocusExplorer {
		if p := m.explorer.CursorPath(); p != "" {
			return p
		}
	}
	if p := m.editor.Path(); p != "" {
		return p
	}
	return m.explorer.CursorPath()
}

// xferUploadTarget is the explorer folder (or the folder of the explorer
// file) when the explorer has focus, else the workspace root.
func (m *Model) xferUploadTarget() string {
	if m.focus == FocusExplorer {
		if p := m.explorer.CursorPath(); p != "" {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return filepath.Dir(p)
			}
			return p
		}
	}
	cwd, _ := os.Getwd()
	return cwd
}

// ── Download ──

// downloadPath starts a download of path (a file, or a folder as
// .tar.gz) to the user's computer, or explains how when the terminal
// can not.
func (m *Model) downloadPath(path string) tea.Cmd {
	if path == "" {
		return m.pushToastDetail(toast.Warn, "Nothing to download", "Pick a file or folder in the explorer.")
	}
	if m.xfer != nil {
		return m.pushToastDetail(toast.Warn, "A download is running", "Wait for it, or run File: Cancel Download.")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return m.toastErr("Download failed", err)
	}
	size := fi.Size()
	if fi.IsDir() {
		size = dirSize(path)
	}
	sup := xferSupport()
	method := sup.Download
	if method == transfer.Local {
		return m.startLocalDownload(path, size)
	}
	why := sup.Why
	limit := xferMaxKitty
	if method == transfer.ITerm2 {
		limit = xferMaxITerm
	}
	if method != transfer.None && size > limit {
		why = fmt.Sprintf("%s is %s; %s takes at most %s", filepath.Base(path), humanBytes(size), methodLabel(method), humanBytes(limit))
		method = transfer.None
	}
	if method == transfer.None {
		return m.openXferFallback(xferPending{path: path, dir: fi.IsDir()}, sup.Terminal, why)
	}
	p := xferPending{path: path, dir: fi.IsDir(), method: method}
	if size > xferWarnBytes {
		m.extras.xfer = p
		m.confirm = confirm.New("Large Download",
			fmt.Sprintf("%s is %s. Sending it through the terminal can take a long time. Download anyway?",
				filepath.Base(path), humanBytes(size)),
			[]confirm.Button{
				{ID: "download", Title: "Download", Style: confirm.StylePrimary},
				{ID: "cancel", Title: "Cancel"},
			})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindTransferLarge
		return nil
	}
	return m.startDownload(p)
}

func (m *Model) startDownload(p xferPending) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	name := filepath.Base(p.path)
	if p.dir {
		name += ".tar.gz"
	}
	run := &xferRun{name: name, cancel: cancel, ch: make(chan tea.Msg, 16)}
	m.xfer = run
	go runDownload(ctx, p, name, run.ch)
	detail := "iTerm2 saves it in your Downloads folder."
	if p.method == transfer.Kitty {
		detail = "kitty asks you to allow it, then saves it in ~/Downloads."
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushKeyed(xferToastKey, toast.Info, "Downloading "+name+"…", detail)
	return tea.Batch(c, waitXfer(run.ch))
}

// runDownload sends one file (or a folder as tar.gz) and reports on ch.
func runDownload(ctx context.Context, p xferPending, name string, ch chan<- tea.Msg) {
	err := func() error {
		var (
			r     io.Reader
			size  int64
			mode  os.FileMode = 0o644
			mtime time.Time
		)
		if p.dir {
			tmp, err := os.CreateTemp("", "termocode-download-*.tar.gz")
			if err != nil {
				return err
			}
			defer func() { _ = os.Remove(tmp.Name()) }()
			defer func() { _ = tmp.Close() }()
			if err := transfer.TarGz(tmp, p.path); err != nil {
				return err
			}
			if size, err = tmp.Seek(0, io.SeekCurrent); err != nil {
				return err
			}
			if _, err := tmp.Seek(0, io.SeekStart); err != nil {
				return err
			}
			r, mtime = tmp, time.Now()
		} else {
			f, err := os.Open(p.path)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			fi, err := f.Stat()
			if err != nil {
				return err
			}
			r, size, mode, mtime = f, fi.Size(), fi.Mode().Perm(), fi.ModTime()
		}
		var last time.Time
		progress := func(n int64) {
			if time.Since(last) < 150*time.Millisecond && n < size {
				return
			}
			last = time.Now()
			select {
			case ch <- xferProgressMsg{done: n, total: size}:
			default:
			}
		}
		switch p.method {
		case transfer.ITerm2:
			data, err := io.ReadAll(io.LimitReader(r, xferMaxITerm+1))
			if err != nil {
				return err
			}
			if int64(len(data)) > xferMaxITerm {
				return transfer.ErrTooBig
			}
			if ctx.Err() != nil {
				return transfer.ErrCanceled
			}
			return xferWrite(transfer.ITermFile(name, data))
		case transfer.Kitty:
			in := xferInput()
			if in == nil {
				return errors.New("kitty's answers can not be read here")
			}
			replies, stop := in.Listen()
			defer time.AfterFunc(xferStopListen, stop)
			return transfer.KittySend(ctx, transfer.KittySendJob{
				ID: randomID(), Dest: xferKittyDest + name, Size: size, Mode: mode, MTime: mtime, Data: r,
				Write: xferWrite, Next: transfer.ChanNext(replies), Progress: progress,
			})
		}
		return fmt.Errorf("unknown method %q", p.method)
	}()
	ch <- xferDoneMsg{name: name, method: p.method, err: err}
}

// ── Upload ──

// uploadTo asks for files from the user's computer into dir.
func (m *Model) uploadTo(dir string) tea.Cmd {
	if dir == "" {
		return nil
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return m.pushToastDetail(toast.Warn, "Upload needs a folder", dir)
	}
	if !insideWorkspace(dir) {
		return m.pushToastDetail(toast.Errr, "Upload refused", dir+" is outside the workspace.")
	}
	sup := transfer.Detect(os.Getenv)
	switch sup.Upload {
	case transfer.Local:
		return m.localUpload(dir)
	case transfer.Kitty:
		m.extras.xfer = xferPending{path: dir, upload: true, method: transfer.Kitty}
		m.prompt = prompt.New("Upload with kitty", "File or folder on your computer:", "~/")
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindUploadSource
		return nil
	case transfer.ITerm2:
		return uploadExecCmd(transfer.ITerm2, "", dir)
	}
	return m.openXferFallback(xferPending{path: dir, upload: true}, sup.Terminal, sup.Why)
}

// onUploadSource is the kitty prompt's answer.
func (m *Model) onUploadSource(src string) tea.Cmd {
	p := m.extras.xfer
	m.extras.xfer = xferPending{}
	src = strings.TrimSpace(src)
	if src == "" || p.path == "" {
		return nil
	}
	if p.method == transfer.Local {
		return localUploadCmd(p.path, []string{expandHome(src)})
	}
	return uploadExecCmd(transfer.Kitty, src, p.path)
}

// uploadExec runs an upload while the app has handed the terminal over.
type uploadExec struct {
	method transfer.Method
	src    string // kitty: the path on the user's computer
	dir    string
	stdout io.Writer
	names  []string
}

func uploadExecCmd(method transfer.Method, src, dir string) tea.Cmd {
	u := &uploadExec{method: method, src: src, dir: dir}
	return tea.Exec(u, func(err error) tea.Msg {
		return xferUploadDoneMsg{dir: dir, names: u.names, err: err}
	})
}

func (u *uploadExec) SetStdin(io.Reader)    {}
func (u *uploadExec) SetStdout(w io.Writer) { u.stdout = w }
func (u *uploadExec) SetStderr(io.Writer)   {}

func (u *uploadExec) Run() error {
	out := u.stdout
	if out == nil {
		out = os.Stdout
	}
	fd := os.Stdin.Fd()
	if term.IsTerminal(fd) {
		if st, err := term.MakeRaw(fd); err == nil {
			defer func() { _ = term.Restore(fd, st) }()
		}
	}
	cr, err := cancelreader.NewReader(os.Stdin)
	if err != nil {
		return err
	}
	defer func() { _ = cr.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	names, err := runUpload(ctx, u.method, u.src, u.dir, cr, out)
	// Stop the reader before Bubble Tea reads the terminal again.
	cr.Cancel()
	u.names = names
	return err
}

// runUpload is the upload itself, on any input / output (tests use
// pipes). It prints a short screen, runs the protocol and places the
// files in dir without replacing any.
func runUpload(ctx context.Context, method transfer.Method, src, dir string, in io.Reader, out io.Writer) ([]string, error) {
	say := func(s string) { _, _ = io.WriteString(out, strings.ReplaceAll(s, "\n", "\r\n")) }
	say("\x1b[2J\x1b[H\ntermocode: upload to " + dir + "\n\n")
	switch method {
	case transfer.ITerm2:
		say("Choose the files in iTerm2's window. Press Ctrl+C to cancel.\n")
		if _, err := io.WriteString(out, transfer.RequestUploadSeq); err != nil {
			return nil, err
		}
		data, err := transfer.ReadITermUpload(ctx, in, xferMaxUpload)
		if err != nil {
			return nil, err
		}
		say("Unpacking…\n")
		return transfer.ExtractTarGz(bytes.NewReader(data), dir, xferMaxUpload)
	case transfer.Kitty:
		say("Asking kitty for " + src + ".\nAllow it in kitty's prompt. Press Ctrl+C to cancel.\n\n")
		stage, err := os.MkdirTemp(dir, ".termocode-upload-")
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(stage) }()
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		filter := transfer.NewInput(nil)
		replies, stop := filter.Listen()
		defer stop()
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := in.Read(buf)
				if keys := filter.Feed(buf[:n]); bytes.IndexByte(keys, 0x03) >= 0 {
					cancel()
					return
				}
				if err != nil {
					return
				}
			}
		}()
		var last time.Time
		err = transfer.KittyReceive(ctx, transfer.KittyReceiveJob{
			ID: randomID(), Path: src, Dir: stage, MaxBytes: xferMaxUpload,
			Write: func(s string) error { _, err := io.WriteString(out, s); return err },
			Next:  transfer.ChanNext(replies),
			Progress: func(got, total int64) {
				if time.Since(last) > 100*time.Millisecond || got == total {
					last = time.Now()
					say("\r\x1b[K  " + humanBytes(got) + " / " + humanBytes(total))
				}
			},
		})
		if err != nil {
			return nil, err
		}
		return transfer.PlaceAll(stage, dir)
	}
	return nil, fmt.Errorf("unknown method %q", method)
}

// ── Fallback ──

// openXferFallback explains how to move the file by hand: the scp
// command (copied with one click), and trzsz when it is installed.
func (m *Model) openXferFallback(p xferPending, terminal, why string) tea.Cmd {
	name := filepath.Base(p.path)
	title := "Download " + name
	msg := "To get " + name + ", run this command on your own computer. It saves the file in the folder where you run it."
	if p.dir {
		msg = "To get the folder " + name + ", run this command on your own computer. It copies the folder into the folder where you run it."
	}
	if p.upload {
		title = "Upload to " + name
		p.scp = transfer.ScpUpload(os.Getenv, sshHost(), p.path)
		msg = "To send files into " + name + ", run this command on your own computer. Put the path of your file in place of FILE."
	} else {
		p.scp = transfer.ScpDownload(os.Getenv, sshHost(), p.path, p.dir)
	}
	tool, side := "tsz", "Download"
	if p.upload {
		tool, side = "trz", "Upload"
	}
	var note []string
	buttons := []confirm.Button{{ID: "copy", Title: "Copy Command", Style: confirm.StylePrimary}}
	if trzszInstalled(p.upload) {
		note = append(note, "trzsz is installed here: with trzsz-ssh (or a trzsz terminal plugin) on your computer, press Use trzsz to send it in this window.")
		buttons = append(buttons, confirm.Button{ID: "trzsz", Title: "Use trzsz"})
	} else {
		note = append(note, "Other ways: kitty and iTerm2 can "+strings.ToLower(side)+" with one click. "+
			"Or install trzsz here ("+tool+") and connect with trzsz-ssh: then "+side+" works in any terminal.")
	}
	if why != "" && !strings.HasPrefix(why, "this terminal") && (terminal == "" || !strings.HasPrefix(why, terminal+" has no")) {
		note = append(note, "("+why+")")
	}
	buttons = append(buttons, confirm.Button{ID: "close", Title: "Close"})
	m.extras.xfer = p
	m.confirm = confirm.New(title, msg, buttons).WithCode(p.scp, strings.Join(note, "\n"))
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindTransferFallback
	return nil
}

// xferLookPath is exec.LookPath; tests replace it.
var xferLookPath = exec.LookPath

func trzszInstalled(upload bool) bool {
	bin := "tsz"
	if upload {
		bin = "trz"
	}
	_, err := xferLookPath(bin)
	return err == nil
}

// trzszCmd runs tsz / trz with the terminal handed over.
func trzszCmd(p xferPending) tea.Cmd {
	var c *exec.Cmd
	if p.upload {
		c = exec.Command("trz", "-d")
		c.Dir = p.path
	} else if p.dir {
		c = exec.Command("tsz", "-d", p.path)
	} else {
		c = exec.Command("tsz", p.path)
	}
	return execProcess(c, func(err error) tea.Msg {
		if p.upload {
			return xferUploadDoneMsg{dir: p.path, err: err}
		}
		if err != nil {
			return ToastMsg{Level: toast.Errr, Title: "trzsz failed", Body: err.Error()}
		}
		return nil
	})
}

// onXferConfirm handles the fallback and "large download" dialogs.
func (m *Model) onXferConfirm(kind confirmKindEnum, id string) tea.Cmd {
	p := m.extras.xfer
	m.extras.xfer = xferPending{}
	switch {
	case kind == confirmKindTransferLarge && id == "download":
		return m.startDownload(p)
	case kind == confirmKindTransferFallback && id == "copy":
		// Only say "copied" when a clipboard took it (writeClipboard).
		switch m.writeClipboard(p.scp) {
		case clipLocal:
			return m.pushToastDetail(toast.Info, "Command copied", p.scp)
		case clipTerminal:
			return m.pushToastDetail(toast.Info, "Command copied (via terminal)", p.scp)
		}
		return m.pushToastDetail(toast.Warn, "Could not copy the command", clipboardHelp)
	case kind == confirmKindTransferFallback && id == "trzsz":
		return trzszCmd(p)
	}
	return nil
}

// cancelDownload stops the running download.
func (m *Model) cancelDownload() tea.Cmd {
	if m.xfer == nil {
		return m.pushToastDetail(toast.Info, "No download is running", "")
	}
	m.xfer.cancel()
	return nil
}

// handleXferMsg handles the transfer messages.
func (m *Model) handleXferMsg(msg tea.Msg) (tea.Cmd, bool) {
	var c tea.Cmd
	switch msg := msg.(type) {
	case xferProgressMsg:
		if m.xfer == nil {
			return nil, true
		}
		pct := 0
		if msg.total > 0 {
			pct = int(msg.done * 100 / msg.total)
		}
		m.toast, c = m.toast.PushKeyed(xferToastKey, toast.Info,
			fmt.Sprintf("Downloading %s… %d%%", m.xfer.name, pct),
			humanBytes(msg.done)+" of "+humanBytes(msg.total))
		return tea.Batch(c, waitXfer(m.xfer.ch)), true
	case xferDoneMsg:
		m.xfer = nil
		m.toast = m.toast.Dismiss(xferToastKey)
		switch {
		case errors.Is(msg.err, transfer.ErrCanceled), errors.Is(msg.err, context.Canceled):
			m.toast, c = m.toast.Push(toast.Info, "Download canceled")
		case msg.err != nil:
			m.toast, c = m.toast.PushDetail(toast.Errr, "Download failed", msg.err.Error())
		case msg.method == transfer.Local:
			m.extras.xferLast = msg.saved
			m.toast, c = m.toast.PushClick(toast.Info, "Saved to "+tildePath(msg.saved),
				"Show in folder (click here)", xferRevealMsg{path: msg.saved})
		case msg.method == transfer.Kitty:
			m.toast, c = m.toast.PushDetail(toast.Info, "Downloaded "+msg.name, "Saved in "+xferKittyDest+" on your computer.")
		default:
			m.toast, c = m.toast.PushDetail(toast.Info, "Sent "+msg.name, "iTerm2 saves it in your Downloads folder.")
		}
		return c, true
	case xferRevealMsg:
		return revealCmd(msg.path), true
	case xferChosenMsg:
		switch {
		case msg.err != nil:
			// The chooser did not open: ask for the path instead.
			c = m.openLocalUploadPrompt(msg.dir)
			var tc tea.Cmd
			m.toast, tc = m.toast.PushDetail(toast.Warn, "The file chooser did not open", msg.err.Error())
			return tea.Batch(c, tc), true
		case len(msg.paths) == 0:
			return nil, true // canceled
		}
		return localUploadCmd(msg.dir, msg.paths), true
	case xferUploadDoneMsg:
		m.explorer.Reload()
		switch {
		case errors.Is(msg.err, transfer.ErrCanceled), errors.Is(msg.err, context.Canceled):
			m.toast, c = m.toast.Push(toast.Info, "Upload canceled")
		case msg.err != nil:
			m.toast, c = m.toast.PushDetail(toast.Errr, "Upload failed", msg.err.Error())
		case len(msg.names) > 0:
			m.explorer.RevealPath(filepath.Join(msg.dir, msg.names[0]))
			m.toast, c = m.toast.PushDetail(toast.Info, "Uploaded "+strings.Join(msg.names, ", "), "into "+msg.dir)
		}
		return c, true
	}
	return nil, false
}
