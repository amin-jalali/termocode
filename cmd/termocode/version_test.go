package main

import (
	"runtime/debug"
	"testing"
)

func TestPickVersion(t *testing.T) {
	bi := func(v string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: v}}, true
		}
	}
	none := func() (*debug.BuildInfo, bool) { return nil, false }
	cases := []struct {
		linked string
		info   func() (*debug.BuildInfo, bool)
		want   string
	}{
		{"v1.2.3", bi("v0.0.1"), "v1.2.3"},
		{"", bi("v0.4.0"), "v0.4.0"},
		{"", bi("(devel)"), "dev"},
		{"", bi(""), "dev"},
		{"", none, "dev"},
	}
	for _, c := range cases {
		if got := pickVersion(c.linked, c.info); got != c.want {
			t.Errorf("pickVersion(%q) = %q, want %q", c.linked, got, c.want)
		}
	}
}

func TestIsVersionArg(t *testing.T) {
	for _, a := range []string{"--version", "-v", "version"} {
		if !isVersionArg(a) {
			t.Errorf("%q should ask for the version", a)
		}
	}
	for _, a := range []string{"-h", "setup", "--versions", ""} {
		if isVersionArg(a) {
			t.Errorf("%q should not ask for the version", a)
		}
	}
}
