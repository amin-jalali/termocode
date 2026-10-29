package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/extproc"
)

// The `termocode mcp` relay (M1 MS-2): an agent CLI starts it as a stdio
// MCP server; it copies newline-framed JSON-RPC to the running UI over
// the workspace's MCP socket and back. Stdout carries protocol lines and
// nothing else; every note goes to stderr.

// Exit codes of `termocode mcp`.
const (
	mcpExitOK    = 0 // stdin ended: the agent is done
	mcpExitLost  = 1 // the UI closed or refused the connection, or a start error
	mcpExitUsage = 2 // bad arguments
)

// mcpDrain is how long the relay waits, after stdin ended, for the UI to
// finish and close its side.
const mcpDrain = 2 * time.Second

// relayLostMessage is what the agent and stderr hear when the UI goes
// away: what happened, in which folder, and what to do next.
func relayLostMessage(workspace string, refused bool, err error) string {
	msg := "termocode closed the connection in " + workspace
	next := "Open termocode in that folder, then restart this agent."
	if refused {
		msg = "termocode refused the connection in " + workspace + " (permission denied: the socket belongs to another user?), or closed it before answering"
		next = "Run termocode and the agent as the same user, then restart this agent."
	}
	if err != io.EOF {
		msg += " (" + err.Error() + ")"
	}
	return msg + ". " + next
}

// relayMCP copies lines between the agent (in, out) and the UI (conn)
// until one side ends. It closes conn. Requests still unanswered when the
// UI goes away get an error reply, so the agent does not wait for them.
func relayMCP(in io.Reader, out io.Writer, conn net.Conn, errw io.Writer, workspace string) int {
	defer conn.Close()
	note := func(line string) { fmt.Fprintf(errw, "termocode mcp: ignored non-JSON line: %s\n", line) }

	var mu sync.Mutex
	pending := map[string]json.RawMessage{} // request ids sent, not yet answered
	var order []string
	gotReply := false

	inEOF := make(chan struct{})
	crash.Go("the MCP relay", func() { // agent -> UI
		r := bufio.NewReaderSize(in, 64<<10)
		for {
			msg, err := extproc.ReadLineMessage(r, note)
			if err != nil {
				if err != io.EOF {
					fmt.Fprintf(errw, "termocode mcp: reading stdin: %v\n", err)
				}
				close(inEOF)
				return
			}
			var h struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if json.Unmarshal(msg, &h) == nil && len(h.ID) > 0 && h.Method != "" {
				mu.Lock()
				if _, dup := pending[string(h.ID)]; !dup {
					order = append(order, string(h.ID))
				}
				pending[string(h.ID)] = h.ID
				mu.Unlock()
			}
			if _, err := conn.Write(append(msg, '\n')); err != nil {
				return // the UI went away; the other side reports it
			}
		}
	})

	fromUI := make(chan error, 1)
	crash.Go("the MCP relay", func() { // UI -> agent
		r := bufio.NewReaderSize(conn, 64<<10)
		for {
			msg, err := extproc.ReadLineMessage(r, note)
			if err != nil {
				fromUI <- err
				return
			}
			var h struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if json.Unmarshal(msg, &h) == nil && len(h.ID) > 0 && h.Method == "" {
				mu.Lock()
				delete(pending, string(h.ID))
				mu.Unlock()
			}
			mu.Lock()
			gotReply = true
			mu.Unlock()
			if _, err := out.Write(append(msg, '\n')); err != nil {
				fromUI <- err
				return
			}
		}
	})

	select {
	case <-inEOF:
		// The agent is done. Tell the UI, then give it a moment to finish.
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
			select {
			case <-fromUI:
			case <-time.After(mcpDrain):
			}
		}
		return mcpExitOK
	case err := <-fromUI:
		select { // stdin may have ended at the same time: that is a clean end
		case <-inEOF:
			return mcpExitOK
		default:
		}
		mu.Lock()
		refused := !gotReply
		var ids []json.RawMessage
		for _, k := range order {
			if id, ok := pending[k]; ok {
				ids = append(ids, id)
			}
		}
		mu.Unlock()
		msg := relayLostMessage(workspace, refused, err)
		fmt.Fprintf(errw, "termocode mcp: %s\n", msg)
		for _, id := range ids {
			_ = writeRPCError(out, id, -32001, msg)
		}
		return mcpExitLost
	}
}

// writeRPCError writes one JSON-RPC error reply line.
func writeRPCError(w io.Writer, id json.RawMessage, code int, message string) error {
	b, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{JSONRPC: "2.0", ID: id, Error: struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{code, message}})
	if err != nil {
		return err
	}
	_, err = w.Write(append(bytes.TrimSpace(b), '\n'))
	return err
}

// mcpUsage is the help text of `termocode mcp`.
const mcpUsage = `Usage: termocode mcp [--dir <workspace>]

Run termocode as a stdio MCP server for an agent CLI (Claude Code, Codex,
Gemini CLI, OpenCode). It relays the agent to the termocode that is open
in the workspace, so the agent can use the editor's tools.

  --dir <workspace>   the folder termocode is open in (default: the current
                      folder, or the nearest parent folder with termocode open)

When termocode is not open in the folder, the tool list still answers and
every tool call fails with a message to open termocode there first.
Messages go to stderr; stdout carries only protocol lines.
`
