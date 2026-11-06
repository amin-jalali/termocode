package svcinstall

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		_ = os.WriteFile(p, []byte(got), 0o644)
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s differs:\n%s", name, got)
	}
}

func TestSystemdUnitGolden(t *testing.T) {
	s := Spec{GOOS: "linux", Home: "/home/u", Exe: "/usr/local/bin/termocode", Workspace: "/home/u/my proj"}
	f, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "unit.golden", f.Content)
	if !strings.HasPrefix(f.Path, "/home/u/.config/systemd/user/termocode-") || !strings.HasSuffix(f.Path, ".service") {
		t.Errorf("path %q", f.Path)
	}
}

func TestPlistGolden(t *testing.T) {
	s := Spec{GOOS: "darwin", Home: "/Users/u", Exe: "/usr/local/bin/termocode", Workspace: "/Users/u/a&b"}
	f, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "plist.golden", f.Content)
	if !strings.HasPrefix(f.Path, "/Users/u/Library/LaunchAgents/dev.termocode.") || !strings.HasSuffix(f.Path, ".plist") {
		t.Errorf("path %q", f.Path)
	}
}

func TestPathsStayUnderFakeHome(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		f, err := Render(Spec{GOOS: goos, Home: "/fake/home", Exe: "/bin/tc", Workspace: "/w"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(f.Path, "/fake/home/") {
			t.Errorf("%s: %q leaves HOME", goos, f.Path)
		}
	}
	a, _ := Render(Spec{GOOS: "linux", Home: "/h", Exe: "/bin/tc", Workspace: "/w1"})
	b, _ := Render(Spec{GOOS: "linux", Home: "/h", Exe: "/bin/tc", Workspace: "/w2"})
	if a.Path == b.Path {
		t.Error("two workspaces share one unit")
	}
}

func TestRenderRefusesUnsafe(t *testing.T) {
	for _, bad := range []string{"/a\"b", "/a\nb", "/a\\b", "rel/path"} {
		if _, err := Render(Spec{GOOS: "linux", Home: "/h", Exe: bad, Workspace: "/w"}); err == nil {
			t.Errorf("exe %q accepted", bad)
		}
	}
	if _, err := Render(Spec{GOOS: "linux", Home: "", Exe: "/bin/tc", Workspace: "/w"}); err == nil {
		t.Error("empty HOME accepted")
	}
}

func TestUnsupportedSystems(t *testing.T) {
	for _, goos := range []string{"android", "windows", "freebsd"} {
		_, err := Render(Spec{GOOS: goos, Home: "/h", Exe: "/bin/tc", Workspace: "/w"})
		if err == nil {
			t.Fatalf("%s accepted", goos)
		}
	}
	_, err := Render(Spec{GOOS: "android", Home: "/h", Exe: "/bin/tc", Workspace: "/w"})
	if !strings.Contains(err.Error(), "Termux") {
		t.Errorf("no Termux message: %v", err)
	}
	// A linux build run inside Termux has GOOS linux but no systemd.
	_, err = Render(Spec{GOOS: "linux", Termux: true, Home: "/h", Exe: "/bin/tc", Workspace: "/w"})
	if err == nil || !strings.Contains(err.Error(), "Termux") || !strings.Contains(err.Error(), "termux-wake-lock") {
		t.Errorf("linux in Termux: %v", err)
	}
	if err := Install(Spec{GOOS: "linux", Termux: true, Home: t.TempDir(), Exe: "/bin/tc", Workspace: "/w"}, &bytes.Buffer{}, func() bool { return true }); err == nil {
		t.Error("install in Termux went on")
	}
}

// Spec (WFX-17): Termux is found by TERMUX_VERSION or a PREFIX inside
// com.termux.
func TestInTermux(t *testing.T) {
	for env, want := range map[string]bool{
		"TERMUX_VERSION=0.118.0":                 true,
		"PREFIX=/data/data/com.termux/files/usr": true,
		"PREFIX=/usr/local":                      false,
		"":                                       false,
	} {
		k, v, _ := strings.Cut(env, "=")
		got := InTermux(func(n string) string {
			if n == k {
				return v
			}
			return ""
		})
		if got != want {
			t.Errorf("%q: %v, want %v", env, got, want)
		}
	}
}

// fakeCtl puts a fake systemctl/launchctl/loginctl on PATH that logs argv.
func fakeCtl(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fakes")
	}
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "calls.log")
	for _, n := range []string{"systemctl", "launchctl", "loginctl"} {
		script := "#!/bin/sh\necho \"" + n + " $*\" >> '" + log + "'\ncase \"$*\" in *is-active*) echo active;; *Linger*) echo \"${FAKE_LINGER:-no}\";; esac\n"
		if err := os.WriteFile(filepath.Join(bin, n), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func TestRoundTripLinux(t *testing.T) {
	log := fakeCtl(t)
	home := t.TempDir()
	s := Spec{GOOS: "linux", Home: home, Exe: "/bin/tc", Workspace: "/w", UID: 1000}
	var out bytes.Buffer

	// Preview only: nothing is written without a yes.
	if err := Install(s, &out, func() bool { return false }); !errors.Is(err, ErrCancelled) {
		t.Fatalf("declined install: %v", err)
	}
	f, _ := Render(s)
	if _, err := os.Stat(f.Path); err == nil {
		t.Fatal("file written without consent")
	}
	if !strings.Contains(out.String(), "ExecStart=") {
		t.Errorf("no preview:\n%s", out.String())
	}

	out.Reset()
	if err := Install(s, &out, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(f.Path); string(b) != f.Content {
		t.Fatalf("file content differs")
	}
	if !strings.Contains(out.String(), "enable-linger") {
		t.Errorf("no linger hint:\n%s", out.String())
	}
	if !strings.Contains(string(mustRead(t, log)), "loginctl show-user 1000 --property=Linger --value") {
		t.Errorf("linger not checked:\n%s", mustRead(t, log))
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "systemctl --user enable --now "+filepath.Base(f.Path)) {
		t.Errorf("calls:\n%s", calls)
	}

	out.Reset()
	if err := Status(s, &out); err != nil || !strings.Contains(out.String(), "installed") || !strings.Contains(out.String(), "active") {
		t.Errorf("status %v:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "enable-linger") {
		t.Errorf("status has no linger hint while linger is off:\n%s", out.String())
	}

	if err := Uninstall(s, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.Path); err == nil {
		t.Error("unit still there")
	}
	if err := Uninstall(s, &out); err != nil {
		t.Errorf("second uninstall: %v", err)
	}
}

func TestRoundTripDarwin(t *testing.T) {
	log := fakeCtl(t)
	home := t.TempDir()
	s := Spec{GOOS: "darwin", Home: home, Exe: "/bin/tc", Workspace: "/w", UID: 501}
	var out bytes.Buffer
	if err := Install(s, &out, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	f, _ := Render(s)
	if _, err := os.Stat(f.Path); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "launchctl bootstrap gui/501 "+f.Path) {
		t.Errorf("calls:\n%s", calls)
	}
	if err := Uninstall(s, &out); err != nil {
		t.Fatal(err)
	}
	calls, _ = os.ReadFile(log)
	if !strings.Contains(string(calls), "launchctl bootout gui/501/dev.termocode.") {
		t.Errorf("calls:\n%s", calls)
	}
	if _, err := os.Stat(f.Path); err == nil {
		t.Error("plist still there")
	}
}

func TestUninstallKeepsForeignFile(t *testing.T) {
	fakeCtl(t)
	home := t.TempDir()
	s := Spec{GOOS: "linux", Home: home, Exe: "/bin/tc", Workspace: "/w"}
	f, _ := Render(s)
	_ = os.MkdirAll(filepath.Dir(f.Path), 0o755)
	_ = os.WriteFile(f.Path, []byte("[Unit]\nDescription=mine\n"), 0o644)
	var out bytes.Buffer
	if err := Uninstall(s, &out); err == nil {
		t.Error("removed a file we did not write")
	}
	if _, err := os.Stat(f.Path); err != nil {
		t.Error("foreign file deleted")
	}
}

func TestInstallRefusesForeignFile(t *testing.T) {
	fakeCtl(t)
	home := t.TempDir()
	s := Spec{GOOS: "linux", Home: home, Exe: "/bin/tc", Workspace: "/w"}
	f, _ := Render(s)
	_ = os.MkdirAll(filepath.Dir(f.Path), 0o755)
	_ = os.WriteFile(f.Path, []byte("mine"), 0o644)
	if err := Install(s, &bytes.Buffer{}, func() bool { return true }); err == nil {
		t.Error("overwrote a file we did not write")
	}
	if b, _ := os.ReadFile(f.Path); string(b) != "mine" {
		t.Error("foreign file changed")
	}
}

// Spec (WFX-17, plan §4.1): the linger hint shows only when it is needed:
// when loginctl says linger is on, install and status do not ask for it.
func TestLingerHintOnlyWhenNeeded(t *testing.T) {
	fakeCtl(t)
	t.Setenv("FAKE_LINGER", "yes")
	s := Spec{GOOS: "linux", Home: t.TempDir(), Exe: "/bin/tc", Workspace: "/w", UID: 1000}
	var out bytes.Buffer
	if err := Install(s, &out, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "enable-linger") || !strings.Contains(out.String(), "boot") {
		t.Errorf("linger on, install output:\n%s", out.String())
	}
	out.Reset()
	if err := Status(s, &out); err != nil || strings.Contains(out.String(), "enable-linger") {
		t.Errorf("linger on, status %v:\n%s", err, out.String())
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
