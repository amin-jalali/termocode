package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func taskRun(t *testing.T, dir string, env map[string]string, args ...string) (int, string, string) {
	t.Helper()
	oldDir, oldHere := taskDir, taskHere
	taskDir = func(string) string { return dir }
	taskHere = func(string) string { return "/w" }
	t.Cleanup(func() { taskDir, taskHere = oldDir, oldHere })
	var out, errw bytes.Buffer
	code := runTaskIn(args, &out, &errw, func(k string) string { return env[k] },
		func() (string, error) { return "/w", nil }, func() time.Time { return time.Unix(1700000000, 0) })
	return code, out.String(), errw.String()
}

func TestTaskCommandArgumentErrors(t *testing.T) {
	dir := t.TempDir()
	for _, a := range [][]string{
		nil, {"list", "--bogus"}, {"add"}, {"show"}, {"show", "1", "2"}, {"claim"}, {"claim", "1", "--as"}, {"claim", "1", "x", "y"},
		{"status", "1"}, {"status"}, {"note", "1"}, {"note"}, {"nonsense"},
	} {
		if code, _, _ := taskRun(t, dir, nil, a...); code != 2 {
			t.Errorf("%v: %d", a, code)
		}
	}
	// Unknown ids fail, not usage.
	for _, a := range [][]string{{"show", "99"}, {"claim", "99"}, {"status", "99", "done"}, {"note", "99", "hi"}} {
		if code, _, errw := taskRun(t, dir, nil, a...); code != 1 || errw == "" {
			t.Errorf("%v: %d %q", a, code, errw)
		}
	}
	var out, errw bytes.Buffer
	boom := errors.New("boom")
	if code := runTaskIn([]string{"list"}, &out, &errw, func(string) string { return "" }, func() (string, error) { return "", boom }, time.Now); code != 1 || !strings.Contains(errw.String(), "no current folder") {
		t.Errorf("no cwd: %d %q", code, errw.String())
	}
}

func TestTaskListJSONEmptyAndShowsOwnerless(t *testing.T) {
	dir := t.TempDir()
	if code, out, _ := taskRun(t, dir, nil, "list", "--json"); code != 0 || strings.TrimSpace(out) != "[]" {
		t.Errorf("empty json: %d %q", code, out)
	}
	if code, out, _ := taskRun(t, dir, nil, "add", "Write docs"); code != 0 || strings.TrimSpace(out) != "1" {
		t.Fatalf("add: %d %q", code, out)
	}
	if code, out, _ := taskRun(t, dir, nil, "list"); code != 0 || !strings.Contains(out, "-") || !strings.Contains(out, "Write docs") {
		t.Errorf("list: %d %q", code, out)
	}
	if code, out, _ := taskRun(t, dir, nil, "show", "#1"); code != 0 || strings.Contains(out, "owner:") {
		t.Errorf("show: %d %q", code, out)
	}
}

func TestTaskOwnerOrder(t *testing.T) {
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if taskOwner("me", get(nil)) != "me" {
		t.Fatal("--as first")
	}
	if taskOwner("", get(map[string]string{"TERMOCODE_TASK_OWNER": "env"})) != "env" {
		t.Fatal("env second")
	}
	if got := taskOwner("", get(map[string]string{"TERMOCODE_TAB_ID": "t9", "USER": "u"})); got != "agent t9" {
		t.Fatalf("tab third: %q", got)
	}
	if taskOwner("", get(map[string]string{"USER": "u"})) != "u" {
		t.Fatal("user last")
	}
}
