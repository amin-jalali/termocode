package bench

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// clkTck is the kernel's tick rate for /proc CPU times (100 on Linux).
const clkTck = 100

// Sample is memory and CPU of a process and every child it started
// (language servers, helpers), read from /proc.
type Sample struct {
	At       time.Time
	Procs    int
	RSSKB    int64  // resident memory, summed
	CPUTicks uint64 // user+system ticks, summed
	Wakeups  uint64 // voluntary context switches, summed over all threads
}

// ParseStat reads the parent pid and utime+stime ticks from /proc/<pid>/stat.
// The command name sits in parentheses and may hold spaces or parentheses.
func ParseStat(line string) (ppid int, ticks uint64, err error) {
	i := strings.LastIndexByte(line, ')')
	if i < 0 {
		return 0, 0, errors.New("bench: bad stat line")
	}
	f := strings.Fields(line[i+1:])
	// f[0]=state f[1]=ppid ... f[11]=utime f[12]=stime
	if len(f) < 13 {
		return 0, 0, errors.New("bench: short stat line")
	}
	ppid, err = strconv.Atoi(f[1])
	if err != nil {
		return 0, 0, err
	}
	u, err1 := strconv.ParseUint(f[11], 10, 64)
	s, err2 := strconv.ParseUint(f[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, errors.New("bench: bad cpu fields")
	}
	return ppid, u + s, nil
}

// ParseStatus reads VmRSS (kB) and voluntary context switches from
// /proc/<pid>/status (or a thread's status).
func ParseStatus(s string) (rssKB int64, voluntary uint64) {
	for _, l := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "kB"))
		switch k {
		case "VmRSS":
			rssKB, _ = strconv.ParseInt(v, 10, 64)
		case "voluntary_ctxt_switches":
			voluntary, _ = strconv.ParseUint(v, 10, 64)
		}
	}
	return
}

func readPPID(pid int) (int, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	pp, _, err := ParseStat(string(b))
	return pp, err == nil
}

// Tree lists root and all its descendants (root first).
func Tree(root int) []int {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	children := map[int][]int{}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if pp, ok := readPPID(pid); ok {
			children[pp] = append(children[pp], pid)
		}
	}
	if _, ok := readPPID(root); !ok {
		return nil
	}
	out := []int{root}
	for i := 0; i < len(out); i++ {
		out = append(out, children[out[i]]...)
	}
	return out
}

// Take samples the whole tree under root now.
func Take(root int) Sample {
	s := Sample{At: time.Now()}
	for _, pid := range Tree(root) {
		p := "/proc/" + strconv.Itoa(pid)
		b, err := os.ReadFile(p + "/stat")
		if err != nil {
			continue
		}
		_, ticks, err := ParseStat(string(b))
		if err != nil {
			continue
		}
		st, err := os.ReadFile(p + "/status")
		if err != nil {
			continue
		}
		rss, _ := ParseStatus(string(st))
		s.Procs++
		s.RSSKB += rss
		s.CPUTicks += ticks
		tasks, _ := filepath.Glob(p + "/task/*/status")
		for _, tf := range tasks {
			if tb, err := os.ReadFile(tf); err == nil {
				_, v := ParseStatus(string(tb))
				s.Wakeups += v
			}
		}
	}
	return s
}

// Idle is what a program costs while nobody uses it.
type Idle struct {
	Seconds       float64
	CPUSeconds    float64 // CPU time used in the window
	WakeupsPerSec float64
	RSSKB         int64
	Procs         int
}

func idleFrom(a, b Sample, d time.Duration, tck float64) Idle {
	id := Idle{Seconds: d.Seconds(), RSSKB: b.RSSKB, Procs: b.Procs}
	if b.CPUTicks > a.CPUTicks {
		id.CPUSeconds = float64(b.CPUTicks-a.CPUTicks) / tck
	}
	if b.Wakeups > a.Wakeups && d > 0 {
		id.WakeupsPerSec = float64(b.Wakeups-a.Wakeups) / d.Seconds()
	}
	return id
}

// MeasureIdle samples the tree under root, waits d, and samples again.
func MeasureIdle(root int, d time.Duration) Idle {
	a := Take(root)
	time.Sleep(d)
	b := Take(root)
	return idleFrom(a, b, b.At.Sub(a.At), clkTck)
}
