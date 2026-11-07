package workflow

import (
	"errors"
	"strings"
	"testing"
)

// Spec (CMP-23): a score step lists checks (name + test or value, maybe
// max and weight), a pass mark (0.7 by default) and must_pass.
func TestParseScoreStep(t *testing.T) {
	f, warns, err := Parse("s.jsonc", []byte(`{"version":1,"name":"s","steps":[
	  {"id":"t","type":"tests"},
	  {"id":"grade","type":"score","pass":0.6,"must_pass":true,"checks":[
	    {"name":"tests","test":"steps.t.ok","weight":3},
	    {"name":"judge","value":"steps.j.json.rating","max":10}
	  ]},
	  {"id":"plain","type":"score","checks":[{"name":"a","test":"true"}]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings %+v", warns)
	}
	sp := f.Steps[1].Score
	if sp == nil || sp.Pass != 0.6 || !sp.MustPass || len(sp.Checks) != 2 {
		t.Fatalf("score %+v", sp)
	}
	if c := sp.Checks[0]; c.Name != "tests" || c.Test != "steps.t.ok" || c.Value != "" || c.Weight != 3 || c.Max != 1 || c.Line != 4 {
		t.Errorf("check 0 %+v", c)
	}
	if c := sp.Checks[1]; c.Name != "judge" || c.Value != "steps.j.json.rating" || c.Max != 10 || c.Weight != 1 {
		t.Errorf("check 1 %+v", c)
	}
	if p := f.Steps[2].Score; p == nil || p.Pass != 0.7 || p.MustPass {
		t.Errorf("defaults %+v", p)
	}
}

func TestParseScoreStepBad(t *testing.T) {
	step := func(body string) string {
		return `{"version":1,"name":"bad","steps":[{"type":"score"` + body + `}]}`
	}
	cases := []struct{ name, src, want string }{
		{"no checks", step(``), `"checks" is missing`},
		{"empty checks", step(`,"checks":[]`), "checks: must be a list with at least one check"},
		{"check not object", step(`,"checks":[1]`), "checks[0]: must be an object"},
		{"both", step(`,"checks":[{"name":"a","test":"x","value":"y"}]`), `has both "test" and "value"`},
		{"neither", step(`,"checks":[{"name":"a"}]`), `needs "test" (true or false) or "value"`},
		{"no name", step(`,"checks":[{"test":"x"}]`), `"name" is missing`},
		{"dup name", step(`,"checks":[{"name":"a","test":"x"},{"name":"a","test":"y"}]`), `"a" is already the name of a check`},
		{"pass range", step(`,"pass":2,"checks":[{"name":"a","test":"x"}]`), "pass: must be from 0 to 1"},
		{"weight range", step(`,"checks":[{"name":"a","test":"x","weight":0}]`), "weight: must be from 0.01 to 100"},
		{"max on test", step(`,"checks":[{"name":"a","test":"x","max":10}]`), `"max" is only for a "value" check`},
		{"too many", step(`,"checks":[` + strings.TrimSuffix(strings.Repeat(`{"name":"a","test":"x"},`, 21), ",") + `]`), "has 21 checks; the most is 20"},
	}
	for _, c := range cases {
		_, _, err := Parse("bad.jsonc", []byte(c.src))
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("%s: err %v, want a *ParseError", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s:\n got  %s\n want %s", c.name, err, c.want)
		}
	}
}
