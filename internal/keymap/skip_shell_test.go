package keymap

import "testing"

// Spec: Given no terminal_skip_shell setting, then only Quit skips the
// shell; "QuickOpen" adds Quick Open, "-Quit" removes Quit and unknown
// names are ignored.
func TestSkipShellSet(t *testing.T) {
	if s := SkipShellSet(nil); !s[ActionQuit] || len(s) != 1 {
		t.Fatalf("default = %v, want only Quit", s)
	}
	s := SkipShellSet([]string{"QuickOpen", " -Quit ", "NoSuchAction"})
	if !s[ActionQuickOpen] || s[ActionQuit] || len(s) != 1 {
		t.Fatalf("set = %v, want only QuickOpen", s)
	}
}
