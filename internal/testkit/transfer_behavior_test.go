package testkit

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/transfer"
)

// fakeTerminal stands for the user's terminal in the transfer tests: it
// keeps everything termocode writes to it and, as kitty, answers the
// file transfer commands (through transfer.Input, like real input).
type fakeTerminal struct {
	mu    sync.Mutex
	out   []byte
	in    *transfer.Input
	kitty bool
	data  bytes.Buffer // kitty: the data of file f1
	zip   bool
	dest  string
	done  bool // kitty: finish seen
}

func newFakeTerminal(t *testing.T, kitty bool) *fakeTerminal {
	f := &fakeTerminal{in: transfer.NewInput(nil), kitty: kitty}
	t.Cleanup(app.SetTransferTerminal(f, f.in))
	return f
}

func (f *fakeTerminal) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.out = append(f.out, p...)
	if !f.kitty {
		return len(p), nil
	}
	for _, pl := range transfer.ParseOSC(p, "5113") {
		r := transfer.ParseKittyReply(string(pl))
		reply := func(kv ...transfer.KV) {
			s := transfer.KittyCmd(append([]transfer.KV{{K: "ac", V: "status"}, {K: "id", V: r["id"]}}, kv...)...)
			f.in.Inject(strings.TrimSuffix(strings.TrimPrefix(s, "\x1b]5113;"), "\x1b\\"))
		}
		switch r["ac"] {
		case "send":
			reply(transfer.KV{K: "st", V: transfer.B64("OK")})
		case "file":
			f.dest, f.zip = r.Name(), r["zip"] == "zlib"
			reply(transfer.KV{K: "fid", V: r["fid"]}, transfer.KV{K: "st", V: transfer.B64("STARTED")})
		case "data", "end_data":
			b, _ := base64.StdEncoding.DecodeString(r["d"])
			f.data.Write(b)
			if r["ac"] == "end_data" {
				reply(transfer.KV{K: "fid", V: r["fid"]}, transfer.KV{K: "st", V: transfer.B64("OK")})
			}
		case "finish":
			f.done = true
		}
	}
	return len(p), nil
}

// kittyFile is the file the fake kitty saved, unpacked.
func (f *fakeTerminal) kittyFile(t *testing.T) (string, []byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.done {
		return "", nil, false
	}
	b := f.data.Bytes()
	if f.zip {
		zr, err := zlib.NewReader(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if b, err = io.ReadAll(zr); err != nil {
			t.Fatal(err)
		}
	}
	return f.dest, b, true
}

// itermFile is the first OSC 1337 File the terminal got.
func (f *fakeTerminal) itermFile(t *testing.T) (string, []byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seqs := transfer.ParseOSC(f.out, "1337")
	if len(seqs) == 0 {
		return "", nil, false
	}
	name, data, err := transfer.DecodeITermFile(seqs[0])
	if err != nil {
		t.Fatal(err)
	}
	return name, data, true
}

func (f *fakeTerminal) wrote() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.out)
}

func untarGz(t *testing.T, b []byte) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(tr)
		out[h.Name] = string(body)
	}
}

// Spec: Given termocode in kitty,
// when the user right-clicks a.txt in the explorer and picks Download,
// then kitty gets the file with its file transfer protocol (the bytes
// decode back to a.txt) in ~/Downloads, and a toast says it is done.
func TestDownloadFileWithKitty(t *testing.T) {
	t.Setenv(transfer.EnvOverride, "kitty")
	term := newFakeTerminal(t, true)
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "a.txt")
	clickText(d, "Download")
	var dest string
	var got []byte
	if !eventually(d, func() bool {
		var ok bool
		dest, got, ok = term.kittyFile(t)
		return ok
	}) {
		t.Fatalf("kitty got no finished file:\n%s", screen(d))
	}
	if string(got) != "alpha\nbravo\ncharlie\n" || dest != "~/Downloads/a.txt" {
		t.Fatalf("kitty saved %q at %q", got, dest)
	}
	if !d.WaitForText("Downloaded a.txt", 3*time.Second) {
		t.Errorf("no done toast:\n%s", screen(d))
	}
}

// Spec: Given termocode in iTerm2,
// when the user downloads the folder notes,
// then iTerm2 gets one OSC 1337 File "notes.tar.gz" whose tar.gz holds
// both notes files.
func TestDownloadFolderWithITerm2(t *testing.T) {
	t.Setenv(transfer.EnvOverride, "iterm2")
	term := newFakeTerminal(t, false)
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "notes")
	clickText(d, "Download as .tar.gz")
	var name string
	var data []byte
	if !eventually(d, func() bool {
		var ok bool
		name, data, ok = term.itermFile(t)
		return ok
	}) {
		t.Fatalf("no OSC 1337 File written:\n%s", screen(d))
	}
	if name != "notes.tar.gz" {
		t.Errorf("name %q", name)
	}
	files := untarGz(t, data)
	if files["notes/todo.md"] != "# Todo\n\n- milk\n" || files["notes/done.md"] != "# Done\n" {
		t.Errorf("archive holds %v", files)
	}
}

// Spec: Given a terminal without file transfer,
// when the user downloads a.txt,
// then nothing is written to the terminal and a dialog shows the scp
// command for a.txt; Copy Command copies it.
func TestDownloadFallbackShowsScp(t *testing.T) {
	t.Setenv(transfer.EnvOverride, "off")
	term := newFakeTerminal(t, false)
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "a.txt")
	clickText(d, "Download")
	if !d.WaitForText("scp", 2*time.Second) || !strings.Contains(screen(d), "workspace/a.txt'") {
		t.Fatalf("no scp command:\n%s", screen(d))
	}
	clip := stubClipboard(t, true)
	clickText(d, "Copy Command")
	if !d.WaitForText("Command copied", 2*time.Second) {
		t.Errorf("no copied toast:\n%s", screen(d))
	}
	if got := clip.text(); !strings.HasPrefix(got, "scp '") || !strings.HasSuffix(got, "workspace/a.txt' .") {
		t.Errorf("copied %q", got)
	}
	if term.wrote() != 0 {
		t.Errorf("wrote %d bytes to the terminal", term.wrote())
	}
}

// Spec: Given the scp dialog and a clipboard that does not take the text,
// when the user presses Copy Command,
// then termocode does not claim it copied anything.
func TestDownloadFallbackCopyFails(t *testing.T) {
	t.Setenv(transfer.EnvOverride, "off")
	newFakeTerminal(t, false)
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "a.txt")
	clickText(d, "Download")
	if !d.WaitForText("Copy Command", 2*time.Second) {
		t.Fatalf("no dialog:\n%s", screen(d))
	}
	stubClipboard(t, false)
	clickText(d, "Copy Command")
	if !d.WaitForText("Could not copy", 2*time.Second) {
		t.Errorf("no failure toast:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "Command copied") {
		t.Errorf("claims copied:\n%s", screen(d))
	}
}

// fakeClipboard records what Copy Command copied.
type fakeClipboard struct {
	mu  sync.Mutex
	got string
}

func (c *fakeClipboard) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.got
}

// stubClipboard replaces the clipboard (CI machines have none); ok is
// what the stub reports.
func stubClipboard(t *testing.T, ok bool) *fakeClipboard {
	c := &fakeClipboard{}
	t.Cleanup(app.SetTransferClipboard(func(s string) bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		if ok {
			c.got = s
		}
		return ok
	}))
	return c
}

// Spec: Given a terminal without file transfer,
// when the user right-clicks the folder notes and picks Upload Here...,
// then a dialog shows the scp command that copies into notes.
func TestUploadFallbackShowsScp(t *testing.T) {
	t.Setenv(transfer.EnvOverride, "off")
	newFakeTerminal(t, false)
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "notes")
	clickText(d, "Upload Here...")
	if !d.WaitForText("scp -r FILE", 2*time.Second) || !strings.Contains(screen(d), "notes/'") {
		t.Fatalf("no scp upload command:\n%s", screen(d))
	}
}

// Spec: Given iTerm2 and a "large" file (the limit is set low),
// when the user downloads it,
// then a dialog asks first; Cancel sends nothing, Download sends it.
func TestDownloadLargeAsksFirst(t *testing.T) {
	t.Setenv(transfer.EnvOverride, "iterm2")
	t.Cleanup(app.SetTransferWarnBytes(5))
	term := newFakeTerminal(t, false)
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "b.txt")
	clickText(d, "Download")
	if !d.WaitForText("Large Download", 2*time.Second) {
		t.Fatalf("no warning:\n%s", screen(d))
	}
	clickButton(d, "Cancel")
	d.Settle(100 * time.Millisecond)
	if term.wrote() != 0 {
		t.Fatal("sent after Cancel")
	}
	rightClickExplorer(d, "b.txt")
	clickText(d, "Download")
	d.WaitForText("Large Download", 2*time.Second)
	clickButton(d, "Download")
	if !eventually(d, func() bool { _, b, ok := term.itermFile(t); return ok && string(b) == "one\ntwo\nthree\n" }) {
		t.Fatalf("not sent after Download:\n%s", screen(d))
	}
}

// Spec: Given iTerm2 and c.txt under the explorer cursor,
// when the user runs File: Download to This Computer from the palette,
// then c.txt is sent.
func TestDownloadFromPalette(t *testing.T) {
	t.Setenv(transfer.EnvOverride, "iterm2")
	term := newFakeTerminal(t, false)
	d := New(t, Options{Files: textFiles()})
	clickExplorer(d, "c.txt")
	d.Palette("transfer-download")
	if !eventually(d, func() bool {
		n, b, ok := term.itermFile(t)
		return ok && n == "c.txt" && string(b) == "red\ngreen\nblue\n"
	}) {
		t.Fatalf("c.txt not sent:\n%s", screen(d))
	}
}
