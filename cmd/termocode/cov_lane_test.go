package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/lanemove"
)

// laneRepo makes a git repository with one commit and one new file, with
// a fake HOME, and returns its folder.
func laneRepo(t *testing.T) (repo, home string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	repo = filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = repo
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-q", "-m", "first")
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo, home
}

// A lane packs to a file, shows what it holds, and unpacks into a new
// worktree; also through stdout and stdin.
func TestLanePackInfoUnpackRoundTrip(t *testing.T) {
	repo, home := laneRepo(t)
	env := func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	pack := filepath.Join(t.TempDir(), "x.tlane")
	var out, errw bytes.Buffer
	if code := runLaneIn([]string{"pack", "--out", pack, repo}, nil, &out, &errw, env); code != 0 {
		t.Fatalf("pack: %d %s", code, errw.String())
	}
	if !strings.Contains(out.String(), "packed") || !strings.Contains(out.String(), "changes:") {
		t.Fatalf("pack output %q", out.String())
	}
	out.Reset()
	if code := runLaneIn([]string{"info", pack}, nil, &out, &errw, env); code != 0 || !strings.Contains(out.String(), "commits:") {
		t.Fatalf("info: %d %q %s", code, out.String(), errw.String())
	}
	out.Reset()
	if code := runLaneIn([]string{"unpack", "--repo", repo, "--name", "lane-copy", pack}, nil, &out, &errw, env); code != 0 {
		t.Fatalf("unpack: %d %q %s", code, out.String(), errw.String())
	}
	if !strings.Contains(out.String(), "unpacked") {
		t.Fatalf("unpack output %q", out.String())
	}
	// Through a pipe: pack to stdout, unpack from stdin.
	var packed bytes.Buffer
	if code := runLaneIn([]string{"pack", "--out", "-", repo}, nil, &packed, &errw, env); code != 0 || packed.Len() == 0 {
		t.Fatalf("pack to stdout: %d %s", code, errw.String())
	}
	out.Reset()
	if code := runLaneIn([]string{"unpack", "--repo", repo, "--name", "lane-copy-2", "-"}, &packed, &out, &errw, env); code != 0 {
		t.Fatalf("unpack from stdin: %d %q %s", code, out.String(), errw.String())
	}
}

func TestLaneArgumentErrors(t *testing.T) {
	env := func(string) string { return "" }
	var out, errw bytes.Buffer
	for _, args := range [][]string{
		{"pack", "a", "b"}, {"info"}, {"pull"}, {"push"}, {"push", "a", "b", "c"}, {"nonsense"}, {"pack", "--bogus"},
	} {
		errw.Reset()
		if code := runLaneIn(args, nil, &out, &errw, env); code != 2 {
			t.Errorf("%v: code %d", args, code)
		}
	}
	if code := runLaneIn([]string{"pack", "--out", "-", t.TempDir()}, nil, &out, &errw, env); code != 1 {
		t.Errorf("pack of a non-repo folder: %d", code)
	}
	if code := runLaneIn([]string{"pack", t.TempDir()}, nil, &out, &errw, env); code != 1 {
		t.Errorf("pack of a non-repo folder to a file: %d", code)
	}
	if code := runLaneIn([]string{"push", "--", "-oProxyCommand=x:/a"}, nil, &out, &errw, env); code != 1 {
		t.Errorf("push to a flag-like host: %d", code)
	}
}

// pull and push run ssh; with an ssh that fails, both say so.
func TestLanePullAndPushWithFailingSSH(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\necho no route >&2\nexit 255\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	repo, home := laneRepo(t)
	env := func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	var out, errw bytes.Buffer
	if code := runLaneIn([]string{"pull", "host:/srv/proj"}, nil, &out, &errw, env); code != 1 || !strings.Contains(errw.String(), "could not be packed on host") {
		t.Errorf("pull: %d %q", code, errw.String())
	}
	errw.Reset()
	if code := runLaneIn([]string{"push", repo, "host:/srv/proj"}, nil, &out, &errw, env); code != 1 || !strings.Contains(errw.String(), "could not be unpacked on host") {
		t.Errorf("push: %d %q", code, errw.String())
	}
}

func TestLaneWorktreeDirAndInfoText(t *testing.T) {
	home := t.TempDir()
	if laneWorktreeDir(home) != "" {
		t.Fatal("no config, no folder")
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	cfg := filepath.Join(home, "xdg", "termocode", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(`{ // comment
"worktree_dir": "/w"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := laneWorktreeDir(home); got != "/w" {
		t.Fatalf("got %q", got)
	}
	if err := os.WriteFile(cfg, []byte(`{"worktree_dir": 5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := laneWorktreeDir(home); got != "" {
		t.Fatalf("bad json type: %q", got)
	}
	txt := laneInfoText(lanemove.Manifest{Host: "box", Path: "/p", PackedAt: time.Unix(0, 0), Skipped: []string{"x"}})
	if !strings.Contains(txt, "box:/p") || !strings.Contains(txt, "detached") || !strings.Contains(txt, "left out") {
		t.Fatalf("info text %q", txt)
	}
}

func TestLaneSpoolRoundTrip(t *testing.T) {
	p, err := laneSpool(strings.NewReader("data"))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p)
	if b, _ := os.ReadFile(p); string(b) != "data" {
		t.Fatalf("spooled %q", b)
	}
}

func TestRunLaneWrapperHelp(t *testing.T) {
	if runLane([]string{"--help"}) != 0 {
		t.Fatal("lane --help")
	}
}

// --chats adds the Claude Code chats of the folder (none here) to a pack.
func TestLanePackWithChatsFlag(t *testing.T) {
	repo, home := laneRepo(t)
	env := func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	var out, errw bytes.Buffer
	if code := runLaneIn([]string{"pack", "--chats", "--out", "-", repo}, nil, &out, &errw, env); code != 0 {
		t.Fatalf("pack --chats: %d %s", code, errw.String())
	}
	// push through a fake ssh that swallows the pack and says something.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\ncat >/dev/null\necho unpacked there\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	out.Reset()
	if code := runLaneIn([]string{"push", "--chats", repo, "host:/srv/proj"}, nil, &out, &errw, env); code != 0 || !strings.Contains(out.String(), "the lane is on host now") {
		t.Fatalf("push: %d %q %s", code, out.String(), errw.String())
	}
}
