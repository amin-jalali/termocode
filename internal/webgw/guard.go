//go:build unix

package webgw

import "net/http"

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
	g.next.ServeHTTP(w, r)
}

// refuse ends a request with a short plain-text error. The text names the
// rule that refused it, never a value from the request.
func refuse(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg + "\n"))
}
