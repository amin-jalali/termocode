//go:build unix

package main

import (
	"io/fs"
	"net/http"
	"regexp"
	"strings"

	"github.com/amin-jalali/termocode/internal/webgw"
	"github.com/amin-jalali/termocode/internal/webgw/assets"
)

// hostRE is a Host the page's CSP may name: a name or an address, with
// an optional port. Anything else could add words to the policy.
var hostRE = regexp.MustCompile(`^([A-Za-z0-9.-]+|\[[0-9A-Fa-f:.]+\])(:[0-9]{1,5})?$`)

// pageHandler serves the relay page (relay.html at /) and the panel's
// built files under /assets/, the same bytes the laptop serves. The CSP
// is the panel's own for this host, with one Trusted Types policy
// ("relay") so relay.js can load app.js once its tunnel is ready.
// tlsProxy: a TLS proxy is in front (the page is https).
func pageHandler(tlsProxy bool) http.Handler {
	page, err := fs.ReadFile(assets.FS(), "relay.html")
	files := webgw.Assets()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if !hostRE.MatchString(r.Host) {
			http.Error(w, "bad host", http.StatusBadRequest)
			return
		}
		scheme := "http"
		if r.TLS != nil || tlsProxy {
			scheme = "https"
		}
		csp := strings.Replace(webgw.CSP(webgw.Site{Scheme: scheme, Hosts: []string{r.Host}}), "trusted-types 'none'", "trusted-types relay", 1)
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(self), geolocation=(), usb=(), clipboard-read=()")
		switch {
		case r.URL.Path == "/":
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				h.Set("Allow", "GET")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(page)
		case strings.HasPrefix(r.URL.Path, "/assets/"):
			files.ServeHTTP(w, r) // the editor's frame sets its own policy
		default:
			http.NotFound(w, r)
		}
	})
}
