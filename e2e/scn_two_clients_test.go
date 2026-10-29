//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// replyModel is an OpenAI-compatible model that answers every request
// with "reply-<the last user message>".
type replyModel struct{ srv *httptest.Server }

func newReplyModel(t *testing.T) *replyModel {
	t.Helper()
	rm := &replyModel{}
	rm.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		last := ""
		for _, m := range req.Messages {
			if m.Role == "user" {
				last = fmt.Sprint(m.Content)
			}
		}
		if i := strings.LastIndex(last, "question-"); i >= 0 {
			last = strings.Fields(last[i:])[0]
		}
		out, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "reply-" + last}}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", out)
	}))
	t.Cleanup(rm.srv.Close)
	return rm
}

func (rm *replyModel) env() []string {
	return []string{"TERMOCODE_AI_PROVIDER=ollama", "TERMOCODE_AI_MODEL=fake", "TERMOCODE_AI_BASE_URL=" + rm.srv.URL + "/v1"}
}

// startWindow starts one termocode on ws: a local terminal, so no
// session; a second one is a second, independent window.
func startWindow(t *testing.T, home, ws string, env []string) *Term {
	t.Helper()
	env = append(env, "GIT_AUTHOR_NAME=E2E Tester", "GIT_AUTHOR_EMAIL=e2e@example.com",
		"GIT_COMMITTER_NAME=E2E Tester", "GIT_COMMITTER_EMAIL=e2e@example.com")
	tm := Start(t, StartOpts{Home: home, Workspace: ws, Env: env, Cols: 200, Rows: 40}) // wide: toasts beside the chat panel stay readable
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	return tm
}

// askAI opens and focuses the chat (a window may start with it open) and
// sends q; the reply model answers "reply-"+q.
func (tm *Term) askAI(q string) {
	tm.t.Helper()
	if !tm.Has("AI CHAT") {
		tm.Send("\x1ba")
		tm.WaitText("AI CHAT", uiTimeout)
	}
	tm.settle()
	if tm.Has("F6 focus") {
		tm.Send("\x1b[17~")
		tm.WaitGone("F6 focus", uiTimeout)
	}
	tm.Type(q)
	tm.Send(KeyEnter)
	tm.WaitText("reply-"+q, 20*time.Second)
	tm.settle()
}

// gitOut runs git in ws.
func gitOut(t *testing.T, ws string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = ws
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// Spec (SCN-10): two termocode windows on one folder (no session: a
// second local terminal; with a session a second client takes over,
// TestSessionTakeover). Window A has a chat before B starts.
//   - Edit: B follows A's save; when B has unsaved edits, B's Ctrl+S asks
//     (no silent overwrite, no freeze); Overwrite writes B's text and A
//     follows.
//   - Commit: while A's git holds the index lock, B's stage says so
//     clearly; then both commit, in turn, without a lock left behind.
//   - AI: B does not take A's open chat; each window's turns land in its
//     own chat file, none lost.
//   - MCP: only one window serves MCP for the folder; the other says why.
func TestSCN10TwoWindowsOneFolder(t *testing.T) {
	home, ws := t.TempDir(), NewRepo(t)
	notes := filepath.Join(ws, "notes.txt")
	writeFile(t, filepath.Join(home, ".config", "termocode", "config.json"), `{"mcp_server":"on","setup_done":true}`)

	a := startWindow(t, home, ws, newReplyModel(t).env())
	a.settle()
	a.askAI("question-a1")
	b := startWindow(t, home, ws, newReplyModel(t).env())
	b.WaitText("MCP server not started", uiTimeout)
	b.settle()

	// ── Edit the same file ──
	for _, tm := range []*Term{a, b} {
		tm.OpenFile("notes.txt")
		tm.WaitText("first note", uiTimeout)
		tm.settle()
		tm.Send(KeyEsc) // Normal mode, whatever Quick Open left
		tm.settle()
		tm.Send(KeyEsc)
		tm.settle()
	}
	a.Type("ofrom-window-a")
	a.WaitText("from-window-a", uiTimeout)
	a.Send(KeyEsc)
	a.settle()
	a.Send(KeyCtrlS)
	waitFileHas(t, notes, "from-window-a", uiTimeout)
	if got := readFileOrEmpty(notes); got != "first note\nfrom-window-a\n" {
		t.Fatalf("A saved: %q", got)
	}
	b.WaitText("from-window-a", 15*time.Second) // B had no edits: it reloads

	b.Type("Gofrom-window-b")
	b.Send(KeyEsc)
	b.WaitText("from-window-b", uiTimeout)
	a.Type("Gofrom-a-again")
	a.WaitText("from-a-again", uiTimeout)
	a.Send(KeyEsc)
	a.settle()
	a.Send(KeyCtrlS)
	waitFileHas(t, notes, "from-a-again", uiTimeout)
	b.settle()
	b.Send(KeyCtrlS)
	b.WaitText("File changed on disk", uiTimeout)
	b.settle()
	if got := readFileOrEmpty(notes); got != "first note\nfrom-window-a\nfrom-a-again\n" {
		t.Fatalf("B wrote before the answer: %q", got)
	}
	b.Send(KeyEnter) // Cancel is first and focused: keep both
	b.WaitGone("File changed on disk", uiTimeout)
	b.Send(KeyCtrlS)
	b.WaitText("File changed on disk", uiTimeout)
	b.settle()
	b.ClickText("Overwrite")
	waitFileHas(t, notes, "from-window-b", uiTimeout)
	if got := readFileOrEmpty(notes); got != "first note\nfrom-window-a\nfrom-window-b\n" {
		t.Fatalf("after Overwrite: %q", got)
	}
	a.WaitText("from-window-b", 15*time.Second) // A had no edits: it reloads
	b.settle()
	b.CheckScreen("B after overwrite", mainFrame)

	// ── Commit from both ──
	lock := filepath.Join(ws, ".git", "index.lock")
	writeFile(t, lock, "") // A's git is busy
	b.Palette("Git: Stage All Changes")
	b.WaitText("Another git command", uiTimeout)
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	b.settle()
	b.Palette("Git: Stage All Changes")
	b.WaitText("Staged all changes", uiTimeout)
	b.Palette("Git: Commit...")
	b.WaitText("staged file", uiTimeout)
	b.Type("commit from window b")
	b.Send(KeyEnter)
	b.WaitText("Committed", uiTimeout)
	writeFile(t, filepath.Join(ws, "a-only.txt"), "from a\n")
	a.Palette("Git: Stage All Changes")
	a.WaitText("Staged all changes", uiTimeout)
	a.Palette("Git: Commit...")
	a.WaitText("staged file", uiTimeout)
	a.Type("commit from window a")
	a.Send(KeyEnter)
	a.WaitText("Committed", uiTimeout)
	log := gitOut(t, ws, "log", "--format=%s", "-3")
	if !strings.HasPrefix(log, "commit from window a\ncommit from window b\n") {
		t.Errorf("git log:\n%s", log)
	}
	if exists(lock) {
		t.Error("index.lock left behind")
	}

	// ── AI in both ──
	b.askAI("question-b1")
	if b.Has("question-a1") {
		t.Errorf("window B shows window A's chat:\n%s", b.Screen())
	}
	a.askAI("question-a2")
	chats, _ := filepath.Glob(filepath.Join(home, ".config", "termocode", "ai", "chats", "*.json"))
	var all []string
	for _, f := range chats {
		all = append(all, readFileOrEmpty(f))
	}
	if len(chats) != 2 {
		t.Fatalf("%d chat files, want 2 (one per window):\n%s", len(chats), strings.Join(all, "\n"))
	}
	for _, q := range []string{"question-a1", "question-a2", "question-b1"} {
		n := 0
		for _, c := range all {
			if strings.Contains(c, `"reply-`+q+`"`) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s: in %d chat files, want 1:\n%s", q, n, strings.Join(all, "\n"))
		}
	}

	for _, tm := range []*Term{b, a} {
		tm.Send(KeyEsc)
		tm.settle()
		tm.Quit()
	}
}
