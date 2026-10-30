package app

import (
	"fmt"
	"strings"
)

// The web panel's settings (ADR 0020, old plan 5.1). They live only in
// the user's config.json: the AI can never change them (aiNever), and
// project files are never read for them. A bad value is the default and
// one warning (ADR 0005), never a failed start.

// webConfig is the web settings, checked.
type webConfig struct {
	On          bool // web_panel: "on"
	Port        int  // 0: random
	IdleMinutes int
	MaxHours    int
	OpenBrowser bool
	MaxViewers  int
	Tailnet     bool     // web_listen: "tailnet" (WEB-31)
	RequireTOTP bool     // web_require_totp (WEB-48)
	Problems    []string // one line per bad value, for a warning toast
}

// webSettings reads and checks the web settings from config.json.
func webSettings() webConfig {
	s := loadSettings()
	c := webConfig{IdleMinutes: 15, MaxHours: 12, OpenBrowser: true, MaxViewers: 4}
	switch strings.ToLower(strings.TrimSpace(s.WebPanel)) {
	case "", "off":
	case "on":
		c.On = true
	default:
		c.Problems = append(c.Problems, fmt.Sprintf("web_panel %q: use \"on\" or \"off\"", s.WebPanel))
	}
	switch strings.ToLower(strings.TrimSpace(s.WebListen)) {
	case "", "loopback":
	case "tailnet":
		c.Tailnet = true
	default:
		c.Problems = append(c.Problems, fmt.Sprintf("web_listen %q: use \"loopback\" or \"tailnet\"", s.WebListen))
	}
	intIn := func(key string, v *int, lo, hi int, dst *int) {
		if v == nil {
			return
		}
		if *v < lo || *v > hi {
			c.Problems = append(c.Problems, fmt.Sprintf("%s %d: use %d..%d", key, *v, lo, hi))
			return
		}
		*dst = *v
	}
	if s.WebPort != nil && *s.WebPort != 0 {
		intIn("web_port", s.WebPort, 1024, 65535, &c.Port)
	}
	intIn("web_idle_minutes", s.WebIdleMinutes, 1, 120, &c.IdleMinutes)
	intIn("web_max_hours", s.WebMaxHours, 1, 24, &c.MaxHours)
	intIn("web_max_viewers", s.WebMaxViewers, 1, 8, &c.MaxViewers)
	if s.WebRequireTOTP != nil {
		c.RequireTOTP = *s.WebRequireTOTP
	}
	if s.WebOpenBrowser != nil {
		c.OpenBrowser = *s.WebOpenBrowser
	}
	return c
}
