package shellint

import "testing"

// Spec (T1): one table of the sequences the scripts (and other shells'
// integrations, like kitty's and VS Code's) send, as nvim's TermRequest
// hands them over: with or without the ESC ] prefix and the BEL / ST end.
func TestParseTable(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want Event
	}{
		{"\x1b]133;A", true, Event{Kind: PromptStart}},
		{"\x1b]133;A\a", true, Event{Kind: PromptStart}},
		{"133;A;aid=42", true, Event{Kind: PromptStart}},
		{"\x1b]133;B\x1b\\", true, Event{Kind: InputStart}},
		{"\x1b]133;C", true, Event{Kind: CommandStart}},
		{"\x1b]133;C;cmdline_url=false", true, Event{Kind: CommandStart, Cmd: "false"}},
		{"\x1b]133;C;cmdline_url=cd /tmp%3B echo \"a%3Bb\"", true, Event{Kind: CommandStart, Cmd: `cd /tmp; echo "a;b"`}},
		{"\x1b]133;C;cmdline_url=echo 100%25%0Aecho x", true, Event{Kind: CommandStart, Cmd: "echo 100%\necho x"}},
		{"\x1b]133;C;cmdline=ls -la", true, Event{Kind: CommandStart, Cmd: "ls -la"}},
		{"\x1b]133;C;cmdline_url=bad%zzescape%", true, Event{Kind: CommandStart, Cmd: "bad%zzescape%"}},
		{"\x1b]133;D;1", true, Event{Kind: CommandEnd, Exit: 1, HasExit: true}},
		{"\x1b]133;D;0\a", true, Event{Kind: CommandEnd, Exit: 0, HasExit: true}},
		{"133;D;130;aid=7", true, Event{Kind: CommandEnd, Exit: 130, HasExit: true}},
		{"\x1b]133;D", true, Event{Kind: CommandEnd}},
		{"\x1b]133;D;x", true, Event{Kind: CommandEnd}},
		{"\x1b]7;file://host/home/u/my%20dir", true, Event{Kind: Cwd, Host: "host", Cwd: "/home/u/my dir"}},
		{"\x1b]7;file:///tmp\a", true, Event{Kind: Cwd, Cwd: "/tmp"}},
		{"\x1b]7;file://h/a%3Bb", true, Event{Kind: Cwd, Host: "h", Cwd: "/a;b"}},
		{"\x1b]7;kitty-shell-cwd://h/x", true, Event{Kind: Cwd, Host: "h", Cwd: "/x"}},
		// Not ours, or broken: ignored.
		{"\x1b]7;", false, Event{}},
		{"\x1b]7;file://host", false, Event{}},
		{"\x1b]7;http://host/x", false, Event{}},
		{"\x1b]133;Z", false, Event{}},
		{"\x1b]133;", false, Event{}},
		{"\x1b]0;window title", false, Event{}},
		{"\x1b]52;c;aGVsbG8=", false, Event{}},
		{"\x1bP+q544e\x1b\\", false, Event{}},
		{"", false, Event{}},
	}
	for _, c := range cases {
		got, ok := Parse(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// The parser never panics on odd input (cheap fuzz over prefixes).
func TestParseNoPanic(t *testing.T) {
	s := "\x1b]133;C;cmdline_url=a%2;b%\x1b]7;file://h/%%"
	for i := 0; i <= len(s); i++ {
		Parse(s[:i])
		Parse(s[i:])
	}
}
