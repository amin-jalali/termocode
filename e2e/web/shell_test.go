//go:build browser

package web

import (
	"strings"
	"testing"
)

// The page loads from the real gateway, trades the link for a token,
// connects to /ws with hello and shows the status the hub sends.
func TestPageConnectsAndShowsStatus(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			c := p.Login("c-1")
			c.Send(t, Delta("status", 2, statusData("feat/web", 3)))
			p.WaitJS(`document.querySelector(".strip .branch")?.textContent.includes("feat/web")`)
			if got := p.Text(".strip .dirty"); !strings.Contains(got, "3") {
				t.Errorf("dirty chip %q, want 3 files", got)
			}
		})
	}
}
