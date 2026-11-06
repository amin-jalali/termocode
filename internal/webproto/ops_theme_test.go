package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (WEBP-7): the laptop's color theme from the web. The page lists
// the laptop's themes and the custom theme's colors (viewer); picking a
// theme and changing one custom color are the user's own preferences,
// changed at once (controller, audited), as the settings are.
func TestThemeOps(t *testing.T) {
	for op, w := range map[string]struct {
		role  Role
		audit Audit
		args  string
	}{
		OpThemeList:     {RoleViewer, AuditNo, `{}`},
		OpThemeSet:      {RoleController, AuditYes, `{"id":"one-dark"}`},
		OpThemeColors:   {RoleViewer, AuditNo, `{}`},
		OpThemeColorSet: {RoleController, AuditYes, `{"key":"BgEditor","color":"#1e1e2e"}`},
	} {
		p, ok := Lookup(op)
		if !ok || p.Role != w.role || p.Audit != w.audit || p.LaptopCard || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
		if _, err := DecodeArgs(op, json.RawMessage(w.args)); err != nil {
			t.Errorf("%s %s: %v", op, w.args, err)
		}
		if _, ok := ResultOf(op); !ok {
			t.Errorf("%s has no result type", op)
		}
	}
	for _, good := range []string{`{"key":"AIAccent","color":"#abc"}`, `{"key":"BgEditor","color":""}`} {
		if _, err := DecodeArgs(OpThemeColorSet, json.RawMessage(good)); err != nil {
			t.Errorf("%s: %v", good, err)
		}
	}
	for _, bad := range []string{`{"key":"","color":"#fff"}`, `{"key":"Bg Editor","color":"#fff"}`, `{"key":"BgEditor","color":"red"}`,
		`{"key":"BgEditor","color":"#12"}`, `{"key":"BgEditor","color":"#gggggg"}`, `{"key":"` + strings.Repeat("x", 80) + `","color":"#fff"}`} {
		if _, err := DecodeArgs(OpThemeColorSet, json.RawMessage(bad)); err == nil {
			t.Errorf("color %s: accepted", bad)
		}
	}
	for _, bad := range []string{`{"id":""}`, `{"id":"../x"}`, `{"id":"One Dark"}`} {
		if _, err := DecodeArgs(OpThemeSet, json.RawMessage(bad)); err == nil {
			t.Errorf("set %s: accepted", bad)
		}
	}
}
