package transfer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── Encoders: golden bytes ──

// The example from the kitty spec: action=send id=test name=somefile
// size=3 data=01 02 03.
func TestKittyCmdGolden(t *testing.T) {
	got := KittyCmd(KV{"ac", "send"}, KV{"id", "test"}, KV{"n", B64("somefile")}, KV{"sz", "3"}, KV{"d", base64.StdEncoding.EncodeToString([]byte{1, 2, 3})})
	want := "\x1b]5113;ac=send;id=test;n=c29tZWZpbGU=;sz=3;d=AQID\x1b\\"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestParseKittyReply(t *testing.T) {
	r := ParseKittyReply("ac=status;id=x1;fid=f;st=" + B64("EPERM:User refused") + ";sz=12")
	if r["ac"] != "status" || r["id"] != "x1" || r["fid"] != "f" || r["sz"] != "12" {
		t.Fatalf("bad parse: %v", r)
	}
	if r.Status() != "EPERM:User refused" {
		t.Errorf("status %q", r.Status())
	}
	// A plain (not base64) status is read as it is.
	if s := ParseKittyReply("ac=status;id=x;st=OK").Status(); s != "OK" {
		t.Errorf("plain status %q", s)
	}
}

func TestITermFileGolden(t *testing.T) {
	got := ITermFile("a b.txt", []byte("hi"))
	want := "\x1b]1337;File=name=YSBiLnR4dA==;size=2;inline=0:aGk=\x07"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if RequestUploadSeq != "\x1b]1337;RequestUpload=format=tgz\x07" {
		t.Errorf("RequestUpload %q", RequestUploadSeq)
	}
}

// ParseOSC finds every OSC of one number in a stream, with either
// terminator, and decodes an iTerm2 File back to the bytes.
func TestParseOSCRoundTrip(t *testing.T) {
	data := bytes.Repeat([]byte{0, 1, 2, 250}, 1000)
	stream := "frame\x1b[1;1H" + ITermFile("x.bin", data) + "more\x1b]52;c;eA==\x07"
	got := ParseOSC([]byte(stream), "1337")
	if len(got) != 1 {
		t.Fatalf("%d OSC 1337, want 1", len(got))
	}
	name, body, err := DecodeITermFile(got[0])
	if err != nil || name != "x.bin" || !bytes.Equal(body, data) {
		t.Fatalf("decode: name=%q err=%v equal=%v", name, err, bytes.Equal(body, data))
	}
}

// ── kitty send (download) ──

// fakeKitty plays the terminal side of a kitty send session.
type fakeKitty struct {
	t       *testing.T
	refuse  bool
	replies chan string
	buf     []byte
	data    bytes.Buffer
	zip     bool
	name    string
	cmds    []Reply
	maxD    int
}

func newFakeKitty(t *testing.T) *fakeKitty {
	return &fakeKitty{t: t, replies: make(chan string, 1<<14)}
}

func (f *fakeKitty) Write(p []byte) (int, error) {
	f.buf = append(f.buf, p...)
	for {
		i := bytes.Index(f.buf, []byte("\x1b]5113;"))
		if i < 0 {
			return len(p), nil
		}
		j := bytes.Index(f.buf[i:], []byte("\x1b\\"))
		if j < 0 {
			return len(p), nil
		}
		r := ParseKittyReply(string(f.buf[i+7 : i+j]))
		f.buf = f.buf[i+j+2:]
		f.cmds = append(f.cmds, r)
		f.on(r)
	}
}

func (f *fakeKitty) reply(kv ...KV) {
	f.replies <- strings.TrimSuffix(strings.TrimPrefix(KittyCmd(kv...), "\x1b]5113;"), "\x1b\\")
}

func (f *fakeKitty) on(r Reply) {
	id := r["id"]
	switch r["ac"] {
	case "send":
		if f.refuse {
			f.reply(KV{"ac", "status"}, KV{"id", id}, KV{"st", B64("EPERM:User refused")})
			return
		}
		f.reply(KV{"ac", "status"}, KV{"id", id}, KV{"st", B64("OK")})
	case "file":
		f.name = r.Name()
		f.zip = r["zip"] == "zlib"
		f.reply(KV{"ac", "status"}, KV{"id", id}, KV{"fid", r["fid"]}, KV{"st", B64("STARTED")})
	case "data", "end_data":
		d, err := base64.StdEncoding.DecodeString(r["d"])
		if err != nil {
			f.t.Errorf("bad base64 chunk: %v", err)
		}
		f.maxD = max(f.maxD, len(r["d"]))
		f.data.Write(d)
		f.reply(KV{"ac", "status"}, KV{"id", id}, KV{"fid", r["fid"]}, KV{"st", B64("PROGRESS")})
		if r["ac"] == "end_data" {
			f.reply(KV{"ac", "status"}, KV{"id", id}, KV{"fid", r["fid"]}, KV{"st", B64("OK")})
		}
	}
}

func (f *fakeKitty) next(ctx context.Context, d time.Duration) (Reply, error) {
	select {
	case s := <-f.replies:
		return ParseKittyReply(s), nil
	case <-time.After(d):
		return nil, ErrTimeout
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeKitty) payload(t *testing.T) []byte {
	if !f.zip {
		return f.data.Bytes()
	}
	zr, err := zlib.NewReader(bytes.NewReader(f.data.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestKittySendRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
	}{{"report.txt", 20000}, {"empty.txt", 0}, {"photo.png", 9000}} {
		data := make([]byte, tc.size)
		for i := range data {
			data[i] = byte(i * 7 % 251)
		}
		f := newFakeKitty(t)
		var progress int64
		err := KittySend(context.Background(), KittySendJob{
			ID: "tc1", Dest: "~/Downloads/" + tc.name, Size: int64(len(data)), Data: bytes.NewReader(data),
			Write: func(s string) error { _, err := f.Write([]byte(s)); return err },
			Next:  f.next, Progress: func(n int64) { progress = n },
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := f.payload(t); !bytes.Equal(got, data) {
			t.Fatalf("%s: terminal got %d bytes, want %d", tc.name, len(got), len(data))
		}
		if f.name != "~/Downloads/"+tc.name {
			t.Errorf("dest %q", f.name)
		}
		if f.maxD > 4096 {
			t.Errorf("a data chunk is %d base64 bytes, the spec allows 4096", f.maxD)
		}
		if tc.name == "photo.png" && f.zip {
			t.Error("an already compressed file was zlib compressed again")
		}
		if tc.name == "report.txt" && !f.zip {
			t.Error("a text file was not compressed")
		}
		if progress != int64(len(data)) {
			t.Errorf("progress %d, want %d", progress, len(data))
		}
		last := f.cmds[len(f.cmds)-1]
		if last["ac"] != "finish" {
			t.Errorf("last command %v, want finish", last)
		}
	}
}

func TestKittySendRefused(t *testing.T) {
	f := newFakeKitty(t)
	f.refuse = true
	err := KittySend(context.Background(), KittySendJob{
		ID: "r", Dest: "~/Downloads/x", Size: 1, Data: strings.NewReader("x"),
		Write: func(s string) error { _, err := f.Write([]byte(s)); return err }, Next: f.next,
	})
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("err %v, want refused", err)
	}
	for _, c := range f.cmds {
		if c["ac"] == "file" || c["ac"] == "data" {
			t.Fatalf("sent %v after the terminal refused", c)
		}
	}
}

// No answer (not really kitty): a clear time-out error, then cancel.
func TestKittySendNoAnswer(t *testing.T) {
	var out strings.Builder
	err := KittySend(context.Background(), KittySendJob{
		ID: "n", Dest: "~/x", Size: 1, Data: strings.NewReader("x"), PermissionWait: 50 * time.Millisecond,
		Write: func(s string) error { out.WriteString(s); return nil },
		Next: func(ctx context.Context, d time.Duration) (Reply, error) {
			time.Sleep(d)
			return nil, ErrTimeout
		},
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err %v, want time-out", err)
	}
	if !strings.Contains(out.String(), "ac=cancel") {
		t.Error("no cancel sent after the time-out")
	}
}

// ── kitty receive (upload) ──

func TestKittyReceive(t *testing.T) {
	// The terminal side: lists ~/proj (a folder with a.txt and sub/b.txt)
	// and sends the data of every requested file.
	files := map[string]string{"/home/u/proj/a.txt": "alpha", "/home/u/proj/sub/b.txt": "bravo"}
	replies := make(chan string, 100)
	put := func(kv ...KV) {
		replies <- strings.TrimSuffix(strings.TrimPrefix(KittyCmd(kv...), "\x1b]5113;"), "\x1b\\")
	}
	var sent []Reply
	write := func(s string) error {
		for _, p := range ParseOSC([]byte(s), "5113") {
			r := ParseKittyReply(string(p))
			sent = append(sent, r)
			switch {
			case r["ac"] == "file" && r["fid"] == "r0":
				if r.Name() != "~/proj" {
					t.Errorf("asked for %q", r.Name())
				}
				put(KV{"ac", "status"}, KV{"id", "u"}, KV{"st", B64("OK")})
				put(KV{"ac", "file"}, KV{"id", "u"}, KV{"fid", "r0"}, KV{"st", "t1"}, KV{"n", B64("/home/u/proj")}, KV{"ft", "directory"})
				put(KV{"ac", "file"}, KV{"id", "u"}, KV{"fid", "r0"}, KV{"st", "t2"}, KV{"n", B64("/home/u/proj/a.txt")}, KV{"ft", "regular"}, KV{"sz", "5"}, KV{"pr", "t1"})
				put(KV{"ac", "file"}, KV{"id", "u"}, KV{"fid", "r0"}, KV{"st", "t3"}, KV{"n", B64("/home/u/proj/sub")}, KV{"ft", "directory"}, KV{"pr", "t1"})
				put(KV{"ac", "file"}, KV{"id", "u"}, KV{"fid", "r0"}, KV{"st", "t4"}, KV{"n", B64("/home/u/proj/sub/b.txt")}, KV{"ft", "regular"}, KV{"sz", "5"}, KV{"pr", "t3"})
				put(KV{"ac", "file"}, KV{"id", "u"}, KV{"fid", "r0"}, KV{"st", "t5"}, KV{"n", B64("/home/u/proj/evil")}, KV{"ft", "symlink"}, KV{"pr", "t1"})
				put(KV{"ac", "status"}, KV{"id", "u"}, KV{"st", B64("OK")}, KV{"n", B64("/home/u")})
			case r["ac"] == "file":
				body := files[r.Name()]
				put(KV{"ac", "data"}, KV{"id", "u"}, KV{"fid", r["fid"]}, KV{"d", base64.StdEncoding.EncodeToString([]byte(body[:2]))})
				put(KV{"ac", "end_data"}, KV{"id", "u"}, KV{"fid", r["fid"]}, KV{"d", base64.StdEncoding.EncodeToString([]byte(body[2:]))})
			}
		}
		return nil
	}
	next := func(ctx context.Context, d time.Duration) (Reply, error) {
		select {
		case s := <-replies:
			return ParseKittyReply(s), nil
		case <-time.After(d):
			return nil, ErrTimeout
		}
	}
	staging := t.TempDir()
	err := KittyReceive(context.Background(), KittyReceiveJob{ID: "u", Path: "~/proj", Dir: staging, Write: write, Next: next})
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{"proj/a.txt": "alpha", "proj/sub/b.txt": "bravo"} {
		b, err := os.ReadFile(filepath.Join(staging, rel))
		if err != nil || string(b) != want {
			t.Errorf("%s = %q (%v), want %q", rel, b, err, want)
		}
	}
	if _, err := os.Lstat(filepath.Join(staging, "proj", "evil")); err == nil {
		t.Error("a symlink from the client was made")
	}
	if last := sent[len(sent)-1]; last["ac"] != "finish" {
		t.Errorf("last command %v, want finish", last)
	}
}

// A listed name outside the requested folder is refused (a broken or
// hostile terminal must not write outside the target).
func TestKittyReceiveRejectsEscapingNames(t *testing.T) {
	for _, bad := range []string{"/home/u/other/x.txt", "/home/u/proj/../../etc/x"} {
		replies := make(chan string, 20)
		put := func(kv ...KV) {
			replies <- strings.TrimSuffix(strings.TrimPrefix(KittyCmd(kv...), "\x1b]5113;"), "\x1b\\")
		}
		write := func(s string) error {
			for _, p := range ParseOSC([]byte(s), "5113") {
				r := ParseKittyReply(string(p))
				if r["ac"] == "file" && r["fid"] == "r0" {
					put(KV{"ac", "status"}, KV{"id", "u"}, KV{"st", B64("OK")})
					put(KV{"ac", "file"}, KV{"id", "u"}, KV{"fid", "r0"}, KV{"st", "t1"}, KV{"n", B64("/home/u/proj")}, KV{"ft", "directory"})
					put(KV{"ac", "file"}, KV{"id", "u"}, KV{"fid", "r0"}, KV{"st", "t2"}, KV{"n", B64(bad)}, KV{"ft", "regular"}, KV{"pr", "t1"})
					put(KV{"ac", "status"}, KV{"id", "u"}, KV{"st", B64("OK")})
				} else if r["ac"] == "file" {
					put(KV{"ac", "end_data"}, KV{"id", "u"}, KV{"fid", r["fid"]}, KV{"d", B64("pwned")})
				}
			}
			return nil
		}
		next := func(ctx context.Context, d time.Duration) (Reply, error) {
			select {
			case s := <-replies:
				return ParseKittyReply(s), nil
			case <-time.After(d):
				return nil, ErrTimeout
			}
		}
		root := t.TempDir()
		staging := filepath.Join(root, "in")
		_ = os.Mkdir(staging, 0o755)
		err := KittyReceive(context.Background(), KittyReceiveJob{ID: "u", Path: "~/proj", Dir: staging, Write: write, Next: next})
		if err == nil {
			t.Errorf("%s: no error", bad)
		}
		_ = filepath.Walk(root, func(p string, fi os.FileInfo, _ error) error {
			if fi != nil && !fi.IsDir() {
				t.Errorf("%s: wrote %s", bad, p)
			}
			return nil
		})
	}
}

// ── iTerm2 upload reply ──

func TestReadITermUpload(t *testing.T) {
	tgz := tarGzOf(t, map[string]string{"x.txt": "hello"})
	b64 := base64.StdEncoding.EncodeToString(tgz)
	// iTerm2 sends "ok", the base64 in lines, then an empty line.
	in := "ok\r\n" + b64[:10] + "\n" + b64[10:] + "\n\n"
	got, err := ReadITermUpload(context.Background(), strings.NewReader(in), 1<<20)
	if err != nil || !bytes.Equal(got, tgz) {
		t.Fatalf("err %v, equal %v", err, bytes.Equal(got, tgz))
	}
	if _, err := ReadITermUpload(context.Background(), strings.NewReader("abort\n"), 1<<20); !errors.Is(err, ErrCanceled) {
		t.Errorf("abort: %v", err)
	}
	if _, err := ReadITermUpload(context.Background(), strings.NewReader("\x03"), 1<<20); !errors.Is(err, ErrCanceled) {
		t.Errorf("ctrl+c: %v", err)
	}
	if _, err := ReadITermUpload(context.Background(), strings.NewReader("ok\n"+b64+"\n\n"), 10); !errors.Is(err, ErrTooBig) {
		t.Errorf("limit: %v", err)
	}
}

// ── Archives and path safety ──

func tarGzOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestTarGzRoundTrip(t *testing.T) {
	src := filepath.Join(t.TempDir(), "proj")
	must(t, os.MkdirAll(filepath.Join(src, "sub", "empty"), 0o755))
	must(t, os.WriteFile(filepath.Join(src, "a.txt"), []byte("alpha"), 0o644))
	must(t, os.WriteFile(filepath.Join(src, "sub", "b.sh"), []byte("#!/bin/sh\n"), 0o755))
	var buf bytes.Buffer
	if err := TarGz(&buf, src); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	names, err := ExtractTarGz(&buf, dest, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "proj" {
		t.Errorf("top names %v, want [proj]", names)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "proj", "a.txt")); string(b) != "alpha" {
		t.Errorf("a.txt = %q", b)
	}
	if fi, err := os.Stat(filepath.Join(dest, "proj", "sub", "b.sh")); err != nil || fi.Mode()&0o100 == 0 {
		t.Errorf("b.sh lost its exec bit: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(dest, "proj", "sub", "empty")); err != nil || !fi.IsDir() {
		t.Error("empty folder missing")
	}
}

// Extract never writes outside dest and never replaces a file: a name
// that exists gets " (1)".
func TestExtractSafety(t *testing.T) {
	dest := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dest, "x.txt"), []byte("mine"), 0o644))
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(h *tar.Header, body string) {
		h.Size = int64(len(body))
		_ = tw.WriteHeader(h)
		_, _ = tw.Write([]byte(body))
	}
	add(&tar.Header{Name: "x.txt", Mode: 0o644, Typeflag: tar.TypeReg}, "theirs")
	add(&tar.Header{Name: "../evil.txt", Mode: 0o644, Typeflag: tar.TypeReg}, "bad")
	add(&tar.Header{Name: "/abs.txt", Mode: 0o644, Typeflag: tar.TypeReg}, "bad")
	add(&tar.Header{Name: "link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink}, "")
	add(&tar.Header{Name: "hard", Linkname: "x.txt", Typeflag: tar.TypeLink}, "")
	_ = tw.Close()
	_ = gz.Close()
	names, err := ExtractTarGz(&buf, dest, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "x.txt")); string(b) != "mine" {
		t.Errorf("x.txt replaced: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "x (1).txt")); string(b) != "theirs" {
		t.Errorf("x (1).txt = %q", b)
	}
	if len(names) != 1 || names[0] != "x (1).txt" {
		t.Errorf("names %v", names)
	}
	for _, p := range []string{filepath.Join(filepath.Dir(dest), "evil.txt"), filepath.Join(dest, "link"), filepath.Join(dest, "hard"), filepath.Join(dest, "abs.txt")} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s was written", p)
		}
	}
	// Too big: refused, nothing left behind.
	dest2 := t.TempDir()
	if _, err := ExtractTarGz(bytes.NewReader(tarGzOf(t, map[string]string{"big": strings.Repeat("x", 100)})), dest2, 10); !errors.Is(err, ErrTooBig) {
		t.Errorf("limit: %v", err)
	}
	if ents, _ := os.ReadDir(dest2); len(ents) != 0 {
		t.Errorf("left %v after a refused extract", ents)
	}
}

func TestUniqueName(t *testing.T) {
	dir := t.TempDir()
	if got := UniqueName(dir, "a.txt"); got != "a.txt" {
		t.Errorf("free name: %q", got)
	}
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "a (1).txt"), nil, 0o644))
	if got := UniqueName(dir, "a.txt"); got != "a (2).txt" {
		t.Errorf("got %q", got)
	}
	must(t, os.Mkdir(filepath.Join(dir, "proj"), 0o755))
	if got := UniqueName(dir, "proj"); got != "proj (1)" {
		t.Errorf("folder: %q", got)
	}
}

func TestInside(t *testing.T) {
	root := t.TempDir()
	for p, want := range map[string]bool{
		root:                           true,
		filepath.Join(root, "a", "b"):  true,
		filepath.Join(root, "..", "x"): false,
		filepath.Dir(root):             false,
		root + "-other":                false,
		filepath.Join(root, "a", "..", "..", "y"): false,
	} {
		if got := Inside(root, p); got != want {
			t.Errorf("Inside(%q) = %v, want %v", p, got, want)
		}
	}
}

// ── Capability selection ──

func TestDetect(t *testing.T) {
	cases := []struct {
		env          map[string]string
		down, up     Method
		terminalName string
	}{
		{map[string]string{"TERM": "xterm-kitty"}, Kitty, Kitty, "kitty"},
		{map[string]string{"KITTY_WINDOW_ID": "3", "TERM": "xterm-256color"}, Kitty, Kitty, "kitty"},
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, ITerm2, ITerm2, "iTerm2"},
		// Over SSH only LC_* is sent by default; iTerm2 sets LC_TERMINAL.
		{map[string]string{"LC_TERMINAL": "iTerm2", "TERM": "xterm-256color"}, ITerm2, ITerm2, "iTerm2"},
		{map[string]string{"TERM_PROGRAM": "WezTerm"}, None, None, "WezTerm"},
		{map[string]string{"TERM_PROGRAM": "ghostty"}, None, None, "Ghostty"},
		{map[string]string{"TERM": "xterm-256color"}, None, None, ""},
		// tmux eats the replies: no kitty / iTerm2 transfer inside it.
		{map[string]string{"TERM": "xterm-kitty", "TMUX": "/tmp/tmux-1/default,1,0"}, None, None, "kitty"},
		// The user can force a protocol.
		{map[string]string{"TERM": "xterm-256color", "TERMOCODE_TRANSFER": "kitty"}, Kitty, Kitty, ""},
		{map[string]string{"TERM": "xterm-kitty", "TERMOCODE_TRANSFER": "off"}, None, None, "kitty"},
		{map[string]string{"TERM_PROGRAM": "WezTerm", "TERMOCODE_TRANSFER": "iterm2"}, ITerm2, ITerm2, "WezTerm"},
	}
	for _, c := range cases {
		s := Detect(func(k string) string { return c.env[k] })
		if s.Download != c.down || s.Upload != c.up || s.Terminal != c.terminalName {
			t.Errorf("%v: got %+v, want down=%v up=%v term=%q", c.env, s, c.down, c.up, c.terminalName)
		}
	}
}

// ── Fallback commands ──

func TestScpCommands(t *testing.T) {
	env := map[string]string{"SSH_CONNECTION": "10.0.0.5 50000 192.168.1.20 22", "USER": "amin"}
	get := func(k string) string { return env[k] }
	if got := ScpDownload(get, "devbox", "/srv/my proj/a.txt", false); got != "scp 'amin@192.168.1.20:/srv/my proj/a.txt' ." {
		t.Errorf("download: %q", got)
	}
	if got := ScpDownload(get, "devbox", "/srv/proj", true); got != "scp -r 'amin@192.168.1.20:/srv/proj' ." {
		t.Errorf("download dir: %q", got)
	}
	if got := ScpUpload(get, "devbox", "/srv/it's"); got != `scp -r FILE 'amin@192.168.1.20:/srv/it'\''s/'` {
		t.Errorf("upload: %q", got)
	}
	// Not over SSH: the host name.
	env = map[string]string{"USER": "amin"}
	if got := ScpDownload(get, "devbox", "/a", false); got != "scp 'amin@devbox:/a' ." {
		t.Errorf("no ssh: %q", got)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
