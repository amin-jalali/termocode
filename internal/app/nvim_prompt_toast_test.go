package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/toast"
)

// Spec (WEBP-10): when nvim's last message is its raw write line
// (`"/repo/main.go" 9L, 81B written`), the toast is a short clean
// "Saved main.go" note (info, no full path, no line/byte counts);
// any other message stays a warn toast as written.
func TestNvimPromptToastCleansWriteLine(t *testing.T) {
	cases := []struct {
		in    string
		sev   toast.Severity
		title string
	}{
		{`"/home/u/repo/main.go" 9L, 81B written`, toast.Info, "Saved main.go"},
		{`"scripts/notes.txt" [New] 12L, 340B written`, toast.Info, "Saved notes.txt"},
		{`"/tmp/a b.txt" [noeol] 1L, 3B written`, toast.Info, "Saved a b.txt"},
		{`"/repo/x.go" 3 lines, 20 bytes written`, toast.Info, "Saved x.go"},
		{`E433: No tags file`, toast.Warn, "E433: No tags file"},
		{`written`, toast.Warn, "written"},
	}
	for _, c := range cases {
		sev, title := nvimPromptToast(c.in)
		if sev != c.sev || title != c.title {
			t.Errorf("nvimPromptToast(%q) = %v %q, want %v %q", c.in, sev, title, c.sev, c.title)
		}
	}
}
