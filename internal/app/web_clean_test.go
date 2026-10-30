package app

import "testing"

// Spec (WEB-7, T50): webClean keeps tabs and newlines and drops every
// other C0/C1 control and DEL (an escape sequence cannot reach the page);
// webName also drops the bidi embeddings, overrides and isolates, so a
// file or chat name cannot reorder the UI around it. File bodies keep
// bidi controls (the page isolates each line).
func TestWebCleanTable(t *testing.T) {
	for _, tc := range []struct{ in, clean, name string }{
		{"plain", "plain", "plain"},
		{"a\tb\nc", "a\tb\nc", "a b c"},
		{"red \x1b[31mtext\x1b[0m", "red [31mtext[0m", "red [31mtext[0m"},
		{"bell\x07 nul\x00 cr\r del\x7f", "bell nul cr del", "bell nul cr del"},
		{"c1 \u0085\u009b end", "c1  end", "c1  end"},
		{"evil‮gnp.exe", "evil‮gnp.exe", "evilgnp.exe"},
		{"iso ⁦x⁩ ‪y‬", "iso ⁦x⁩ ‪y‬", "iso x y"},
		{"فارسی name", "فارسی name", "فارسی name"},
		{"marks ‏‎ stay", "marks ‏‎ stay", "marks ‏‎ stay"},
		{"bad \xff utf8", "bad � utf8", "bad � utf8"},
	} {
		if got := webClean(tc.in); got != tc.clean {
			t.Errorf("webClean(%q) = %q, want %q", tc.in, got, tc.clean)
		}
		if got := webName(tc.in); got != tc.name {
			t.Errorf("webName(%q) = %q, want %q", tc.in, got, tc.name)
		}
	}
}
