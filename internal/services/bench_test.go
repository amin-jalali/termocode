package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// bigProject is a 20-service compose config and its ps output.
func bigProject() (cfg, ps []byte) {
	var c, p strings.Builder
	c.WriteString(`{"name":"big","services":{`)
	for i := 0; i < 20; i++ {
		if i > 0 {
			c.WriteString(",")
		}
		fmt.Fprintf(&c, `"svc%d":{"image":"img:%d","ports":[{"target":%d,"published":"%d","protocol":"tcp"}],"environment":{"A_PASSWORD":"longsecretvalue%d","PORT":"%d"},"depends_on":{"svc0":{}}}`, i, i, 8000+i, 8000+i, i, 8000+i)
		fmt.Fprintf(&p, `{"Service":"svc%d","Name":"big-svc%d-1","Image":"img:%d","State":"running","Health":"","ExitCode":0,"Status":"Up 2 hours","Publishers":[{"URL":"0.0.0.0","TargetPort":%d,"PublishedPort":%d,"Protocol":"tcp"}]}`+"\n", i, i, i, 8000+i, 8000+i)
	}
	c.WriteString(`}}`)
	return []byte(c.String()), []byte(p.String())
}

func BenchmarkParse20(b *testing.B) {
	cfg, ps := bigProject()
	now := time.Now()
	for i := 0; i < b.N; i++ {
		if _, err := ParseConfig(cfg); err != nil {
			b.Fatal(err)
		}
		if _, err := ParsePS(ps, now); err != nil {
			b.Fatal(err)
		}
	}
}

// Spec (SV-13): parsing a 20-service refresh stays under 1 ms
// (BenchmarkParse20 gives the real number, ~0.2 ms on a dev laptop).
//
// A wall-clock budget fails on a loaded CI runner, so the test compares
// with a baseline measured in the same run on the same bytes: a plain
// json.Unmarshal into any, the work the parsers cannot avoid. Load and
// the race detector slow both sides alike; an O(n²) loop, a regexp built
// per call or a process start would still blow the ratio.
func TestParse20Budget(t *testing.T) {
	cfg, ps := bigProject()
	lines := bytes.Split(bytes.TrimSpace(ps), []byte("\n"))
	now := time.Now()
	parse := func() {
		_, _ = ParseConfig(cfg)
		_, _ = ParsePS(ps, now)
	}
	baseline := func() {
		var v any
		_ = json.Unmarshal(cfg, &v)
		for _, l := range lines {
			_ = json.Unmarshal(l, &v)
		}
	}
	// Short interleaved rounds; the fastest round of each is the one the
	// scheduler disturbed least.
	perOp := func(f func()) time.Duration {
		const n = 10
		start := time.Now()
		for i := 0; i < n; i++ {
			f()
		}
		return time.Since(start) / n
	}
	bestParse, bestBase := time.Duration(1<<62), time.Duration(1<<62)
	for round := 0; round < 30; round++ {
		bestBase = min(bestBase, perOp(baseline))
		bestParse = min(bestParse, perOp(parse))
	}
	// Measured: ~2x without and ~3.2x with the race detector (it slows the
	// typed decode more than the generic one).
	maxRatio := 4.0
	if raceOn {
		maxRatio = 6
	}
	ratio := float64(bestParse) / float64(bestBase)
	t.Logf("parse %v, plain json %v, ratio %.2f", bestParse, bestBase, ratio)
	if ratio > maxRatio {
		t.Errorf("ParseConfig + ParsePS of 20 services: %v per refresh, %.1fx a plain json decode of the same bytes (%v); budget %.0fx",
			bestParse, ratio, bestBase, maxRatio)
	}
}
