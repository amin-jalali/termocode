// Package score grades the output of agent and AI steps (CMP-23).
//
// A score step of a workflow lists checks. Each check gives a value
// from 0 to 1: a test (true = 1, false = 0) or a value (a number, kept
// in 0..1). The step's score is the weighted mean of its checks, and it
// passes when the score reaches the pass mark. A check that cannot be
// read (a bad expression, a missing field) counts 0 and keeps its error,
// so one broken check never hides the others.
//
// Runs keep their results, so the scores of many runs can be compared:
// the same workflow with another model, prompt or profile (its vars).
// This package is pure: no I/O, no clock, no AI.
package score

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// DefaultPass is the pass mark when a score step does not set one.
const DefaultPass = 0.7

// Check is the result of one check.
type Check struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
	Value  float64 `json:"value"` // 0..1
	Error  string  `json:"error,omitempty"`
}

// Result is the result of one score step.
type Result struct {
	Score  float64 `json:"score"` // 0..1, the weighted mean
	Pass   float64 `json:"pass"`  // the pass mark
	Passed bool    `json:"passed"`
	Checks []Check `json:"checks"`
}

// ErrNotScore is the error of a value that is no bool and no number.
var ErrNotScore = errors.New("is not true/false or a number")

// Value turns the value of a check expression into 0..1: true is 1,
// false and null are 0, a number is kept in 0..1 (NaN is 0). Anything
// else is an error.
func Value(v any) (float64, error) {
	switch x := v.(type) {
	case nil:
		return 0, nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case float64:
		return clamp(x), nil
	case int:
		return clamp(float64(x)), nil
	}
	return 0, fmt.Errorf("%T %w", v, ErrNotScore)
}

func clamp(f float64) float64 {
	switch {
	case math.IsNaN(f) || f <= 0:
		return 0
	case f >= 1:
		return 1
	}
	return f
}

// Grade makes the Result of checks: the weighted mean of their values
// (a weight of 0 or less counts 1). No checks give the score 0.
func Grade(checks []Check, pass float64) Result {
	if math.IsNaN(pass) || pass < 0 || pass > 1 {
		pass = DefaultPass
	}
	out := Result{Pass: pass, Checks: make([]Check, len(checks))}
	var sum, weights float64
	for i, c := range checks {
		if c.Weight <= 0 || math.IsNaN(c.Weight) || math.IsInf(c.Weight, 0) {
			c.Weight = 1
		}
		c.Value = clamp(c.Value)
		out.Checks[i] = c
		sum += c.Value * c.Weight
		weights += c.Weight
	}
	if weights > 0 {
		out.Score = round(sum / weights)
	}
	out.Passed = len(checks) > 0 && out.Score >= pass
	return out
}

// round keeps 4 decimals, so a run file and the screen agree.
func round(f float64) float64 { return math.Round(f*1e4) / 1e4 }

// Failed lists the names of the checks below 1, the worst first.
func (r Result) Failed() []string {
	list := make([]Check, 0, len(r.Checks))
	for _, c := range r.Checks {
		if c.Value < 1 {
			list = append(list, c)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Value < list[j].Value })
	names := make([]string, len(list))
	for i, c := range list {
		names[i] = c.Name
	}
	return names
}

// Summary is the scores of one run (all its score steps and items).
type Summary struct {
	Count  int     `json:"count"`  // results
	Passed int     `json:"passed"` // results that passed
	Mean   float64 `json:"mean"`   // mean score (0 when Count is 0)
}

// Summarize sums up results.
func Summarize(results []Result) Summary {
	s := Summary{Count: len(results)}
	var sum float64
	for _, r := range results {
		sum += r.Score
		if r.Passed {
			s.Passed++
		}
	}
	if s.Count > 0 {
		s.Mean = round(sum / float64(s.Count))
	}
	return s
}

// Group is the summary of every run with the same variant.
type Group struct {
	Variant string  `json:"variant"` // "k=v k=v" of the run's vars ("" = none)
	Runs    int     `json:"runs"`
	Count   int     `json:"count"`
	Passed  int     `json:"passed"`
	Mean    float64 `json:"mean"` // mean of the run means
}

// Variant is the text of a run's vars, sorted: "model=fast prompt=b".
func Variant(vars map[string]any) string {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + fmt.Sprint(vars[k])
	}
	return strings.Join(parts, " ")
}

// Compare groups run summaries by variant (runs with no result are
// left out), the best mean first; equal means sort by the variant.
func Compare(variants []string, sums []Summary) []Group {
	idx := map[string]int{}
	var out []Group
	for i, s := range sums {
		if i >= len(variants) || s.Count == 0 {
			continue
		}
		v := variants[i]
		j, ok := idx[v]
		if !ok {
			j = len(out)
			idx[v] = j
			out = append(out, Group{Variant: v})
		}
		g := &out[j]
		g.Mean = (g.Mean*float64(g.Runs) + s.Mean) / float64(g.Runs+1)
		g.Runs++
		g.Count += s.Count
		g.Passed += s.Passed
	}
	for i := range out {
		out[i].Mean = round(out[i].Mean)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Mean != out[j].Mean {
			return out[i].Mean > out[j].Mean
		}
		return out[i].Variant < out[j].Variant
	})
	return out
}
