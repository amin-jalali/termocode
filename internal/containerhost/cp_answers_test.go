package containerhost

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
)

// What file operations say when the engine's `cp` stream is odd: no
// archive, a cut archive, a special file, a container stopped mid-way.

func openScripted(t *testing.T) (*Host, *scripted) {
	t.Helper()
	x := engine(runningInspect, func([]string) (string, string, int, error) { return "uid=0\ngid=0\n", "", 0, nil })
	h, err := openWith(t, x, "")
	if err != nil {
		t.Fatal(err)
	}
	return h, x
}

func tarStream(t *testing.T, hd tar.Header, body string, cut bool) func([]string) (io.ReadCloser, error) {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&hd); err != nil {
		t.Fatal(err)
	}
	if body != "" {
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if !cut {
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
	}
	data := buf.Bytes()
	if cut {
		data = data[:512+len(body)/2] // the header and half the body
	}
	return func([]string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
}

func TestReadFileSpecialAndCutArchives(t *testing.T) {
	h, x := openScripted(t)
	ctx := t.Context()
	x.stream = tarStream(t, tar.Header{Name: "fifo", Typeflag: tar.TypeFifo, Mode: 0o644}, "", false)
	if _, err := h.FS().ReadFile(ctx, "fifo"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("fifo: %v", err)
	}
	body := strings.Repeat("x", 4000)
	x.stream = tarStream(t, tar.Header{Name: "big.log", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}, body, true)
	if _, err := h.FS().ReadFile(ctx, "big.log"); err == nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("cut archive: %v", err)
	}
}

func TestCpStreamWithoutArchive(t *testing.T) {
	h, x := openScripted(t)
	ctx := t.Context()
	x.stream = func([]string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("")), nil }
	cases := []struct {
		name   string
		answer func([]string) (string, string, int, error)
		check  func(error) bool
		online bool
	}{
		{"engine says nothing", func([]string) (string, string, int, error) { return "", "", 0, nil },
			func(err error) bool { return strings.Contains(err.Error(), "the engine sent no archive") }, true},
		{"engine cannot start", func([]string) (string, string, int, error) { return "", "", -1, errors.New("fork failed") },
			func(err error) bool { return strings.Contains(err.Error(), "fork failed") }, true},
		{"file gone", func([]string) (string, string, int, error) {
			return "", "Error response from daemon: Could not find the file /app/x in container api-1\n", 1, nil
		}, func(err error) bool { return errors.Is(err, fs.ErrNotExist) }, true},
		{"container stopped", func([]string) (string, string, int, error) {
			return "", "Error response from daemon: container api-1 is not running\n", 1, nil
		}, func(err error) bool { return errors.Is(err, ErrNotRunning) }, false},
	}
	for _, c := range cases {
		x.answer = c.answer
		_, err := h.FS().Stat(ctx, "x")
		if err == nil || !c.check(err) {
			t.Errorf("%s: err = %v", c.name, err)
		}
		if h.Online() != c.online {
			t.Errorf("%s: online = %v", c.name, h.Online())
		}
	}
}

func TestCpStreamStartFailure(t *testing.T) {
	h, x := openScripted(t)
	x.stream = nil
	_, err := h.FS().ReadFile(t.Context(), "a.txt")
	var pe *fs.PathError
	if !errors.As(err, &pe) || pe.Op != "read" || pe.Path != "/app/a.txt" {
		t.Fatalf("err = %v", err)
	}
}

// The container stops between the check and the write: the write fails
// with ErrNotRunning, not a vague error.
func TestWriteFileContainerStoppedMidWrite(t *testing.T) {
	h, x := openScripted(t)
	x.stream = tarStream(t, tar.Header{Name: "a.txt", Typeflag: tar.TypeReg, Mode: 0o600, Size: 2}, "hi", false)
	x.answer = func(args []string) (string, string, int, error) {
		if args[0] == "cp" {
			return "", "Error response from daemon: No such container: api-1\n", 1, nil
		}
		return "", "", 0, nil
	}
	if err := h.FS().WriteFile(context.Background(), "a.txt", []byte("new"), 0o644); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("err = %v", err)
	}
	x.answer = func(args []string) (string, string, int, error) { return "", "", -1, errors.New("broken pipe") }
	if err := h.FS().WriteFile(context.Background(), "a.txt", []byte("new"), 0o644); err == nil || !strings.Contains(err.Error(), "broken pipe") {
		t.Fatalf("err = %v", err)
	}
}
