//go:build browser

package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/amin-jalali/termocode/internal/webhl"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Screenshots of the main views for a person to look at (not compared):
// WEB_SHOTS_DIR=/some/dir go test -tags browser -run TestShots ./web/
// The fake hub answers like a real one: a Go file coloured by the hub's
// own lexer, a chat, a shell and a git status.

const shotsEnv = "WEB_SHOTS_DIR"

var shotFile = `package main

import (
	"bytes"
	"io"
	"strings"
)

// shortenCoverLine cuts the package list that "go test -coverpkg" repeats
// on every "coverage: N% of statements in ..." line.
func shortenCoverLine(line string) string {
	const marker = "of statements in "
	i := strings.Index(line, marker)
	if i < 0 {
		return line
	}
	tail := ""
	if strings.HasSuffix(line, "\n") {
		tail = "\n"
	}
	return line[:i+len(marker)] + "-coverpkg" + tail
}

// lineFilter passes whole lines through f to w. Close writes the last
// unfinished line.
type lineFilter struct {
	w   io.Writer
	f   func(string) string
	buf []byte
}

func (l *lineFilter) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := string(l.buf[:i+1])
		l.buf = l.buf[i+1:]
		if _, err := io.WriteString(l.w, l.f(line)); err != nil {
			return len(p), err
		}
	}
}
`

// shotHub answers the page from fixed data until the test ends.
func shotHub(t *testing.T, c *HubConn) {
	lines := strings.Split(strings.TrimSuffix(shotFile, "\n"), "\n")
	snaps := map[string]any{
		"chat": chatWith(false,
			userMsg(0, "Why does the coverage line repeat the package list?"),
			aiMsg(1, md("doc", mdPara("go test -coverpkg prints every package on each line. shortenCoverLine in cmd/covreport/filter.go cuts it after the marker."),
				mdPara("I can add a test for a line without a newline at the end."))),
			userMsg(2, "Yes, add it please")),
		"cards": map[string]any{"cards": []any{}},
		"chats": map[string]any{"chats": []any{}},
		"tree": webproto.TreeSnap{Roots: []webproto.TreeRoot{{Path: "", Label: "termocode"}}, Dirs: map[string]webproto.TreeDir{
			"":              {Entries: []webproto.TreeEntry{{Name: "cmd", Dir: true}, {Name: "internal", Dir: true}, {Name: "docs", Dir: true}, {Name: "go.mod"}, {Name: "README.md", Git: "M"}, {Name: "Makefile"}}},
			"cmd":           {Entries: []webproto.TreeEntry{{Name: "covreport", Dir: true}, {Name: "termocode", Dir: true}}},
			"cmd/covreport": {Entries: []webproto.TreeEntry{{Name: "filter.go", Git: "M"}, {Name: "filter_test.go"}, {Name: "main.go"}}},
		}},
		"bufs":  map[string]any{"bufs": []any{map[string]any{"buf": 2, "path": "cmd/covreport/filter.go", "dirty": false, "active": true}, map[string]any{"buf": 4, "path": "README.md", "dirty": true, "active": false}}},
		"buf:2": bufSnap(2, "cmd/covreport/filter.go", lines...),
		"terms": termsSnap(termItem(7, "zsh", false, true), termItem(8, "go test", true, false)),
		"term:7": termSnap(7, 0, 100, "~/termocode main ❯ go test ./cmd/covreport/", "ok  	github.com/amin-jalali/termocode/cmd/covreport	0.412s",
			"~/termocode main ❯ git status --short", " M cmd/covreport/filter.go", " M README.md", "~/termocode main ❯ "),
		"git": webproto.GitSnap{Repo: true, Branch: webproto.GitBranch{Name: "main", Ahead: 1},
			Files: []webproto.GitFile{{Path: "cmd/covreport/filter.go", X: " ", Y: "M", Unstaged: true}, {Path: "README.md", X: "M", Y: " ", Staged: true}, {Path: "notes.txt", X: "?", Y: "?", Untracked: true}},
			Log:   []webproto.GitCommit{{Hash: "60ed1c8", Subject: "test(e2e): the MCP relay's stderr is read under a lock", Author: "Amin", When: time.Now().Add(-time.Hour).UnixMilli()}}},
	}
	for _, tp := range []string{"chat", "cards", "chats", "tree", "bufs", "git"} { // asked for at login (git: on a desktop)
		c.Send(t, Snap(tp, 1, snaps[tp]))
	}
	go func() {
		for {
			var m webproto.Msg
			select {
			case m = <-c.in:
			case <-c.done:
				return
			}
			switch v := m.(type) {
			case *webproto.Sub:
				for _, tp := range v.Topics {
					if d, ok := snaps[tp]; ok {
						_ = c.send(Snap(tp, 1, d))
					}
				}
			case *webproto.Req:
				var data any
				switch v.Op {
				case "buf.open":
					data = map[string]any{"buf": 2, "path": "cmd/covreport/filter.go", "tick": 3, "lines": len(lines), "dirty": false, "ft": "go", "readonly": false}
				case "git.text":
					// The index's copy: the file before its two changed rows.
					old := strings.Replace(strings.Replace(shotFile, `"-coverpkg"`, `"…"`, 1), "\ttail := \"\"\n", "\tvar tail string\n", 1)
					data = webproto.GitText{Path: "cmd/covreport/filter.go", Rev: "index", Text: old}
				case "buf.hl":
					var a webproto.HlArgs
					_ = json.Unmarshal(v.Args, &a)
					res, _ := webhl.Chroma("filter.go", "go", lines, 3, a.First, a.Last, webproto.MaxHlSpans)
					data = res
				default:
					_ = c.send(&webproto.ErrMsg{T: webproto.KindErr, ID: v.ID, Code: webproto.CodeNotFound, Msg: "Not in this test."})
					continue
				}
				_ = c.send(&webproto.Res{T: webproto.KindRes, ID: v.ID, Data: raw(data)})
			}
		}
	}()
}

func TestShots(t *testing.T) {
	dir := os.Getenv(shotsEnv)
	if dir == "" {
		t.Skip("set " + shotsEnv + " to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	wide := Viewport{Name: "desktop", W: 1600, H: 960, Scale: 1}
	for _, vp := range []Viewport{wide, Phone} {
		for _, scheme := range []string{"dark", "light"} {
			t.Run(vp.Name+"-"+scheme, func(t *testing.T) {
				s := StartServer(t)
				p := NewPage(t, s, vp)
				p.Media(map[string]string{"prefers-reduced-motion": "reduce", "prefers-color-scheme": scheme})
				c := p.Login("c-shot")
				shotHub(t, c)
				shot := func(name, hash, ready string) {
					Eval[bool](p, `(location.hash = `+jsq(hash)+`, true)`)
					snap(t, p, filepath.Join(dir, vp.Name+"-"+scheme+"-"+name+".png"), ready)
				}
				fileReady := `document.querySelector('.crow .hl-k')`
				if !vp.Mobile {
					fileReady = `document.querySelector(".desk-editor")?.contentDocument?.querySelector(".view-line .hl-k")`
				}
				shot("file", "#/files/cmd/covreport/filter.go", fileReady)
				shot("chat", "#/chat", `document.querySelector('[data-view="chat"] .msg')`)
				shot("term", "#/term", `document.querySelector('[data-view="term"] .trow')`)
				shot("git", "#/git", `document.querySelector('.git-row-wrap')`)
				if !vp.Mobile {
					diffDoc := `document.querySelector('[data-view="diff"] .desk-editor')?.contentDocument`
					shot("diff", "#/git/cmd/covreport/filter.go", diffDoc+`?.querySelector(".editor.modified .char-insert") && `+diffDoc+`.querySelector(".editor.modified .view-line .hl-kc")`)
				}
			})
		}
	}
}

// snap waits for ready (and the fonts) and writes a screenshot to file.
func snap(t *testing.T, p *Page, file, ready string) {
	t.Helper()
	p.WaitJS(ready + ` && document.fonts.status === "loaded"`)
	ctx, cancel := context.WithTimeout(p.Ctx, 10*time.Second)
	defer cancel()
	png, err := chromedp.Run(ctx, chromedp.CaptureScreenshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, png, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The workbench's layout at the common laptop screen sizes, in its
// states: the panel shown, maximized and on the right, the side bar
// hidden (WEB_SHOTS_DIR as TestShots).
func TestLayoutShots(t *testing.T) {
	dir := os.Getenv(shotsEnv)
	if dir == "" {
		t.Skip("set " + shotsEnv + " to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, vp := range []Viewport{{Name: "1280", W: 1280, H: 800, Scale: 1}, {Name: "1440", W: 1440, H: 900, Scale: 1}, {Name: "1920", W: 1920, H: 1080, Scale: 1}} {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			p.Media(map[string]string{"prefers-reduced-motion": "reduce", "prefers-color-scheme": "dark"})
			c := p.Login("c-shot")
			shotHub(t, c)
			name := func(n string) string { return filepath.Join(dir, "layout-"+vp.Name+"-"+n+".png") }
			fileReady := `document.querySelector(".desk-editor")?.contentDocument?.querySelector(".view-line .hl-k")`
			Eval[bool](p, `(location.hash = "#/files/cmd/covreport/filter.go", true)`)
			snap(t, p, name("editor"), fileReady)
			Eval[bool](p, `(location.hash = "#/term", true)`)
			snap(t, p, name("panel"), fileReady+` && document.querySelector(".wb-panel .trow")`)
			layoutKey(p, "b")
			snap(t, p, name("noside"), `!document.querySelector(".wb-side")`)
			layoutKey(p, "b")
			p.TapJS(`document.querySelector('.wb-panel [data-act="panel-max"]')`)
			snap(t, p, name("panelmax"), `document.querySelector(".wb.is-panel-max")`)
			p.TapJS(`document.querySelector('.wb-panel [data-act="panel-max"]')`)
			p.TapJS(`document.querySelector('.wb-panel [data-act="panel-more"]')`)
			p.TapJS(`[...document.querySelectorAll(".ctx-menu button, [role=menuitem]")].find((b) => b.textContent.includes("Move Panel Right"))`)
			snap(t, p, name("panelright"), `document.querySelector(".wb.is-panel-right")`)
			Eval[bool](p, `(location.hash = "#/git/cmd/covreport/filter.go", true)`)
			diffDoc := `document.querySelector('[data-view="diff"] .desk-editor')?.contentDocument`
			snap(t, p, name("diff"), diffDoc+`?.querySelector(".editor.modified .char-insert")`)
		})
	}
}

// layoutKey presses Ctrl/Cmd and k on the page.
func layoutKey(p *Page, k string) {
	p.T.Helper()
	Eval[bool](p, `(dispatchEvent(new KeyboardEvent("keydown", { key: `+jsq(k)+`, ctrlKey: true, bubbles: true })), true)`)
}
