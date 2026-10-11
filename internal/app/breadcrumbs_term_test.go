package app

import (
	"reflect"
	"testing"
)

func TestTerminalBreadcrumbs(t *testing.T) {
	cases := map[string][]string{
		"term://~/termocode//3747078:/bin/sh": {"Terminal", "sh"},
		"term:///tmp/x//12:/usr/bin/zsh -l":   {"Terminal", "zsh"},
		"term://~//99:":                       {"Terminal"},
	}
	for in, want := range cases {
		got, ok := terminalBreadcrumbs(in)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("terminalBreadcrumbs(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	if _, ok := terminalBreadcrumbs("/home/a/main.go"); ok {
		t.Error("plain path treated as terminal")
	}
}
