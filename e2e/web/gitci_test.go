//go:build browser

package web

import "testing"

// Git: Fix CI with AI puts the laptop's message (the failing log) in the
// chat input and opens the chat; nothing is sent until the user sends.
func TestGitFixCIWithAI(t *testing.T) {
	p, c := openDeskSCM(t, nil)
	runCmd(t, p, c, "Git: Fix CI with AI", true)
	reply(t, c, waitReq(t, c, "ci.fix_prompt"), map[string]any{"prompt": "CI failed on branch main (workflow CI).\n\nFind the cause and fix it."})
	c.Send(t, Snap("chat", 1, map[string]any{"id": "c1", "title": "", "messages": []any{}}))
	p.WaitJS(`document.querySelector(".composer textarea")?.value.startsWith("CI failed on branch main")`)
	if got := Eval[string](p, `document.querySelector(".toasts")?.textContent ?? ""`); got == "" {
		t.Error("no toast says to check and send the message")
	}
}
