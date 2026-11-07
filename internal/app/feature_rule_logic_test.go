package app

import (
	"reflect"
	"testing"
)

// Spec (STAB-1): the feature rule. A feature (palette command, setting or
// key action) with no behavior test fails unless it sits on the reviewed
// baseline of old gaps. The baseline only shrinks: a baseline id that has
// a test now, or that no longer exists, must be removed from it.
func TestFeatureRuleProblems(t *testing.T) {
	features := []feature{
		{ID: "palette/new-cmd", Needles: []string{`"new-cmd"`}},
		{ID: "palette/tested", Needles: []string{`"tested"`}},
		{ID: "palette/old-gap", Needles: []string{`"old-gap"`}},
		{ID: "palette/old-gap-now-tested", Needles: []string{`"old-gap-now-tested"`}},
		{ID: "setting/setting-x", Needles: []string{`"setting-x"`, `"x_key"`}},
		{ID: "key/Quit", Needles: []string{"keymap.ActionQuit", `"Quit"`}},
	}
	tests := `m.dispatchPaletteAction("tested")
cfg := map[string]any{"x_key": true}
m.dispatchPaletteAction("old-gap-now-tested")
k := keymap.ActionQuit`
	baseline := []string{"palette/old-gap", "palette/old-gap-now-tested", "palette/gone"}

	newGaps, stale := featureRuleProblems(features, tests, baseline)
	if want := []string{"palette/new-cmd"}; !reflect.DeepEqual(newGaps, want) {
		t.Errorf("new gaps = %v, want %v", newGaps, want)
	}
	if want := []string{"palette/gone", "palette/old-gap-now-tested"}; !reflect.DeepEqual(stale, want) {
		t.Errorf("stale baseline = %v, want %v", stale, want)
	}
}

// Spec (STAB-1): untestedFeatures lists the ids with no needle in the test
// text, sorted, so the baseline file is stable.
func TestUntestedFeaturesSorted(t *testing.T) {
	features := []feature{
		{ID: "palette/b", Needles: []string{`"b"`}},
		{ID: "palette/a", Needles: []string{`"a"`}},
		{ID: "palette/c", Needles: []string{`"c"`}},
	}
	got := untestedFeatures(features, `x("c")`)
	if want := []string{"palette/a", "palette/b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("untested = %v, want %v", got, want)
	}
}
