package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/extproc"
)

// Elicitation (MCPX-14, PR-17/18): during a tool call a server may ask
// the user for input (form mode) or to open a page (URL mode). The
// request goes to the call's Elicitor (WithElicitor), which the app
// shows as a Deny-first card; without one (no UI) the answer is decline.
// termocode never fills an answer by itself, and never from the secret
// store.

// Elicitation modes and answers.
const (
	ElicitForm = "form"
	ElicitURL  = "url"

	ElicitAccept  = "accept"
	ElicitDecline = "decline"
	ElicitCancel  = "cancel"
)

// ElicitField is one field of a form request (the schema's properties
// are flat: string, number, integer, boolean, enum).
type ElicitField struct {
	Name, Title, Description string
	Type                     string // string, number, integer, boolean
	Format                   string // email, uri, date, date-time, password …
	Enum                     []string
	Default                  any
	Required                 bool
}

// Label is the field's name for people.
func (f ElicitField) Label() string {
	if f.Title != "" {
		return f.Title
	}
	return f.Name
}

var secretWords = []string{"password", "passwd", "secret", "token", "apikey", "api_key", "api-key", "private_key", "credential"}

// Secret reports a field that looks like a password or key: the UI masks
// it and warns (servers must not ask for these in a form).
func (f ElicitField) Secret() bool {
	if f.Format == "password" {
		return true
	}
	low := strings.ToLower(f.Name + " " + f.Title)
	for _, w := range secretWords {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// ElicitRequest is one elicitation/create. Server is set by the caller
// that knows it (the app's tool wrapper).
type ElicitRequest struct {
	Server        string
	Mode          string
	Message       string
	Fields        []ElicitField
	URL           string
	ElicitationID string
}

// ElicitResult is the user's answer.
type ElicitResult struct {
	Action  string
	Content map[string]any
}

// Elicitor asks the user. It may block until the user answers or ctx
// ends (then it should return ElicitCancel).
type Elicitor func(ctx context.Context, req ElicitRequest) ElicitResult

type elicitorKey struct{}

// WithElicitor makes the tool calls run with ctx ask the user through e.
func WithElicitor(ctx context.Context, e Elicitor) context.Context {
	return context.WithValue(ctx, elicitorKey{}, e)
}

func elicitorFrom(ctx context.Context) Elicitor {
	e, _ := ctx.Value(elicitorKey{}).(Elicitor)
	return e
}

// parseElicit reads elicitation/create params.
func parseElicit(raw json.RawMessage) (ElicitRequest, error) {
	var p struct {
		Mode            string          `json:"mode"`
		Message         string          `json:"message"`
		URL             string          `json:"url"`
		ElicitationID   string          `json:"elicitationId"`
		RequestedSchema json.RawMessage `json:"requestedSchema"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return ElicitRequest{}, err
	}
	r := ElicitRequest{Mode: p.Mode, Message: p.Message, URL: p.URL, ElicitationID: p.ElicitationID}
	if r.Mode == "" {
		r.Mode = ElicitForm
	}
	switch r.Mode {
	case ElicitURL:
		if r.URL == "" {
			return r, fmt.Errorf("url mode without a url")
		}
		return r, nil
	case ElicitForm:
	default:
		return r, fmt.Errorf("unknown mode %q", r.Mode)
	}
	var s struct {
		Properties json.RawMessage `json:"properties"`
		Required   []string        `json:"required"`
	}
	_ = json.Unmarshal(p.RequestedSchema, &s)
	names, err := objectKeys(s.Properties)
	if err != nil {
		return r, err
	}
	var props map[string]struct {
		Type        string   `json:"type"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Format      string   `json:"format"`
		Enum        []string `json:"enum"`
		Default     any      `json:"default"`
	}
	_ = json.Unmarshal(s.Properties, &props)
	req := map[string]bool{}
	for _, n := range s.Required {
		req[n] = true
	}
	for _, n := range names {
		v := props[n]
		r.Fields = append(r.Fields, ElicitField{Name: n, Title: v.Title, Description: v.Description, Type: v.Type,
			Format: v.Format, Enum: v.Enum, Default: v.Default, Required: req[n]})
	}
	return r, nil
}

// objectKeys lists a JSON object's keys in their order.
func objectKeys(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("properties is not an object")
	}
	var keys []string
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		keys = append(keys, t.(string))
		var skip json.RawMessage
		if err := d.Decode(&skip); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

// handleElicit answers elicitation/create off the reader goroutine.
func (c *Client) handleElicit(m extproc.Message, reply replyFunc) {
	req, err := parseElicit(m.Params)
	if err != nil {
		reply(nil, &extproc.Error{Code: extproc.CodeInvalidParams, Message: "elicitation: " + err.Error()})
		return
	}
	c.mu.Lock()
	e, ctx, hold := c.elicit, c.elicitCtx, c.hold
	c.mu.Unlock()
	if hold == nil {
		hold = func(bool) {}
	}
	if e == nil {
		reply(map[string]any{"action": ElicitDecline}, nil)
		return
	}
	crash.GoWith("mcp.elicit", func() {
		hold(true) // no timeout while the user answers
		res := e(ctx, req)
		hold(false)
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
		reply(out, nil)
	}, func() { // a crashed question is cancelled; the server is not left waiting
		hold(false)
		reply(map[string]any{"action": ElicitCancel}, nil)
	})
}

// Elicit asks through ctx's elicitor (decline when there is none).
func Elicit(ctx context.Context, req ElicitRequest) ElicitResult {
	if e := elicitorFrom(ctx); e != nil {
		return e(ctx, req)
	}
	return ElicitResult{Action: ElicitDecline}
}
