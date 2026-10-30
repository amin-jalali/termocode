//go:build browser

package web

import (
	"context"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// The page is installable only on an https (tailnet) site (WEB-35): it
// links the app manifest there and Chrome reads it without a problem;
// on loopback there is neither the link nor the file. No service worker
// is ever registered.
func TestManifestOnlyInTailnet(t *testing.T) {
	for _, https := range []bool{false, true} {
		name := map[bool]string{false: "loopback", true: "tailnet"}[https]
		t.Run(name, func(t *testing.T) {
			s := startServer(t, https)
			p := NewPage(t, s, Phone)
			p.Login("c-1")
			if bad := p.Problems(); len(bad) > 0 {
				t.Errorf("problems: %v", bad)
			}
			hasLink := Eval[bool](p, `document.querySelector('link[rel="manifest"]') !== null`)
			Eval[bool](p, `(fetch("/manifest.webmanifest").then((r) => (window.__st = r.status)), true)`)
			p.WaitJS(`window.__st !== undefined`)
			status := Eval[float64](p, `window.__st`)
			if !https {
				if hasLink || status != 404 {
					t.Errorf("loopback: manifest link %v, GET = %v; want none and 404", hasLink, status)
				}
			} else {
				if !hasLink || status != 200 {
					t.Fatalf("tailnet: manifest link %v, GET = %v; want both", hasLink, status)
				}
				var res page.GetAppManifestResult
				p.Do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
					var err error
					res, err = cdp.Call(ctx, tg, page.GetAppManifest, page.GetAppManifestParams{})
					return err
				}))
				if len(res.Errors) > 0 || res.Data == "" {
					t.Errorf("Chrome read the manifest with problems: %+v", res)
				}
			}
			Eval[bool](p, `(navigator.serviceWorker ? navigator.serviceWorker.getRegistrations().then((r) => (window.__sw = r.length)) : (window.__sw = 0), true)`)
			p.WaitJS(`window.__sw !== undefined`)
			if Eval[float64](p, `window.__sw`) > 0 {
				t.Error("a service worker is registered")
			}
		})
	}
}
