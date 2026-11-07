package main

import (
	"reflect"
	"testing"

	"github.com/amin-jalali/termocode/internal/app"
)

// CMP-99: `termocode --lite [dir]` takes the flag out and sets
// TERMOCODE_LITE=1, so the session server it starts runs Lite too.
func TestLiteFlag(t *testing.T) {
	set := map[string]string{}
	setenv := func(k, v string) error { set[k] = v; return nil }
	if got := applyLiteFlag([]string{"--lite", "proj"}, setenv); !reflect.DeepEqual(got, []string{"proj"}) || set[app.EnvLite] != "1" {
		t.Errorf("args %q env %v", got, set)
	}
	set = map[string]string{}
	if got := applyLiteFlag([]string{"proj"}, setenv); !reflect.DeepEqual(got, []string{"proj"}) || len(set) != 0 {
		t.Errorf("no flag: args %q env %v", got, set)
	}
}
