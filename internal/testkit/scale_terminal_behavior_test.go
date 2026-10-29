package testkit

import (
	"strconv"
	"testing"
	"time"
)

// Spec (SCN-12): Given a terminal tab,
// when a command prints one million lines,
// then the output finishes within its bound, keys stay responsive while
// it runs and after, and the Go heap stays bounded (the output is not
// kept in the app).
//
// Local (race off): 1M lines 3.4 s, worst key 4 ms, heap +0.1 MB, the
// terminal buffer keeps its scrollback (10 000 lines).
func TestScaleTerminalMillionLines(t *testing.T) {
	skipShortScale(t)
	needBash(t)
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: textFiles()})
	open(d, "a.txt")
	openBashTerminal(d)
	base := heapMB()
	t0 := time.Now()
	termSend(d, 0, "seq 1 1000000; echo done-$((40+2))\n")
	var key time.Duration
	for !d.Failed() && time.Since(t0) < 60*time.Second*raceFactor() {
		key = max(key, d.Timed(KeyMsg("x")))
		d.Settle(100 * time.Millisecond)
		if d.WaitForText("done-42", 10*time.Millisecond) {
			break
		}
	}
	if !d.WaitForText("done-42", time.Second) {
		t.Fatalf("seq did not finish:\n%s", screen(d))
	}
	within(t, "print 1M lines", time.Since(t0), 10*time.Second)
	key = max(key, worstKey(d, "x", "backspace", "x", "backspace"))
	within(t, "worst key while printing and after", key, 100*time.Millisecond)
	heapWithin(t, "1M lines of output", base, 40)
	// nvim keeps only its scrollback, not the million lines.
	n := lua(d, `for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.bo[b].buftype == 'terminal' then return tostring(vim.api.nvim_buf_line_count(b)) end
end
return '0'`)
	if lines, _ := strconv.Atoi(n); lines == 0 || lines > 100_001 {
		t.Errorf("terminal buffer holds %s lines, want 1..100001 (scrollback)", n)
	}
}
