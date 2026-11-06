package testkit

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Source Control: Git: Generate Commit Message with AI (GITX-8).

// Spec: Given a staged change, when the user runs Generate Commit Message
// with AI, then the staged diff goes to the AI, its answer (no code
// fence) shows in the message box, nothing is committed yet, and Enter
// commits it.
func TestSCMAICommitMessage(t *testing.T) {
	s, srv := newAIScript(t, aiReply{text: "```\nfeat: count to eleven\n```"})
	d := aiDriverConfig(t, srv.URL, reviewFiles(), true, `"ai_inline":false`)
	if err := writeAndStage(d.Dir, "r.txt", scn22Staged); err != nil {
		t.Fatal(err)
	}
	d.Action("view:git")
	d.Palette("git-refresh")
	d.Palette("git-ai-commit-msg")
	if !d.WaitForText("count to eleven", 5*time.Second) { // the box scrolls to the caret
		t.Fatalf("no message in the box:%s", dump(d))
	}
	s.mu.Lock()
	sent := strings.Join(s.bodies, "\n")
	s.mu.Unlock()
	if !strings.Contains(sent, "+eleven") {
		t.Errorf("the staged diff was not sent: %s", sent)
	}
	if subject := gitLogSubject(t, d.Dir); subject == "feat: count to eleven" {
		t.Fatal("committed before the user said so")
	}
	d.Key("enter")
	if !d.WaitFor(func() bool { return gitLogSubject(t, d.Dir) == "feat: count to eleven" }, 5*time.Second) {
		t.Errorf("Enter did not commit the message: %q", gitLogSubject(t, d.Dir))
	}
}

func gitLogSubject(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "log", "-1", "--format=%s")
	cmd.Dir = dir
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}
