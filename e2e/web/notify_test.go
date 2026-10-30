//go:build browser

package web

import (
	"slices"
	"testing"
	"time"
)

// OS notifications (WEB-37): fixed texts from a closed list, asked for
// only after a click, only while the page is in the background.

// fakeNotification records each notification in window.__notes as
// "title|body" and each permission request in window.__asked.
const fakeNotification = `(() => {
	window.__notes = []; window.__asked = 0;
	class N {
		constructor(title, o) { window.__notes.push(title + "|" + ((o && o.body) || "")); this.onclick = null; }
		close() {}
		static requestPermission() { window.__asked++; N.permission = "granted"; return Promise.resolve("granted"); }
	}
	N.permission = "default";
	Object.defineProperty(window, "Notification", {configurable: true, writable: true, value: N});
})()`

// notifyTexts is the closed list: nothing else may ever be shown.
var notifyTexts = []string{
	"termocode|The AI waits for your approval.",
	"termocode|A run finished.",
	"termocode|A run failed.",
}

func notes(p *Page) []string { return Eval[[]string](p, `window.__notes`) }

const notifyBtn = `document.querySelector(".notify-on")`

// The page never asks for the permission by itself; one click on
// "Notify me" in More asks once, and the choice stays on this device.
func TestNotifyAsksOnlyAfterAClick(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.onNewDocument(fakeNotification)
	c := p.Login("c-1")
	p.setHidden(true)
	c.Send(t, Snap("cards", 1, cardsData(toolCard("card-1", "go test ./..."))))
	c.Send(t, attention("run_failed", "r-1"))
	p.Holds(`window.__asked === 0 && window.__notes.length === 0`, 300*time.Millisecond)
	p.setHidden(false)

	p.GoTab("more")
	p.TapJS(notifyBtn)
	p.WaitJS(`window.__asked === 1 && JSON.parse(localStorage.getItem("termocode.prefs.v1")).notify === true`)
	p.WaitJS(`document.querySelector(".notify-off")`) // now it can be turned off
	p.Tap(".notify-off")
	p.WaitJS(`JSON.parse(localStorage.getItem("termocode.prefs.v1")).notify === false`)
}

// Turned on, a new card or a finished run in the background shows a
// notification with a fixed text, never the command, a path or the AI's
// words; on screen there is none; the title counts the waiting cards and
// marks a run that ended unseen.
func TestNotifyFixedTexts(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.onNewDocument(fakeNotification)
	c := p.Login("c-1")
	c.Send(t, Snap("cards", 1, cardsData()))
	p.GoTab("more")
	p.TapJS(notifyBtn)
	p.WaitJS(`window.__asked === 1`)

	c.Send(t, attention("run_done", "r-0")) // on screen: no notification
	p.Holds(`window.__notes.length === 0`, 300*time.Millisecond)

	p.setHidden(true)
	c.Send(t, Delta("cards", 2, map[string]any{"op": "upsert", "card": toolCard("card-1", "rm -rf /home/secret/project")}))
	p.WaitJS(`window.__notes.length === 1`)
	p.WaitJS(`document.title === "(1) termocode"`)
	c.Send(t, attention("run_failed", "r-1"))
	c.Send(t, attention("task_done", "build"))
	p.WaitJS(`window.__notes.length === 3`)
	got := notes(p)
	for _, n := range got {
		if !slices.Contains(notifyTexts, n) {
			t.Errorf("notification %q is not one of the fixed texts", n)
		}
	}
	if got[0] != notifyTexts[0] || got[1] != notifyTexts[2] || got[2] != notifyTexts[1] {
		t.Errorf("notifications %q", got)
	}
	c.Send(t, Delta("cards", 3, map[string]any{"op": "remove", "id": "card-1", "by": "laptop"}))
	p.WaitJS(`document.title === "(•) termocode"`)
	p.setHidden(false)
	p.WaitJS(`document.title === "termocode"`)
}
