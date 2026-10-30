//go:build browser

package web

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/audits"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// requireEnv set to "require" turns a missing Chrome into a failure (CI).
const requireEnv = "TERMOCODE_WEB_E2E"

// dumpEnv names a folder for a screenshot and the record of each failed
// test (CI uploads it).
const dumpEnv = "WEB_E2E_DUMP_DIR"

// Viewport is a screen the page runs on.
type Viewport struct {
	Name   string
	W, H   int64
	Scale  float64
	Mobile bool // mobile metrics and touch input
}

var (
	// Phone is the design's phone (web UX 6.1): 390×844 with touch.
	Phone = Viewport{Name: "phone", W: 390, H: 844, Scale: 3, Mobile: true}
	// Desktop is a laptop-sized window with a mouse.
	Desktop = Viewport{Name: "desktop", W: 1280, H: 800, Scale: 1}
	// Both runs a check on the phone and on the desktop.
	Both = []Viewport{Phone, Desktop}
)

// chromePath finds Chrome or Chromium; CHROME_PATH wins.
func chromePath() string {
	if p := os.Getenv("CHROME_PATH"); p != "" {
		return p
	}
	var names []string
	switch runtime.GOOS {
	case "darwin":
		names = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default:
		names = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"}
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}

// Page is one Chrome tab on one Server, with everything the page did
// recorded: requests, console errors, exceptions and CSP reports.
type Page struct {
	T   *testing.T
	Ctx context.Context
	S   *Server
	VP  Viewport

	mu       sync.Mutex
	requests []string
	problems []string
}

// NewPage starts a headless Chrome with a fresh profile, sized to vp, and
// starts recording. Chrome is stopped when the test ends.
func NewPage(t *testing.T, s *Server, vp Viewport) *Page {
	t.Helper()
	exe := chromePath()
	if exe == "" {
		if os.Getenv(requireEnv) == "require" {
			t.Fatalf("no Chrome found and %s=require", requireEnv)
		}
		t.Skip("no Chrome found; set CHROME_PATH or install Chrome")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(exe),
		chromedp.UserDataDir(t.TempDir()),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-component-update", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("lang", "en-US"),
	)
	if os.Getenv("CI") != "" {
		// Hosted runners do not allow Chrome's user-namespace sandbox. The
		// page is our own, served from loopback.
		opts = append(opts, chromedp.NoSandbox)
	}
	actx, acancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(actx)
	p := &Page{T: t, Ctx: ctx, S: s, VP: vp}
	t.Cleanup(func() {
		if t.Failed() {
			p.dump()
		}
		cancel()
		acancel()
	})
	// Subscribe before anything loads, so nothing is missed.
	requests := chromedp.Events(ctx, network.RequestWillBeSent)
	sockets := chromedp.Events(ctx, network.WebSocketCreated)
	console := chromedp.Console(ctx)
	issues := chromedp.Events(ctx, audits.IssueAdded)
	go func() {
		for ev, err := range requests {
			if err != nil {
				return
			}
			p.record(&p.requests, ev.Request.URL)
		}
	}()
	go func() {
		for ev, err := range sockets {
			if err != nil {
				return
			}
			p.record(&p.requests, ev.URL)
		}
	}()
	go func() {
		for m, err := range console {
			if err != nil {
				return
			}
			if m.Type == chromedp.ConsoleError || m.Type == chromedp.ConsoleException || m.Type == chromedp.ConsoleWarning {
				p.record(&p.problems, fmt.Sprintf("console %s: %s", m.Type, m.Text))
			}
		}
	}()
	go func() {
		for ev, err := range issues {
			if err != nil {
				return
			}
			if ev.Issue != nil && ev.Issue.Code == audits.InspectorIssueCodeContentSecurityPolicyIssue {
				d := ""
				if ev.Issue.Details != nil && ev.Issue.Details.ContentSecurityPolicyIssueDetails != nil {
					c := ev.Issue.Details.ContentSecurityPolicyIssueDetails
					d = fmt.Sprintf(" %s %s", c.ViolatedDirective, c.BlockedURL)
				}
				p.record(&p.problems, "csp:"+d)
			}
		}
	}()
	enable := chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		if _, err := cdp.Call(ctx, t, network.Enable, network.EnableParams{}); err != nil {
			return err
		}
		if _, err := cdp.Call(ctx, t, log.Enable, cdp.Empty{}); err != nil {
			return err
		}
		_, err := cdp.Call(ctx, t, audits.Enable, cdp.Empty{})
		return err
	})
	view := []chromedp.EmulateViewportOption{chromedp.EmulateScale(vp.Scale)}
	if vp.Mobile {
		view = append(view, chromedp.EmulateMobile, chromedp.EmulateTouch)
	}
	if err := chromedp.Do(ctx, enable, chromedp.EmulateViewport(vp.W, vp.H, view...)); err != nil {
		t.Fatalf("start Chrome: %v", err)
	}
	return p
}

func (p *Page) record(into *[]string, s string) {
	p.mu.Lock()
	*into = append(*into, s)
	p.mu.Unlock()
}

// Requests returns every URL the page asked for, WebSockets included.
func (p *Page) Requests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.requests...)
}

// Problems returns console errors and warnings, exceptions and CSP reports.
func (p *Page) Problems() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.problems...)
}

// Do runs actions with the test's wait as the time limit.
func (p *Page) Do(actions ...chromedp.Action[chromedp.Void]) {
	p.T.Helper()
	ctx, cancel := context.WithTimeout(p.Ctx, wait)
	defer cancel()
	if err := chromedp.Do(ctx, actions...); err != nil {
		p.T.Fatalf("%s: %v", p.VP.Name, err)
	}
}

// Eval runs a JavaScript expression in the page (DevTools evaluation,
// which the page's CSP does not cover) and returns its JSON value.
func Eval[T any](p *Page, js string) T {
	p.T.Helper()
	ctx, cancel := context.WithTimeout(p.Ctx, wait)
	defer cancel()
	v, err := chromedp.Run(ctx, chromedp.Evaluate[T](js))
	if err != nil {
		p.T.Fatalf("%s: eval %s: %v", p.VP.Name, short(js), err)
	}
	return v
}

// WaitJS waits until the expression is true.
func (p *Page) WaitJS(js string) {
	p.T.Helper()
	deadline := time.Now().Add(wait)
	for {
		ctx, cancel := context.WithTimeout(p.Ctx, wait)
		ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool]("!!("+js+")"))
		cancel()
		if err == nil && ok {
			return
		}
		if time.Now().After(deadline) {
			p.T.Fatalf("%s: waited %s for %s (last error: %v)", p.VP.Name, wait, short(js), err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Holds checks that the expression stays true for d.
func (p *Page) Holds(js string, d time.Duration) {
	p.T.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if !Eval[bool](p, "!!("+js+")") {
			p.T.Fatalf("%s: %s stopped being true", p.VP.Name, short(js))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func short(js string) string {
	js = strings.Join(strings.Fields(js), " ")
	if len(js) > 120 {
		return js[:120] + "…"
	}
	return js
}

// Tap presses the first element matching the CSS selector: a touch tap
// on the phone, a mouse click on the desktop. The element is scrolled
// into view first.
func (p *Page) Tap(sel string) {
	p.T.Helper()
	p.WaitJS(fmt.Sprintf(`document.querySelector(%q)`, sel))
	pt := Eval[struct{ X, Y float64 }](p, fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		el.scrollIntoView({block: "center", inline: "center"});
		const r = el.getBoundingClientRect();
		return {X: r.left + r.width / 2, Y: r.top + r.height / 2};
	})()`, sel))
	if p.VP.Mobile {
		p.Do(chromedp.TapXY(pt.X, pt.Y))
	} else {
		p.Do(chromedp.MouseClickXY(pt.X, pt.Y))
	}
}

// Key sends key presses to the focused element (chromedp kb names, e.g. "\r").
func (p *Page) Key(keys string) {
	p.T.Helper()
	p.Do(chromedp.KeyEvent(keys))
}

// Text returns the text of the first element matching sel ("" if none).
func (p *Page) Text(sel string) string {
	p.T.Helper()
	return Eval[string](p, fmt.Sprintf(`(document.querySelector(%q)?.textContent ?? "")`, sel))
}

// dump writes a screenshot, the page's text, its requests and problems
// to $WEB_E2E_DUMP_DIR, for a failed test.
func (p *Page) dump() {
	dir := os.Getenv(dumpEnv)
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	name := filepath.Join(dir, strings.NewReplacer("/", "_", " ", "_").Replace(p.T.Name()))
	ctx, cancel := context.WithTimeout(p.Ctx, 5*time.Second)
	defer cancel()
	if png, err := chromedp.Run(ctx, chromedp.CaptureScreenshot()); err == nil {
		_ = os.WriteFile(name+".png", png, 0o644)
	}
	text, _ := chromedp.Run(ctx, chromedp.Evaluate[string](`location.href + "\n\n" + document.body.innerText`))
	rec := text + "\n\nrequests:\n" + strings.Join(p.Requests(), "\n") + "\n\nproblems:\n" + strings.Join(p.Problems(), "\n") + "\n"
	_ = os.WriteFile(name+".txt", []byte(rec), 0o644)
}
