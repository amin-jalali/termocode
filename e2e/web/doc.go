// Package web drives the termocode web page in a real Chrome (chromedp).
//
// The tests serve the embedded page with the real guard, headers, CSP and
// /auth of internal/webgw, and put a fake hub behind /ws: a Go WebSocket
// server that speaks internal/webproto and checks every browser message
// with the strict decoder. No termocode binary and no nvim are needed.
//
// The tests carry the build tag "browser" (go test -tags browser). They
// skip when no Chrome is found, unless TERMOCODE_WEB_E2E=require is set
// (CI), where a missing Chrome fails. Build with the webdev tag as well
// (-tags browser,webdev) to run the card tests on the component kit page.
package web
