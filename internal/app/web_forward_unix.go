//go:build unix

package app

import "github.com/amin-jalali/termocode/internal/webgw"

// Tailnet port previews run tailscale serve in the foreground: its
// config ends with the process (web_forward.go).
func init() {
	webForwardServe = func(httpsPort int, target string) (webFwdServe, error) {
		return webgw.StartTailscaleServeOn(httpsPort, target)
	}
}
