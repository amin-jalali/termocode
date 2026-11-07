//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// E2E-AI (part): fork a chat, and the CI chip with Alt+C (Fix with AI),
// on the real binary. The model is the scripted reply model; gh is a
// fake script on a temp PATH (never the real gh, never GitHub).

// chatFilesWith lists the chat files under home that hold text.
func chatFilesWith(home, text string) []string {
	var out []string
	_ = filepath.WalkDir(home, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), text) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// Spec (G-FORK): two turns, then AI: Fork Chat from Message… picks the
// second question. The fork opens with the first turn only and the
// second question back in the input; the original chat stays saved with
// both turns. Enter sends the question again in the fork.
func TestAIForkChatFromMessage(t *testing.T) {
	rm := newReplyModel(t)
	home, ws := t.TempDir(), NewRepo(t)
	tm := startWindow(t, home, ws, rm.env())
	tm.settle()

	tm.askAI("question-one")
	tm.askAI("question-two")

	tm.Palette("AI: Fork Chat from Message…")
	tm.WaitText("Fork Chat from Message", uiTimeout)
	// Rows are newest first: the answer, then the question.
	tm.WaitText("AI: reply-question-two", uiTimeout)
	tm.WaitText("You: question-two", uiTimeout)
	tm.Type("You: question-two")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitGone("Fork Chat from Message", uiTimeout)

	tm.WaitText("edit the question and press Enter", uiTimeout)
	tm.WaitGone("reply-question-two", uiTimeout)
	if !tm.Has("reply-question-one") {
		tm.dump("fork")
		t.Fatal("the fork lost the first turn")
	}
	if !tm.Has("question-two") {
		tm.dump("fork")
		t.Fatal("the picked question is not back in the input")
	}

	// The original chat is kept, with both turns.
	deadline := time.Now().Add(10 * time.Second)
	for len(chatFilesWith(home, "reply-question-two")) == 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if len(chatFilesWith(home, "reply-question-two")) == 0 {
		t.Fatal("the original chat (with the second answer) was not kept on disk")
	}
	if n := len(chatFilesWith(home, "reply-question-one")); n < 2 {
		t.Fatalf("want the original and the fork on disk (both hold the first answer), got %d file(s)", n)
	}

	// Enter sends the question again, in the fork.
	tm.Send(KeyEnter)
	tm.WaitText("reply-question-two", 20*time.Second)
}

// ciFakeGH is a gh that reports one failed run for any branch, no pull
// request, and a failed log with a marker; it logs its arguments.
const ciFakeGH = `#!/bin/sh
echo "$@" >> "$CI_GH_LOG"
case "$1 $2" in
"run list")
	echo '[{"databaseId":4242,"workflowName":"ci","displayTitle":"e2e commit","status":"completed","conclusion":"failure","headSha":"abc123","url":"https://example.invalid/runs/4242"}]'
	exit 0 ;;
"pr view")
	echo 'no pull requests found for branch' >&2
	exit 1 ;;
"run view")
	printf 'test\tgo test\t2026-01-01T00:00:00.000Z ok   pkg/a\n'
	printf 'test\tgo test\t2026-01-01T00:00:01.000Z --- FAIL: TestCIMarker7d31 (0.00s)\n'
	printf 'test\tgo test\t2026-01-01T00:00:02.000Z FAIL\n'
	exit 0 ;;
esac
echo "fake gh: unexpected $*" >&2
exit 1
`

// Spec (G-CI): a red run of the branch shows the CI chip "CI ✗ 1" in the
// status bar; Alt+C puts the failing log in the AI chat input (not sent:
// the user checks it and presses Enter). gh is only asked to read.
func TestCIChipAndFixWithAI(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(ciFakeGH), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	ghLog := filepath.Join(t.TempDir(), "gh.log")
	rm := newReplyModel(t)
	home, ws := t.TempDir(), NewRepo(t)
	env := append(rm.env(), "CI_GH_LOG="+ghLog,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	tm := startWindow(t, home, ws, env)
	tm.settle()

	tm.WaitText("CI ✗ 1", 20*time.Second)

	tm.Send("\x1bc") // Alt+C on a legacy terminal
	tm.WaitText("AI CHAT", uiTimeout)
	tm.WaitText("Check the message in the chat and press Enter", uiTimeout)
	tm.WaitText("CI failed on branch", uiTimeout)
	tm.WaitText("TestCIMarker7d31", uiTimeout)

	b, _ := os.ReadFile(ghLog)
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] + " " + f[1] {
		case "run list", "pr view", "run view":
		default:
			t.Errorf("gh was asked something other than a read: %q", line)
		}
	}
	if !strings.Contains(string(b), "run view 4242 --log-failed") {
		t.Errorf("Alt+C did not read the failed log of run 4242; gh log:\n%s", b)
	}
}
