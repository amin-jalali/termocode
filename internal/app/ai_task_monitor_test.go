package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// OMX-6: the new lines of a terminal window are the ones after what the
// last look showed (blank padding rows do not count).
func TestAINewLines(t *testing.T) {
	for _, c := range []struct {
		prev, cur, want []string
	}{
		{nil, []string{"a", "b", ""}, []string{"a", "b"}},
		{[]string{"a", "b", ""}, []string{"a", "b", "c", "", ""}, []string{"c"}},
		{[]string{"a", "b"}, []string{"b", "c", "d"}, []string{"c", "d"}},
		{[]string{"x", "y"}, []string{"c", "d"}, []string{"c", "d"}}, // scrolled past: all
		{[]string{"a", "b"}, []string{"a", "b"}, []string{}},
	} {
		got := aiNewLines(c.prev, c.cur)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("prev %v cur %v: %v, want %v", c.prev, c.cur, got, c.want)
		}
	}
}

// OMX-6: the monitor hands the agent each batch of new output and a last
// one with the exit code; nothing when nothing is new; it stops when
// nobody reads.
func TestAITaskMonitorBatches(t *testing.T) {
	looks := []struct {
		running bool
		lines   []string
	}{
		{true, []string{"compiling a"}},
		{true, []string{"compiling a"}}, // nothing new: no batch
		{true, []string{"compiling a", "compiling b", "token sk-" + "test0123456789abcdefghij"}},
		{false, []string{"compiling b", "token sk-" + "test0123456789abcdefghij", "done"}},
	}
	var mu sync.Mutex
	var got []string
	i := 0
	mo := aiMonitor{label: "build", every: time.Millisecond,
		look: func(context.Context) (bool, int, []string, error) {
			l := looks[min(i, len(looks)-1)]
			i++
			return l.running, 2, l.lines, nil
		},
		push: func(text string) bool {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, text)
			return true
		}}
	mo.run(context.Background())
	if len(got) != 3 {
		t.Fatalf("batches %q", got)
	}
	wantAll(t, got[0], "[task build: 1 new line]", "compiling a")
	wantAll(t, got[1], "[task build: 2 new lines]", "compiling b", "!sk-test0123")
	wantAll(t, got[2], "[task build ended with exit code 2; 1 new line]", "done")

	n := 0
	stop := aiMonitor{label: "x", every: time.Millisecond,
		look: func(context.Context) (bool, int, []string, error) {
			n++
			return true, 0, []string{strings.Repeat("l", n)}, nil
		},
		push: func(string) bool { return false }}
	stop.run(context.Background()) // returns: nobody reads
	if n != 1 {
		t.Fatalf("looked %d times after nobody read", n)
	}
}
