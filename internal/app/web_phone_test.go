package app

import (
	"slices"
	"strings"
	"testing"
)

// Spec (MOB-31): "web_phone": "on" in the user's config.json turns on
// phone watch (a paired phone that watches keeps the panel up); unset or
// "off" is off (safe choice first); a bad value is off and one problem.
func TestWebSettingsPhoneWatch(t *testing.T) {
	withConfigDir(t)
	if c := webSettings(); c.PhoneWatch {
		t.Fatal("phone watch on by default")
	}
	writeUserConfig(t, `{"web_phone":"on"}`)
	if c := webSettings(); !c.PhoneWatch || len(c.Problems) != 0 {
		t.Fatalf("on: %+v", c)
	}
	writeUserConfig(t, `{"web_phone":"off"}`)
	if c := webSettings(); c.PhoneWatch || len(c.Problems) != 0 {
		t.Fatalf("off: %+v", c)
	}
	writeUserConfig(t, `{"web_phone":"always"}`)
	if c := webSettings(); c.PhoneWatch || len(c.Problems) != 1 || !strings.Contains(c.Problems[0], "web_phone") {
		t.Fatalf("bad value: %+v", c)
	}
}

// Spec (MOB-31): the gateway gets --phone-watch only when web_phone is on.
func TestWebGatewayArgsPhoneWatch(t *testing.T) {
	base := webConfig{IdleMinutes: 15, MaxHours: 12, MaxViewers: 4}
	if args := webGatewayArgs(base, "/w", "/a", "/d", 0); slices.Contains(args, "--phone-watch") {
		t.Fatalf("off: %q", args)
	}
	base.PhoneWatch = true
	if args := webGatewayArgs(base, "/w", "/a", "/d", 0); !slices.Contains(args, "--phone-watch") {
		t.Fatalf("on: %q", args)
	}
}

// Spec (MOB-31, T31): web_phone is file only and the AI can never set it.
func TestWebPhoneIsAINever(t *testing.T) {
	p, ok := aiSettingPolicies["web_phone"]
	if !ok || p.Gate != aiGateNever {
		t.Fatal("the AI may change web_phone")
	}
	found := false
	for _, d := range fileOnlySettings {
		if d.Key == "web_phone" {
			found = strings.Contains(d.Category, "file only")
		}
	}
	if !found {
		t.Fatal("web_phone is not documented as file only")
	}
}
