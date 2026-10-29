package containers

import "testing"

func TestSeriesKeepsLastWindow(t *testing.T) {
	var s Series
	if _, ok := s.Last(); ok {
		t.Fatal("empty series has a last sample")
	}
	for i := 0; i < StatWindow+15; i++ {
		s.Push(Sample{CPU: float64(i), HasCPU: true})
	}
	if len(s.Samples) != StatWindow {
		t.Fatalf("len = %d", len(s.Samples))
	}
	if s.Samples[0].CPU != 15 {
		t.Errorf("oldest = %v, want 15", s.Samples[0].CPU)
	}
	if l, _ := s.Last(); l.CPU != float64(StatWindow+14) {
		t.Errorf("last = %v", l.CPU)
	}
}
