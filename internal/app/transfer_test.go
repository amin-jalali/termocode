package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/transfer"
)

// A child process must get the real terminal: Bubble Tea would hand it
// transfer.Input (a pipe for the child). Every exec goes through
// execProcess, which sets os.Stdin.
func TestExecProcessKeepsTTYStdin(t *testing.T) {
	if c := withTTYStdin(exec.Command("true")); c.Stdin != os.Stdin {
		t.Fatalf("stdin %v, want os.Stdin", c.Stdin)
	}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "headless.go" {
			continue
		}
		b, _ := os.ReadFile(f)
		if bytes.Contains(b, []byte("tea.ExecProcess(")) {
			t.Errorf("%s calls tea.ExecProcess: use execProcess", f)
		}
	}
}

func tgz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// iTerm2 upload: the request goes out, the base64 tar.gz comes back on
// the input, the files land in the folder, a taken name gets " (1)".
func TestRunUploadITerm2(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString(tgz(t, map[string]string{"a.txt": "theirs", "b.txt": "new"}))
	in := strings.NewReader("ok\n" + b64 + "\n\n")
	var out bytes.Buffer
	names, err := runUpload(context.Background(), transfer.ITerm2, "", dir, in, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), transfer.RequestUploadSeq) {
		t.Error("no RequestUpload sent")
	}
	for name, want := range map[string]string{"a.txt": "mine", "a (1).txt": "theirs", "b.txt": "new"} {
		if b, _ := os.ReadFile(filepath.Join(dir, name)); string(b) != want {
			t.Errorf("%s = %q, want %q", name, b, want)
		}
	}
	if len(names) != 2 {
		t.Errorf("names %v", names)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 3 {
		t.Errorf("left %d entries (a staging folder?)", len(ents))
	}
}

// kittyTerminal answers a kitty receive session on the input pipe.
type kittyTerminal struct {
	t     *testing.T
	in    *io.PipeWriter
	files map[string]string // absolute path on "their" machine → body
}

func (k *kittyTerminal) Write(p []byte) (int, error) {
	for _, pl := range transfer.ParseOSC(p, "5113") {
		r := transfer.ParseKittyReply(string(pl))
		id := r["id"]
		put := func(kv ...transfer.KV) {
			_, _ = io.WriteString(k.in, transfer.KittyCmd(append([]transfer.KV{{K: "id", V: id}}, kv...)...))
		}
		switch {
		case r["ac"] == "file" && r["fid"] == "r0":
			put(transfer.KV{K: "ac", V: "status"}, transfer.KV{K: "st", V: transfer.B64("OK")})
			i := 0
			for name, body := range k.files {
				i++
				put(transfer.KV{K: "ac", V: "file"}, transfer.KV{K: "fid", V: "r0"}, transfer.KV{K: "st", V: "t" + string(rune('0'+i))},
					transfer.KV{K: "n", V: transfer.B64(name)}, transfer.KV{K: "ft", V: "regular"}, transfer.KV{K: "sz", V: "3"})
				_ = body
			}
			put(transfer.KV{K: "ac", V: "status"}, transfer.KV{K: "st", V: transfer.B64("OK")})
		case r["ac"] == "file":
			put(transfer.KV{K: "ac", V: "end_data"}, transfer.KV{K: "fid", V: r["fid"]},
				transfer.KV{K: "d", V: base64.StdEncoding.EncodeToString([]byte(k.files[r.Name()]))})
		}
	}
	return len(p), nil
}

func TestRunUploadKitty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "r.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	pr, pw := io.Pipe()
	defer pr.Close()
	term := &kittyTerminal{t: t, in: pw, files: map[string]string{"/home/u/r.txt": "abc"}}
	names, err := runUpload(context.Background(), transfer.Kitty, "~/r.txt", dir, pr, term)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "r (1).txt")); string(b) != "abc" || len(names) != 1 || names[0] != "r (1).txt" {
		t.Errorf("got %q, names %v", b, names)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "r.txt")); string(b) != "mine" {
		t.Error("r.txt replaced")
	}
}

// Ctrl+C while kitty waits for the user ends the upload, nothing written.
func TestRunUploadKittyCtrlC(t *testing.T) {
	dir := t.TempDir()
	done := make(chan error, 1)
	go func() {
		_, err := runUpload(context.Background(), transfer.Kitty, "~/x", dir, strings.NewReader("\x03"), io.Discard)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, transfer.ErrCanceled) {
			t.Errorf("err %v, want canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ctrl+C did not stop the upload")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("left %v", ents)
	}
}
