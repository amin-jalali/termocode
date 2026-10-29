package session

import (
	"strings"
	"testing"
	"time"
)

func TestReconnectDelay(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second}
	for i, w := range want {
		if got := reconnectDelay(i + 1); got != w {
			t.Errorf("attempt %d: %v, want %v", i+1, got, w)
		}
	}
}

func TestOfflineLine(t *testing.T) {
	got := offlineLine(3, 80, 24, true)
	for _, want := range []string{"\x1b7", "\x1b[24;1H", "Reconnecting… (attempt 3)", "typing is paused", "Ctrl+C to quit", "\x1b8"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if got := offlineLine(1, 80, 24, false); !strings.Contains(got, "Reconnecting... (attempt 1) - typing") {
		t.Errorf("ascii line: %q", got)
	}
	// Never wider than the terminal.
	if got := offlineLine(1, 20, 5, true); !strings.Contains(got, "\x1b[0;7m Reconnecting… (atte\x1b[0m") {
		t.Errorf("narrow line: %q", got)
	}
}

func TestOfflineQuit(t *testing.T) {
	for in, want := range map[string]bool{"\x03": true, "ab\x03": true, "\x1b[99;5u": true, "abc": false, "\x1b[A": false} {
		if got := offlineQuit([]byte(in)); got != want {
			t.Errorf("%q: %v, want %v", in, got, want)
		}
	}
}

func TestReconnectNote(t *testing.T) {
	cases := []struct {
		d       time.Duration
		dropped bool
		want    string
	}{
		{12 * time.Second, false, "Reconnected after 12 s"},
		{300 * time.Millisecond, false, "Reconnected"},
		{5 * time.Minute, true, "Reconnected after 5 min · keys typed while offline were dropped"},
	}
	for _, c := range cases {
		if got := reconnectNote(c.d, c.dropped); got != c.want {
			t.Errorf("%v %v: %q, want %q", c.d, c.dropped, got, c.want)
		}
	}
}
