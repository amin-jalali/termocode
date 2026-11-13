package bench

import (
	"strings"
	"testing"
	"time"
)

func startCat(t *testing.T) *Session {
	t.Helper()
	s, err := Start(Spec{Bin: "sh", Args: []string{"-c", "printf ready; exec cat"}, Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSessionWaitForSeesOutput(t *testing.T) {
	s := startCat(t)
	d, err := s.WaitForText("ready", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if d <= 0 || d > 5*time.Second {
		t.Fatalf("odd wait time %v", d)
	}
}

func TestSessionWaitForTimesOut(t *testing.T) {
	s := startCat(t)
	if _, err := s.WaitForText("never-shown", 150*time.Millisecond); err == nil {
		t.Fatal("want a timeout error")
	}
}

func TestSessionTimeKeyMeasuresEcho(t *testing.T) {
	s := startCat(t)
	if _, err := s.WaitForText("ready", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	d, n, err := s.TimeKey([]byte("q"), "q", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if d <= 0 || d > 2*time.Second {
		t.Fatalf("latency %v", d)
	}
	if n < 1 {
		t.Fatalf("bytes after key = %d", n)
	}
}

func TestSessionCountsBytesAndStripsEscapes(t *testing.T) {
	s, err := Start(Spec{Bin: "sh", Args: []string{"-c", `printf '\033[31mred\033[0m'; sleep 1`}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.WaitForText("red", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if s.BytesOut() < len("\x1b[31mred\x1b[0m") {
		t.Fatalf("bytes = %d", s.BytesOut())
	}
	if strings.Contains(s.Text(), "\x1b") {
		t.Fatal("Text() must have no escapes")
	}
}

func TestSessionCloseKillsChild(t *testing.T) {
	s := startCat(t)
	pid := s.Pid()
	s.Close()
	if alive(pid) {
		t.Fatalf("child %d still alive", pid)
	}
}

func TestSummarize(t *testing.T) {
	var ds []time.Duration
	for i := 1; i <= 100; i++ {
		ds = append(ds, time.Duration(i)*time.Millisecond)
	}
	st := Summarize(ds)
	if st.N != 100 || st.Min != time.Millisecond || st.Max != 100*time.Millisecond {
		t.Fatalf("%+v", st)
	}
	if st.P50 != 50*time.Millisecond && st.P50 != 51*time.Millisecond {
		t.Fatalf("p50 %v", st.P50)
	}
	if st.P95 != 95*time.Millisecond && st.P95 != 96*time.Millisecond {
		t.Fatalf("p95 %v", st.P95)
	}
	if (Summarize(nil) != Stats{}) {
		t.Fatal("empty input must give zero stats")
	}
}
