package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
)

// Multi Round-Trip Requests (2026-07-28, MCPX-20): instead of asking the
// client in the middle of a call, a server answers resultType
// "input_required" with inputRequests (elicitation/create, roots/list,
// sampling/createMessage). termocode answers each one the same way it
// answers the old server→client requests (the elicitation card, the
// workspace roots, sampling refused), then sends the request again with
// the inputResponses and the server's requestState, as is.

// maxRounds caps the retries of one request: a server that keeps asking
// must not loop for ever.
const maxRounds = 10

type inputRequired struct {
	ResultType    string                     `json:"resultType"`
	InputRequests map[string]json.RawMessage `json:"inputRequests"`
	RequestState  *string                    `json:"requestState"`
}

// needsInput reads an InputRequiredResult ("incomplete" was its name in
// a draft).
func needsInput(raw json.RawMessage) (inputRequired, bool) {
	var r inputRequired
	if json.Unmarshal(raw, &r) != nil {
		return r, false
	}
	return r, r.ResultType == "input_required" || r.ResultType == "incomplete"
}

// roundTrips sends method and answers the server's input requests until
// the result is complete.
func (c *Client) roundTrips(ctx context.Context, method string, params any) (json.RawMessage, error) {
	base := withMeta(params)
	send := base
	for round := 0; ; round++ {
		raw, err := c.send(ctx, method, send)
		if err != nil {
			return nil, err
		}
		ir, ok := needsInput(raw)
		if !ok {
			return raw, nil
		}
		if round >= maxRounds {
			return nil, fmt.Errorf("%s: the server asked for input %d times; termocode stopped", method, maxRounds)
		}
		answers, err := c.answerInputs(ctx, ir.InputRequests)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", method, err)
		}
		send = map[string]any{}
		for k, v := range base {
			send[k] = v
		}
		if len(answers) > 0 {
			send["inputResponses"] = answers
		}
		if ir.RequestState != nil {
			send["requestState"] = *ir.RequestState
		}
	}
}

// answerInputs answers each input request, in key order.
func (c *Client) answerInputs(ctx context.Context, reqs map[string]json.RawMessage) (map[string]any, error) {
	keys := make([]string, 0, len(reqs))
	for k := range reqs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := map[string]any{}
	for _, k := range keys {
		var r struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(reqs[k], &r); err != nil {
			return nil, fmt.Errorf("bad input request %q: %w", k, err)
		}
		switch r.Method {
		case "elicitation/create":
			req, err := parseElicit(r.Params)
			if err != nil {
				return nil, fmt.Errorf("elicitation: %w", err)
			}
			out[k] = c.ask(ctx, req)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		case "roots/list":
			out[k] = c.rootsResult()
		case "sampling/createMessage":
			return nil, errors.New("the server asked to use the model (sampling); termocode refuses this on purpose")
		default:
			return nil, fmt.Errorf("the server asked for %q, which termocode does not support", r.Method)
		}
	}
	return out, nil
}

// ask shows one elicitation to the user (decline without a UI) and
// returns the ElicitResult for the server. The call's timeout stops
// while the user answers.
func (c *Client) ask(ctx context.Context, req ElicitRequest) map[string]any {
	e := elicitorFrom(ctx)
	if e == nil {
		return map[string]any{"action": ElicitDecline}
	}
	c.mu.Lock()
	hold := c.hold
	c.mu.Unlock()
	if hold != nil {
		hold(true)
		defer hold(false)
	}
	return elicitAnswer(req, e(ctx, req))
}

// elicitAnswer is the ElicitResult sent back for the user's answer.
func elicitAnswer(req ElicitRequest, res ElicitResult) map[string]any {
	out := map[string]any{"action": res.Action}
	if res.Action == "" {
		out["action"] = ElicitCancel
	}
	if res.Action == ElicitAccept && req.Mode == ElicitForm {
		content := res.Content
		if content == nil {
			content = map[string]any{}
		}
		out["content"] = content
	}
	return out
}

// rootsResult is the answer to roots/list: the workspace folders.
func (c *Client) rootsResult() map[string]any {
	roots := []map[string]any{}
	if c.opts.Roots != nil {
		for _, r := range c.opts.Roots() {
			u := url.URL{Scheme: "file", Path: filepath.ToSlash(r)}
			roots = append(roots, map[string]any{"uri": u.String(), "name": filepath.Base(r)})
		}
	}
	return map[string]any{"roots": roots}
}
