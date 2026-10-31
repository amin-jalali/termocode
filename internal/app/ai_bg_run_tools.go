package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
)

// ── agent_run_wait / agent_run_send (OMX-6) ───────────────────────────
//
// Both are read only for the user's files and ask no card: wait only
// waits, and send talks to a run the user allowed already (a step that
// may write still asks on its own cards). Secrets in a message are
// refused, as in every agent tool.

const (
	aiRunWaitDefault = 300 // seconds
	aiRunWaitMax     = 900
)

// aiRunWaitInfo is agent_run_wait's answer.
type aiRunWaitInfo struct {
	Ended  bool             `json:"ended"`
	Note   string           `json:"note,omitempty"`
	Status aiRunStatusInfo  `json:"status"`
	Result *aiRunResultInfo `json:"result,omitempty"`
}

func aiRunWaitTools(call aiIDECaller) []ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	return []ai.Tool{
		{
			Name:    "agent_run_wait",
			Display: "wait for an agent run",
			Description: "Wait until a background agent run of this chat ends (or timeout_s passes), then return its status " +
				"and, when it ended, its result. Only for runs that run in the background (agent_run_start said " +
				"\"running\"); a run that waits for the chat cannot end while you wait. Read only.",
			Params: map[string]any{
				"run_id":    str("the id agent_run_start returned"),
				"timeout_s": map[string]any{"type": "integer", "description": fmt.Sprintf("most seconds to wait (default %d, at most %d)", aiRunWaitDefault, aiRunWaitMax)},
			},
			Required: []string{"run_id"},
			Describe: func(a map[string]any) string {
				return "Wait for agent run " + aiCardText(ai.ArgString(a, "run_id"), 20)
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				return aiRunWait(ctx, call, strings.TrimSpace(ai.ArgString(a, "run_id")), aiWaitSeconds(a))
			},
		},
		{
			Name:    "agent_run_send",
			Display: "message an agent run's step",
			Description: "Send a message to a running step of an agent run of this chat (to steer it: a hint, a narrower " +
				"task, a question to answer in its result). The step reads it after its current tool call, in the same " +
				"turn. step is the step id or title; leave it out when one step runs. Not for a step of your own run.",
			Params: map[string]any{
				"run_id":  str("the run id"),
				"step":    str("the step id or title (optional when only one step runs)"),
				"message": str("what to tell the step, short and concrete"),
			},
			Required: []string{"run_id", "message"},
			Validate: func(a map[string]any) error { return aiArgsSecretError(a) },
			Describe: func(a map[string]any) string {
				if aiArgsSecretError(a) != nil {
					return aiSecretCardText
				}
				return "Message to run " + aiCardText(ai.ArgString(a, "run_id"), 20) + ": " + aiCardText(ai.ArgString(a, "message"), 60)
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				if err := aiArgsSecretError(a); err != nil {
					return "", err
				}
				id := strings.TrimSpace(ai.ArgString(a, "run_id"))
				step, text := strings.TrimSpace(ai.ArgString(a, "step")), ai.ArgString(a, "message")
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiToolRunSend(ctx, id, step, text) })
			},
		},
	}
}

func aiWaitSeconds(a map[string]any) int {
	n := aiRunWaitDefault
	if v, ok := a["timeout_s"].(float64); ok {
		n = int(v)
	}
	return max(1, min(n, aiRunWaitMax))
}

// aiRunWait waits off the UI goroutine for background run id to end.
func aiRunWait(ctx context.Context, call aiIDECaller, id string, secs int) (string, error) {
	var fin chan struct{}
	var bg *aiBgRun
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		f, err := m.aiRunFind(id)
		if err != nil {
			return "", nil, err
		}
		switch {
		case f.bg != nil && !f.bg.done:
			if _, run := agentrun.NodeOf(ctx); run == id {
				return "", nil, errors.New("a step cannot wait for its own run")
			}
			bg, fin = f.bg, f.bg.fin
			bg.waiters.Add(1)
		case f.bg == nil && (f.queued != nil || f.live):
			return "", nil, errors.New("this run waits for the chat (it may change files), so it cannot end while you " +
				"wait: finish your answer; it starts after it, and agent_run_status reads it later")
		}
		return "", nil, nil
	}); err != nil {
		return "", err
	}
	ended := fin == nil
	if bg != nil {
		defer bg.waiters.Add(-1)
		t := time.NewTimer(time.Duration(secs) * time.Second)
		select {
		case <-fin:
			ended = true
		case <-t.C:
		case <-ctx.Done():
		}
		t.Stop()
	}
	var out aiRunWaitInfo
	_, err := call(context.WithoutCancel(ctx), func(m *Model) (string, tea.Cmd, error) {
		st, err := m.aiRunStatusOf(id)
		if err != nil {
			return "", nil, err
		}
		out.Status, out.Ended = st, ended
		if ended {
			if res, _, err := m.aiRunResultOf(id); err == nil {
				out.Result = &res
			}
		} else {
			out.Note = fmt.Sprintf("It still runs after %d s; wait again or go on with other work.", secs)
		}
		return "", nil, nil
	})
	if err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return aiItermocJSON(out)
}

// aiToolRunSend hands text to a running step of run id (UI goroutine).
func (m *Model) aiToolRunSend(ctx context.Context, id, step, text string) (string, tea.Cmd, error) {
	s := m.aiS()
	if _, run := agentrun.NodeOf(ctx); run != "" && run == id {
		return "", nil, errors.New("a step cannot send messages to its own run")
	}
	f, err := m.aiRunFind(id)
	if err != nil {
		return "", nil, err
	}
	h := s.aiRunHandle(id)
	if h == nil || f.msg == nil {
		return "", nil, fmt.Errorf("run %s does not run now (agent_run_status says why)", id)
	}
	var running []ai.StoredStep
	target := ""
	for _, st := range f.msg.Steps {
		if agentdag.State(st.State) != agentdag.StateRunning {
			continue
		}
		running = append(running, st)
		if step != "" && (st.ID == step || strings.EqualFold(aiCleanText(st.Title), step)) {
			target = st.ID
		}
	}
	switch {
	case len(running) == 0:
		return "", nil, fmt.Errorf("no step of run %s runs now", id)
	case step == "" && len(running) > 1:
		var names []string
		for _, st := range running {
			names = append(names, st.ID)
		}
		return "", nil, fmt.Errorf("%d steps run now (%s): name one in step", len(running), strings.Join(names, ", "))
	case step == "":
		target = running[0].ID
	case target == "":
		return "", nil, fmt.Errorf("step %q of run %s does not run now", aiCardText(step, 30), id)
	}
	if err := h.Send(target, text); err != nil {
		return "", nil, err
	}
	return "Sent to step " + target + " of run " + id + ": it reads it after its current tool call.", nil, nil
}
