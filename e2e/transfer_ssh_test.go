//go:build e2e

package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/transfer"
)

// SCN-28: Download over SSH with each way the app knows: iTerm2's
// OSC 1337 File, the scp fallback card, trzsz (tsz on the server), and a
// large file (ask first, then cancel). "Over SSH" is the SSH_* variables
// sshd sets; no real SSH host is needed, because every method only talks
// to the terminal (the PTY of this test) or hands it over to a tool.

// sshEnv is the environment of a session that sshd started.
var sshEnv = []string{
	"SSH_CONNECTION=10.0.0.5 50000 10.0.0.9 22",
	"SSH_CLIENT=10.0.0.5 50000 22",
	"USER=dev",
}

// startSSH starts termocode in ws as if over SSH, with extra env.
func startSSH(t *testing.T, ws string, env ...string) *Term {
	t.Helper()
	tm := Start(t, StartOpts{Workspace: ws, Env: append(append([]string{}, sshEnv...), env...)})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	return tm
}

// openForDownload opens name in the editor and leaves the editor focus.
func openForDownload(tm *Term, name, first string) {
	tm.t.Helper()
	tm.OpenFile(name)
	tm.WaitText(first, uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
}

// noKitty fails when the app sent any kitty transfer command.
func noKitty(t *testing.T, tm *Term) {
	t.Helper()
	if m := kittyCmdRe.FindAllString(tm.rawOutput(), -1); len(m) > 0 {
		t.Errorf("kitty commands were sent: %q", m)
	}
}

// iTerm2 is found from LC_TERMINAL (ssh sends LC_*); the whole file goes
// in one OSC 1337 File sequence with its name.
func TestDownloadITerm2OverSSH(t *testing.T) {
	ws := NewRepo(t)
	want := bigFile(t, ws)
	tm := startSSH(t, ws, "LC_TERMINAL=iTerm2")
	openForDownload(tm, "big.txt", "line 0:")
	tm.Palette("File: Download to This Computer")
	tm.WaitText("Sent big.txt", uiTimeout)

	var got [][]byte
	for _, p := range transfer.ParseOSC([]byte(tm.rawOutput()), "1337") {
		if strings.HasPrefix(string(p), "File=") {
			got = append(got, p)
		}
	}
	if len(got) != 1 {
		t.Fatalf("want one OSC 1337 File sequence, got %d", len(got))
	}
	name, data, err := transfer.DecodeITermFile(got[0])
	if err != nil {
		t.Fatal(err)
	}
	if name != "big.txt" {
		t.Errorf("name %q", name)
	}
	if !bytes.Equal(data, want) {
		t.Errorf("iTerm2 got %d bytes, want %d", len(data), len(want))
	}
	noKitty(t, tm)
	tm.Quit()
}

// Over SSH in a terminal with no file transfer: the card shows the scp
// command for the user's own computer (user@server-address:path).
func TestDownloadScpFallbackOverSSH(t *testing.T) {
	ws := NewRepo(t)
	bigFile(t, ws)
	tm := startSSH(t, ws)
	openForDownload(tm, "big.txt", "line 0:")
	tm.Palette("File: Download to This Computer")
	tm.WaitText("Download big.txt", uiTimeout)
	tm.WaitText("Copy Command", uiTimeout)
	scr := tm.Screen()
	for _, s := range []string{"scp '", "dev@10.0.0.9:"} {
		if !strings.Contains(scr, s) {
			t.Errorf("card misses %q:\n%s", s, scr)
		}
	}
	if _, err := exec.LookPath("tsz"); err != nil && strings.Contains(scr, "Use trzsz") {
		t.Errorf("Use trzsz offered without tsz:\n%s", scr)
	}
	// Close is safe: nothing was sent.
	tm.Send(KeyEsc)
	tm.WaitGone("Copy Command", uiTimeout)
	noKitty(t, tm)
	if strings.Contains(tm.rawOutput(), "\x1b]1337;File=") {
		t.Error("an iTerm2 File sequence was sent")
	}
	tm.Quit()
}

// With tsz on the server the card offers Use trzsz; it runs tsz with the
// file, the terminal handed over, and the app comes back after it.
func TestDownloadTrzszOverSSH(t *testing.T) {
	ws := NewRepo(t)
	bigFile(t, ws)
	bin := t.TempDir()
	out := filepath.Join(t.TempDir(), "tsz-args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TSZ_OUT\"\n"
	if err := os.WriteFile(filepath.Join(bin, "tsz"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	tm := startSSH(t, ws, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "TSZ_OUT="+out)
	openForDownload(tm, "big.txt", "line 0:")
	tm.Palette("File: Download to This Computer")
	tm.WaitText("Use trzsz", uiTimeout)
	tm.settle()
	tm.Send("\t") // Copy Command → Use trzsz
	tm.settle()
	tm.Send(KeyEnter)
	deadline := time.Now().Add(uiTimeout)
	var args []byte
	for {
		if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
			args = b
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("tsz did not run:\n%s", tm.Screen())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := strings.TrimSpace(string(args)); filepath.Base(got) != "big.txt" || strings.Contains(got, "\n") {
		t.Errorf("tsz args %q, want the one path of big.txt", got)
	}
	// Back in the app after tsz ended.
	tm.WaitFor("status bar again", uiTimeout, func(ls []string) bool {
		return mainFrame.Bottom(ls[len(ls)-1])
	})
	if tm.Has("trzsz failed") {
		t.Errorf("trzsz failed:\n%s", tm.Screen())
	}
	tm.Quit()
}

// A file over the warning size asks first; Esc (the safe answer) sends
// nothing. Download starts kitty; File: Cancel Download stops it while
// kitty still waits for the user's Allow.
func TestDownloadLargeCancelOverSSH(t *testing.T) {
	ws := NewRepo(t)
	path := filepath.Join(ws, "huge.txt")
	if err := os.WriteFile(path, []byte("huge file head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm := startSSH(t, ws, "TERMOCODE_TRANSFER=kitty")
	openForDownload(tm, "huge.txt", "huge file head")
	// Grow it on disk after it was opened (sparse: no real 60 MB write).
	if err := os.Truncate(path, 60<<20); err != nil {
		t.Fatal(err)
	}

	tm.Palette("File: Download to This Computer")
	tm.WaitText("Large Download", uiTimeout)
	tm.settle()
	tm.Send(KeyEsc)
	tm.WaitGone("Large Download", uiTimeout)
	tm.settle()
	noKitty(t, tm)
	if tm.Has("Downloading huge.txt") {
		t.Errorf("Esc started the download:\n%s", tm.Screen())
	}

	tm.Palette("File: Download to This Computer")
	tm.WaitText("Large Download", uiTimeout)
	tm.settle()
	tm.Send(KeyEnter) // Download (the first button)
	tm.WaitText("Downloading huge.txt", uiTimeout)
	tm.WaitFor("kitty send command", uiTimeout, func([]string) bool {
		return len(kittyCmdRe.FindAllString(tm.rawOutput(), -1)) > 0
	})
	// A second download is refused while one runs.
	tm.Palette("File: Download to This Computer")
	tm.WaitText("A download is running", uiTimeout)
	tm.Palette("File: Cancel Download")
	tm.WaitText("Download canceled", uiTimeout)
	if tm.Has("Downloaded huge.txt") {
		t.Errorf("canceled download reported as done:\n%s", tm.Screen())
	}
	tm.Quit()
}

// Over iTerm2 a file above its cap (one sequence, held in memory) falls
// back to the scp card and says why.
func TestDownloadITerm2TooBigFallsBack(t *testing.T) {
	ws := NewRepo(t)
	path := filepath.Join(ws, "huge.txt")
	if err := os.WriteFile(path, []byte("huge file head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm := startSSH(t, ws, "LC_TERMINAL=iTerm2")
	openForDownload(tm, "huge.txt", "huge file head")
	if err := os.Truncate(path, 300<<20); err != nil {
		t.Fatal(err)
	}
	tm.Palette("File: Download to This Computer")
	tm.WaitText("Copy Command", uiTimeout)
	tm.WaitText("iTerm2 takes at most", uiTimeout)
	tm.Send(KeyEsc)
	tm.WaitGone("Copy Command", uiTimeout)
	if strings.Contains(tm.rawOutput(), "\x1b]1337;File=") {
		t.Error("an iTerm2 File sequence was sent for a file above the cap")
	}
	tm.Quit()
}
