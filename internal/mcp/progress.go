package mcp

import (
	"context"
	"strconv"
)

// Progress (MCPX-19, PR-23): notifications/progress of a running call
// reach the call's ctx (WithProgress), so the chat can show what a long
// call does.

// Progress is one notifications/progress.
type Progress struct {
	Progress, Total float64
	Message         string
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// Text is "message · 3 of 10 (30%)", shorter when parts are missing.
func (p Progress) Text() string {
	count := ""
	switch {
	case p.Total > 0:
		count = num(p.Progress) + " of " + num(p.Total) + " (" + strconv.Itoa(int(p.Progress*100/p.Total)) + "%)"
	case p.Progress > 0:
		count = num(p.Progress)
	}
	switch {
	case p.Message == "":
		return count
	case count == "":
		return p.Message
	}
	return p.Message + " · " + count
}

type progressFnKey struct{}

// WithProgress makes the tool calls run with ctx report progress to fn
// (from the connection's goroutine).
func WithProgress(ctx context.Context, fn func(Progress)) context.Context {
	return context.WithValue(ctx, progressFnKey{}, fn)
}

func progressFrom(ctx context.Context) func(Progress) {
	fn, _ := ctx.Value(progressFnKey{}).(func(Progress))
	return fn
}
