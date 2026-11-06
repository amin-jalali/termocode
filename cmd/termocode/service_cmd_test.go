package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func svcEnv(t *testing.T) (home string) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("service files are Linux and macOS only")
	}
	bin := t.TempDir()
	for _, n := range []string{"systemctl", "launchctl"} {
		if err := os.WriteFile(filepath.Join(bin, n), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return t.TempDir()
}

func TestServiceInstallAsksFirstAndDefaultsToNo(t *testing.T) {
	home := svcEnv(t)
	ws := t.TempDir()
	var out, errw bytes.Buffer
	// Empty answer (or EOF) is a no: nothing is written.
	code := runServiceIn([]string{"install"}, strings.NewReader("\n"), &out, &errw, ws, home, "/bin/termocode")
	if code == 0 {
		t.Errorf("declined install exited 0")
	}
	if !strings.Contains(out.String(), "[y/N]") || !strings.Contains(out.String(), "__session-server") {
		t.Errorf("no preview or prompt:\n%s", out.String())
	}
	if n := countFiles(home); n != 0 {
		t.Errorf("%d files written without consent", n)
	}
	// y installs; status and uninstall follow.
	out.Reset()
	if code := runServiceIn([]string{"install"}, strings.NewReader("y\n"), &out, &errw, ws, home, "/bin/termocode"); code != 0 {
		t.Fatalf("install exit %d: %s", code, errw.String())
	}
	if countFiles(home) != 1 {
		t.Errorf("want one file under HOME")
	}
	out.Reset()
	if code := runServiceIn([]string{"status"}, nil, &out, &errw, ws, home, "/bin/termocode"); code != 0 || !strings.Contains(out.String(), "installed") {
		t.Errorf("status %d: %s", code, out.String())
	}
	if code := runServiceIn([]string{"uninstall"}, nil, &out, &errw, ws, home, "/bin/termocode"); code != 0 {
		t.Errorf("uninstall exit %d: %s", code, errw.String())
	}
	if countFiles(home) != 0 {
		t.Errorf("file left after uninstall")
	}
}

func TestServiceInstallYesSkipsPrompt(t *testing.T) {
	home := svcEnv(t)
	var out, errw bytes.Buffer
	if code := runServiceIn([]string{"install", "--yes"}, nil, &out, &errw, t.TempDir(), home, "/bin/termocode"); code != 0 {
		t.Fatalf("exit %d: %s", code, errw.String())
	}
}

func TestServiceUsage(t *testing.T) {
	var out, errw bytes.Buffer
	if code := runServiceIn([]string{"bogus"}, nil, &out, &errw, "/w", "/h", "/bin/tc"); code == 0 {
		t.Error("unknown subcommand accepted")
	}
	if code := runServiceIn(nil, nil, &out, &errw, "/w", "/h", "/bin/tc"); code == 0 {
		t.Error("no subcommand accepted")
	}
	if !tuiCommandIsNo("service") {
		t.Error("service must not hold the GC for a UI frame")
	}
}

func tuiCommandIsNo(c string) bool { return !tuiCommand([]string{c}) }

func countFiles(root string) (n int) {
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			n++
		}
		return nil
	})
	return n
}
