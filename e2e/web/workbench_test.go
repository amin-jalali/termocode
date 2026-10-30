//go:build browser

package web

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop workbench: the laptop editor's layout. A tab per open
// file, a status bar at the bottom with the file's facts, panels that
// open and close from the keyboard.

// openBufAs opens path as buffer buf with extra snapshot fields.
func openBufAs(t *testing.T, p *Page, c *HubConn, buf int, path string, extra map[string]any, lines ...string) {
	t.Helper()
	Eval[bool](p, `(location.hash = `+jsq("#/files/"+path)+`, true)`)
	r := nextOpen(t, c)
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"buf": buf, "path": path, "tick": 3, "lines": len(lines), "dirty": false, "ft": "go", "readonly": false})})
	topic := fmt.Sprint("buf:", buf)
	waitSubSkipping(t, c, topic)
	snap := bufSnap(buf, path, lines...)
	for k, v := range extra {
		snap[k] = v
	}
	c.Send(t, Snap(topic, 1, snap))
	p.WaitJS(filesView + `.querySelector(".crow, .desk-editor")`)
}

// nextOpen reads the page's messages until a buf.open; a buf.close of
// the file it leaves is answered on the way.
func nextOpen(t *testing.T, c *HubConn) *webproto.Req {
	t.Helper()
	for {
		switch m := c.Next(t).(type) {
		case *webproto.Req:
			switch m.Op {
			case "buf.open":
				return m
			case "buf.close":
				c.Send(t, &webproto.Res{T: webproto.KindRes, ID: m.ID})
			default:
				t.Fatalf("page sent %s; want buf.open", show(m))
			}
		}
	}
}

func TestWorkbenchLayoutTabsAndStatusBar(t *testing.T) {
	p, c := openFiles(t, Desktop)
	// The frame: a 48 px activity bar, a 22 px status bar at the bottom.
	if w := Eval[float64](p, `document.querySelector(".layout-desktop .rail").getBoundingClientRect().width`); w != 48 {
		t.Errorf("activity bar %.0f px wide, want 48", w)
	}
	geo := Eval[[]float64](p, `(() => { const r = document.querySelector(".statusbar").getBoundingClientRect(); return [r.height, r.bottom, innerHeight] })()`)
	if geo[0] != 22 || geo[1] != geo[2] {
		t.Errorf("status bar %v: want 22 px tall at the bottom", geo)
	}
	if !Eval[bool](p, `document.querySelector(".statusbar .branch")?.textContent.includes("main")`) {
		t.Error("the status bar has no branch")
	}

	openBufAs(t, p, c, 2, "src/a.go", map[string]any{"ts": 4, "et": false, "eol": "unix"}, "package a", "", "func f() {}")
	p.WaitJS(`document.querySelector('.ed-tab.is-on [data-path="src/a.go"]')`)
	p.WaitJS(`document.querySelector(".statusbar .sb-lang")?.textContent === "Go"`)
	for sel, want := range map[string]string{".sb-pos": "Ln 1, Col 1", ".sb-indent": "Tab size: 4", ".sb-enc": "UTF-8", ".sb-eol": "LF"} {
		if got := p.Text(".statusbar " + sel); got != want {
			t.Errorf("status bar %s = %q, want %q", sel, got, want)
		}
	}
	if got := Eval[string](p, `[...document.querySelectorAll(".wb-editor .crumbs .crumb")].map((e) => e.textContent).join("/")`); got != "src" {
		t.Errorf("breadcrumb folders %q, want src", got)
	}
	if !Eval[bool](p, `document.querySelector(".wb-editor .crumbs .ficon.fi-blue") !== null`) {
		t.Error("the breadcrumb has no Go file icon before the name")
	}
	if Eval[bool](p, filesView+`.querySelector(".head-back") !== null`) {
		t.Error("the desktop file view has a phone back button")
	}
	// The editor's cursor is the status bar's line and column.
	p.WaitJS(deskDoc + `?.querySelector(".view-lines .view-line")`)
	deskFocus(p)
	p.Do(chromedp.KeyEvent(kb.Home, modKey()))
	p.Do(chromedp.KeyEvent(kb.ArrowDown + kb.ArrowDown + kb.ArrowRight))
	p.WaitJS(`document.querySelector(".statusbar .sb-pos")?.textContent === "Ln 3, Col 2"`)

	// A second file: two tabs; the first one comes back with a click.
	openBufAs(t, p, c, 4, "b.go", nil, "package b")
	if n := Eval[int](p, `document.querySelectorAll(".ed-tab").length`); n != 2 {
		t.Fatalf("%d tabs, want 2", n)
	}
	p.TapJS(`document.querySelector('.ed-tab-open[data-path="src/a.go"]')`)
	nextOpen(t, c)
	p.WaitJS(`location.hash === "#/files/src/a.go" && document.querySelector('.ed-tab.is-on [data-path="src/a.go"]')`)

	// Ctrl+B hides the side bar and shows it again; Ctrl+J opens the terminal panel.
	p.Key("\x1b") // nothing focused that eats keys
	Eval[bool](p, `(document.dispatchEvent(new KeyboardEvent("keydown", {key: "b", ctrlKey: true, bubbles: true})), true)`)
	p.WaitJS(`!document.querySelector(".wb-side")`)
	Eval[bool](p, `(document.dispatchEvent(new KeyboardEvent("keydown", {key: "b", ctrlKey: true, bubbles: true})), true)`)
	p.WaitJS(`document.querySelector(".wb-side")`)
	Eval[bool](p, `(document.dispatchEvent(new KeyboardEvent("keydown", {key: "j", ctrlKey: true, bubbles: true})), true)`)
	p.WaitJS(`document.querySelector(".wb-panel .wb-panel-title.is-on")?.textContent === "Terminal" && document.querySelector('.wb-panel [data-view="term"]')`)

	// Closing the active tab shows its neighbour; the laptop keeps its buffer.
	p.TapJS(`document.querySelector(".ed-tab.is-on .ed-tab-x")`)
	p.WaitJS(`document.querySelectorAll(".ed-tab").length === 1 && location.hash === "#/files/b.go"`)
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}

// The side bar and the chat resize by dragging their edge (and with the
// arrow keys on the focused edge); the size stays for the next visit.
func TestWorkbenchResizeKeepsSize(t *testing.T) {
	p, _ := openFiles(t, Desktop)
	before := Eval[float64](p, `document.querySelector(".wb-side").getBoundingClientRect().width`)
	Eval[bool](p, `(document.querySelector(".sash-x").focus(), true)`)
	p.Key(kb.ArrowRight + kb.ArrowRight) // 16 px each
	p.WaitJS(fmt.Sprintf(`document.querySelector(".wb-side").getBoundingClientRect().width === %v`, before+32))
	Eval[bool](p, `(location.reload(), true)`)
	p.WaitJS(fmt.Sprintf(`document.querySelector(".wb-side")?.getBoundingClientRect().width === %v`, before+32))
}

// waitSubSkipping is WaitSub that lets the unsubs and buf.close of a file
// the editor leaves go by.
func waitSubSkipping(t *testing.T, c *HubConn, topic string) {
	t.Helper()
	for {
		switch m := c.Next(t).(type) {
		case *webproto.Sub:
			for _, x := range m.Topics {
				if x == topic {
					return
				}
			}
		case *webproto.Unsub:
		case *webproto.Req:
			if m.Op != "buf.close" {
				t.Fatalf("page sent %s while a sub for %s was expected", show(m), topic)
			}
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: m.ID})
		default:
			t.Fatalf("page sent %s while a sub for %s was expected", show(m), topic)
		}
	}
}

// Every view is an item of the desktop's activity bar, with its icon,
// a tooltip with the keys, and a badge; side views open in the side bar,
// Settings and This device in the editor area.
func TestWorkbenchActivityBar(t *testing.T) {
	p, c := openFiles(t, Desktop)
	ids := Eval[[]string](p, `[...document.querySelectorAll("nav.act-bar .act-item")].map((e) => e.dataset.act)`)
	if got := strings.Join(ids, ","); got != "files,search,git,run,testing,db,containers,mcp,chat,device,settings" {
		t.Errorf("activity bar items %s", got)
	}
	if tip := Eval[string](p, `document.querySelector('.act-item[data-act="files"]').title`); !strings.HasSuffix(tip, "+Shift+E)") || !strings.HasPrefix(tip, "Explorer (") {
		t.Errorf("Explorer tooltip %q", tip)
	}
	c.Send(t, Snap("status", 2, statusData("main", 3)))
	p.WaitJS(`document.querySelector('.act-item[data-act="git"] .act-badge')?.textContent === "3"`)
	// Search opens in the side bar.
	p.Tap(`.act-item[data-act="search"]`)
	p.WaitJS(`document.querySelector('.wb-side[data-side="search"] .view') && location.hash.startsWith("#/more/search")`)
	// Databases open in the side bar too (a tree); Settings opens in the
	// editor area and the side bar keeps Databases.
	p.Tap(`.act-item[data-act="db"]`)
	p.WaitJS(`document.querySelector('.wb-side[data-side="db"] [data-view="db"]') && location.hash === "#/more/db"`)
	p.Tap(`.act-item[data-act="settings"]`)
	p.WaitJS(`location.hash === "#/more/settings" && document.querySelector('.wb-editor .view') && document.querySelector('.wb-side[data-side="db"]') && document.querySelector('.act-item[data-act="settings"][aria-current="page"]')`)
	// A second click on the active side item hides the side bar.
	p.Tap(`.act-item[data-act="files"]`)
	p.WaitJS(`document.querySelector('.wb-side[data-side="files"]')`)
	p.Tap(`.act-item[data-act="files"]`)
	p.WaitJS(`!document.querySelector(".wb-side")`)
}
