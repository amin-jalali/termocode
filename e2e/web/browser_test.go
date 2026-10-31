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
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/audits"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
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
	// Tablet is a window under the desktop workbench's width, with a mouse:
	// the file view shows numbered rows there, as on a phone.
	Tablet = Viewport{Name: "tablet", W: 1024, H: 768, Scale: 1}
	// RowViews are the screens whose file view is the numbered rows (the
	// desktop has the workbench editor, deskeditor_test.go).
	RowViews = []Viewport{Phone, Tablet}
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
	return NewPageLang(t, s, vp, "en-US")
}

// NewPageLang is NewPage with a browser language (navigator.language),
// e.g. "fa-IR".
func NewPageLang(t *testing.T, s *Server, vp Viewport, lang string) *Page {
	t.Helper()
	exe := chromePath()
	if exe == "" {
		if os.Getenv(requireEnv) == "require" {
			t.Fatalf("no Chrome found and %s=require", requireEnv)
		}
		t.Skip("no Chrome found; set CHROME_PATH or install Chrome")
	}
	var err error
	for try := 1; try <= startTries; try++ {
		var p *Page
		if p, err = startPage(t, s, vp, lang, exe, startWait); err == nil {
			return p
		}
		t.Logf("start Chrome (try %d of %d): %v", try, startTries, err)
	}
	t.Fatalf("start Chrome: %v", err)
	return nil
}

// A Chrome start that gets no answer is stopped and tried again with a
// fresh Chrome: on the macOS runner one start in many hangs for ever in
// Target.attachToTarget (2026-10-30, the whole suite timed out on it).
var (
	startWait  = time.Minute
	startTries = 3
)

// startPage is one try of NewPageLang: Chrome must start, open its tab
// and take the settings within wait, or it is stopped and an error says so.
func startPage(t *testing.T, s *Server, vp Viewport, lang, exe string, wait time.Duration) (*Page, error) {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(exe),
		chromedp.UserDataDir(t.TempDir()),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-component-update", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("lang", lang),
		chromedp.Flag("accept-lang", lang),
	)
	if s.Site.Scheme == "https" {
		opts = append(opts, chromedp.Flag("ignore-certificate-errors", true)) // the test's own certificate
	}
	if os.Getenv("CI") != "" {
		// Hosted runners do not allow Chrome's user-namespace sandbox. The
		// page is our own, served from loopback.
		opts = append(opts, chromedp.NoSandbox)
	}
	actx, acancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(actx)
	stop := func() {
		cancel()
		acancel()
	}
	// The watchdog ends a start that hangs: every call waiting on Chrome
	// returns once the context is canceled, and Chrome is killed.
	var hung atomic.Bool
	watchdog := time.AfterFunc(wait, func() {
		hung.Store(true)
		stop()
	})
	p := &Page{T: t, Ctx: ctx, S: s, VP: vp}
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
	err := chromedp.Do(ctx, enable, chromedp.EmulateViewport(vp.W, vp.H, view...))
	if !watchdog.Stop() || hung.Load() {
		stop()
		return nil, fmt.Errorf("Chrome gave no answer in %s (last error: %v)", wait, err)
	}
	if err != nil {
		stop()
		return nil, err
	}
	t.Cleanup(func() {
		if t.Failed() {
			p.dump()
		}
		stop()
	})
	return p, nil
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

// Reload reloads the page and waits for the new document: a check right
// after location.reload() can still run in the old one, or in neither.
func (p *Page) Reload() {
	p.T.Helper()
	Eval[bool](p, `(window.__tcOldDoc = true, location.reload(), true)`)
	p.WaitJS(`!window.__tcOldDoc && document.readyState === "complete"`)
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
	p.TapJS(fmt.Sprintf(`document.querySelector(%q)`, sel))
}

// TapJS is Tap for the element a JavaScript expression returns.
func (p *Page) TapJS(el string) {
	p.T.Helper()
	p.WaitJS(el)
	// The point is read only once the fonts are in and the target has
	// stood still, in view, for a few frames, and the point hits the
	// target: a view still scrolling (a smooth scroll, slow on the macOS
	// runner) or a late font swap would carry it away from a point read
	// at once (the press then missed the button). A target that stays
	// covered (on purpose) is tapped after 60 more still frames.
	ctx, cancel := context.WithTimeout(p.Ctx, wait)
	defer cancel()
	pt, err := chromedp.Run(ctx, chromedp.Evaluate[struct{ X, Y float64 }](`document.fonts.ready.then(() => new Promise((done) => {
		const el = `+el+`;
		el.scrollIntoView({block: "center", inline: "center"});
		let last = "", still = 0;
		const look = () => {
			const r = el.getBoundingClientRect(), at = [r.left, r.top, r.width, r.height].join();
			still = at === last ? still + 1 : 0;
			last = at;
			if (still >= 3) {
				const x = r.left + r.width / 2, y = r.top + r.height / 2;
				if (x >= 0 && y >= 0 && x <= innerWidth && y <= innerHeight) {
					const hit = document.elementFromPoint(x, y);
					if ((hit && el.contains(hit)) || still >= 63) return done({X: x, Y: y});
				} else {
					el.scrollIntoView({block: "center", inline: "center"});
					still = 0;
				}
			}
			requestAnimationFrame(look);
		};
		requestAnimationFrame(look);
	}))`, chromedp.EvalAwaitPromise))
	if err != nil {
		p.T.Fatalf("%s: tap %s: the target did not stand still in view within %s: %v", p.VP.Name, short(el), wait, err)
	}
	if p.VP.Mobile {
		p.Do(chromedp.TapXY(pt.X, pt.Y))
	} else {
		p.Do(chromedp.MouseClickXY(pt.X, pt.Y))
	}
}

// ButtonByText is a JavaScript expression for the first button whose
// text is exactly text.
func ButtonByText(text string) string {
	return fmt.Sprintf(`[...document.querySelectorAll("button")].find((b) => b.textContent.trim() === %q)`, text)
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

// Media emulates CSS media features, e.g. "prefers-color-scheme": "light"
// or "prefers-reduced-motion": "reduce". Features left out are not emulated.
func (p *Page) Media(features map[string]string) {
	p.T.Helper()
	var fs []*emulation.MediaFeature
	for k, v := range features {
		fs = append(fs, &emulation.MediaFeature{Name: k, Value: v})
	}
	p.Do(chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Features: fs})
		return err
	}))
}
