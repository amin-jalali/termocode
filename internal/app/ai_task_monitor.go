package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/crash"
)

// ── Task monitor (OMX-6) ──────────────────────────────────────────────
//
// tasks_run with monitor: true hands the task's new output to the agent
// that started it, in batches, while it keeps working: after its tool
// calls it reads "[task build: 12 new lines] …" (Agent.Inbox), and a
// last batch with the exit code when the task ends. The monitor stops
// with the turn (or step) that started it.

const (
	aiMonitorEvery    = 5 * time.Second
	aiMonitorBatch    = 40   // lines in one batch (the newest)
	aiMonitorBytes    = 4000 // bytes in one batch
	aiMonitorMaxNotes = 60
)

// aiMonitor watches one task run.
type aiMonitor struct {
	label string
	every time.Duration
	// look reads the run: running, its exit code, its last lines.
	look func(ctx context.Context) (running bool, code int, lines []string, err error)
	// push hands a batch to the agent; false: nobody reads now.
	push func(text string) bool
}

func (mo aiMonitor) run(ctx context.Context) {
	var prev []string
	notes := 0
	t := time.NewTicker(mo.every)
	defer t.Stop()
	for notes < aiMonitorMaxNotes {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		running, code, lines, err := mo.look(ctx)
		if err != nil {
			mo.push(fmt.Sprintf("[task %s: the monitor stopped: %s]", mo.label, ai.Redact(err.Error())))
			return
		}
		fresh := aiNewLines(prev, lines)
		prev = lines
		if len(fresh) == 0 && running {
			continue
		}
		head := fmt.Sprintf("[task %s: %d new line%s]", mo.label, len(fresh), plural(len(fresh)))
		if !running {
			head = fmt.Sprintf("[task %s ended with exit code %d; %d new line%s]", mo.label, code, len(fresh), plural(len(fresh)))
		}
		if !mo.push(aiMonitorText(head, fresh)) || !running {
			return
		}
		notes++
	}
}

// aiMonitorText is one batch: the head and the newest lines that fit.
func aiMonitorText(head string, lines []string) string {
	if len(lines) > aiMonitorBatch {
		head += fmt.Sprintf(" (the last %d)", aiMonitorBatch)
		lines = lines[len(lines)-aiMonitorBatch:]
	}
	body := ai.Redact(strings.Join(lines, "\n"))
	if len(body) > aiMonitorBytes {
		body = "…" + body[len(body)-aiMonitorBytes:]
	}
	if body == "" {
		return head
	}
	return head + "\n" + body
}

// aiNewLines are the lines of cur after what prev showed (a terminal
// shows a window of its output, padded with blank rows): the lines after
// the place where prev's last lines appear in cur, or all of cur when
// they do not.
func aiNewLines(prev, cur []string) []string {
	trim := func(ls []string) []string {
		for len(ls) > 0 && strings.TrimSpace(ls[len(ls)-1]) == "" {
			ls = ls[:len(ls)-1]
		}
		return ls
	}
	prev, cur = trim(prev), trim(cur)
	if len(prev) == 0 {
		return cur
	}
	for k := min(len(prev), 5); k > 0; k-- { // prev's last k lines, as many as still show
		tail := prev[len(prev)-k:]
		for end := len(cur); end >= k; end-- {
			if slices.Equal(cur[end-k:end], tail) {
				return cur[end:]
			}
		}
	}
	return cur
}

// aiStartTaskMonitor starts the monitor of task label (buffer buf) for
// the agent of ctx (UI goroutine; call reads the terminal).
func (m *Model) aiStartTaskMonitor(ctx context.Context, call aiIDECaller, label string, buf int) bool {
	key := aiMailKeyOf(ctx)
	if key == "" {
		return false // not an agent of this chat (an outside agent)
	}
	mail := &m.aiS().bgMail
	mo := aiMonitor{label: label, every: aiMonitorEvery,
		push: func(text string) bool { return mail.push(key, text) },
		look: func(ctx context.Context) (bool, int, []string, error) {
			var running bool
			var code int
			var lines []string
			_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
				r := m.ensureTasks().runs[buf]
				if r == nil {
					return "", nil, fmt.Errorf("its terminal tab was closed")
				}
				running, code = r.Running, r.Code
				lines = m.aiTaskOutput(buf, 200)
				return "", nil, nil
			})
			return running, code, lines, err
		}}
	crash.Go("a task monitor", func() { mo.run(ctx) })
	return true
}
