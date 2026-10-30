//go:build browser

package web

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Each tab is a history step, so the phone's back button walks back
// through the tabs and forward again (web UX 4.4).
func TestBackButtonWalksTheTabs(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			p.Login("c-1")
			p.GoTab("files")
			p.GoTab("git")
			p.GoTab("more")
			label := map[string]string{"chat": "Chat", "files": "Files", "git": "Git"}
			current := func(tab string) {
				t.Helper()
				p.WaitJS(`location.hash === "#/` + tab + `" && document.querySelector('nav .nav-item[aria-current="page"]')?.textContent.includes("` + label[tab] + `")`)
			}
			p.Do(chromedp.NavigateBack())
			current("git")
			p.Do(chromedp.NavigateBack())
			current("files")
			p.Do(chromedp.NavigateBack())
			current("chat")
			p.Do(chromedp.NavigateForward())
			current("files")
			// Going back never leaves the page or reconnects.
			s.Hub.NoConn(t, 300*time.Millisecond)
		})
	}
}
