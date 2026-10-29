package transfer

import (
	"compress/zlib"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/crash"
)

// kitty file transfer protocol, see
// https://sw.kovidgoyal.net/kitty/file-transfer-protocol/
//
// Every command is ESC ] 5113 ; key=value ; … ESC \. Names (n), status
// (st) are base64; data (d) is base64 of at most 4096 bytes.

// KV is one key=value of a kitty command.
type KV struct{ K, V string }

// KittyCmd encodes a kitty file transfer command.
func KittyCmd(kvs ...KV) string {
	var b strings.Builder
	b.WriteString("\x1b]5113;")
	for i, kv := range kvs {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(kv.K)
		b.WriteByte('=')
		b.WriteString(kv.V)
	}
	b.WriteString("\x1b\\")
	return b.String()
}

// Reply is one decoded kitty command (usually the terminal's answer).
type Reply map[string]string

// ParseKittyReply decodes the payload of an OSC 5113 (after "5113;").
func ParseKittyReply(s string) Reply {
	r := Reply{}
	for _, kv := range strings.Split(s, ";") {
		k, v, _ := strings.Cut(kv, "=")
		if k != "" {
			r[k] = v
		}
	}
	return r
}

// Status is the decoded st value. The spec says base64; a plain word
// (some versions, or a short "OK") is read as it is.
func (r Reply) Status() string { return decodeText(r["st"]) }

// Name is the decoded n value.
func (r Reply) Name() string { return decodeText(r["n"]) }

func decodeText(v string) string {
	if v == "" {
		return ""
	}
	if b, err := base64.StdEncoding.DecodeString(v); err == nil && utf8.Valid(b) && printable(b) {
		return string(b)
	}
	return v
}

func printable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return len(b) > 0
}

// isError is a kitty error status (EPERM:…, ENOENT:…, EIO:…).
func isError(st string) bool {
	return len(st) > 1 && st[0] == 'E' && st[1] >= 'A' && st[1] <= 'Z'
}

// NextFunc returns the next reply from the terminal, waiting at most
// wait (0: only one already there). It returns ErrTimeout when none came.
type NextFunc func(ctx context.Context, wait time.Duration) (Reply, error)

// ChanNext reads replies (OSC 5113 payloads) from ch.
func ChanNext(ch <-chan string) NextFunc {
	return func(ctx context.Context, wait time.Duration) (Reply, error) {
		if wait <= 0 {
			select {
			case s := <-ch:
				return ParseKittyReply(s), nil
			default:
				return nil, ErrTimeout
			}
		}
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case s := <-ch:
			return ParseKittyReply(s), nil
		case <-t.C:
			return nil, ErrTimeout
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

const (
	kittyChunk          = 3072 // raw bytes per data command: 4096 after base64
	kittyPermissionWait = 3 * time.Minute
	kittyReplyWait      = time.Minute
)

// KittySendJob is one file to send to the terminal (a download for the
// user).
type KittySendJob struct {
	ID    string // session id: [0-9a-zA-Z_:./@-]
	Dest  string // path on the user's machine (~/ is their home)
	Size  int64
	Mode  os.FileMode
	MTime time.Time
	Data  io.Reader

	Write    func(string) error // to the terminal
	Next     NextFunc           // replies from the terminal
	Progress func(sent int64)   // optional: raw bytes sent so far

	PermissionWait time.Duration // default 3 minutes (the user clicks Allow)
}

// KittySend runs one kitty "send" session: ask, wait for the user's OK,
// send the file (zlib when it helps) and finish. On any error it cancels
// the session.
func KittySend(ctx context.Context, j KittySendJob) (err error) {
	id := j.ID
	send := func(kvs ...KV) error {
		return j.Write(KittyCmd(append([]KV{kvs[0], {"id", id}}, kvs[1:]...)...))
	}
	defer func() {
		if err != nil {
			_ = send(KV{"ac", "cancel"})
		}
	}()
	if err = send(KV{"ac", "send"}); err != nil {
		return err
	}
	if err = kittyWaitOK(ctx, j.Next, id, "", orDefault(j.PermissionWait, kittyPermissionWait)); err != nil {
		return err
	}
	zip := compressible(j.Dest)
	meta := []KV{{"ac", "file"}, {"fid", "f1"}, {"n", B64(j.Dest)}, {"sz", strconv.FormatInt(j.Size, 10)}, {"ft", "regular"}}
	if !j.MTime.IsZero() {
		meta = append(meta, KV{"mod", strconv.FormatInt(j.MTime.UnixNano(), 10)})
	}
	if p := j.Mode.Perm(); p != 0 {
		meta = append(meta, KV{"prm", strconv.Itoa(int(p))})
	}
	if zip {
		meta = append(meta, KV{"zip", "zlib"})
	}
	if err = send(meta...); err != nil {
		return err
	}

	counted := &countReader{r: j.Data}
	var src io.Reader = counted
	if zip {
		pr, pw := io.Pipe()
		crash.Go("the download", func() {
			zw := zlib.NewWriter(pw)
			_, cerr := io.Copy(zw, counted)
			if cerr == nil {
				cerr = zw.Close()
			}
			_ = pw.CloseWithError(cerr)
		})
		defer pr.Close()
		src = pr
	}
	read := func() ([]byte, bool, error) {
		b := make([]byte, kittyChunk)
		n, rerr := io.ReadFull(src, b)
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			return b[:n], true, nil
		}
		return b, false, rerr
	}
	cur, eof, err := read()
	if err != nil {
		return err
	}
	fileOK := false
	for {
		if err = ctx.Err(); err != nil {
			return ErrCanceled
		}
		next, nextEOF := []byte(nil), true
		if !eof {
			if next, nextEOF, err = read(); err != nil {
				return err
			}
		}
		last := eof || (nextEOF && len(next) == 0)
		ac := "data"
		if last {
			ac = "end_data"
		}
		if err = send(KV{"ac", ac}, KV{"fid", "f1"}, KV{"d", base64.StdEncoding.EncodeToString(cur)}); err != nil {
			return err
		}
		if j.Progress != nil {
			j.Progress(counted.n)
		}
		// Errors (disk full, refused file) come back while we send.
		for {
			r, rerr := j.Next(ctx, 0)
			if rerr != nil {
				break
			}
			if r["id"] != id {
				continue
			}
			switch st := r.Status(); {
			case strings.HasPrefix(st, "EPERM"):
				return fmt.Errorf("refused in the terminal (%s)", st)
			case isError(st):
				return errors.New(st)
			case last && st == "OK" && r["fid"] == "f1":
				fileOK = true
			}
		}
		if last {
			break
		}
		cur, eof = next, nextEOF
	}
	if !fileOK {
		if err = kittyWaitOK(ctx, j.Next, id, "f1", kittyReplyWait); err != nil {
			return err
		}
	}
	if j.Progress != nil {
		j.Progress(counted.n)
	}
	return send(KV{"ac", "finish"})
}

// kittyWaitOK waits for st=OK for the session (fid "") or one file. A
// PROGRESS / STARTED resets the wait; an error status is returned.
func kittyWaitOK(ctx context.Context, next NextFunc, id, fid string, wait time.Duration) error {
	for {
		r, err := next(ctx, wait)
		if errors.Is(err, ErrTimeout) {
			return fmt.Errorf("%w (is this really kitty?)", ErrTimeout)
		}
		if err != nil {
			return ErrCanceled
		}
		if r["id"] != id {
			continue
		}
		st := r.Status()
		switch {
		case strings.HasPrefix(st, "EPERM"):
			return fmt.Errorf("refused in the terminal (%s)", st)
		case isError(st):
			return errors.New(st)
		case st == "OK" && (fid == "" || r["fid"] == fid):
			return nil
		}
	}
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// compressible is false for files that are compressed already.
func compressible(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".gz", ".tgz", ".zip", ".xz", ".zst", ".bz2", ".7z", ".rar", ".jar", ".whl",
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".heic",
		".mp3", ".mp4", ".mkv", ".mov", ".webm", ".ogg", ".flac", ".pdf", ".woff", ".woff2":
		return false
	}
	return true
}

// KittyReceiveJob asks the terminal for one file or folder on the user's
// machine (an upload for the user).
type KittyReceiveJob struct {
	ID   string
	Path string // on the user's machine: absolute or ~/…
	Dir  string // an empty folder that gets the file or folder

	Write    func(string) error
	Next     NextFunc
	Progress func(got, total int64)
	MaxBytes int64 // 0: no limit

	PermissionWait time.Duration
}

type kittyEntry struct {
	rel    string
	ft     string
	remote string
	name   string
	size   int64
	mode   os.FileMode
}

// KittyReceive runs one kitty "receive" session. Every name the terminal
// lists must lie inside the requested path, or nothing is written.
// Symlinks and hard links are skipped.
func KittyReceive(ctx context.Context, j KittyReceiveJob) (err error) {
	id := j.ID
	send := func(kvs ...KV) error {
		return j.Write(KittyCmd(append([]KV{kvs[0], {"id", id}}, kvs[1:]...)...))
	}
	defer func() {
		if err != nil {
			_ = send(KV{"ac", "cancel"})
		}
	}()
	if err = send(KV{"ac", "receive"}, KV{"sz", "1"}); err != nil {
		return err
	}
	if err = send(KV{"ac", "file"}, KV{"fid", "r0"}, KV{"n", B64(j.Path)}); err != nil {
		return err
	}
	// Permission, then the listing, then st=OK.
	permitted := false
	var listing []Reply
	wait := orDefault(j.PermissionWait, kittyPermissionWait)
	for {
		r, rerr := j.Next(ctx, wait)
		if errors.Is(rerr, ErrTimeout) {
			return fmt.Errorf("%w (is this really kitty?)", ErrTimeout)
		}
		if rerr != nil {
			return ErrCanceled
		}
		if r["id"] != id {
			continue
		}
		st := r.Status()
		if r["ac"] == "file" {
			if isError(st) {
				return fmt.Errorf("%s: %s", j.Path, st)
			}
			permitted = true
			listing = append(listing, r)
			wait = kittyReplyWait
			continue
		}
		switch {
		case strings.HasPrefix(st, "EPERM"):
			return fmt.Errorf("refused in the terminal (%s)", st)
		case isError(st):
			return errors.New(st)
		case st == "OK" && !permitted:
			permitted = true
			wait = kittyReplyWait
		case st == "OK":
			goto listed
		}
	}
listed:
	entries, total, err := kittyEntries(listing)
	if err != nil {
		return err
	}
	if j.MaxBytes > 0 && total > j.MaxBytes {
		return ErrTooBig
	}
	var got int64
	for _, e := range entries {
		dst := filepath.Join(j.Dir, filepath.FromSlash(e.rel))
		switch e.ft {
		case "directory":
			if err = os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		case "regular", "":
		default:
			continue // symlink, link: skipped
		}
		if err = os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err = send(KV{"ac", "file"}, KV{"fid", e.remote}, KV{"n", B64(e.name)}); err != nil {
			return err
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		f, ferr := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if ferr != nil {
			return ferr
		}
		err = kittyReceiveData(ctx, j, e.remote, f, &got, total)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return send(KV{"ac", "finish"})
}

func kittyReceiveData(ctx context.Context, j KittyReceiveJob, fid string, w io.Writer, got *int64, total int64) error {
	for {
		r, err := j.Next(ctx, kittyReplyWait)
		if errors.Is(err, ErrTimeout) {
			return err
		}
		if err != nil {
			return ErrCanceled
		}
		if r["id"] != j.ID || r["fid"] != fid {
			continue
		}
		if st := r.Status(); isError(st) {
			return errors.New(st)
		}
		if r["ac"] != "data" && r["ac"] != "end_data" {
			continue
		}
		b, derr := unB64(r["d"])
		if derr != nil {
			return derr
		}
		*got += int64(len(b))
		if j.MaxBytes > 0 && *got > j.MaxBytes {
			return ErrTooBig
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
		if j.Progress != nil {
			j.Progress(*got, total)
		}
		if r["ac"] == "end_data" {
			return nil
		}
	}
}

// kittyEntries checks the listing: one root (no parent), every other
// name inside it. rel is the path below the root's parent ("proj/a.txt").
func kittyEntries(listing []Reply) ([]kittyEntry, int64, error) {
	if len(listing) == 0 {
		return nil, 0, errors.New("nothing to upload")
	}
	root := path.Clean(listing[0].Name())
	if !strings.HasPrefix(root, "/") && !isWinAbs(root) {
		return nil, 0, fmt.Errorf("bad name %q from the terminal", root)
	}
	base := path.Dir(root)
	var out []kittyEntry
	var total int64
	for _, r := range listing {
		name := r.Name()
		if path.Clean(name) != strings.TrimSuffix(name, "/") {
			return nil, 0, fmt.Errorf("bad name %q from the terminal", name)
		}
		name = path.Clean(name)
		if name != root && !strings.HasPrefix(name, root+"/") {
			return nil, 0, fmt.Errorf("%q is outside %q", name, root)
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(name, base), "/")
		if err := checkRel(rel); err != nil {
			return nil, 0, err
		}
		sz, _ := strconv.ParseInt(r["sz"], 10, 64)
		prm, _ := strconv.ParseInt(r["prm"], 10, 64)
		total += sz
		out = append(out, kittyEntry{rel: rel, ft: r["ft"], remote: r.Status(), name: name, size: sz, mode: os.FileMode(prm) & 0o777})
	}
	return out, total, nil
}

func isWinAbs(p string) bool { return len(p) > 2 && p[0] == '/' && p[2] == ':' }
