package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Meta says when and where a run happened.
type Meta struct {
	Date    string
	Machine string
	Runs    int
	Warmup  int
}

// EditorInfo is a version record of one editor.
type EditorInfo struct {
	ID, Name, Version string
}

// IdleRow is the idle memory and CPU of one editor.
type IdleRow struct {
	Editor        string
	Seconds       float64
	RSSKB         int64
	CPUSeconds    float64
	WakeupsPerSec float64
	Procs         int
}

// Data is everything a run produced. It is saved as JSON: the raw data.
type Data struct {
	Meta    Meta
	Editors []EditorInfo
	Results []Result
	Idle    []IdleRow
	Skipped []string
}

// Save writes the raw data as JSON.
func (d Data) Save(path string) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Load reads raw data saved by Save.
func Load(path string) (Data, error) {
	var d Data
	b, err := os.ReadFile(path)
	if err != nil {
		return d, err
	}
	return d, json.Unmarshal(b, &d)
}

// Spot is a measure where termocode is slower than the best other editor.
type Spot struct {
	Measure Measure
	Rival   string
	Ratio   float64 // termocode p50 / rival p50
	Ours    time.Duration
	Theirs  time.Duration
}

func byMeasure(d Data) map[Measure][]Result {
	m := map[Measure][]Result{}
	for _, r := range d.Results {
		if len(r.Samples) > 0 {
			m[r.Measure] = append(m[r.Measure], r)
		}
	}
	return m
}

func compare(d Data) (slower, faster []Spot) {
	for _, m := range AllMeasures {
		var ours *Result
		rs := byMeasure(d)[m]
		for i := range rs {
			if rs[i].Editor == "termocode" {
				ours = &rs[i]
			}
		}
		if ours == nil {
			continue
		}
		o := Summarize(ours.Samples).P50
		var best *Result
		var bestP50 time.Duration
		for i := range rs {
			if rs[i].Editor == "termocode" {
				continue
			}
			if p := Summarize(rs[i].Samples).P50; best == nil || p < bestP50 {
				best, bestP50 = &rs[i], p
			}
		}
		if best == nil || bestP50 <= 0 {
			continue
		}
		s := Spot{Measure: m, Rival: best.Editor, Ratio: float64(o) / float64(bestP50), Ours: o, Theirs: bestP50}
		if s.Ratio > 1 {
			slower = append(slower, s)
		} else {
			faster = append(faster, s)
		}
	}
	return
}

// SlowSpots lists measures where termocode's median is more than ratio times
// the best other editor's. Use it to open PERF tasks.
func SlowSpots(d Data, ratio float64) []Spot {
	slower, _ := compare(d)
	var out []Spot
	for _, s := range slower {
		if s.Ratio > ratio {
			out = append(out, s)
		}
	}
	return out
}

func fmtDur(d time.Duration) string {
	return fmt.Sprintf("%.1f ms", float64(d)/float64(time.Millisecond))
}

func median(v []int) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]int(nil), v...)
	sort.Ints(s)
	if len(s)%2 == 1 {
		return float64(s[len(s)/2])
	}
	return float64(s[len(s)/2-1]+s[len(s)/2]) / 2
}

func bar(v, max time.Duration) string {
	if max <= 0 {
		return ""
	}
	n := int(float64(v)/float64(max)*20 + 0.5)
	return strings.Repeat("█", max1(n))
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

const methodText = `Every number comes from a scripted run of the real program in a real terminal (a PTY), driven by the checked-in scripts in internal/bench. Nothing is tuned for the benchmark.

- Each run starts a fresh process with an empty, isolated HOME (a cold start), with no user config.
- Warm-up runs are done first and thrown away; the rest are reported as median (p50) and p95. Raw samples are in the JSON file next to this report.
- Time is measured from sending the key (or starting the program) until the expected text shows on screen, so it includes everything between the key and the screen.
- The same generated project files are used for every editor. No private code.
- Memory and CPU are summed over the program and all its child processes (language servers too).
- Where an editor is missing, or a measure needs a plugin or a language server that is not pinned, the row is left out and listed under "Not run". A missing row means "not measured", never "slow".
- At least 10 samples are needed to trust a row; fewer are flagged.
- One machine, one run. Treat differences under about 20 percent as noise until they repeat.`

// Markdown renders the report: versions, tables, bars, both sides of the
// comparison, what was not run, and the method.
func Markdown(d Data) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Editor benchmark\n\nDate: %s. Machine: %s. Runs per measure: %d (warm-up: %d).\n\n", d.Meta.Date, d.Meta.Machine, d.Meta.Runs, d.Meta.Warmup)
	b.WriteString("## Versions\n\n| Editor | Version |\n|---|---|\n")
	for _, e := range d.Editors {
		fmt.Fprintf(&b, "| %s | %s |\n", e.Name, e.Version)
	}
	name := map[string]string{}
	for _, e := range d.Editors {
		name[e.ID] = e.Name
	}
	nm := func(id string) string {
		if n := name[id]; n != "" {
			return n
		}
		return id
	}
	b.WriteString("\n## Timings\n\nMedian (p50) and p95 per measure. Fastest first. Bars are scaled to the slowest p50.\n")
	bm := byMeasure(d)
	for _, m := range AllMeasures {
		rs := bm[m]
		if len(rs) == 0 {
			continue
		}
		sort.SliceStable(rs, func(i, j int) bool { return Summarize(rs[i].Samples).P50 < Summarize(rs[j].Samples).P50 })
		worst := Summarize(rs[len(rs)-1].Samples).P50
		fmt.Fprintf(&b, "\n### %s\n\n| Editor | Samples | p50 | p95 | min | max | |\n|---|---|---|---|---|---|---|\n", m)
		var few []string
		for _, r := range rs {
			s := Summarize(r.Samples)
			fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %s | %s |\n", nm(r.Editor), s.N, fmtDur(s.P50), fmtDur(s.P95), fmtDur(s.Min), fmtDur(s.Max), bar(s.P50, worst))
			if s.N < 10 {
				few = append(few, nm(r.Editor))
			}
		}
		if len(few) > 0 {
			fmt.Fprintf(&b, "\nNote: fewer than 10 samples for %s; do not trust these rows.\n", strings.Join(few, ", "))
		}
	}
	var frames strings.Builder
	for _, m := range AllMeasures {
		for _, r := range bm[m] {
			if len(r.FrameBytes) > 0 {
				fmt.Fprintf(&frames, "| %s | %s | %.0f |\n", nm(r.Editor), m, median(r.FrameBytes))
			}
		}
	}
	if frames.Len() > 0 {
		b.WriteString("\n## Output size\n\nMedian bytes the editor wrote to the terminal for one step (bytes per update).\n\n| Editor | Measure | bytes per update |\n|---|---|---|\n" + frames.String())
	}
	if len(d.Idle) > 0 {
		b.WriteString("\n## Idle\n\nMemory (resident, all processes), CPU seconds and wakeups while nobody uses the editor.\n\n| Editor | Window | Processes | Memory | CPU seconds | Wakeups per second |\n|---|---|---|---|---|---|\n")
		for _, r := range d.Idle {
			fmt.Fprintf(&b, "| %s | %.0f s | %d | %.0f MB | %.2f | %.1f |\n", nm(r.Editor), r.Seconds, r.Procs, float64(r.RSSKB)/1024, r.CPUSeconds, r.WakeupsPerSec)
		}
	}
	slower, faster := compare(d)
	b.WriteString("\n## Where termocode is slower\n\n")
	if len(slower) == 0 {
		b.WriteString("Nowhere in the measures that were run.\n")
	}
	for _, s := range slower {
		fmt.Fprintf(&b, "- %s: %s against %s for %s (%.1fx)\n", s.Measure, fmtDur(s.Ours), fmtDur(s.Theirs), nm(s.Rival), s.Ratio)
	}
	b.WriteString("\n## Where termocode is faster\n\n")
	if len(faster) == 0 {
		b.WriteString("Nowhere in the measures that were run.\n")
	}
	for _, s := range faster {
		fmt.Fprintf(&b, "- %s: %s against %s for %s (%.1fx)\n", s.Measure, fmtDur(s.Ours), fmtDur(s.Theirs), nm(s.Rival), 1/s.Ratio)
	}
	b.WriteString("\n## Not run\n\n")
	if len(d.Skipped) == 0 {
		b.WriteString("Everything in the plan ran.\n")
	}
	for _, s := range d.Skipped {
		fmt.Fprintf(&b, "- %s\n", s)
	}
	b.WriteString("\n## Method\n\n" + methodText + "\n")
	return b.String()
}
