package main

import (
	"bytes"
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"
)

func runnerCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	dir, folder := t.TempDir(), t.TempDir()
	return runnerCLIIn(t, dir, folder, stdin, args...)
}

func runnerCLIIn(t *testing.T, dir, folder, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errw bytes.Buffer
	code := runRunnerIn(args, strings.NewReader(stdin), &out, &errw,
		func() (string, error) { return folder, nil }, func() (string, error) { return dir, nil })
	return code, out.String(), errw.String()
}

func TestRunnerCLIErrorsAndUsage(t *testing.T) {
	var out, errw bytes.Buffer
	boom := errors.New("boom")
	if code := runRunnerIn([]string{"list"}, nil, &out, &errw, nil, func() (string, error) { return "", boom }); code != 1 || !strings.Contains(errw.String(), "no queue folder") {
		t.Errorf("queue dir error: %d %q", code, errw.String())
	}
	for _, c := range []struct {
		args []string
		code int
	}{
		{[]string{"serve", "--bogus"}, 2}, {[]string{"serve", "--pool", "0"}, 2}, {[]string{"serve", "--pool=x"}, 2},
		{[]string{"add"}, 2}, {[]string{"add", "--bogus", "x"}, 2}, {[]string{"list", "--x"}, 2},
		{[]string{"show"}, 2}, {[]string{"show", "a", "b"}, 2}, {[]string{"cancel"}, 2}, {[]string{"nonsense"}, 2},
		{[]string{"show", "no-such-task"}, 1}, {[]string{"cancel", "no-such-task"}, 1},
	} {
		if code, _, _ := runnerCLI(t, "", c.args...); code != c.code {
			t.Errorf("%v: %d, want %d", c.args, code, c.code)
		}
	}
	// The current folder cannot be found.
	errw.Reset()
	bad := func() (string, error) { return "", boom }
	dir := t.TempDir()
	if code := runRunnerIn([]string{"add", "do it"}, nil, &out, &errw, bad, func() (string, error) { return dir, nil }); code != 1 || !strings.Contains(errw.String(), "no current folder") {
		t.Errorf("add without cwd: %d %q", code, errw.String())
	}
	if code := runRunnerIn([]string{"serve"}, nil, &out, &errw, bad, func() (string, error) { return dir, nil }); code != 1 {
		t.Errorf("serve without cwd: %d", code)
	}
}

func TestRunnerCLIAddFlagsListShowCancel(t *testing.T) {
	dir, folder := t.TempDir(), t.TempDir()
	code, out, errw := runnerCLIIn(t, dir, folder, "", "add", "--folder", folder, "--agent=claude", "--model", "m1", "--base", "main", "--read-only", "--", "look", "--at", "this")
	if code != 0 {
		t.Fatalf("add: %d %s", code, errw)
	}
	id := strings.TrimSpace(out)
	if code, out, _ := runnerCLIIn(t, dir, folder, "", "add", "-"); code != 2 && code != 0 {
		t.Logf("add - : %d %s", code, out)
	}
	if code, out, _ := runnerCLIIn(t, dir, folder, "piped prompt", "add", "-"); code != 0 || out == "" {
		t.Errorf("add from stdin: %d %q", code, out)
	}
	if code, out, _ := runnerCLIIn(t, dir, folder, "", "list"); code != 0 || !strings.Contains(out, id) || !strings.Contains(out, "look") {
		t.Errorf("list: %d %q", code, out)
	}
	if code, out, _ := runnerCLIIn(t, dir, folder, "", "list", "--json"); code != 0 || !strings.HasPrefix(out, "[") {
		t.Errorf("list json: %d %q", code, out)
	}
	if code, out, _ := runnerCLIIn(t, dir, folder, "", "show", id); code != 0 || !strings.Contains(out, "read-only") || !strings.Contains(out, "Prompt:") {
		t.Errorf("show: %d %q", code, out)
	}
	if code, out, _ := runnerCLIIn(t, dir, folder, "", "show", id, "--json"); code != 0 || !strings.Contains(out, id) {
		t.Errorf("show json: %d %q", code, out)
	}
	if code, out, _ := runnerCLIIn(t, dir, folder, "", "cancel", id); code != 0 || !strings.Contains(out, "canceled") {
		t.Errorf("cancel: %d %q", code, out)
	}
	if code, out, _ := runnerCLIIn(t, dir, folder, "", "list", "--json"); code != 0 || out == "" {
		t.Errorf("list after cancel: %d", code)
	}
	empty := t.TempDir()
	if code, out, _ := runnerCLIIn(t, empty, folder, "", "list", "--json"); code != 0 || strings.TrimSpace(out) != "[]" {
		t.Errorf("empty list json: %d %q", code, out)
	}
}

func TestRunnerShortCutsLongFirstLine(t *testing.T) {
	if got := runnerShort("abcdef\nsecond", 4); got != "abc…" {
		t.Fatalf("%q", got)
	}
	if got := runnerShort("ab\nc", 4); got != "ab" {
		t.Fatalf("%q", got)
	}
}

// serve runs until it is interrupted, then returns 0.
func TestRunnerServeStopsOnInterrupt(t *testing.T) {
	dir, folder := t.TempDir(), t.TempDir()
	done := make(chan int, 1)
	var out, errw bytes.Buffer
	go func() {
		done <- runRunnerIn([]string{"serve", "--folder", folder, "--pool", "1"}, nil, &out, &errw,
			func() (string, error) { return folder, nil }, func() (string, error) { return dir, nil })
	}()
	time.Sleep(700 * time.Millisecond)
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serve: %d %s", code, errw.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop")
	}
}
