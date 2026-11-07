package acp

import (
	"context"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/crash"
)

// Provider runs chat turns on an ACP agent's session, so the agent is a
// normal termocode chat thread. Only the newest user message is sent:
// the agent keeps its own history. Its tool calls show as short lines;
// the agent runs its own tools, asking through the Handler.
type Provider struct {
	Client  *Client
	Session string
	Title   string // the agent's name
}

func (p *Provider) Name() string { return p.Title }

func (p *Provider) Configured() bool { return p != nil && p.Client != nil && p.Session != "" }

// SupportsTools is true so the chat never reads the agent's text as a
// termocode tool call; the agent gets no termocode tools (it has its own).
func (p *Provider) SupportsTools() bool { return true }

// Stream sends the last user message as one prompt.
func (p *Provider) Stream(ctx context.Context, req ai.Request) <-chan ai.Chunk {
	ch := make(chan ai.Chunk, 64)
	text := lastUser(req.Messages)
	crash.Go("acp.Stream", func() {
		defer close(ch)
		if text == "" {
			ch <- ai.Chunk{Done: true, Err: fmt.Errorf("%s: nothing to send", p.Title)}
			return
		}
		r := &renderer{}
		send := func(s string) {
			if s == "" {
				return
			}
			select {
			case ch <- ai.Chunk{Delta: s}:
			case <-ctx.Done():
			}
		}
		stop, err := p.Client.Prompt(ctx, p.Session, text, func(u Update) { send(r.render(u)) })
		if err == nil {
			send(StopNote(stop))
		}
		ch <- ai.Chunk{Done: true, Err: err}
	})
	return ch
}

func lastUser(ms []ai.Message) string {
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i].Role == ai.RoleUser && strings.TrimSpace(ms[i].Content) != "" {
			return ms[i].Content
		}
	}
	return ""
}

// StopNote says why a turn ended when it was not a normal end ("" for
// end_turn).
func StopNote(reason string) string {
	switch reason {
	case "", "end_turn":
		return ""
	case "max_tokens":
		return "\n\n_(the agent stopped: token limit)_"
	case "max_turn_requests":
		return "\n\n_(the agent stopped: too many steps)_"
	case "refusal":
		return "\n\n_(the agent refused)_"
	case "cancelled":
		return "\n\n_(stopped)_"
	}
	return "\n\n_(the agent stopped: " + reason + ")_"
}

// renderer turns updates into chat text: message chunks as they come,
// one line per tool call when it starts and when it fails, the plan when
// it changes, and a short diff summary for edits.
type renderer struct {
	titles map[string]string
	plan   string
	midTxt bool // the last output was message text (a tool line needs a break)
}

func (r *renderer) line(s string) string {
	pre := "\n"
	if r.midTxt {
		pre = "\n\n"
	}
	r.midTxt = false
	return pre + s + "\n"
}

func (r *renderer) render(u Update) string {
	if r.titles == nil {
		r.titles = map[string]string{}
	}
	switch u.Kind {
	case UpdateMessage:
		if u.Text != "" {
			r.midTxt = true
		}
		return u.Text
	case UpdateToolCall:
		t := u.Tool
		if t.Title != "" {
			r.titles[t.ID] = t.Title
		}
		out := r.line("⚙ " + toolTitle(t, r.titles))
		return out + diffSummary(t.Content)
	case UpdateToolUpdate:
		t := u.Tool
		if t.Title != "" {
			r.titles[t.ID] = t.Title
		}
		out := ""
		if t.Status == "failed" {
			out = r.line("✗ " + toolTitle(t, r.titles) + " failed")
		}
		return out + diffSummary(t.Content)
	case UpdatePlan:
		var b strings.Builder
		for _, e := range u.Plan {
			mark := "[ ]"
			switch e.Status {
			case "completed":
				mark = "[x]"
			case "in_progress":
				mark = "[~]"
			}
			b.WriteString("- " + mark + " " + e.Content + "\n")
		}
		plan := b.String()
		if plan == "" || plan == r.plan {
			return ""
		}
		r.plan = plan
		return r.line("Plan\n" + strings.TrimRight(plan, "\n"))
	}
	return ""
}

func toolTitle(t ToolCall, titles map[string]string) string {
	if s := titles[t.ID]; s != "" {
		return s
	}
	if t.Kind != "" {
		return t.Kind
	}
	return "tool " + t.ID
}

// diffSummary is one "± path (+a −r)" line per diff in content.
func diffSummary(cs []ToolContent) string {
	var b strings.Builder
	for _, c := range cs {
		if c.Type != "diff" || c.Path == "" {
			continue
		}
		add, del := diffCounts(c.OldText, c.NewText)
		fmt.Fprintf(&b, "  ± %s (+%d −%d)\n", c.Path, add, del)
	}
	return b.String()
}
