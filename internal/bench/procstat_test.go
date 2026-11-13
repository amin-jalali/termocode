package bench

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

func TestParseStatHandlesSpacesAndParensInName(t *testing.T) {
	// pid (comm) state ppid pgrp session tty tpgid flags minflt cminflt majflt cmajflt utime stime
	line := "123 (my (odd) name) S 45 1 1 0 -1 4194560 10 0 0 0 700 300 0 0 20 0 1 0 5 1000 100"
	ppid, ticks, err := ParseStat(line)
	if err != nil {
		t.Fatal(err)
	}
	if ppid != 45 || ticks != 1000 {
		t.Fatalf("ppid=%d ticks=%d", ppid, ticks)
	}
	if _, _, err := ParseStat("garbage"); err == nil {
		t.Fatal("want an error")
	}
}

func TestParseStatusReadsRSSAndSwitches(t *testing.T) {
	s := "Name:\tx\nVmRSS:\t  2048 kB\nvoluntary_ctxt_switches:\t12\nnonvoluntary_ctxt_switches:\t3\n"
	rss, vol := ParseStatus(s)
	if rss != 2048 || vol != 12 {
		t.Fatalf("rss=%d vol=%d", rss, vol)
	}
	if rss, vol := ParseStatus("Name:\tkernel\n"); rss != 0 || vol != 0 {
		t.Fatalf("no memory lines must read as zero, got %d %d", rss, vol)
	}
}

func TestTreeFindsGrandchildren(t *testing.T) {
	cmd := spawn.Command("sh", "-c", "sleep 30 & wait")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	var pids []int
	for i := 0; i < 50; i++ {
		pids = Tree(cmd.Process.Pid)
		if len(pids) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(pids) < 2 || pids[0] != cmd.Process.Pid {
		t.Fatalf("tree %v: want the root and its sleep child", pids)
	}
}

func TestTakeSumsMemoryOverTheTree(t *testing.T) {
	cmd := spawn.Command("sh", "-c", "sleep 30 & wait")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	time.Sleep(200 * time.Millisecond)
	s := Take(cmd.Process.Pid)
	if s.Procs < 2 || s.RSSKB <= 0 {
		t.Fatalf("%+v", s)
	}
	one := Take(cmd.Process.Pid)
	if one.RSSKB < s.RSSKB/2 {
		t.Fatalf("samples differ too much: %+v %+v", s, one)
	}
}

func TestMeasureIdleOfASleepingProgramIsNearZero(t *testing.T) {
	cmd := spawn.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	id := MeasureIdle(cmd.Process.Pid, 500*time.Millisecond)
	if id.CPUSeconds > 0.1 || id.WakeupsPerSec > 50 || id.RSSKB <= 0 {
		t.Fatalf("%+v", id)
	}
}

func TestIdleDeltaIgnoresProcessesThatExited(t *testing.T) {
	a := Sample{CPUTicks: 500, Wakeups: 100}
	b := Sample{CPUTicks: 400, Wakeups: 90} // a child exited, totals fell
	id := idleFrom(a, b, time.Second, 100)
	if id.CPUSeconds != 0 || id.WakeupsPerSec != 0 {
		t.Fatalf("%+v: a fall must count as zero, not negative", id)
	}
}
