package score

import (
	"errors"
	"math"
	"slices"
	"testing"
)

// Spec (CMP-23): a check value is true = 1, false/null = 0, a number
// kept in 0..1; anything else is an error.
func TestValue(t *testing.T) {
	cases := []struct {
		in   any
		want float64
		err  bool
	}{
		{true, 1, false},
		{false, 0, false},
		{nil, 0, false},
		{0.25, 0.25, false},
		{7.0, 1, false},
		{-3.0, 0, false},
		{math.NaN(), 0, false},
		{2, 1, false},
		{"good", 0, true},
		{[]any{1.0}, 0, true},
	}
	for _, tc := range cases {
		got, err := Value(tc.in)
		if got != tc.want || (err != nil) != tc.err {
			t.Errorf("Value(%v) = %v, %v; want %v, err %v", tc.in, got, err, tc.want, tc.err)
		}
		if err != nil && !errors.Is(err, ErrNotScore) {
			t.Errorf("Value(%v) error %v is not ErrNotScore", tc.in, err)
		}
	}
}

// Spec: the score is the weighted mean; weight 0 counts 1; values are
// kept in 0..1; it passes at the pass mark; no checks never pass.
func TestGrade(t *testing.T) {
	r := Grade([]Check{{Name: "tests", Weight: 3, Value: 1}, {Name: "judge", Value: 0.5}, {Name: "size", Weight: 0, Value: 0}}, 0.6)
	// (3*1 + 1*0.5 + 1*0) / 5 = 0.7
	if r.Score != 0.7 || r.Pass != 0.6 || !r.Passed {
		t.Fatalf("grade %+v", r)
	}
	if r.Checks[1].Weight != 1 || r.Checks[2].Weight != 1 {
		t.Errorf("default weights %+v", r.Checks)
	}
	if got := r.Failed(); !slices.Equal(got, []string{"size", "judge"}) {
		t.Errorf("failed %q", got)
	}
	if r := Grade([]Check{{Name: "a", Value: 0.69}}, 0.7); r.Passed {
		t.Errorf("0.69 passed at 0.7: %+v", r)
	}
	if r := Grade([]Check{{Name: "a", Value: 5}}, -1); r.Score != 1 || r.Pass != DefaultPass || !r.Passed {
		t.Errorf("clamp and default pass %+v", r)
	}
	if r := Grade(nil, 0); r.Passed || r.Score != 0 {
		t.Errorf("no checks %+v", r)
	}
	if r := Grade([]Check{{Name: "a", Value: 1}, {Name: "b", Value: 0}, {Name: "c", Value: 0}}, 0.5); r.Score != 0.3333 {
		t.Errorf("rounding %v", r.Score)
	}
}

// Spec: runs are summed up and grouped by their vars, the best first.
func TestSummarizeCompare(t *testing.T) {
	s := Summarize([]Result{{Score: 1, Passed: true}, {Score: 0.5}})
	if s != (Summary{Count: 2, Passed: 1, Mean: 0.75}) {
		t.Fatalf("summary %+v", s)
	}
	if Summarize(nil) != (Summary{}) {
		t.Fatal("empty summary")
	}
	if v := Variant(map[string]any{"prompt": "b", "model": "fast", "n": 2.0}); v != "model=fast n=2 prompt=b" {
		t.Errorf("variant %q", v)
	}
	if Variant(nil) != "" {
		t.Error("no vars")
	}
	groups := Compare(
		[]string{"model=fast", "model=deep", "model=fast", "model=none"},
		[]Summary{{Count: 1, Passed: 0, Mean: 0.4}, {Count: 2, Passed: 2, Mean: 0.9}, {Count: 1, Passed: 1, Mean: 0.8}, {}},
	)
	want := []Group{
		{Variant: "model=deep", Runs: 1, Count: 2, Passed: 2, Mean: 0.9},
		{Variant: "model=fast", Runs: 2, Count: 2, Passed: 1, Mean: 0.6},
	}
	if !slices.Equal(groups, want) {
		t.Errorf("compare %+v\nwant %+v", groups, want)
	}
}
