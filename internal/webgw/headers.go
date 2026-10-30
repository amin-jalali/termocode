//go:build unix

package webgw

import (
	"net/http"
	"strings"
)

// CSP is the Content-Security-Policy for site: only our own scripts,
// styles, fonts and images; no inline code, no frames, no forms, no base
// URL. connect-src names the WebSocket origins in full as well as 'self',
// because older engines do not count ws:// as 'self'.
func CSP(site Site) string {
	connect := []string{"'self'"}
	for _, h := range site.Hosts {
		connect = append(connect, site.wsScheme()+"://"+h)
	}
	return strings.Join([]string{
		"default-src 'none'",
		"script-src 'self'",
		"style-src 'self'",
		"connect-src " + strings.Join(connect, " "),
		"img-src 'self' data: blob:",
		"font-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"form-action 'none'",
	}, "; ")
}

// setSecurityHeaders writes the fixed security headers every response
// carries, errors included. HSTS is sent only on an https site. It never
// writes Set-Cookie or any Access-Control-* header.
func setSecurityHeaders(h http.Header, csp string, site Site) {
	h.Set("Content-Security-Policy", csp)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), usb=(), clipboard-read=()")
	if site.Scheme == "https" {
		h.Set("Strict-Transport-Security", "max-age=31536000")
	}
}
