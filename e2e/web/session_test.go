//go:build browser

package web

import (
	"github.com/chromedp/chromedp"
)

// statusData is a status topic value as the hub sends it (web design 2.1).
func statusData(branch string, dirty int) map[string]any {
	return map[string]any{
		"root": "/w", "workspace_label": "demo", "branch": branch, "dirty_files": dirty,
		"ai":        map[string]any{"provider_label": "AI", "model": "m", "busy": false, "cards_waiting": 0},
		"terminals": map[string]any{"count": 1, "busy": 0},
		"runs":      map[string]any{"live": 0, "needs_you": 0},
		"web":       map[string]any{"clients": 1, "role": "controller"},
	}
}

// Open loads the page from a fresh one-time link and returns the secret.
func (p *Page) Open() string {
	p.T.Helper()
	url, secret := p.S.LaunchURL(p.T)
	p.Do(chromedp.Navigate(url))
	p.WaitJS(`document.querySelector(".connect-foot button")`)
	return secret
}

// Login opens the page, taps "Open workspace", answers the hello as conn,
// sends the first status (branch main) and toasts snapshots and waits
// until the page says it is online.
func (p *Page) Login(conn string) *HubConn {
	p.T.Helper()
	p.Open()
	p.Tap(".connect-foot button")
	c := p.S.Hub.Accept(p.T)
	c.Handshake(p.T, conn)
	c.Subs(p.T, "status", "toasts")
	c.Send(p.T, Snap("status", 1, statusData("main", 0)))
	c.Send(p.T, Snap("toasts", 1, []any{}))
	p.WaitJS(`document.documentElement.dataset.link === "online"`)
	p.WaitJS(`document.querySelector(".strip .branch")?.textContent.includes("main")`)
	return c
}
