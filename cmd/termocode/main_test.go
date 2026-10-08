package main

import "testing"

func TestResolveFontDelta(t *testing.T) {
	three := 3
	zero := 0
	cases := []struct {
		name string
		env  string
		cfg  *int
		want int
	}{
		{"default", "", nil, -2},
		{"config", "", &three, 3},
		{"config zero", "", &zero, 0},
		{"env wins", "-4", &three, -4},
		{"bad env falls back to config", "abc", &three, 3},
	}
	for _, c := range cases {
		if got := resolveFontDelta(c.env, c.cfg); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
