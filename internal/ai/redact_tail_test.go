package ai

import "testing"

// Spec (WEB-13): SecretTailHold says how many bytes at the end of a text
// could be the start of a registered secret, so a stream holds them back
// until it knows; text that cannot start one holds nothing.
func TestSecretTailHold(t *testing.T) {
	sec := "zq" + "tailhold-secret-91"
	RegisterSecret(sec)
	t.Cleanup(func() { secretsMu.Lock(); delete(secrets, sec); secretsMu.Unlock() })
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"plain words.", 0},
		{"key is z", 1},
		{"key is zqtai", 5},
		{"key is " + sec[:len(sec)-1], len(sec) - 1},
		{"key is " + sec, 0}, // whole: redaction sees it
		{"key is " + sec + " done", 0},
		{sec[:3], 3},
	}
	for _, c := range cases {
		if got := SecretTailHold(c.in); got != c.want {
			t.Errorf("SecretTailHold(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
