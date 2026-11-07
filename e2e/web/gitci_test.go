//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Git: Fix CI with AI puts the laptop's message (the failing log) in the
// chat input and opens the chat; nothing is sent until the user sends.
func TestGitFixCIWithAI(t *testing.T) {
	p, c := openDeskSCM(t, nil)
	runGitCmd(t, p, c, "Git: Fix CI with AI", true)
	reply(t, c, waitReq(t, c, "ci.fix_prompt"), map[string]any{"prompt": "CI failed on branch main (workflow CI).\n\nFind the cause and fix it."})
	c.Send(t, Snap("chat", 1, map[string]any{"id": "c1", "title": "", "messages": []any{}}))
	p.WaitJS(`document.querySelector(".composer textarea")?.value.startsWith("CI failed on branch main")`)
	if got := Eval[string](p, `document.querySelector(".toasts")?.textContent ?? ""`); got == "" {
		t.Error("no toast says to check and send the message")
	}
}

// Git: Fix CI with AI when CI is not red is a note, not an error: an info
// toast that hides by itself, not a red toast that stays until closed.
func TestGitFixCINotRedIsInfo(t *testing.T) {
	p, c := openDeskSCM(t, nil)
	runGitCmd(t, p, c, "Git: Fix CI with AI", true)
	r := waitReq(t, c, "ci.fix_prompt")
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeConflict, Msg: "CI is not red on main."})
	p.WaitJS(`(document.querySelector(".toasts")?.textContent ?? "").includes("CI is not red on main.")`)
	if got := Eval[string](p, `[...document.querySelectorAll(".toast")].find((e) => e.textContent.includes("CI is not red"))?.className ?? ""`); got != "toast sev-info" {
		t.Errorf("toast class = %q, want toast sev-info (a note, not a sticky error)", got)
	}
}
