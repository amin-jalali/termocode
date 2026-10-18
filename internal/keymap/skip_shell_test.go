package keymap

import "testing"

// Spec: Given no terminal_skip_shell setting, then Quit and ContextHelp
// (Shift+F1) skip the shell; "QuickOpen" adds Quick Open, "-Quit" and
// "-ContextHelp" remove them and unknown names are ignored.
func TestSkipShellSet(t *testing.T) {
	if s := SkipShellSet(nil); !s[ActionQuit] || !s[ActionContextHelp] || len(s) != 2 {
		t.Fatalf("default = %v, want Quit and ContextHelp", s)
	}
	s := SkipShellSet([]string{"QuickOpen", " -Quit ", "-ContextHelp", "NoSuchAction"})
	if !s[ActionQuickOpen] || s[ActionQuit] || s[ActionContextHelp] || len(s) != 1 {
		t.Fatalf("set = %v, want only QuickOpen", s)
	}
}
