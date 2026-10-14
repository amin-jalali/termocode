package testkit

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Group R2 through the real app (fake SSE model, real nvim): write_file
// opens the review card; the user decides with the mouse.

const reviewOldA = "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"
const reviewNewA = "one\nTWO\nthree\nfour\nfive\nsix\nseven\neight\nNINE\nten\n"

func reviewFiles() map[string]string {
	f := textFiles()
	f["r.txt"] = reviewOldA
	return f
}

// clickNth clicks the n-th (0-based) place s shows on screen.
func clickNth(d *Driver, s string, n int) {
	d.T.Helper()
	k := 0
	for y, line := range strings.Split(screen(d), "\n") {
		for off := 0; ; {
			j := strings.Index(line[off:], s)
			if j < 0 {
				break
			}
			if k == n {
				d.Click(ansi.StringWidth(line[:off+j])+1, y)
				return
			}
			k++
			off += j + len(s)
		}
	}
	d.T.Fatalf("%q #%d not on screen:\n%s", s, n, screen(d))
}

// Spec: Given review mode (the default), when the model calls write_file,
// then the diff card shows the two changes and nothing is written yet;
// when the user clicks Accept on change 1 and Reject on change 2, then
// the disk and the open tab have exactly change 1, and the model is told
// 1 of 2 was accepted.
func TestAIReviewWriteFileOnScreen(t *testing.T) {
	s, srv := fakeToolModel(t, toolCall{"write_file", map[string]any{"path": "r.txt", "content": reviewNewA}})
	d := aiDriver(t, srv.URL, reviewFiles(), false)
	open(d, "r.txt")
	askAI(d, "change it")
	if !d.WaitForText("Review AI changes", 5*time.Second) {
		t.Fatalf("no review card:\n%s", screen(d))
	}
	for _, want := range []string{"Change 1 of 2", "Change 2 of 2", "Current", "Proposed", "Accept file"} {
		if !strings.Contains(screen(d), want) {
			t.Fatalf("%q not on the card:\n%s", want, screen(d))
		}
	}
	if diskFile(d, "r.txt") != reviewOldA {
		t.Fatal("written before the review")
	}
	clickNth(d, " Accept ", 0) // change 1
	if !d.WaitForText(" Accepted ", 2*time.Second) {
		t.Fatalf("change 1 not accepted:\n%s", screen(d))
	}
	clickNth(d, " Reject ", 0) // change 2 (change 1 now shows Accepted / Undo)
	if !d.WaitForText("fake-done", 5*time.Second) {
		t.Fatalf("the turn did not finish:\n%s", screen(d))
	}
	want := "one\nTWO\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"
	if got := diskFile(d, "r.txt"); got != want {
		t.Fatalf("disk %q", got)
	}
	if got := bufLines(d); got+"\n" != want {
		t.Errorf("buffer %q", got)
	}
	r := s.toolResults()
	if len(r) != 1 || !strings.Contains(r[0], "accepted 1 of 2 changes to r.txt") || !strings.Contains(r[0], "+NINE") {
		t.Errorf("results %q", r)
	}
	if strings.Contains(screen(d), "Review AI changes") {
		t.Errorf("card still shown:\n%s", screen(d))
	}
}

// Spec: Reject file writes nothing and the model is told so.
func TestAIReviewRejectFileOnScreen(t *testing.T) {
	s, srv := fakeToolModel(t, toolCall{"write_file", map[string]any{"path": "r.txt", "content": reviewNewA}})
	d := aiDriver(t, srv.URL, reviewFiles(), false)
	askAI(d, "change it")
	if !d.WaitForText("Reject file", 5*time.Second) {
		t.Fatalf("no review card:\n%s", screen(d))
	}
	clickNth(d, "Reject file", 0)
	if !d.WaitForText("fake-done", 5*time.Second) {
		t.Fatalf("the turn did not finish:\n%s", screen(d))
	}
	if diskFile(d, "r.txt") != reviewOldA {
		t.Errorf("disk changed: %q", diskFile(d, "r.txt"))
	}
	if r := s.toolResults(); len(r) != 1 || !strings.Contains(r[0], "rejected the change to r.txt; nothing was written") {
		t.Errorf("results %q", r)
	}
}

// Spec: Esc hides the card (the chat shows "1 file to review"), and Stop
// ends the turn while the review waits: nothing is written and no card is
// left.
func TestAIReviewStopOnScreen(t *testing.T) {
	_, srv := fakeToolModel(t, toolCall{"write_file", map[string]any{"path": "r.txt", "content": reviewNewA}})
	d := aiDriver(t, srv.URL, reviewFiles(), false)
	askAI(d, "change it")
	if !d.WaitForText("Review AI changes", 5*time.Second) {
		t.Fatalf("no review card:\n%s", screen(d))
	}
	d.Key("esc")
	if !d.WaitForText("1 file to review", 2*time.Second) {
		t.Fatalf("no review bar after Esc:\n%s", screen(d))
	}
	d.Key("ctrl+c")
	if !d.WaitFor(func() bool { return !strings.Contains(screen(d), "file to review") }, 5*time.Second) {
		t.Fatalf("review still waiting after Stop:\n%s", screen(d))
	}
	if diskFile(d, "r.txt") != reviewOldA {
		t.Errorf("disk changed: %q", diskFile(d, "r.txt"))
	}
}
