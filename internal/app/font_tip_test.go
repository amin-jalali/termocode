package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/termfont"
)

// TestFontTipOnce: a font_delta the terminal could not apply gives one
// tip toast, not one per Init (re-attach) or per call.
func TestFontTipOnce(t *testing.T) {
	t.Cleanup(func() { SetFontResult(0, termfont.State{}) })

	SetFontResult(-2, termfont.State{})
	cmd := fontTipCmd()
	if cmd == nil {
		t.Fatal("unapplied font_delta: no tip")
	}
	msg, ok := cmd().(ToastMsg)
	if !ok || msg.Title != termfont.Tip || !strings.Contains(msg.Title, "zoom") {
		t.Errorf("tip toast: %#v", msg)
	}
	if fontTipCmd() != nil {
		t.Error("tip shown twice")
	}

	SetFontResult(0, termfont.State{})
	if fontTipCmd() != nil {
		t.Error("font_delta 0: no tip wanted")
	}
}

// TestFontDeltaSettingHelp: the settings row says it is off by default
// and which terminal can do it.
func TestFontDeltaSettingHelp(t *testing.T) {
	for _, r := range settingsRows {
		if r.ID != "setting-font-delta" {
			continue
		}
		if r.Default != "0" || !strings.Contains(r.Desc, "kitty") || strings.Contains(r.Desc, "OSC 1337") {
			t.Errorf("font_delta row: default %q, desc %q", r.Default, r.Desc)
		}
		return
	}
	t.Fatal("no font_delta row")
}
