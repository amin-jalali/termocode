package containerhost

import (
	"archive/tar"
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/amin-jalali/termocode/internal/workspace"
)

func TestReadFile(t *testing.T) {
	h, d, c := newHost(t)
	c.WriteFile(t, "/app/src/main.go", []byte("package main\n"), 0o644)
	f := containerFS{h}
	for _, p := range []string{"src/main.go", "/app/src/main.go"} {
		data, err := f.ReadFile(t.Context(), p)
		if err != nil || string(data) != "package main\n" {
			t.Fatalf("ReadFile(%q) = %q, %v", p, data, err)
		}
	}
	if d.OpenStreams() != 0 {
		t.Fatal("a stream was left open")
	}
	last := d.Calls()[len(d.Calls())-1].Args
	if last[0] != "cp" || last[1] != "-L" || last[2] != "api-1:/app/src/main.go" || last[3] != "-" {
		t.Fatalf("cp argv = %q", last)
	}
}

func TestReadFileFollowsSymlink(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/real.txt", []byte("real"), 0o644)
	if err := os.Symlink("real.txt", c.Local("/app/link.txt")); err != nil {
		t.Skip("no symlinks:", err)
	}
	data, err := containerFS{h}.ReadFile(t.Context(), "link.txt")
	if err != nil || string(data) != "real" {
		t.Fatalf("ReadFile(link) = %q, %v", data, err)
	}
}

func TestReadFileErrors(t *testing.T) {
	h, d, c := newHost(t)
	c.Mkdir(t, "/app/dir")
	f := containerFS{h}
	if _, err := f.ReadFile(t.Context(), "missing.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := f.ReadFile(t.Context(), "dir"); !errors.Is(err, errIsDir) {
		t.Fatalf("dir: %v", err)
	}
	if _, err := f.ReadFile(t.Context(), "../etc/passwd"); !errors.Is(err, workspace.ErrOutsideRoot) {
		t.Fatalf("outside: %v", err)
	}
	if d.OpenStreams() != 0 {
		t.Fatal("a stream was left open")
	}
}

func TestStat(t *testing.T) {
	h, d, c := newHost(t)
	c.WriteFile(t, "/app/run.sh", []byte("#!/bin/sh\n"), 0o750)
	c.SetOwner("/app/run.sh", 1001, 1002)
	c.WriteFile(t, "/app/big/x", []byte("x"), 0o644)
	f := containerFS{h}

	fi, err := f.Stat(t.Context(), "run.sh")
	if err != nil || fi.Name() != "run.sh" || fi.Size() != 10 || fi.IsDir() || fi.Mode().Perm() != 0o750 {
		t.Fatalf("Stat(file) = %v, %v", fi, err)
	}
	if hd, ok := fi.Sys().(*tar.Header); !ok || hd.Uid != 1001 || hd.Gid != 1002 {
		t.Fatalf("Sys = %+v", fi.Sys())
	}
	for _, p := range []string{"", "big"} {
		fi, err = f.Stat(t.Context(), p)
		if err != nil || !fi.IsDir() {
			t.Fatalf("Stat(%q) = %v, %v", p, fi, err)
		}
	}
	if fi.Name() != "big" {
		t.Fatalf("name = %q", fi.Name())
	}
	if _, err := f.Stat(t.Context(), "nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	if d.OpenStreams() != 0 {
		t.Fatal("a stream was left open")
	}
}

func TestReadFileRefusesTooLarge(t *testing.T) {
	h, d, c := newHost(t, func(c *Config) { c.MaxFileSize = 4 })
	c.WriteFile(t, "/app/big", []byte("0123456789"), 0o644)
	c.WriteFile(t, "/app/ok", []byte("0123"), 0o644)
	if _, err := (containerFS{h}).ReadFile(t.Context(), "big"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("big: %v", err)
	}
	if data, err := (containerFS{h}).ReadFile(t.Context(), "ok"); err != nil || string(data) != "0123" {
		t.Fatalf("ok: %q, %v", data, err)
	}
	if d.OpenStreams() != 0 {
		t.Fatal("a stream was left open")
	}
}
