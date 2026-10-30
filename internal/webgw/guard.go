//go:build unix

package webgw

import "net/http"

// MaxBodyBytes caps every request body. The largest real body is the
// /auth JSON (well under 1 KiB); the WebSocket has its own read limit.
const MaxBodyBytes = 4 << 10

// guard is the first handler of every request. It writes the security
// headers, then refuses anything not meant for this one site before the
// next handler sees it.
type guard struct {
	site Site
	csp  string
	next http.Handler
}

// Guard wraps next with the request checks of the gateway:
//   - Host must be one of site.Hosts exactly, else 421 (DNS rebinding, T1).
//   - Only GET and POST, else 405; OPTIONS too, so there is no CORS
//     preflight answer and never an Access-Control-* header.
//   - An Origin header must be this exact origin; POST and upgrades must
//     send one. Else 403 (cross-site WebSocket and CSRF, T2, T3).
//   - A body is at most MaxBodyBytes: a larger declared one gets 413, an
//     undeclared one stops with an error at the cap (T17).
//
// Every response, refusals included, carries the security headers.
func Guard(site Site, next http.Handler) http.Handler {
	return &guard{site: site, csp: CSP(site), next: next}
}

func (g *guard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w.Header(), g.csp, g.site)
	if !g.site.hasHost(r.Host) || (r.URL.Host != "" && r.URL.Host != r.Host) {
		refuse(w, http.StatusMisdirectedRequest, "wrong host")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		refuse(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !g.originOK(r) {
		refuse(w, http.StatusForbidden, "wrong origin")
		return
	}
	if r.ContentLength > MaxBodyBytes {
		refuse(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	g.next.ServeHTTP(w, r)
}

// originOK: an Origin header, when sent, must be exactly this request's own
// origin (scheme + the allowed Host it came on). A POST or a WebSocket
// upgrade must send one: those are what another site would forge (T2, T3).
func (g *guard) originOK(r *http.Request) bool {
	origins := r.Header.Values("Origin")
	switch len(origins) {
	case 0:
		return r.Method != http.MethodPost && !isUpgrade(r)
	case 1:
		return origins[0] == g.site.Scheme+"://"+r.Host
	default:
		return false
	}
}

// isUpgrade reports a request that asks to switch protocols in any way
// (an Upgrade header with any value): all of them need an exact Origin.
func isUpgrade(r *http.Request) bool {
	return len(r.Header.Values("Upgrade")) > 0
}

// refuse ends a request with a short plain-text error. The text names the
// rule that refused it, never a value from the request.
func refuse(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg + "\n"))
}
