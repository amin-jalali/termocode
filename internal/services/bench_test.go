package services

import (
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

// Spec (SV-13): parsing a 20-service refresh stays under 1 ms.
func TestParse20Budget(t *testing.T) {
	cfg, ps := bigProject()
	r := testing.Benchmark(func(b *testing.B) {
		now := time.Now()
		for i := 0; i < b.N; i++ {
			_, _ = ParseConfig(cfg)
			_, _ = ParsePS(ps, now)
		}
	})
	budget := time.Millisecond
	if raceOn { // the race detector makes this code ~10x slower
		budget *= 20
	}
	if per := time.Duration(r.NsPerOp()); per > budget {
		t.Errorf("ParseConfig + ParsePS of 20 services: %v per refresh (budget 1 ms)", per)
	}
}
