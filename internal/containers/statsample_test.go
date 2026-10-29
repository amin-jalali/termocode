package containers

import (
	"math"
	"testing"
)

func TestParseBytesUnits(t *testing.T) {
	cases := []struct {
		in string
		v  float64
		ok bool
	}{
		{"0B", 0, true},
		{"1.2kB", 1200, true},
		{"8.19kB", 8190, true},
		{"48.2MiB", 48.2 * 1048576, true},
		{"7.7GiB", 7.7 * (1 << 30), true},
		{"3MB", 3e6, true},
		{"2 GiB", 2 * (1 << 30), true},
		{"1.5TiB", 1.5 * (1 << 40), true},
		{"512", 512, true},
		{"--", 0, false},
		{"", 0, false},
		{"abc", 0, false},
		{"5 parsecs", 0, false},
	}
	for _, c := range cases {
		v, ok := ParseBytes(c.in)
		if ok != c.ok || math.Abs(v-c.v) > 1 {
			t.Errorf("ParseBytes(%q) = %v, %v; want %v, %v", c.in, v, ok, c.v, c.ok)
		}
	}
}

func TestParsePercentAndPair(t *testing.T) {
	if v, ok := ParsePercent("12.50%"); !ok || v != 12.5 {
		t.Errorf("percent = %v %v", v, ok)
	}
	for _, bad := range []string{"--", "", "x%", "-3%"} {
		if _, ok := ParsePercent(bad); ok {
			t.Errorf("ParsePercent(%q) ok", bad)
		}
	}
	a, b, ok := ParsePair("48.2MiB / 7.7GiB")
	if !ok || a < 5e7 || b < 8e9 {
		t.Errorf("pair = %v %v %v", a, b, ok)
	}
	if _, _, ok := ParsePair("-- / --"); ok {
		t.Error("-- pair ok")
	}
	if _, _, ok := ParsePair("12MB"); ok {
		t.Error("no slash ok")
	}
}

func TestSampleOfAndMatches(t *testing.T) {
	rows, err := ParseStats([]byte(`{"BlockIO":"0B / 8.19kB","CPUPerc":"12.5%","Container":"abc123def456","ID":"abc123def456","MemPerc":"0.6%","MemUsage":"210MiB / 2GiB","Name":"api-1","NetIO":"1.2kB / 3.4kB","PIDs":"12"}
{"Container":"x","Name":"x","CPUPerc":"--","MemUsage":"-- / --","NetIO":"--","BlockIO":"--"}`))
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %v, %v", rows, err)
	}
	s := SampleOf(rows[0])
	if !s.HasCPU || s.CPU != 12.5 || !s.HasMem || s.MemUsed != 210*1048576 || s.MemLimit != 2*(1<<30) || !s.HasNet || s.NetTx != 3400 || !s.HasBlk || s.BlkWrite != 8190 {
		t.Errorf("sample = %+v", s)
	}
	if z := SampleOf(rows[1]); z.HasCPU || z.HasMem || z.HasNet || z.HasBlk {
		t.Errorf("-- sample = %+v", z)
	}
	long := Container{ID: "abc123def456789000", Names: []string{"api-1"}}
	if !rows[0].Matches(long) || rows[0].Matches(Container{ID: "zzz", Names: []string{"other"}}) {
		t.Error("Matches by id wrong")
	}
	if !(Stat{Name: "api-1"}).Matches(Container{ID: "zzz", Names: []string{"/api-1"}}) {
		t.Error("Matches by name wrong")
	}
}
