//go:build browser

package web

import (
	"fmt"

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

// firstTopics are the topics the page asks for at start on the chat tab:
// the shell's, the chat view's and, on a desktop, the files column's.
func (p *Page) firstTopics() []string {
	ts := []string{"status", "toasts", "chat", "cards", "chats"}
	if !p.VP.Mobile {
		ts = append(ts, "tree", "bufs")
	}
	return ts
}

// Login opens the page, taps "Open workspace", answers the hello as conn,
// sends the first status (branch main) and toasts snapshots and waits
// until the page says it is online.
func (p *Page) Login(conn string) *HubConn {
	p.T.Helper()
	p.Open()
	return p.LoginHere(conn)
}

// LoginHere is Login on a page that already shows the connect screen.
func (p *Page) LoginHere(conn string) *HubConn {
	p.T.Helper()
	p.Tap(".connect-foot button")
	c := p.S.Hub.Accept(p.T)
	c.Handshake(p.T, conn)
	c.Subs(p.T, p.firstTopics()...)
	c.Send(p.T, Snap("status", 1, statusData("main", 0)))
	c.Send(p.T, Snap("toasts", 1, []any{}))
	p.WaitJS(`document.documentElement.dataset.link === "online"`)
	p.WaitJS(`document.querySelector(".strip .branch")?.textContent.includes("main")`)
	return c
}

// GoTab taps a tab in the tab bar (phone) or the rail (desktop) and waits
// until it is the current tab and its view is on screen.
func (p *Page) GoTab(tab string) {
	p.T.Helper()
	i := map[string]int{"chat": 1, "files": 2, "term": 3, "git": 4, "more": 5}[tab]
	p.Tap(fmt.Sprintf("nav .nav-item:nth-child(%d)", i))
	p.WaitJS(fmt.Sprintf(`document.querySelector('nav .nav-item:nth-child(%d)[aria-current="page"]') && document.querySelector('[data-view="%s"]')`, i, tab))
}
