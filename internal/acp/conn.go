// Package acp is termocode's client for the Agent Client Protocol: an
// outside coding agent runs as a child process and talks JSON-RPC 2.0
// over its stdin / stdout, one JSON message per line. The agent streams
// its reply, tool calls and plan as session/update notifications and
// calls back into the client to ask for permission and to read or write
// files. termocode answers those through a Handler, so every permission
// and every file write is the user's card.
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
)

// JSON-RPC error codes the client sends.
const (
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

// maxLine caps one incoming message (a whole file can be in it).
const maxLine = 32 << 20

// ErrClosed is the error of a call on a closed connection.
var ErrClosed = errors.New("acp: the agent connection is closed")

// RPCError is an error answer from the agent.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return "agent error " + strconv.Itoa(e.Code) + ": " + e.Message }

type wireMsg struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

type answer struct {
	result json.RawMessage
	err    error
}

// conn is one JSON-RPC peer: calls out, answers in, requests and
// notifications from the agent go to onRequest / onNotify.
type conn struct {
	w         io.Writer
	wmu       sync.Mutex
	mu        sync.Mutex
	next      int64
	pending   map[int64]chan answer
	closed    bool
	done      chan struct{}
	onRequest func(method string, params json.RawMessage) (any, error)
	onNotify  func(method string, params json.RawMessage)
}

func newConn(w io.Writer) *conn {
	return &conn{w: w, pending: map[int64]chan answer{}, done: make(chan struct{})}
}

// start reads messages until r ends.
func (c *conn) start(r io.Reader) {
	go func() {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64<<10), maxLine)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var m wireMsg
			if err := json.Unmarshal(line, &m); err != nil {
				continue // not a protocol line (an agent's stray print)
			}
			c.dispatch(m)
		}
		c.shutdown()
	}()
}

func (c *conn) dispatch(m wireMsg) {
	switch {
	case m.Method != "" && m.ID != nil: // a request from the agent
		id := *m.ID
		go func() {
			res, err := c.onRequest(m.Method, m.Params)
			c.respond(id, res, err)
		}()
	case m.Method != "": // a notification
		if c.onNotify != nil {
			c.onNotify(m.Method, m.Params)
		}
	case m.ID != nil: // an answer to our call
		id, err := strconv.ParseInt(string(*m.ID), 10, 64)
		if err != nil {
			return
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch == nil {
			return
		}
		if m.Error != nil {
			ch <- answer{err: m.Error}
		} else {
			ch <- answer{result: m.Result}
		}
	}
}

// methodError marks a request the client does not serve.
type methodError struct{ method string }

func (e methodError) Error() string { return "method not found: " + e.method }

// paramsError marks bad params.
type paramsError struct{ err error }

func (e paramsError) Error() string { return "invalid params: " + e.err.Error() }

func (c *conn) respond(id json.RawMessage, res any, err error) {
	out := map[string]any{"jsonrpc": "2.0", "id": id}
	if err != nil {
		code := codeInternal
		var me methodError
		var pe paramsError
		switch {
		case errors.As(err, &me):
			code = codeMethodNotFound
		case errors.As(err, &pe):
			code = codeInvalidParams
		}
		out["error"] = map[string]any{"code": code, "message": err.Error()}
	} else {
		out["result"] = res // nil marshals as null
	}
	_ = c.write(out)
}

func (c *conn) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.w.Write(append(b, '\n'))
	return err
}

// call sends a request and waits for its answer (or ctx, or the end).
func (c *conn) call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.next++
	id := c.next
	ch := make(chan answer, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		c.drop(id)
		return fmt.Errorf("acp %s: %w", method, err)
	}
	select {
	case a := <-ch:
		if a.err != nil {
			return a.err
		}
		if result == nil || len(a.result) == 0 || string(a.result) == "null" {
			return nil
		}
		if err := json.Unmarshal(a.result, result); err != nil {
			return fmt.Errorf("acp %s: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		c.drop(id)
		return ctx.Err()
	case <-c.done:
		return ErrClosed
	}
}

func (c *conn) drop(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *conn) notify(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *conn) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.done)
}
