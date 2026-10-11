//go:build e2e

package e2e

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Spec: Given a workspace whose origin is an HTTP remote that wants a
// password (answers 401), and termocode running in a real terminal, when
// the user runs "Git: Push", then git never prompts on the terminal (no
// "Username for …" over the UI, no frozen app): the push fails with an
// error toast, and the app still answers keys.
func TestGitPushAuthNeverPromptsOnTerminal(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	ws := NewRepo(t)
	for _, args := range [][]string{
		{"remote", "add", "origin", srv.URL + "/repo.git"},
		{"config", "branch.main.remote", "origin"},
		{"config", "branch.main.merge", "refs/heads/main"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = ws
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	tm := Start(t, StartOpts{Cols: 120, Rows: 36, Workspace: ws})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()

	tm.Palette("Git: Push")
	tm.WaitFor("push result", uiTimeout, func(ls []string) bool {
		s := strings.Join(ls, "\n")
		return strings.Contains(s, "Push failed") || strings.Contains(s, "Username for")
	})
	if tm.Has("Username for") || tm.Has("Password for") {
		t.Fatalf("git prompted on the terminal:\n%s", tm.Screen())
	}
	if hits.Load() == 0 {
		t.Error("git never reached the remote")
	}
	tm.step("push failed")

	// The app still works: the palette opens and closes.
	tm.Send(KeyF1)
	tm.WaitText("Command Palette", uiTimeout)
	tm.Send(KeyEsc)
	tm.WaitGone("Command Palette", 3*time.Second)
	tm.Quit()
}
