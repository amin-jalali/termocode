//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Git: Show Tags… lists the tags in the quick input; picking one asks
// here (Cancel first), then the laptop checks it out.
func TestGitTagsPick(t *testing.T) {
	p, c := openDeskSCM(t, nil)
	runCmd(t, p, c, "Git: Show Tags…", true)
	reply(t, c, waitReq(t, c, "git.tags"), map[string]any{"tags": []string{"v2", "v1"}})
	p.WaitJS(`[...document.querySelectorAll(".quick-row .quick-label")].map((e) => e.textContent).join(",") === "v2,v1"`)
	p.Key("v1")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "v1"`)
	p.Key("\r")
	p.WaitJS(`document.querySelector(".ask-text")?.textContent.includes("v1") && document.activeElement === document.querySelector(".ask .choices button")`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, "git.checkout_tag", `{"tag":"v1"}`), webproto.AskRef{Card: "card-6"})
	p.WaitJS(`document.querySelector(".waiting")`)
}

// Git: Show Stash List… lists the entries; picking one asks to apply it.
func TestGitStashListPick(t *testing.T) {
	p, c := openDeskSCM(t, nil)
	runCmd(t, p, c, "Git: Show Stash List…", true)
	reply(t, c, waitReq(t, c, "git.stash_list"), map[string]any{"entries": []map[string]string{{"ref": "stash@{0}", "message": "wip toast"}, {"ref": "stash@{1}", "message": "old try"}}})
	p.WaitJS(`document.querySelectorAll(".quick-row").length === 2`)
	p.Key("old")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "old try"`)
	p.Key("\r")
	p.WaitJS(`document.querySelector(".ask-text")?.textContent.includes("stash@{1}")`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, "git.stash_apply", `{"ref":"stash@{1}"}`), webproto.AskRef{Card: "card-7"})
	p.WaitJS(`document.querySelector(".waiting")`)
}
