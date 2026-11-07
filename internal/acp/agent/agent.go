// Package agent lets another editor use termocode's AI as its agent over
// the Agent Client Protocol: newline-delimited JSON-RPC 2.0 on stdio.
//
// This first slice is chat only: initialize, session/new, session/prompt
// (the reply streams as agent_message_chunk updates), session/cancel and
// authenticate. The agent does not call tools, read or write files, or
// ask for permissions yet.
package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/crash"
)

// ProtocolVersion is the ACP version this agent speaks.
const ProtocolVersion = 1

const (
	maxLine      = 16 << 20 // longest JSON-RPC line read
	maxHistory   = 40       // messages kept per session (system message aside)
	codeParse    = -32700
	codeRequest  = -32600
	codeMethod   = -32601
	codeParams   = -32602
	codeInternal = -32603
	codeBusy     = -32000
)

// StreamFunc streams one AI reply for msgs (see ai.Provider.Stream).
type StreamFunc func(ctx context.Context, msgs []ai.Message) <-chan ai.Chunk

// Server is one ACP agent connection.
type Server struct {
	Name, Version string
	Stream        StreamFunc
	// System is put before the project line of every session's system
	// message ("" = a default).
	System string

	wmu      sync.Mutex
	w        io.Writer
	mu       sync.Mutex
	sessions map[string]*session
	next     int
	wg       sync.WaitGroup
}

type session struct {
	cwd    string
	msgs   []ai.Message // without the system message
	cancel context.CancelFunc
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// Serve reads requests from r and writes answers to w until r ends or
// ctx is done. Running prompts are cancelled and waited for on the way out.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	s.w = w
	s.mu.Lock()
	if s.sessions == nil {
		s.sessions = map[string]*session{}
	}
	s.mu.Unlock()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	var err error
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		s.handle(ctx, []byte(line))
		if ctx.Err() != nil {
			break
		}
	}
	if e := sc.Err(); e != nil {
		err = fmt.Errorf("acp: read: %w", e)
	}
	stop()
	s.wg.Wait()
	return err
}

func (s *Server) handle(ctx context.Context, line []byte) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		s.reply(json.RawMessage("null"), nil, &rpcError{Code: codeParse, Message: "not JSON: " + err.Error()})
		return
	}
	isCall := len(req.ID) > 0 && string(req.ID) != "null"
	if req.Method == "" {
		if isCall {
			s.reply(req.ID, nil, &rpcError{Code: codeRequest, Message: "no method"})
		}
		return // a response to a request we never sent
	}
	switch req.Method {
	case "initialize":
		s.reply(req.ID, map[string]any{
			"protocolVersion": ProtocolVersion,
			"agentCapabilities": map[string]any{
				"loadSession":        false,
				"promptCapabilities": map[string]any{"image": false, "audio": false, "embeddedContext": true},
				"mcpCapabilities":    map[string]any{"http": false, "sse": false},
			},
			"authMethods": []any{},
			"agentInfo":   map[string]any{"name": s.Name, "title": s.Name, "version": s.Version},
		}, nil)
	case "authenticate":
		s.reply(req.ID, map[string]any{}, nil)
	case "session/new":
		var p struct {
			Cwd string `json:"cwd"`
		}
		if err := json.Unmarshal(orEmpty(req.Params), &p); err != nil {
			s.reply(req.ID, nil, &rpcError{Code: codeParams, Message: err.Error()})
			return
		}
		s.mu.Lock()
		s.next++
		id := "s" + strconv.Itoa(s.next)
		s.sessions[id] = &session{cwd: p.Cwd}
		s.mu.Unlock()
		s.reply(req.ID, map[string]any{"sessionId": id}, nil)
	case "session/prompt":
		s.prompt(ctx, req)
	case "session/cancel":
		var p struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(orEmpty(req.Params), &p) // a notification: nothing to answer
		s.mu.Lock()
		if ss := s.sessions[p.SessionID]; ss != nil && ss.cancel != nil {
			ss.cancel()
		}
		s.mu.Unlock()
	default:
		if isCall {
			s.reply(req.ID, nil, &rpcError{Code: codeMethod, Message: "method not found: " + req.Method})
		}
	}
}

// contentBlock is one ACP prompt part.
type contentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	URI      string `json:"uri"`
	Name     string `json:"name"`
	Resource *struct {
		URI  string `json:"uri"`
		Text string `json:"text"`
	} `json:"resource"`
}

// promptText joins the prompt blocks into one user message.
func promptText(blocks []contentBlock) string {
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "resource":
			if b.Resource != nil {
				parts = append(parts, "File "+b.Resource.URI+":\n```\n"+strings.TrimRight(b.Resource.Text, "\n")+"\n```")
			}
		case "resource_link":
			name := b.Name
			if name == "" {
				name = b.URI
			}
			parts = append(parts, "Linked file: "+name+" ("+b.URI+")")
		}
	}
	return strings.Join(parts, "\n\n")
}

func (s *Server) system(cwd string) string {
	sys := s.System
	if sys == "" {
		sys = "You are termocode's coding assistant, used as the agent of the user's editor. " +
			"You cannot run tools or change files in this mode: answer with text and code blocks the user can apply."
	}
	if cwd != "" {
		sys += "\nThe project folder is " + cwd + "."
	}
	return sys
}

// prompt runs one turn in the background so session/cancel can reach it.
func (s *Server) prompt(ctx context.Context, req request) {
	var p struct {
		SessionID string         `json:"sessionId"`
		Prompt    []contentBlock `json:"prompt"`
	}
	if err := json.Unmarshal(orEmpty(req.Params), &p); err != nil {
		s.reply(req.ID, nil, &rpcError{Code: codeParams, Message: err.Error()})
		return
	}
	s.mu.Lock()
	ss := s.sessions[p.SessionID]
	if ss == nil {
		s.mu.Unlock()
		s.reply(req.ID, nil, &rpcError{Code: codeParams, Message: "unknown session: " + p.SessionID})
		return
	}
	if ss.cancel != nil {
		s.mu.Unlock()
		s.reply(req.ID, nil, &rpcError{Code: codeBusy, Message: "a prompt is already running in this session"})
		return
	}
	tctx, cancel := context.WithCancel(ctx)
	ss.cancel = cancel
	user := ai.Message{Role: ai.RoleUser, Content: promptText(p.Prompt)}
	msgs := append([]ai.Message{{Role: ai.RoleSystem, Content: s.system(ss.cwd)}}, ss.msgs...)
	msgs = append(msgs, user)
	s.mu.Unlock()

	s.wg.Add(1)
	crash.Go("agent.prompt", func() {
		defer s.wg.Done()
		defer cancel()
		var b strings.Builder
		var last ai.Chunk
		for c := range s.Stream(tctx, msgs) {
			if c.Delta != "" {
				b.WriteString(c.Delta)
				s.notify("session/update", map[string]any{
					"sessionId": p.SessionID,
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"content":       map[string]any{"type": "text", "text": c.Delta},
					},
				})
			}
			if c.Done {
				last = c
			}
		}
		cancelled := tctx.Err() != nil && ctx.Err() == nil
		s.mu.Lock()
		ss.cancel = nil
		if last.Err == nil || cancelled && b.Len() > 0 {
			ss.msgs = append(ss.msgs, user, ai.Message{Role: ai.RoleAssistant, Content: b.String()})
			if n := len(ss.msgs); n > maxHistory {
				ss.msgs = append([]ai.Message(nil), ss.msgs[n-maxHistory:]...)
			}
		}
		s.mu.Unlock()
		switch {
		case cancelled || errors.Is(last.Err, context.Canceled):
			s.reply(req.ID, map[string]any{"stopReason": "cancelled"}, nil)
		case last.Err != nil:
			s.reply(req.ID, nil, &rpcError{Code: codeInternal, Message: ai.Redact(last.Err.Error())})
		default:
			s.reply(req.ID, map[string]any{"stopReason": "end_turn"}, nil)
		}
	})
}

func orEmpty(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("{}")
	}
	return b
}

func (s *Server) reply(id json.RawMessage, result any, e *rpcError) {
	if e == nil && result == nil {
		result = map[string]any{}
	}
	s.write(response{JSONRPC: "2.0", ID: id, Result: result, Error: e})
}

func (s *Server) notify(method string, params any) {
	s.write(notification{JSONRPC: "2.0", Method: method, Params: params})
}

func (s *Server) write(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return // our own types always marshal
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, _ = s.w.Write(append(b, '\n')) // the editor went away: Serve ends on the read side
}
