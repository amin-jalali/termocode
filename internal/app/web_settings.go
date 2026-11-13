package app

import (
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/relay"
	"github.com/amin-jalali/termocode/internal/webpush"
)

// The web panel's settings (ADR 0020, old plan 5.1). They live only in
// the user's config.json: the AI can never change them (aiNever), and
// project files are never read for them. A bad value is the default and
// one warning (ADR 0005), never a failed start.

// webConfig is the web settings, checked.
type webConfig struct {
	On          bool // not "web_panel": "off": the panel on 127.0.0.1 needs no setting
	Explicit    bool // "web_panel": "on": SSH mode and a script's --yes need it
	Port        int  // 0: random
	IdleMinutes int
	MaxHours    int
	OpenBrowser bool
	MaxViewers  int
	Tailnet     bool   // web_listen: "tailnet" (WEB-31)
	LAN         bool   // web_listen: "lan": https on this Wi-Fi, pinned certificate
	RequireTOTP bool   // web_require_totp (WEB-48)
	AppKeyLogin bool   // web_app_key_login (MOB-27)
	PushDetail  string // web_push_detail (MOB-30): none, count or title
	PhoneWatch  bool   // web_phone: "on": a paired phone that watches keeps the panel up (MOB-31)
	// Relay (web_relay, ADR 0022): the relay's https address; "" is off.
	Relay string
	// ApproveOnLaptop (web_approve_on_laptop): a web action that needs a
	// confirm waits for the laptop's card; off, the page confirms it.
	ApproveOnLaptop bool
	Problems        []string // one line per bad value, for a warning toast
	// The door (MOB-56, plan §3.7): how many projects the paired gateway
	// keeps open at once, and when a project the phone opened headless
	// stops again.
	ProjectsMax        int // web_projects_max
	ProjectIdleMinutes int // web_project_idle_minutes
}

// webSettings reads and checks the web settings from config.json.
func webSettings() webConfig {
	s := loadSettings()
	c := webConfig{On: true, IdleMinutes: 15, MaxHours: 12, OpenBrowser: true, MaxViewers: 4, PushDetail: webpush.DetailCount, ProjectsMax: 4, ProjectIdleMinutes: 60}
	switch strings.ToLower(strings.TrimSpace(s.WebPanel)) {
	case "", "auto":
	case "off":
		c.On = false
	case "on":
		c.Explicit = true
	default:
		c.Problems = append(c.Problems, fmt.Sprintf("web_panel %q: use \"auto\", \"on\" or \"off\"", s.WebPanel))
	}
	switch strings.ToLower(strings.TrimSpace(s.WebListen)) {
	case "", "loopback":
	case "tailnet":
		c.Tailnet = true
	case "lan":
		c.LAN = true
	default:
		c.Problems = append(c.Problems, fmt.Sprintf("web_listen %q: use \"loopback\", \"tailnet\" or \"lan\"", s.WebListen))
	}
	switch strings.ToLower(strings.TrimSpace(s.WebPhone)) {
	case "", "off":
	case "on":
		c.PhoneWatch = true
	default:
		c.Problems = append(c.Problems, fmt.Sprintf("web_phone %q: use \"on\" or \"off\"", s.WebPhone))
	}
	if v := strings.TrimSpace(s.WebRelay); v != "" {
		if _, err := relay.CheckURL(v); err != nil {
			c.Problems = append(c.Problems, fmt.Sprintf("web_relay %q: use https://host[:port]", s.WebRelay))
		} else {
			c.Relay = strings.TrimSuffix(v, "/")
		}
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
	switch d := strings.ToLower(strings.TrimSpace(s.WebPushDetail)); d {
	case "":
	case webpush.DetailNone, webpush.DetailCount, webpush.DetailTitle:
		c.PushDetail = d
	default:
		c.Problems = append(c.Problems, fmt.Sprintf("web_push_detail %q: use \"none\", \"count\" or \"title\"", s.WebPushDetail))
	}
	intIn("web_projects_max", s.WebProjectsMax, 1, 8, &c.ProjectsMax)
	intIn("web_project_idle_minutes", s.WebProjectIdleMinutes, 5, 240, &c.ProjectIdleMinutes)
	if s.WebRequireTOTP != nil {
		c.RequireTOTP = *s.WebRequireTOTP
	}
	if s.WebAppKeyLogin != nil {
		c.AppKeyLogin = *s.WebAppKeyLogin
	}
	if s.WebOpenBrowser != nil {
		c.OpenBrowser = *s.WebOpenBrowser
	}
	if s.WebApproveOnLaptop != nil {
		c.ApproveOnLaptop = *s.WebApproveOnLaptop
	}
	return c
}

// WebPanelExplicitOn reports whether the user's config.json says
// "web_panel": "on" (a script's `termocode web --yes` needs it).
func WebPanelExplicitOn() bool { return webSettings().Explicit }
