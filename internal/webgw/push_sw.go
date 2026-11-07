//go:build unix

package webgw

import (
	_ "embed"
	"net/http"
)

// SWPath is where an installable site serves the push-only service
// worker (MOB-30). Its scope is the whole site; it handles push and
// notification taps only (sw.js).
const SWPath = "/sw.js"

//go:embed sw.js
var swJS []byte

// SWCSP is the worker's own policy: it loads nothing and connects
// nowhere.
const SWCSP = "default-src 'none'"

// ServiceWorker serves sw.js at SWPath and nothing else. The page
// registers it only on an https site, where Web Push works.
func ServiceWorker() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != SWPath {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Security-Policy", SWCSP)
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(swJS)
	})
}
