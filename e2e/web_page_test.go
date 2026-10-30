//go:build e2e && browser

package e2e

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

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/chromedp"
)

// A small Chrome driver for the web tests that run against the real
// binary (build tags e2e and browser). The fake-hub suite in e2e/web has
// its own, bound to its fake server; this one only knows a URL.

// webRequireEnv set to "require" turns a missing Chrome into a failure (CI).
const webRequireEnv = "TERMOCODE_WEB_E2E"

// webDumpEnv names a folder for a screenshot and the page text of a
// failed test (CI uploads it).
const webDumpEnv = "WEB_E2E_DUMP_DIR"

// webWait bounds one wait in the page; the real binary on a slow macOS
// runner needs more than the fake hub.
const webWait = 30 * time.Second

type webViewport struct {
	Name   string
	W, H   int64
	Scale  float64
	Mobile bool // mobile metrics and touch input
}

var (
	webPhone   = webViewport{Name: "phone", W: 390, H: 844, Scale: 3, Mobile: true}
	webDesktop = webViewport{Name: "desktop", W: 1280, H: 800, Scale: 1}
)

// webChromePath finds Chrome or Chromium; CHROME_PATH wins.
func webChromePath() string {
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

// webPage is one headless Chrome tab.
type webPage struct {
	t   *testing.T
	ctx context.Context
	vp  webViewport

	mu       sync.Mutex
	problems []string
}

// newWebPage starts a headless Chrome with a fresh profile, sized to vp.
// Chrome is stopped when the test ends.
func newWebPage(t *testing.T, vp webViewport) *webPage {
	t.Helper()
	exe := webChromePath()
	if exe == "" {
		if os.Getenv(webRequireEnv) == "require" {
			t.Fatalf("no Chrome found and %s=require", webRequireEnv)
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
	p := &webPage{t: t, ctx: ctx, vp: vp}
	t.Cleanup(func() {
		if t.Failed() {
			p.dump()
		}
		cancel()
		acancel()
	})
	console := chromedp.Console(ctx)
	go func() {
		for m, err := range console {
			if err != nil {
				return
			}
			if m.Type == chromedp.ConsoleError || m.Type == chromedp.ConsoleException {
				p.mu.Lock()
				p.problems = append(p.problems, fmt.Sprintf("console %s: %s", m.Type, m.Text))
				p.mu.Unlock()
			}
		}
	}()
	enable := chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, log.Enable, cdp.Empty{})
		return err
	})
	view := []chromedp.EmulateViewportOption{chromedp.EmulateScale(vp.Scale)}
	if vp.Mobile {
		view = append(view, chromedp.EmulateMobile, chromedp.EmulateTouch)
	}
	reduce := chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{
			Features: []*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}})
		return err
	})
	if err := chromedp.Do(ctx, enable, chromedp.EmulateViewport(vp.W, vp.H, view...), reduce); err != nil {
		t.Fatalf("start Chrome: %v", err)
	}
	return p
}

// do runs actions with webWait as the time limit.
func (p *webPage) do(actions ...chromedp.Action[chromedp.Void]) {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(p.ctx, webWait)
	defer cancel()
	if err := chromedp.Do(ctx, actions...); err != nil {
		p.t.Fatalf("%s: %v", p.vp.Name, err)
	}
}

// webEval runs a JavaScript expression in the page and returns its value.
func webEval[T any](p *webPage, js string) T {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(p.ctx, webWait)
	defer cancel()
	v, err := chromedp.Run(ctx, chromedp.Evaluate[T](js))
	if err != nil {
		p.t.Fatalf("%s: eval %s: %v", p.vp.Name, webShort(js), err)
	}
	return v
}

// waitJS waits until the expression is true.
func (p *webPage) waitJS(js string) {
	p.t.Helper()
	p.waitJSFor(js, webWait)
}

func (p *webPage) waitJSFor(js string, d time.Duration) {
	p.t.Helper()
	deadline := time.Now().Add(d)
	for {
		ctx, cancel := context.WithTimeout(p.ctx, webWait)
		ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool]("!!("+js+")"))
		cancel()
		if err == nil && ok {
			return
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("%s: waited %s for %s (last error: %v)", p.vp.Name, d, webShort(js), err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func webShort(js string) string {
	js = strings.Join(strings.Fields(js), " ")
	if len(js) > 140 {
		return js[:140] + "…"
	}
	return js
}

// tapJS presses the element a JavaScript expression returns: a touch tap
// on the phone, a mouse click on the desktop. It first waits until the
// element is on screen and not covered.
func (p *webPage) tapJS(el string) {
	p.t.Helper()
	// Like a user, wait until nothing (a toast, a sheet) covers its middle.
	p.waitJS(`(() => {
		const el = ` + el + `;
		if (!el) return false;
		el.scrollIntoView({block: "center", inline: "center"});
		const r = el.getBoundingClientRect();
		const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
		return hit !== null && (el === hit || el.contains(hit));
	})()`)
	pt := webEval[struct{ X, Y float64 }](p, `(() => {
		const r = (`+el+`).getBoundingClientRect();
		return {X: r.left + r.width / 2, Y: r.top + r.height / 2};
	})()`)
	if p.vp.Mobile {
		p.do(chromedp.TapXY(pt.X, pt.Y))
	} else {
		p.do(chromedp.MouseClickXY(pt.X, pt.Y))
	}
}

// tap is tapJS for the first element matching a CSS selector.
func (p *webPage) tap(sel string) {
	p.t.Helper()
	p.tapJS(fmt.Sprintf(`document.querySelector(%q)`, sel))
}

// keys sends key presses to the focused element.
func (p *webPage) keys(s string) {
	p.t.Helper()
	p.do(chromedp.KeyEvent(s))
}

// webButton is a JavaScript expression for the first button under root
// (a JavaScript expression) whose text is exactly text.
func webButton(root, text string) string {
	return fmt.Sprintf(`[...(%s).querySelectorAll("button")].find((b) => b.textContent.trim() === %q)`, root, text)
}

// webJSQ quotes s as a JavaScript string.
func webJSQ(s string) string { return fmt.Sprintf("%q", s) }

// dump writes a screenshot and the page's text to $WEB_E2E_DUMP_DIR.
func (p *webPage) dump() {
	dir := os.Getenv(webDumpEnv)
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	name := filepath.Join(dir, strings.NewReplacer("/", "_", " ", "_").Replace(p.t.Name()))
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()
	if png, err := chromedp.Run(ctx, chromedp.CaptureScreenshot()); err == nil {
		_ = os.WriteFile(name+".png", png, 0o644)
	}
	text, _ := chromedp.Run(ctx, chromedp.Evaluate[string](`location.pathname + location.hash.replace(/#s=.*/, "#s=…") + "\n\n" + document.body.innerText`))
	p.mu.Lock()
	probs := strings.Join(p.problems, "\n")
	p.mu.Unlock()
	_ = os.WriteFile(name+".txt", []byte(text+"\n\nproblems:\n"+probs+"\n"), 0o644)
}
