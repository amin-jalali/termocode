package bench

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func ms(v ...int) []time.Duration {
	var o []time.Duration
	for _, x := range v {
		o = append(o, time.Duration(x)*time.Millisecond)
	}
	return o
}

func sampleData() Data {
	var fast, slow []time.Duration
	for i := 1; i <= 10; i++ {
		fast = append(fast, time.Duration(10+i)*time.Millisecond)
		slow = append(slow, time.Duration(100+i)*time.Millisecond)
	}
	return Data{
		Meta: Meta{Date: "2026-11-13", Machine: "test box, 4 cores", Runs: 10, Warmup: 1},
		Editors: []EditorInfo{
			{ID: "termocode", Name: "termocode", Version: "termocode 9.9"},
			{ID: "nvim", Name: "Neovim (--clean)", Version: "NVIM v0.11"},
		},
		Results: []Result{
			{Editor: "termocode", Measure: MOpen, Samples: slow},
			{Editor: "nvim", Measure: MOpen, Samples: fast},
			{Editor: "termocode", Measure: MType, Samples: fast, FrameBytes: []int{100, 120}},
			{Editor: "nvim", Measure: MType, Samples: slow, FrameBytes: []int{30, 40}},
			{Editor: "nvim", Measure: MSearch, Samples: ms(5, 6)},
		},
		Idle:    []IdleRow{{Editor: "termocode", RSSKB: 204800, CPUSeconds: 0.5, WakeupsPerSec: 12, Procs: 3, Seconds: 60}},
		Skipped: []string{"Helix: not installed (looked for hx, helix)"},
	}
}

func TestMarkdownHasVersionsMethodAndSkips(t *testing.T) {
	md := Markdown(sampleData())
	for _, want := range []string{"termocode 9.9", "NVIM v0.11", "Helix: not installed", "## Method", "warm-up", "2026-11-13", "test box"} {
		if !strings.Contains(md, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

func TestMarkdownTablesSortedFastestFirstWithBars(t *testing.T) {
	md := Markdown(sampleData())
	i := strings.Index(md, "### open")
	if i < 0 {
		t.Fatal("no open section")
	}
	sec := md[i:]
	if j := strings.Index(sec[5:], "###"); j > 0 {
		sec = sec[:j+5]
	}
	if strings.Index(sec, "Neovim") > strings.Index(sec, "| termocode") {
		t.Fatalf("fastest must come first:\n%s", sec)
	}
	if !strings.Contains(sec, "█") {
		t.Fatalf("no bar chart:\n%s", sec)
	}
	if !strings.Contains(sec, "15.0 ms") && !strings.Contains(sec, "15.5 ms") {
		t.Fatalf("p50 of the fast set missing:\n%s", sec)
	}
}

func TestMarkdownWarnsOnTooFewRuns(t *testing.T) {
	md := Markdown(sampleData())
	if !strings.Contains(md, "fewer than 10 samples") {
		t.Fatal("the 2-sample search row must carry a warning (method: at least 10 runs)")
	}
}

func TestMarkdownFramesAndIdleTables(t *testing.T) {
	md := Markdown(sampleData())
	if !strings.Contains(md, "bytes per update") || !strings.Contains(md, "110") {
		t.Fatal("frame bytes table missing (median of 100,120 = 110)")
	}
	if !strings.Contains(md, "200 MB") || !strings.Contains(md, "12.0") {
		t.Fatalf("idle table missing:\n%s", md)
	}
}

func TestSlowSpotsListsWhereTermocodeLoses(t *testing.T) {
	spots := SlowSpots(sampleData(), 1.25)
	if len(spots) != 1 || spots[0].Measure != MOpen || spots[0].Rival != "nvim" || spots[0].Ratio < 5 {
		t.Fatalf("%+v", spots)
	}
	md := Markdown(sampleData())
	if !strings.Contains(md, "Where termocode is slower") || !strings.Contains(md, "Where termocode is faster") {
		t.Fatal("honest report shows both sides")
	}
}

func TestDataJSONRoundTripsRawSamples(t *testing.T) {
	b, err := json.Marshal(sampleData())
	if err != nil {
		t.Fatal(err)
	}
	var back Data
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Results) != 5 || len(back.Results[0].Samples) != 10 || back.Results[0].Samples[0] != 101*time.Millisecond {
		t.Fatal("raw samples lost")
	}
}

func TestMarkdownIsDeterministic(t *testing.T) {
	if Markdown(sampleData()) != Markdown(sampleData()) {
		t.Fatal("same data must give the same report")
	}
}
