package transfer

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scriptTerm is a scripted kitty terminal. Its answers are queued while
// termocode writes, so Next never has to wait: an empty queue is the
// wait running out (ErrTimeout). late hides answers from a Next that
// does not wait (wait 0), like answers still on the wire.
type scriptTerm struct {
	t        *testing.T
	queue    []string
	sent     []Reply
	onCmd    func(r Reply, put func(kv ...KV))
	failAt   int // the n-th Write fails (1-based), 0: never
	writes   int
	late     bool
	nextErr  error // returned by Next instead of a reply
	cancelOn string
	cancel   context.CancelFunc
}

func (s *scriptTerm) put(kv ...KV) {
	s.queue = append(s.queue, strings.TrimSuffix(strings.TrimPrefix(KittyCmd(kv...), "\x1b]5113;"), "\x1b\\"))
}

func (s *scriptTerm) write(str string) error {
	s.writes++
	if s.failAt == s.writes {
		return errors.New("terminal gone")
	}
	for _, p := range ParseOSC([]byte(str), "5113") {
		r := ParseKittyReply(string(p))
		s.sent = append(s.sent, r)
		if s.cancelOn != "" && r["ac"] == s.cancelOn && s.cancel != nil {
			s.cancel()
		}
		if s.onCmd != nil {
			s.onCmd(r, s.put)
		}
	}
	return nil
}

func (s *scriptTerm) next(ctx context.Context, wait time.Duration) (Reply, error) {
	if s.nextErr != nil {
		return nil, s.nextErr
	}
	if len(s.queue) == 0 || (s.late && wait <= 0) {
		return nil, ErrTimeout
	}
	r := s.queue[0]
	s.queue = s.queue[1:]
	return ParseKittyReply(r), nil
}

func (s *scriptTerm) sentAction(ac string) bool {
	for _, r := range s.sent {
		if r["ac"] == ac {
			return true
		}
	}
	return false
}

func st(id, status string, more ...KV) []KV {
	return append([]KV{{"ac", "status"}, {"id", id}, {"st", B64(status)}}, more...)
}

// okSender answers like kitty: OK for the session, STARTED for the file,
// OK when the data ends. extra runs after each data command.
func okSender(extra func(r Reply, put func(kv ...KV))) func(Reply, func(...KV)) {
	return func(r Reply, put func(kv ...KV)) {
		switch r["ac"] {
		case "send":
			put(st("other", "EPERM:not you")...) // another session: ignored
			put(st(r["id"], "OK")...)
		case "file":
			put(st(r["id"], "STARTED", KV{"fid", "f1"})...)
		case "end_data":
			put(st(r["id"], "OK", KV{"fid", "f1"})...)
		}
		if extra != nil && (r["ac"] == "data" || r["ac"] == "end_data") {
			extra(r, put)
		}
	}
}

func sendJob(term *scriptTerm, name string, data []byte) KittySendJob {
	return KittySendJob{ID: "s1", Dest: "~/Downloads/" + name, Size: int64(len(data)), Data: bytes.NewReader(data), Write: term.write, Next: term.next}
}

func TestKittySendSendsModeAndTime(t *testing.T) {
	term := &scriptTerm{t: t, onCmd: okSender(nil)}
	j := sendJob(term, "a.png", []byte("img"))
	j.Mode = 0o640
	j.MTime = time.Unix(1700000000, 5)
	if err := KittySend(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	var meta Reply
	for _, r := range term.sent {
		if r["ac"] == "file" {
			meta = r
		}
	}
	if meta["prm"] != "416" || meta["mod"] != "1700000000000000005" {
		t.Fatalf("file command %v: want prm=416 and mod in ns", meta)
	}
}

func TestKittySendWriteFails(t *testing.T) {
	for _, at := range []int{1, 2, 3} { // ask, file, first data
		term := &scriptTerm{t: t, onCmd: okSender(nil), failAt: at}
		if err := KittySend(context.Background(), sendJob(term, "a.txt", []byte("x"))); err == nil {
			t.Errorf("write %d fails: want an error", at)
		}
	}
	// A big file fails on a later chunk.
	big := bytes.Repeat([]byte{7}, 3*kittyChunk)
	term := &scriptTerm{t: t, onCmd: okSender(nil), failAt: 4}
	if err := KittySend(context.Background(), sendJob(term, "big.png", big)); err == nil {
		t.Error("later chunk fails: want an error")
	}
}

type brokenReader struct{ after int }

func (b *brokenReader) Read(p []byte) (int, error) {
	if b.after <= 0 {
		return 0, errors.New("disk read error")
	}
	n := min(len(p), b.after)
	b.after -= n
	return n, nil
}

// A local read error (the file went away mid-download) stops the session
// and tells the terminal to cancel.
func TestKittySendSourceReadError(t *testing.T) {
	for name, after := range map[string]int{"first.png": 0, "later.png": kittyChunk + 10, "zipped.txt": 0} {
		term := &scriptTerm{t: t, onCmd: okSender(nil)}
		j := sendJob(term, name, nil)
		j.Data = &brokenReader{after: after}
		err := KittySend(context.Background(), j)
		if err == nil || !strings.Contains(err.Error(), "disk read error") {
			t.Errorf("%s: err = %v", name, err)
		}
		if !term.sentAction("cancel") {
			t.Errorf("%s: no cancel sent", name)
		}
	}
}

func TestKittySendCanceledMidway(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	term := &scriptTerm{t: t, onCmd: okSender(nil), cancelOn: "data", cancel: cancel}
	err := KittySend(ctx, sendJob(term, "big.png", bytes.Repeat([]byte{1}, 3*kittyChunk)))
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("err = %v, want canceled", err)
	}
	if !term.sentAction("cancel") {
		t.Error("no cancel sent")
	}
}

// The terminal reports a problem while data is still being sent: disk
// full, or the user said no to this one file.
func TestKittySendErrorWhileSending(t *testing.T) {
	for status, want := range map[string]string{"ENOSPC:disk full": "ENOSPC", "EPERM:denied": "refused in the terminal"} {
		status := status
		term := &scriptTerm{t: t, onCmd: okSender(func(r Reply, put func(kv ...KV)) {
			put(st("other", "EIO:not ours", KV{"fid", "f1"})...)
			put(st(r["id"], status, KV{"fid", "f1"})...)
		})}
		err := KittySend(context.Background(), sendJob(term, "big.png", bytes.Repeat([]byte{1}, 2*kittyChunk)))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v", status, err)
		}
	}
}

// The final OK comes later than the last chunk: termocode waits for it.
func TestKittySendWaitsForLateOK(t *testing.T) {
	term := &scriptTerm{t: t, onCmd: okSender(nil), late: true}
	var progress []int64
	j := sendJob(term, "a.txt", []byte("hello"))
	j.Progress = func(n int64) { progress = append(progress, n) }
	if err := KittySend(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	if len(progress) == 0 || progress[len(progress)-1] != 5 {
		t.Fatalf("progress %v", progress)
	}
	if last := term.sent[len(term.sent)-1]; last["ac"] != "finish" {
		t.Fatalf("last %v", last)
	}

	// The late answer is an error.
	term = &scriptTerm{t: t, late: true, onCmd: func(r Reply, put func(kv ...KV)) {
		switch r["ac"] {
		case "send":
			put(st(r["id"], "OK")...)
		case "end_data":
			put(st(r["id"], "EIO:write failed", KV{"fid", "f1"})...)
		}
	}}
	if err := KittySend(context.Background(), sendJob(term, "a.txt", []byte("x"))); err == nil || !strings.Contains(err.Error(), "EIO") {
		t.Fatalf("err = %v", err)
	}
	// Or it never comes.
	term = &scriptTerm{t: t, late: true, onCmd: func(r Reply, put func(kv ...KV)) {
		if r["ac"] == "send" {
			put(st(r["id"], "OK")...)
		}
	}}
	if err := KittySend(context.Background(), sendJob(term, "a.txt", []byte("x"))); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want time-out", err)
	}
}

func TestKittySendPermissionAnswers(t *testing.T) {
	// A plain error status for the session.
	term := &scriptTerm{t: t, onCmd: func(r Reply, put func(kv ...KV)) {
		if r["ac"] == "send" {
			put(st(r["id"], "EINVAL:bad request")...)
		}
	}}
	if err := KittySend(context.Background(), sendJob(term, "a", []byte("x"))); err == nil || !strings.Contains(err.Error(), "EINVAL") {
		t.Errorf("err = %v", err)
	}
	// The app closes while waiting for the user's OK.
	term = &scriptTerm{t: t, nextErr: context.Canceled}
	if err := KittySend(context.Background(), sendJob(term, "a", []byte("x"))); !errors.Is(err, ErrCanceled) {
		t.Errorf("err = %v, want canceled", err)
	}
}

func TestChanNext(t *testing.T) {
	ch := make(chan string, 2)
	next := ChanNext(ch)
	ctx := context.Background()
	if _, err := next(ctx, 0); !errors.Is(err, ErrTimeout) {
		t.Errorf("empty, no wait: %v", err)
	}
	ch <- "ac=status;id=a"
	if r, err := next(ctx, 0); err != nil || r["id"] != "a" {
		t.Errorf("ready, no wait: %v %v", r, err)
	}
	ch <- "ac=status;id=b"
	if r, err := next(ctx, time.Minute); err != nil || r["id"] != "b" {
		t.Errorf("ready, wait: %v %v", r, err)
	}
	if _, err := next(ctx, time.Millisecond); !errors.Is(err, ErrTimeout) {
		t.Errorf("empty, short wait: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := next(cctx, time.Minute); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
}

// A status that decodes to control bytes is not text: it is kept as sent.
func TestReplyStatusNotPrintable(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte("OK\x01"))
	if got := (Reply{"st": raw}).Status(); got != raw {
		t.Fatalf("got %q, want the raw value", got)
	}
}

// ── receive ──

// listing answers a receive like kitty: OK, then one file command per
// entry, then OK. data sends each requested file's body in two parts.
type remoteFile struct{ name, ft, body, size, prm string }

func receiver(files []remoteFile, data func(r Reply, put func(kv ...KV))) func(Reply, func(...KV)) {
	return func(r Reply, put func(kv ...KV)) {
		if r["ac"] != "file" {
			return
		}
		if r["fid"] == "r0" {
			put(st("zzz", "OK")...) // another session
			put(st(r["id"], "OK")...)
			for i, f := range files {
				kv := []KV{{"ac", "file"}, {"id", r["id"]}, {"fid", "r0"}, {"st", "t" + string(rune('a'+i))}, {"n", B64(f.name)}, {"ft", f.ft}}
				if f.size != "" {
					kv = append(kv, KV{"sz", f.size})
				}
				if f.prm != "" {
					kv = append(kv, KV{"prm", f.prm})
				}
				put(kv...)
			}
			put(st(r["id"], "OK")...)
			return
		}
		if data != nil {
			data(r, put)
			return
		}
		for _, f := range files {
			if f.name == r.Name() {
				put(KV{"ac", "data"}, KV{"id", r["id"]}, KV{"fid", r["fid"]}, KV{"d", base64.StdEncoding.EncodeToString([]byte(f.body[:len(f.body)/2]))})
				put(KV{"ac", "end_data"}, KV{"id", r["id"]}, KV{"fid", r["fid"]}, KV{"d", base64.StdEncoding.EncodeToString([]byte(f.body[len(f.body)/2:]))})
			}
		}
	}
}

func recvJob(term *scriptTerm, dir string) KittyReceiveJob {
	return KittyReceiveJob{ID: "u", Path: "~/proj", Dir: dir, Write: term.write, Next: term.next}
}

var projFiles = []remoteFile{
	{name: "/home/u/proj", ft: "directory"},
	{name: "/home/u/proj/run.sh", ft: "regular", body: "#!/bin/sh\n", size: "10", prm: "493"},
	{name: "/home/u/proj/empty", ft: "directory"},
}

func TestKittyReceiveModesAndProgress(t *testing.T) {
	term := &scriptTerm{t: t, onCmd: receiver(projFiles, nil)}
	dir := t.TempDir()
	j := recvJob(term, dir)
	var got, total int64
	j.Progress = func(g, tot int64) { got, total = g, tot }
	if err := KittyReceive(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "proj", "run.sh"))
	if err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("run.sh: %v %v, want mode 0755", fi, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "proj", "empty")); err != nil || !fi.IsDir() {
		t.Fatal("empty folder missing")
	}
	if got != 10 || total != 10 {
		t.Fatalf("progress %d/%d", got, total)
	}
}

func TestKittyReceiveRefusals(t *testing.T) {
	statusOnly := func(status string) func(Reply, func(...KV)) {
		return func(r Reply, put func(kv ...KV)) {
			if r["fid"] == "r0" {
				put(st(r["id"], status)...)
			}
		}
	}
	cases := map[string]struct {
		term *scriptTerm
		want string
	}{
		"user said no":   {&scriptTerm{onCmd: statusOnly("EPERM:User refused")}, "refused in the terminal"},
		"other error":    {&scriptTerm{onCmd: statusOnly("EINVAL:bad")}, "EINVAL"},
		"not kitty":      {&scriptTerm{}, "is this really kitty"},
		"app closing":    {&scriptTerm{nextErr: context.Canceled}, ErrCanceled.Error()},
		"terminal gone":  {&scriptTerm{failAt: 1}, "terminal gone"},
		"terminal gone2": {&scriptTerm{failAt: 2}, "terminal gone"},
		"no such file": {&scriptTerm{onCmd: func(r Reply, put func(kv ...KV)) {
			if r["fid"] == "r0" {
				put(st(r["id"], "OK")...)
				put(KV{"ac", "file"}, KV{"id", r["id"]}, KV{"fid", "r0"}, KV{"st", B64("ENOENT:no such file")})
			}
		}}, "~/proj: ENOENT"},
		"empty listing":     {&scriptTerm{onCmd: receiver(nil, nil)}, "nothing to upload"},
		"relative root":     {&scriptTerm{onCmd: receiver([]remoteFile{{name: "proj", ft: "directory"}}, nil)}, "bad name"},
		"unclean name":      {&scriptTerm{onCmd: receiver([]remoteFile{{name: "/home/u/proj", ft: "directory"}, {name: "/home/u/proj//a", ft: "regular"}}, nil)}, "bad name"},
		"root is not a dir": {&scriptTerm{onCmd: receiver([]remoteFile{{name: "/", ft: "directory"}}, nil)}, "bad name"},
		"drive name":        {&scriptTerm{onCmd: receiver([]remoteFile{{name: "/C:", ft: "directory"}}, nil)}, "bad name"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			c.term.t = t
			dir := t.TempDir()
			err := KittyReceive(context.Background(), recvJob(c.term, dir))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if c.term.writes > 0 && c.term.failAt == 0 && !c.term.sentAction("cancel") {
				t.Error("no cancel sent")
			}
			if ents, _ := os.ReadDir(dir); len(ents) != 0 {
				t.Errorf("wrote %v", ents)
			}
		})
	}
}

func TestKittyReceiveTooBig(t *testing.T) {
	// The listing says it is too big.
	term := &scriptTerm{t: t, onCmd: receiver(projFiles, nil)}
	j := recvJob(term, t.TempDir())
	j.MaxBytes = 5
	if err := KittyReceive(context.Background(), j); !errors.Is(err, ErrTooBig) {
		t.Fatalf("listed size: err = %v", err)
	}
	// The listing lies about the size: the data is counted too.
	lying := []remoteFile{{name: "/home/u/proj", ft: "directory"}, {name: "/home/u/proj/a", ft: "regular", body: strings.Repeat("x", 100), size: "1"}}
	term = &scriptTerm{t: t, onCmd: receiver(lying, nil)}
	j = recvJob(term, t.TempDir())
	j.MaxBytes = 10
	if err := KittyReceive(context.Background(), j); !errors.Is(err, ErrTooBig) {
		t.Fatalf("real size: err = %v", err)
	}
}

func TestKittyReceiveDataProblems(t *testing.T) {
	one := []remoteFile{{name: "/home/u/proj", ft: "directory"}, {name: "/home/u/proj/a", ft: "regular", body: "abc", size: "3"}}
	cases := map[string]struct {
		data func(r Reply, put func(kv ...KV))
		want string
	}{
		"read error on the far side": {func(r Reply, put func(kv ...KV)) {
			put(KV{"ac", "data"}, KV{"id", "u"}, KV{"fid", "other"}, KV{"d", B64("zz")}) // another file: ignored
			put(st("u", "PROGRESS", KV{"fid", r["fid"]})...)                             // not data: ignored
			put(st("u", "EIO:read failed", KV{"fid", r["fid"]})...)
		}, "EIO"},
		"bad base64": {func(r Reply, put func(kv ...KV)) {
			put(KV{"ac", "end_data"}, KV{"id", "u"}, KV{"fid", r["fid"]}, KV{"d", "!!!"})
		}, "illegal base64"},
		"data stops": {func(r Reply, put func(kv ...KV)) {
			put(KV{"ac", "data"}, KV{"id", "u"}, KV{"fid", r["fid"]}, KV{"d", B64("a")})
		}, ErrTimeout.Error()},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			term := &scriptTerm{t: t, onCmd: receiver(one, c.data)}
			err := KittyReceive(context.Background(), recvJob(term, t.TempDir()))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if !term.sentAction("cancel") {
				t.Error("no cancel sent")
			}
		})
	}
	t.Run("app closing", func(t *testing.T) {
		term := &scriptTerm{t: t, onCmd: receiver(one, func(r Reply, put func(kv ...KV)) {})}
		j := recvJob(term, t.TempDir())
		calls := 0
		j.Next = func(ctx context.Context, d time.Duration) (Reply, error) {
			calls++
			if calls > 5 { // the listing is read, then the app quits
				return nil, context.Canceled
			}
			return term.next(ctx, d)
		}
		if err := KittyReceive(context.Background(), j); !errors.Is(err, ErrCanceled) {
			t.Fatalf("err = %v", err)
		}
	})
}

// The target folder cannot take the files: a clear error, no panic.
func TestKittyReceivePermissionDeniedOnTarget(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	files := map[string][]remoteFile{
		"dir":  {{name: "/home/u/proj", ft: "directory"}},
		"file": {{name: "/home/u/proj", ft: "regular", body: "x", size: "1"}},
		// The root is a link (skipped); its file needs a new folder.
		"folder": {{name: "/home/u/proj", ft: "symlink"}, {name: "/home/u/proj/sub/a", ft: "regular", body: "x", size: "1"}},
	}
	for name, list := range files {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			ro := filepath.Join(dir, "ro")
			if err := os.Mkdir(ro, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
			term := &scriptTerm{t: t, onCmd: receiver(list, nil)}
			if err := KittyReceive(context.Background(), recvJob(term, ro)); !errors.Is(err, os.ErrPermission) {
				t.Fatalf("err = %v, want permission denied", err)
			}
		})
	}
	// A file that is listed twice is not written twice.
	twice := []remoteFile{{name: "/home/u/proj", ft: "directory"}, {name: "/home/u/proj/a", ft: "regular", body: "x", size: "1"}, {name: "/home/u/proj/a", ft: "regular", body: "y", size: "1"}}
	term := &scriptTerm{t: t, onCmd: receiver(twice, nil)}
	dir := t.TempDir()
	if err := KittyReceive(context.Background(), recvJob(term, dir)); !errors.Is(err, os.ErrExist) {
		t.Fatalf("listed twice: err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "proj", "a")); string(b) != "x" {
		t.Fatalf("a = %q, want the first copy", b)
	}
	// The far side cannot even be asked for the file.
	term = &scriptTerm{t: t, onCmd: receiver(projFiles, nil), failAt: 3}
	if err := KittyReceive(context.Background(), recvJob(term, t.TempDir())); err == nil {
		t.Fatal("write fails: want an error")
	}
}
