package main

import "testing"

// Spec (Group S2): frames are paced over SSH only; a local terminal is
// never asked for its status, so it can never get a stray answer.
func TestFramePacing(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want bool
	}{
		{map[string]string{}, false},
		{map[string]string{"SSH_CONNECTION": "10.0.0.1 5 10.0.0.2 22"}, true},
		{map[string]string{"SSH_TTY": "/dev/pts/3"}, true},
		{map[string]string{"SSH_TTY": "/dev/pts/3", "TERMOCODE_FRAME_PACING": "off"}, false},
		{map[string]string{"TERMOCODE_FRAME_PACING": "on"}, true},
	}
	for _, c := range cases {
		if got := framePacing(func(k string) string { return c.env[k] }); got != c.want {
			t.Errorf("%v: got %v, want %v", c.env, got, c.want)
		}
	}
}
