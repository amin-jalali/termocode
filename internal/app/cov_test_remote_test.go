package app

import (
	"io"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/tests"
)

// Coverage of the stream reading in test_runner_remote.go with canned
// container output.

func scanRemote(t *testing.T, out string, keep bool) ([]string, remoteTestEnd) {
	t.Helper()
	lines := make(chan string, 64)
	end := scanRemoteTestOutput(io.NopCloser(strings.NewReader(out)), lines, keep, "box", "pytest")
	var got []string
	for l := range lines {
		got = append(got, l)
	}
	return got, end
}

func TestCovRemoteScanEnds(t *testing.T) {
	marker := tests.ExitMarker
	got, end := scanRemote(t, "one\ntwo\n"+marker+"0\n", false)
	if len(got) != 2 || end.err != nil {
		t.Errorf("clean run: %v %v", got, end.err)
	}
	if _, end := scanRemote(t, "x\n"+marker+"3\n", false); end.err == nil || !strings.Contains(end.err.Error(), "exit status 3") {
		t.Errorf("exit 3: %v", end.err)
	}
	if _, end := scanRemote(t, marker+"127\n", false); end.err == nil || !strings.Contains(end.err.Error(), "not found in box") {
		t.Errorf("exit 127: %v", end.err)
	}
	if _, end := scanRemote(t, "cut off\n", false); end.err == nil || !strings.Contains(end.err.Error(), "without an exit code") {
		t.Errorf("no marker: %v", end.err)
	}
	_, end = scanRemote(t, "noise\n{\"testResults\":[]}\n"+marker+"0\n", true)
	if string(end.report) != `{"testResults":[]}` {
		t.Errorf("report %q", end.report)
	}
}

func TestCovRemoteWaitLinesAndStops(t *testing.T) {
	lines := make(chan string, 600)
	done := make(chan remoteTestEnd, 1)
	for i := 0; i < 600; i++ {
		lines <- "l"
	}
	close(lines)
	done <- remoteTestEnd{report: []byte("{}")}
	cmd := waitRemoteTestLines(4, lines, done)
	msg := cmd().(testLinesMsg)
	if len(msg.lines) != 500 {
		t.Errorf("batch %d", len(msg.lines))
	}
	msg = msg.next().(testLinesMsg)
	if len(msg.lines) != 100 {
		t.Errorf("rest %d", len(msg.lines))
	}
	if d := msg.next().(testDoneMsg); d.runID != 4 || string(d.report) != "{}" {
		t.Errorf("done %+v", d)
	}
	// a partially filled queue returns what is there
	l2 := make(chan string, 4)
	l2 <- "a"
	l2 <- "b"
	if m2 := waitRemoteTestLines(1, l2, make(chan remoteTestEnd))().(testLinesMsg); len(m2.lines) != 2 {
		t.Errorf("partial %v", m2.lines)
	}
	// remote stop funcs: registered, then stopped by StopTestRuns
	stopped := 0
	liveTestProcs.addRemote(901, func() { stopped++ })
	liveTestProcs.addRemote(902, func() { stopped++ })
	liveTestProcs.removeRemote(902)
	StopTestRuns()
	if stopped != 1 {
		t.Errorf("stopped %d", stopped)
	}
}
