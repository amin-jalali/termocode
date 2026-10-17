package main

import "testing"

// Terminal R10: synchronized output only when the terminal said it knows
// mode 2026; TERMOCODE_SYNC=on/off overrides the answer.
func TestSyncOutputWanted(t *testing.T) {
	cases := []struct {
		env      string
		answered bool
		want     bool
	}{
		{"", true, true},
		{"", false, false},
		{"auto", true, true},
		{"off", true, false},
		{"0", true, false},
		{"on", false, true},
		{"1", false, true},
	}
	for _, c := range cases {
		getenv := func(k string) string {
			if k == "TERMOCODE_SYNC" {
				return c.env
			}
			return ""
		}
		if got := syncOutputWanted(c.answered, getenv); got != c.want {
			t.Errorf("TERMOCODE_SYNC=%q answered=%v: %v, want %v", c.env, c.answered, got, c.want)
		}
	}
}
