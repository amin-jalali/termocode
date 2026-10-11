package testkit

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Source Control against a remote that wants a password. termocode owns
// the terminal, so git must never prompt on it (that would draw over the
// UI and block the app until the user typed blind). Failures must show as
// toasts.

// authRemote starts an HTTP git server on localhost that answers every
// request with 401, and returns its URL and a request counter.
func authRemote(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/repo.git", &hits
}

// promptSpy installs a git credential helper that records what git's
// environment allowed when it asked for a password: GIT_TERMINAL_PROMPT
// and whether a controlling terminal (/dev/tty) was reachable. It answers
// nothing, so git would fall back to prompting. It returns the log path.
func promptSpy(t *testing.T, r *repo) string {
	t.Helper()
	dir := looseTempDir(t)
	log := filepath.Join(dir, "helper.log")
	script := filepath.Join(dir, "helper.sh")
	body := "#!/bin/sh\n" +
		"if (exec 3</dev/tty) 2>/dev/null; then tty=tty; else tty=notty; fi\n" +
		"echo \"$1 prompt=${GIT_TERMINAL_PROMPT:-unset} $tty\" >> '" + log + "'\n" +
		"cat >/dev/null\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	r.git("config", "credential.helper", script)
	return log
}

// userAllowsPrompts sets up the environment of a user whose git may
// prompt: no GIT_TERMINAL_PROMPT=0, no askpass helpers, and localhost not
// sent through the sandbox's dead proxy.
func userAllowsPrompts(t *testing.T) {
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	t.Setenv("GIT_ASKPASS", "")
	t.Setenv("SSH_ASKPASS", "")
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "ALL_PROXY"} {
		t.Setenv(k, "") // the remote is on localhost
	}
}

// Spec: Given a remote that answers 401 and a user whose environment
// allows git to prompt, when the user pushes, fetches or pulls, then git
// is run with terminal prompts off and without a controlling terminal, the
// action fails with an error toast naming the remote problem, and nothing
// hangs.
func TestSCMRemoteAuthFailureNeverPrompts(t *testing.T) {
	url, hits := authRemote(t)
	var spy string
	d, _ := scmDriver(t, func(r *repo) {
		userAllowsPrompts(t) // after the sandbox, which turns prompts off
		r.git("remote", "add", "origin", url)
		spy = promptSpy(t, r)
	})
	for _, op := range []struct{ id, toast string }{
		{"git-push", "Push failed"},
		{"git-fetch", "Fetch failed"},
		{"git-pull", "Pull failed"},
	} {
		before := hits.Load()
		d.Palette(op.id)
		if !until(d, func() bool { return strings.Contains(text(d), op.toast) }) {
			t.Errorf("%s: no %q toast:%s", op.id, op.toast, dump(d))
		}
		if hits.Load() == before {
			t.Errorf("%s: git never reached the remote", op.id)
		}
		if !strings.Contains(text(d), "could not read Username") {
			t.Errorf("%s: toast does not say credentials are missing:%s", op.id, dump(d))
		}
		d.Key("esc")
	}
	b, _ := os.ReadFile(spy)
	log := strings.TrimSpace(string(b))
	if log == "" {
		t.Fatal("git never asked the credential helper")
	}
	for _, l := range strings.Split(log, "\n") {
		if !strings.Contains(l, "prompt=0") {
			t.Errorf("git ran with terminal prompts allowed: %q", l)
		}
		if !strings.Contains(l, "notty") {
			t.Errorf("git could reach the terminal: %q", l)
		}
	}
}
