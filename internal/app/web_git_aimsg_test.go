package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// "Write it for me" (git.ai_message): with no AI set up, or nothing
// staged, the page hears why; else the staged diff goes to the laptop's
// AI and its message (no code fence) comes back. Nothing is committed.
func TestWebGitAIMessage(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	c := webTestSub(t, &m)
	m.aiS().provider = nil
	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitAIMessage, `{}`)); got != "conflict" {
		t.Fatalf("no AI: %q", got)
	}
	p := gitxWithAI(&m, "```\nAdd the bee file\n\nThe tests need it.\n```")
	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitAIMessage, `{}`)); got != "conflict" {
		t.Fatalf("nothing staged: %q", got)
	}
	webTestWrite(t, dir, "b.txt", "bee\n")
	run("add", "b.txt")
	webTestGitLoad(&m)
	head := strings.TrimSpace(run("rev-parse", "HEAD"))
	var r webproto.GitAIMessage
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitAIMessage, `{}`), &r)
	if r.Message != "Add the bee file\n\nThe tests need it." {
		t.Errorf("message %q", r.Message)
	}
	if !strings.Contains(p.prompt(), "+bee") {
		t.Errorf("the staged diff was not sent:\n%s", p.prompt())
	}
	if now := strings.TrimSpace(run("rev-parse", "HEAD")); now != head {
		t.Error("git.ai_message committed")
	}
}
