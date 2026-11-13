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

// A terminal program redraws only what changed: after a jump the stream holds
// "line 0000001" and then "50000" at a cursor position. The text the user sees
// is "line 0050000". Matching must be on the screen, not on the byte stream.
func TestSessionMatchesTheScreenNotTheStream(t *testing.T) {
	s, err := Start(Spec{Bin: "sh", Args: []string{"-c", `printf 'line 0000001'; sleep 0.2; printf '\033[1;8H50000'; sleep 5`}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.WaitForText("line 0050000", 3*time.Second); err != nil {
		t.Fatalf("the screen shows it, the stream does not: %v", err)
	}
}

func TestSessionTimeKeyIgnoresTextAlreadyOnScreen(t *testing.T) {
	s := startCat(t)
	if _, err := s.WaitForText("ready", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // the second "q" is already on screen from the first
		d, _, err := s.TimeKey([]byte("q"), "q", 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if d <= 0 {
			t.Fatalf("latency %v", d)
		}
	}
	if n := strings.Count(s.Text(), "q"); n != 2 {
		t.Fatalf("screen has %d q, want 2", n)
	}
}

func TestSessionAnswersTerminalQueries(t *testing.T) {
	// Programs like termocode ask the terminal who it is (DA1) and wait.
	s, err := Start(Spec{Bin: "bash", Args: []string{"-c", `printf '\033[c'; IFS= read -r -s -n 3 -t 3 reply; case "$reply" in *'[?'*) printf got-answer;; *) printf no-answer;; esac; sleep 3`}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.WaitForText("got-answer", 4*time.Second); err != nil {
		t.Fatalf("no reply to DA1: %v (screen %q)", err, s.Text())
	}
}
