package git

import "testing"

// GITX-29 (O24, P11): #123, issue keys and commit hashes in a line of
// text are found by offset, and turned into web links.
func TestRefAt(t *testing.T) {
	cases := []struct {
		line string
		off  int
		kind RefKind
		text string
	}{
		{"fixes #123 now", 7, RefIssue, "123"},
		{"fixes #123 now", 6, RefIssue, "123"},
		{"(#7)", 2, RefIssue, "7"},
		{"see ABC-42.", 5, RefKey, "ABC-42"},
		{"commit 1a2b3c4d done", 9, RefSHA, "1a2b3c4d"},
		{"e45932c3 Merge pull request #192", 0, RefSHA, "e45932c3"},
		{"e45932c3 Merge pull request #192", 29, RefIssue, "192"},
	}
	for _, c := range cases {
		r, ok := RefAt(c.line, c.off)
		if !ok || r.Kind != c.kind || r.Text != c.text {
			t.Errorf("RefAt(%q, %d) = %+v %v; want %v %q", c.line, c.off, r, ok, c.kind, c.text)
		}
		if ok && c.line[r.Start:r.End] != map[RefKind]string{RefIssue: "#", RefKey: "", RefSHA: ""}[c.kind]+c.text {
			t.Errorf("RefAt(%q): span %d..%d", c.line, r.Start, r.End)
		}
	}
	none := []struct {
		line string
		off  int
	}{
		{"fixes #123 now", 2},   // not on it
		{"a#123", 2},            // inside a word
		{"&#123;", 2},           // HTML entity
		{"#12ab", 1},            // not a number
		{"x/#5", 3},             // URL fragment
		{"UTF-8 text", 1},       // common names, not keys
		{"SHA-256 sum", 1},      // common names, not keys
		{"CVE-2024-1234", 1},    // more after the number
		{"abcdefab", 2},         // a word, no digit
		{"1234567 bytes", 2},    // a number, no letter
		{"1a2b3c", 1},           // too short
		{"x1a2b3c4d", 3},        // inside a word
		{"deadbeef-1a2b3c4", 9}, // part of a UUID-like run
	}
	for _, c := range none {
		if r, ok := RefAt(c.line, c.off); ok {
			t.Errorf("RefAt(%q, %d) = %+v; want none", c.line, c.off, r)
		}
	}
}

func TestRefLinks(t *testing.T) {
	if got := IssueLink("https://github.com/o/r", "12"); got != "https://github.com/o/r/issues/12" {
		t.Errorf("github issue %q", got)
	}
	if got := IssueLink("https://gitlab.com/g/p", "12"); got != "https://gitlab.com/g/p/-/issues/12" {
		t.Errorf("gitlab issue %q", got)
	}
	if got, ok := KeyLink("https://jira.example.com/browse/{key}", "ABC-1"); !ok || got != "https://jira.example.com/browse/ABC-1" {
		t.Errorf("key link %q %v", got, ok)
	}
	if got, ok := KeyLink("https://t.example.com/browse/", "ABC-1"); !ok || got != "https://t.example.com/browse/ABC-1" {
		t.Errorf("key link without {key} %q %v", got, ok)
	}
	for _, bad := range []string{"", "  ", "javascript:alert(1)", "file:///etc/{key}", "ftp://x/{key}"} {
		if got, ok := KeyLink(bad, "ABC-1"); ok {
			t.Errorf("KeyLink(%q) = %q; want none", bad, got)
		}
	}
}
