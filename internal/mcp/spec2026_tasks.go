package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// The tasks extension (io.modelcontextprotocol/tasks, MCPX-38): a
// stateless server may answer a long tools/call with a task handle
// (resultType "task") instead of the result. termocode declares the
// extension and waits for the task inside the same call: it polls
// tasks/get, each "working" answer counts as progress (the chat shows
// its statusMessage, the call's timeout starts again), a question
// (input_required) goes through the elicitation card and back with
// tasks/update, and a call the user stops sends tasks/cancel. So a long
// task is an ordinary long call: Ctrl+B moves it to the background.

const tasksExtension = "io.modelcontextprotocol/tasks"

// Poll interval bounds (the server suggests pollIntervalMs).
const (
	taskPollMin     = 20 * time.Millisecond
	taskPollDefault = time.Second
	taskPollMax     = 10 * time.Second
)

type taskState struct {
	TaskID         string                     `json:"taskId"`
	Status         string                     `json:"status"`
	StatusMessage  string                     `json:"statusMessage"`
	PollIntervalMs int64                      `json:"pollIntervalMs"`
	Result         json.RawMessage            `json:"result"`
	Error          *extproc.Error             `json:"error"`
	InputRequests  map[string]json.RawMessage `json:"inputRequests"`
}

// readTask reads a task from a result: {"task": {...}} or the task's
// fields at the top.
func readTask(raw json.RawMessage) (taskState, string) {
	var r struct {
		ResultType string     `json:"resultType"`
		Task       *taskState `json:"task"`
		taskState
	}
	if json.Unmarshal(raw, &r) != nil {
		return taskState{}, ""
	}
	if r.Task != nil {
		return *r.Task, r.ResultType
	}
	return r.taskState, r.ResultType
}

// awaitTask follows a task handle until the task ends; progress is the
// call's progress hook (nil: none).
func (c *Client) awaitTask(ctx context.Context, raw json.RawMessage, progress func(Progress)) (json.RawMessage, error) {
	t, _ := readTask(raw)
	if t.TaskID == "" {
		return nil, errors.New("the server answered with a task without a taskId")
	}
	id := t.TaskID
	c.opts.log("tools/call runs as task %s", id)
	wait := taskPollDefault
	for {
		if t.PollIntervalMs > 0 { // the last one the server named stays
			wait = min(max(time.Duration(t.PollIntervalMs)*time.Millisecond, taskPollMin), taskPollMax)
		}
		switch t.Status {
		case "completed":
			if len(t.Result) == 0 {
				return nil, fmt.Errorf("task %s ended without a result", id)
			}
			return t.Result, nil
		case "failed":
			if t.Error != nil {
				return nil, t.Error
			}
			return nil, fmt.Errorf("task %s failed", id)
		case "cancelled":
			return nil, fmt.Errorf("the server cancelled task %s", id)
		case "input_required":
			answers, err := c.answerInputs(ctx, t.InputRequests)
			if err != nil {
				c.cancelTask(id)
				return nil, err
			}
			if _, err := c.call(ctx, "tasks/update", map[string]any{"taskId": id, "inputResponses": answers}); err != nil {
				return nil, c.taskErr(ctx, id, err)
			}
		default: // working
			if progress != nil && t.StatusMessage != "" {
				progress(Progress{Message: t.StatusMessage})
			}
		}
		select {
		case <-ctx.Done():
			c.cancelTask(id)
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		next, err := c.call(ctx, "tasks/get", map[string]any{"taskId": id})
		if err != nil {
			return nil, c.taskErr(ctx, id, err)
		}
		t, _ = readTask(next)
	}
}

func (c *Client) taskErr(ctx context.Context, id string, err error) error {
	if ctx.Err() != nil {
		c.cancelTask(id)
		return ctx.Err()
	}
	return err
}

// cancelTask tells the server to stop the task (best effort).
func (c *Client) cancelTask(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = c.call(ctx, "tasks/cancel", map[string]any{"taskId": id})
}
