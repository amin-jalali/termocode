package bench

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

// RunOpts says what a full run does.
type RunOpts struct {
	Editors  []Editor
	Env      Env
	Runs     int
	Warmup   int
	Measures []Measure     // default: AllMeasures
	IdleFor  time.Duration // 0 = no idle measure
	Machine  string
	Date     string
}

// RunAll runs every installed editor through every measure. Nothing is
// dropped quietly: missing editors, unsupported measures and failed runs are
// all listed in Data.Skipped.
func RunAll(o RunOpts, logf func(string)) Data {
	if logf == nil {
		logf = func(string) {}
	}
	ms := o.Measures
	if len(ms) == 0 {
		ms = AllMeasures
	}
	d := Data{Meta: Meta{Date: o.Date, Machine: o.Machine, Runs: o.Runs, Warmup: o.Warmup}}
	for _, e := range o.Editors {
		in, skip := e.Detect()
		if in == nil {
			d.Skipped = append(d.Skipped, skip)
			logf(skip)
			continue
		}
		d.Editors = append(d.Editors, EditorInfo{ID: e.ID, Name: e.Name, Version: in.Version})
		if e.Kind != Terminal {
			d.Skipped = append(d.Skipped, fmt.Sprintf("%s: installed (%s) but not measured: the GUI harness (window start, screen-capture latency) needs X11 with xdotool and ffmpeg and a per-program script", e.Name, in.Version))
			continue
		}
		for _, m := range ms {
			if _, ok := e.Scripts[m]; !ok {
				d.Skipped = append(d.Skipped, fmt.Sprintf("%s / %s: %s", e.ID, m, e.Unsupported[m]))
				continue
			}
			logf(fmt.Sprintf("%s / %s", e.ID, m))
			r, err := in.Run(m, o.Env, o.Runs, o.Warmup)
			if err != nil {
				d.Skipped = append(d.Skipped, fmt.Sprintf("%s / %s: failed: %v", e.ID, m, err))
				logf("  failed: " + err.Error())
				continue
			}
			d.Results = append(d.Results, r)
		}
		if o.IdleFor > 0 {
			if row, err := idleOf(in, o); err != nil {
				d.Skipped = append(d.Skipped, fmt.Sprintf("%s / idle: failed: %v", e.ID, err))
			} else {
				d.Idle = append(d.Idle, row)
			}
		}
	}
	return d
}

func idleOf(in *Install, o RunOpts) (IdleRow, error) {
	mk, ok := in.Scripts[MOpen]
	if !ok {
		return IdleRow{}, fmt.Errorf("no open script")
	}
	home, err := os.MkdirTemp("", "bench-home-")
	if err != nil {
		return IdleRow{}, err
	}
	defer os.RemoveAll(home)
	sc := mk(o.Env)
	s, err := Start(Spec{Bin: in.Path, Args: sc.Args, Env: IsolatedEnv(home)})
	if err != nil {
		return IdleRow{}, err
	}
	defer s.Close()
	if _, err := s.WaitForText(sc.ReadyText, 20*time.Second); err != nil {
		return IdleRow{}, err
	}
	time.Sleep(time.Second) // let start-up work finish: idle means idle
	id := MeasureIdle(s.Pid(), o.IdleFor)
	return IdleRow{Editor: in.ID, Seconds: id.Seconds, RSSKB: id.RSSKB, CPUSeconds: id.CPUSeconds, WakeupsPerSec: id.WakeupsPerSec, Procs: id.Procs}, nil
}

// MachineInfo is a one-line description of this machine for the report.
func MachineInfo() string {
	cpu := ""
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == "model name" {
				cpu = strings.TrimSpace(v)
				break
			}
		}
	}
	mem := ""
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		var kb int64
		if _, err := fmt.Sscanf(string(b), "MemTotal: %d kB", &kb); err == nil {
			mem = fmt.Sprintf(", %.0f GB RAM", float64(kb)/1048576)
		}
	}
	kern, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	return fmt.Sprintf("%s, %d threads%s, Linux %s, %s", cpu, runtime.NumCPU(), mem, strings.TrimSpace(string(kern)), runtime.GOARCH)
}
