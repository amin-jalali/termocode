package fakedocker

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func readTar(t *testing.T, s string) map[string]*tar.Header {
	t.Helper()
	out := map[string]*tar.Header{}
	tr := tar.NewReader(strings.NewReader(s))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out[h.Name] = h
	}
}

func TestCpOutFileAndFolder(t *testing.T) {
	d := New()
	c := d.Add(t, "api-1")
	c.WriteFile(t, "/app/src/main.go", []byte("package main"), 0o640)
	c.SetOwner("/app/src/main.go", 33, 44)
	ctx := context.Background()

	out, _, code, err := d.Capture(ctx, "", "docker", "cp", "api-1:/app/src/main.go", "-")
	if err != nil || code != 0 {
		t.Fatalf("cp out = %d, %v", code, err)
	}
	h := readTar(t, out)["main.go"]
	if h == nil || h.Mode&0o777 != 0o640 || h.Uid != 33 || h.Gid != 44 || h.Size != 12 {
		t.Fatalf("header = %+v", h)
	}

	out, _, _, _ = d.Capture(ctx, "", "docker", "cp", "api-1:/app", "-")
	hs := readTar(t, out)
	for _, name := range []string{"app/", "app/src/", "app/src/main.go"} {
		if hs[name] == nil {
			t.Errorf("no %q in %v", name, hs)
		}
	}
}

func TestCpOutMissingAndUnknownContainer(t *testing.T) {
	d := New()
	d.Add(t, "api-1")
	_, stderr, code, _ := d.Capture(context.Background(), "", "docker", "cp", "api-1:/nope", "-")
	if code == 0 || !strings.Contains(stderr, "Could not find the file /nope") {
		t.Fatalf("missing = %d %q", code, stderr)
	}
	_, stderr, code, _ = d.Capture(context.Background(), "", "docker", "cp", "db:/x", "-")
	if code == 0 || !strings.Contains(stderr, "No such container: db") {
		t.Fatalf("unknown = %d %q", code, stderr)
	}
}

// tarOf builds a tar; a regular entry's Linkname is its body (test
// shorthand).
func tarOf(t *testing.T, hs ...tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, h := range hs {
		body := h.Linkname
		h.Linkname = ""
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(body))
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCpInKeepsOwnerOnlyInArchiveMode(t *testing.T) {
	d := New()
	c := d.Add(t, "api-1")
	c.Mkdir(t, "/app")
	in := tarOf(t,
		tar.Header{Name: "new/", Typeflag: tar.TypeDir, Mode: 0o755, Uid: 7, Gid: 8},
		tar.Header{Name: "new/f.txt", Typeflag: tar.TypeReg, Mode: 0o600, Uid: 7, Gid: 8, Linkname: "hello"},
	)
	if _, stderr, code, err := d.CaptureInput(context.Background(), "", in, "docker", "cp", "-a", "-", "api-1:/app"); err != nil || code != 0 {
		t.Fatalf("cp in = %d %q %v", code, stderr, err)
	}
	data, err := os.ReadFile(c.Local("/app/new/f.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("file = %q, %v", data, err)
	}
	fi, _ := os.Stat(c.Local("/app/new/f.txt"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode())
	}
	if uid, gid, _ := c.Owner("/app/new/f.txt"); uid != 7 || gid != 8 {
		t.Fatalf("owner = %d:%d", uid, gid)
	}
	if len(d.Received()) != 2 {
		t.Fatalf("received = %v", d.Received())
	}

	_, _, _, _ = d.CaptureInput(context.Background(), "", in, "docker", "cp", "-", "api-1:/app")
	if uid, _, _ := c.Owner("/app/new/f.txt"); uid != 0 {
		t.Fatalf("without -a the owner should become root, got %d", uid)
	}
}

func TestCpInPodmanKeepsOwnerWithArchiveFalse(t *testing.T) {
	d := New()
	d.Kind = "podman"
	c := d.Add(t, "api-1")
	c.Mkdir(t, "/app")
	in := tarOf(t, tar.Header{Name: "f", Typeflag: tar.TypeReg, Mode: 0o644, Uid: 9, Gid: 9, Linkname: "x"})
	_, _, _, _ = d.CaptureInput(context.Background(), "", in, "podman", "cp", "--archive=false", "-", "api-1:/app")
	if uid, _, _ := c.Owner("/app/f"); uid != 9 {
		t.Fatalf("owner = %d", uid)
	}
}

func TestCpInRefusesEscapesAndMissingFolder(t *testing.T) {
	d := New()
	c := d.Add(t, "api-1")
	c.Mkdir(t, "/app")
	in := tarOf(t, tar.Header{Name: "../evil", Typeflag: tar.TypeReg, Mode: 0o644, Linkname: "x"})
	if _, _, code, _ := d.CaptureInput(context.Background(), "", in, "docker", "cp", "-", "api-1:/app"); code == 0 {
		t.Fatal("escape accepted")
	}
	if _, _, code, _ := d.CaptureInput(context.Background(), "", nil, "docker", "cp", "-", "api-1:/nope"); code == 0 {
		t.Fatal("missing folder accepted")
	}
}

func TestGlobalContextFlagIsLogged(t *testing.T) {
	d := New()
	d.Add(t, "api-1")
	_, _, _, _ = d.Capture(context.Background(), "", "docker", "--context", "prod", "cp", "api-1:/", "-")
	calls := d.Calls()
	if len(calls) != 1 || calls[0].Context != "prod" || calls[0].Args[0] != "cp" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestStreamCountsOpenStreams(t *testing.T) {
	d := New()
	c := d.Add(t, "api-1")
	c.WriteFile(t, "/f", []byte("x"), 0o644)
	r, stop, err := d.Stream(context.Background(), "", "docker", "cp", "api-1:/f", "-")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(r)
	if d.OpenStreams() != 1 {
		t.Fatal("stream not counted")
	}
	stop()
	stop()
	if d.OpenStreams() != 0 {
		t.Fatal("stop not counted once")
	}
}

func TestExecRunsShWithMappedPositionalArgs(t *testing.T) {
	d := New()
	c := d.Add(t, "api-1")
	c.WriteFile(t, "/app/a b.txt", []byte("hi"), 0o644)
	out, stderr, code, err := d.Capture(context.Background(), "", "docker", "exec", "-i", "-w", "/app", "-u", "1000", "api-1",
		"sh", "-c", `cat -- "$1"; pwd`, "sh", "/app/a b.txt")
	if err != nil || code != 0 || out != "hi/app\n" {
		t.Fatalf("exec = %q %q %d %v", out, stderr, code, err)
	}
	ex := c.Execs()
	if len(ex) != 1 || ex[0].Dir != "/app" || ex[0].User != "1000" || ex[0].Argv[0] != "sh" {
		t.Fatalf("execs = %+v", ex)
	}
}

func TestExecOnlyFindsImageTools(t *testing.T) {
	d := New()
	c := d.Add(t, "api-1")
	out, _, code, _ := d.Capture(context.Background(), "", "docker", "exec", "api-1", "sh", "-c", "command -v git || echo none")
	if code != 0 || out != "none\n" {
		t.Fatalf("git found without AddTool: %q", out)
	}
	c.AddTool(t, "git")
	out, _, _, _ = d.Capture(context.Background(), "", "docker", "exec", "api-1", "git", "status")
	if out != "git status\n" {
		t.Fatalf("tool = %q", out)
	}
	_, stderr, code, _ := d.Capture(context.Background(), "", "docker", "exec", "api-1", "gopls")
	if code != 127 || !strings.Contains(stderr, "executable file not found") {
		t.Fatalf("missing tool = %d %q", code, stderr)
	}
}

func TestExecNoShellAndStopped(t *testing.T) {
	d := New()
	c := d.Add(t, "api-1")
	c.NoShell()
	if _, _, code, _ := d.Capture(context.Background(), "", "docker", "exec", "api-1", "sh", "-c", "true"); code != 127 {
		t.Fatalf("sh ran in a distroless image: %d", code)
	}
	c.Stop()
	_, stderr, code, _ := d.Capture(context.Background(), "", "docker", "exec", "api-1", "echo", "x")
	if code == 0 || !strings.Contains(stderr, "is not running") {
		t.Fatalf("stopped = %d %q", code, stderr)
	}
}

func TestExecMissingWorkdir(t *testing.T) {
	d := New()
	d.Add(t, "api-1")
	if _, _, code, _ := d.Capture(context.Background(), "", "docker", "exec", "-w", "/nope", "api-1", "echo"); code != 126 {
		t.Fatalf("code = %d", code)
	}
}

func TestInspectContainerAndImage(t *testing.T) {
	d := New()
	c := d.Add(t, "api-1")
	c.User, c.Image, c.Arch = "node", "node:22", "arm64"
	c.Mounts = []Mount{{Type: "bind", Source: "/home/u/p", Destination: "/app", RW: true}}
	out, _, code, _ := d.Capture(context.Background(), "", "docker", "inspect", "--type", "container", "api-1")
	for _, want := range []string{`"Running":true`, `"User":"node"`, `"Destination":"/app"`, `"Image":"sha256:node:22"`} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("inspect lacks %s: %s", want, out)
		}
	}
	out, _, _, _ = d.Capture(context.Background(), "", "docker", "image", "inspect", "sha256:node:22")
	if !strings.Contains(out, `"Architecture":"arm64"`) {
		t.Fatalf("image inspect = %s", out)
	}
	c.Stop()
	out, _, _, _ = d.Capture(context.Background(), "", "docker", "inspect", "--type", "container", "api-1")
	if !strings.Contains(out, `"Running":false`) {
		t.Fatalf("stopped inspect = %s", out)
	}
	if _, _, code, _ := d.Capture(context.Background(), "", "docker", "inspect", "--type", "container", "db"); code == 0 {
		t.Fatal("unknown container inspected")
	}
}

func TestCpOutFollowsAbsoluteLinksInsideTheContainer(t *testing.T) {
	d := New()
	c := d.Add(t, "api-1")
	c.WriteFile(t, "/etc/app.conf", []byte("conf"), 0o644)
	c.Mkdir(t, "/app")
	if err := os.Symlink("/etc/app.conf", c.Local("/app/conf")); err != nil {
		t.Skip("no symlinks:", err)
	}
	out, _, code, _ := d.Capture(context.Background(), "", "docker", "cp", "-L", "api-1:/app/conf", "-")
	h := readTar(t, out)["conf"]
	if code != 0 || h == nil || h.Typeflag != tar.TypeReg || h.Size != 4 {
		t.Fatalf("followed = %d %+v", code, h)
	}
	out, _, _, _ = d.Capture(context.Background(), "", "docker", "cp", "api-1:/app/conf", "-")
	if h := readTar(t, out)["conf"]; h == nil || h.Typeflag != tar.TypeSymlink || h.Linkname != "/etc/app.conf" {
		t.Fatalf("not followed = %+v", h)
	}
}

func TestRemovedContainerIsGone(t *testing.T) {
	d := New()
	c := d.Add(t, "api-1")
	c.WriteFile(t, "/app/a.txt", []byte("a"), 0o644)
	c.Remove()
	ctx := context.Background()
	for _, argv := range [][]string{
		{"docker", "cp", "api-1:/app/a.txt", "-"},
		{"docker", "inspect", "--type", "container", "api-1"},
	} {
		_, stderr, code, err := d.Capture(ctx, "", argv...)
		if err != nil || code == 0 || !strings.Contains(strings.ToLower(stderr), "no such container") {
			t.Fatalf("%q = %d %q %v", argv, code, stderr, err)
		}
	}
	if _, err := os.Stat(c.Local("/app/a.txt")); err != nil {
		t.Fatalf("files of a removed container should stay for checks: %v", err)
	}
}
