//go:build unix

package webgw

import (
	"net/http"
	"strings"
)

// PushPolicy is the Trusted Types policy that makes the push worker's
// URL (lib/push.ts).
const PushPolicy = "sw"

// CSP is the Content-Security-Policy for site: only our own scripts,
// styles, fonts and images; no inline code, no frames, no forms, no base
// URL; an installable (https) site may read its own manifest. Trusted
// Types are required with no policy allowed (an https site: only
// PushPolicy, for the push worker's one URL), so even a bug cannot hand
// a string to an HTML or script sink (never-do 20); the page builds DOM
// from text nodes only. connect-src names the WebSocket origins in full
// as well as 'self', because older engines do not count ws:// as 'self'.
func CSP(site Site) string {
	connect := []string{"'self'"}
	for _, h := range site.Hosts {
		connect = append(connect, site.wsScheme()+"://"+h)
	}
	parts := []string{
		"default-src 'none'",
		"script-src 'self'",
		"style-src 'self'",
		"connect-src " + strings.Join(connect, " "),
		"img-src 'self' data: blob:",
		"font-src 'self'",
		// The desktop editor runs in a frame of this site (FrameCSP).
		"frame-src 'self'",
	}
	if site.Installable() {
		parts = append(parts, "manifest-src 'self'")
	}
	// Trusted Types: no policy, except on an https site (the only one
	// with Web Push) the push worker's "sw", which lib/push.ts makes and
	// which passes only /sw.js?l=en|fa (serviceWorker.register is a sink).
	tt := "trusted-types 'none'"
	if site.Installable() {
		tt = "trusted-types " + PushPolicy
	}
	return strings.Join(append(parts,
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"form-action 'none'",
		"require-trusted-types-for 'script'",
		tt,
	), "; ")
}

// FramePath is the desktop editor's frame (cmd/webbuild's monaco.html).
const FramePath = "/assets/monaco.html"

// FrameCSP is the policy of the desktop editor's frame. The vendored
// editor writes its own style rules and builds its rows as HTML, so the
// frame allows inline styles and names the editor's Trusted Types
// policies. Everything else stays shut: only this site's scripts (with
// their SRI hashes), no network at all (the frame never holds the link
// or a token; it talks to the page by messages only), fonts and images
// from data: URLs (the editor's icon font), and only this site may frame
// it. The page itself keeps CSP.
func FrameCSP() string {
	return strings.Join([]string{
		"default-src 'none'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"font-src data:",
		"img-src data:",
		"connect-src 'none'",
		"worker-src 'none'",
		"frame-src 'none'",
		"frame-ancestors 'self'",
		"base-uri 'none'",
		"form-action 'none'",
		"require-trusted-types-for 'script'",
		"trusted-types editorViewLayer standaloneColorizer tokenizeToString domLineBreaksComputer richScreenReaderContent diffReview diffEditorWidget defaultWorkerFactory dompurify editorGhostText stickyScrollViewLayer 'allow-duplicates'",
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
	h.Set("Permissions-Policy", "camera=(), microphone=(self), geolocation=(), usb=(), clipboard-read=()") // the page itself may listen: chat dictation (CMP-48)
	if site.Scheme == "https" {
		h.Set("Strict-Transport-Security", "max-age=31536000")
	}
}
