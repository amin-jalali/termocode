//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// arabicScript reports whether s has Arabic-script letters (base or the
// shaped presentation forms termocode draws).
func arabicScript(s string) bool {
	for _, r := range s {
		if (r >= 0x0600 && r <= 0x06FF) || (r >= 0xFB50 && r <= 0xFDFF) || (r >= 0xFE70 && r <= 0xFEFF) {
			return true
		}
	}
	return false
}

// Spec (Group S3): the look depends on the user's own terminal, which the
// server only knows from the environment ssh passes on. With an unset
// locale (common on servers), no COLORTERM and no known Nerd Font:
// Persian text still shows as letters (no <d8> byte escapes, no U+FFFD),
// icons are Unicode (no Nerd private-use glyphs that would be boxes), and
// TERMOCODE_COLORS=256 (or GNU screen) means no 24-bit colour sequences.
func TestSSHClientLook(t *testing.T) {
	cases := []struct {
		name    string
		env     []string
		no24bit bool
	}{
		{"locale unset", []string{"LANG=", "LC_ALL=", "LC_CTYPE=", "COLORTERM="}, false},
		{"256 colours", []string{"LANG=", "LC_ALL=", "COLORTERM=", "TERMOCODE_COLORS=256"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newSessionRig(t)
			r.env = append(r.env, c.env...)
			if err := os.WriteFile(filepath.Join(r.ws, "fa.txt"), []byte("سلام دنیا — Persian line\nsecond line\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			tm := r.start()
			off := tm.rawLen()
			tm.OpenFile("fa.txt")
			tm.WaitText("Persian line", uiTimeout)
			tm.step("opened fa.txt")
			scr := tm.Screen()
			var row string
			for _, l := range tm.Lines() {
				if strings.Contains(l, "Persian line") {
					row = l
				}
			}
			if !arabicScript(row) {
				t.Errorf("Persian not drawn as letters: %q", row)
			}
			if strings.Contains(scr, "<d8>") || strings.Contains(scr, "<d9>") {
				t.Errorf("Persian shown as byte escapes:\n%s", scr)
			}
			for _, r := range scr {
				if r >= 0xE000 && r <= 0xF8FF {
					t.Errorf("Nerd Font glyph %U on screen with no Nerd Font known", r)
					break
				}
			}
			if c.no24bit {
				raw := string(tm.rawSince(off))
				if strings.Contains(raw, "38;2;") || strings.Contains(raw, "48;2;") {
					t.Errorf("24-bit colour sent with TERMOCODE_COLORS=256")
				}
			}
			tm.Quit()
		})
	}
}

// Spec (Group S4): termocode inside tmux or GNU screen on the server: no
// persistent session (the multiplexer keeps it alive), no host escapes
// that would stick to the pane (scrollback erase, background paint),
// the multiplexer's key encoding works, and under screen no 24-bit colour.
func TestInsideMultiplexer(t *testing.T) {
	cases := []struct {
		name    string
		env     []string
		no24bit bool
	}{
		{"tmux", []string{"TMUX=/tmp/tmux-1000/default,1,0", "TERM=tmux-256color", "VTE_VERSION=7600"}, false},
		{"screen", []string{"STY=1234.pts-0.host", "TERM=screen-256color", "COLORTERM=", "VTE_VERSION=7600"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newSessionRig(t)
			r.env = append(r.env, c.env...)
			tm := r.start()
			if _, err := os.Stat(r.socket()); err == nil {
				t.Errorf("a persistent session started inside %s", c.name)
			}
			tm.OpenFile("main.go")
			tm.WaitText("func main()", uiTimeout)
			tm.Send(KeyEsc)
			tm.settle()
			tm.Send("\x1b[4~") // End, as tmux and screen send it
			tm.WaitFor("cursor at the line end", uiTimeout, func(ls []string) bool {
				return strings.Contains(ls[len(ls)-1], "Col 12") // last char of "package main"
			})
			raw := string(tm.rawSince(0))
			if strings.Contains(raw, "\x1b[3J") || strings.Contains(raw, "\x1b]11;#") {
				t.Errorf("host escapes sent inside %s", c.name)
			}
			if c.no24bit && (strings.Contains(raw, "38;2;") || strings.Contains(raw, "48;2;")) {
				t.Errorf("24-bit colour sent inside %s", c.name)
			}
			tm.Quit()
		})
	}
}
