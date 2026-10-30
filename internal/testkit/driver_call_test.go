package testkit

import (
	"bytes"
	"runtime"
	"testing"
	"time"
)

func goroutineID() string {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	return string(bytes.Fields(b)[1])
}

// The model's calls run on one goroutine, as Bubble Tea runs Update and
// View on its event loop: a fresh goroutine per call grew its stack on
// every Update and View, which the real app never pays. A panic is
// still caught, and calls go on after it.
func TestDriverCallsRunOnOneGoroutine(t *testing.T) {
	d := &Driver{T: t, opt: Options{StepTimeout: 5 * time.Second}}
	var ids []string
	for i := 0; i < 3; i++ {
		if !d.call("probe", func() { ids = append(ids, goroutineID()) }) {
			t.Fatal("probe failed")
		}
	}
	if ids[0] != ids[1] || ids[1] != ids[2] {
		t.Fatalf("calls ran on goroutines %v, want one", ids)
	}
	ft := &fakeTB{TB: t}
	d.T = ft
	if d.call("boom", func() { panic("boom") }) {
		t.Fatal("a panicking call reported success")
	}
	if !ft.failed {
		t.Fatal("the panic was not reported")
	}
	d.T = t
	if !d.call("after", func() { ids = append(ids, goroutineID()) }) {
		t.Fatal("call after a panic failed")
	}
}

// fakeTB records Errorf instead of failing the real test.
type fakeTB struct {
	testing.TB
	failed bool
}

func (f *fakeTB) Errorf(string, ...any) { f.failed = true }
func (f *fakeTB) Helper()               {}

// A frame's rows that passed the checks last frame are not measured
// again; a changed or bad row always is.
func TestFrameChecksSkipRowsThatPassed(t *testing.T) {
	var memo rowMemo
	good := "abcd\nefgh\nijkl"
	if p := frameProblems(good, 4, 3, &memo); p != nil {
		t.Fatal(p)
	}
	n := rowsChecked.Load()
	if p := frameProblems(good, 4, 3, &memo); p != nil {
		t.Fatal(p)
	}
	if d := rowsChecked.Load() - n; d != 0 {
		t.Fatalf("an unchanged frame checked %d rows again, want 0", d)
	}
	n = rowsChecked.Load()
	bad := "abcd\nefghX\nijkl"
	for i := 0; i < 2; i++ {
		if p := frameProblems(bad, 4, 3, &memo); len(p) != 1 {
			t.Fatalf("call %d: problems %q, want one (row 1)", i, p)
		}
	}
	if d := rowsChecked.Load() - n; d != 2 {
		t.Fatalf("checked %d rows, want 2 (the bad row, both times)", d)
	}
	if p := frameProblems(good, 5, 3, &memo); len(p) != 3 {
		t.Fatalf("another width: %d problems, want 3", len(p))
	}
}
