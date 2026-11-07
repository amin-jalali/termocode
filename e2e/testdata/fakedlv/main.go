// Command fakedlv is a fake `dlv dap -l <addr>` for the e2e tests
// (SCN-20): a tiny debug adapter that speaks the Debug Adapter Protocol
// on one TCP connection. It never runs a program: after configurationDone
// it reports one stop in main.fakeFrame (main.go line 3, a local answer =
// 42); continue ends the "program" with one line of output. Every request
// command is appended to $FAKEDLV_LOG, one per line, so the test sees what
// termocode asked. It exits on disconnect, a closed connection, or after
// two minutes (never left behind).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

func main() {
	addr := ""
	for i, a := range os.Args {
		if a == "-l" && i+1 < len(os.Args) {
			addr = os.Args[i+1]
		}
	}
	if len(os.Args) < 2 || os.Args[1] != "dap" || addr == "" {
		fmt.Fprintln(os.Stderr, "fakedlv: usage: dlv dap -l host:port")
		os.Exit(2)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakedlv:", err)
		os.Exit(1)
	}
	go func() {
		time.Sleep(2 * time.Minute)
		os.Exit(3)
	}()
	// nvim-dap waits for this line (or retries the port) before it connects.
	fmt.Printf("DAP server listening at: %s\n", ln.Addr())
	conn, err := ln.Accept()
	if err != nil {
		os.Exit(1)
	}
	_ = ln.Close()
	serve(conn)
}

type adapter struct {
	w   io.Writer
	mu  sync.Mutex
	seq int
	dir string // the workspace (the adapter's working directory)
}

func serve(conn net.Conn) {
	defer conn.Close()
	dir, _ := os.Getwd()
	a := &adapter{w: conn, dir: dir}
	r := bufio.NewReader(conn)
	for {
		msg, err := readMsg(r)
		if err != nil {
			return
		}
		var req struct {
			Seq       int             `json:"seq"`
			Type      string          `json:"type"`
			Command   string          `json:"command"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(msg, &req) != nil || req.Type != "request" {
			continue
		}
		logLine(req.Command)
		if a.handle(req.Seq, req.Command) {
			return
		}
	}
}

// handle answers one request; true ends the session.
func (a *adapter) handle(seq int, cmd string) bool {
	switch cmd {
	case "initialize":
		a.respond(seq, cmd, map[string]any{"supportsConfigurationDoneRequest": true})
		a.event("initialized", nil)
	case "configurationDone":
		a.respond(seq, cmd, nil)
		a.event("output", map[string]any{"category": "stdout", "output": "fake-program-started-20\n"})
		a.event("stopped", map[string]any{"reason": "breakpoint", "threadId": 1, "allThreadsStopped": true})
	case "threads":
		a.respond(seq, cmd, map[string]any{"threads": []any{map[string]any{"id": 1, "name": "main"}}})
	case "stackTrace":
		a.respond(seq, cmd, map[string]any{"totalFrames": 1, "stackFrames": []any{map[string]any{
			"id": 1000, "name": "main.fakeFrame", "line": 3, "column": 1,
			"source": map[string]any{"name": "main.go", "path": filepath.Join(a.dir, "main.go")},
		}}})
	case "scopes":
		a.respond(seq, cmd, map[string]any{"scopes": []any{map[string]any{
			"name": "Locals", "variablesReference": 10, "expensive": false,
		}}})
	case "variables":
		a.respond(seq, cmd, map[string]any{"variables": []any{map[string]any{
			"name": "answer", "value": "42", "type": "int", "variablesReference": 0,
		}}})
	case "evaluate":
		a.respond(seq, cmd, map[string]any{"result": "42", "variablesReference": 0})
	case "continue":
		a.respond(seq, cmd, map[string]any{"allThreadsContinued": true})
		a.event("continued", map[string]any{"threadId": 1, "allThreadsContinued": true})
		a.event("output", map[string]any{"category": "stdout", "output": "fake-program-done-20\n"})
		a.event("exited", map[string]any{"exitCode": 0})
		a.event("terminated", nil)
	case "disconnect", "terminate":
		a.respond(seq, cmd, nil)
		if cmd == "disconnect" {
			return true
		}
		a.event("terminated", nil)
	default: // launch, setBreakpoints, setExceptionBreakpoints, …
		var body any
		if cmd == "setBreakpoints" {
			body = map[string]any{"breakpoints": []any{}}
		}
		a.respond(seq, cmd, body)
	}
	return false
}

func (a *adapter) respond(reqSeq int, cmd string, body any) {
	m := map[string]any{"type": "response", "request_seq": reqSeq, "success": true, "command": cmd}
	if body != nil {
		m["body"] = body
	}
	a.send(m)
}

func (a *adapter) event(name string, body any) {
	m := map[string]any{"type": "event", "event": name}
	if body != nil {
		m["body"] = body
	}
	a.send(m)
}

func (a *adapter) send(m map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq++
	m["seq"] = a.seq
	b, _ := json.Marshal(m)
	fmt.Fprintf(a.w, "Content-Length: %d\r\n\r\n%s", len(b), b)
}

// readMsg reads one Content-Length framed message.
func readMsg(r *bufio.Reader) ([]byte, error) {
	n := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			n, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	if n < 0 {
		return nil, fmt.Errorf("no Content-Length")
	}
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return b, err
}

func logLine(s string) {
	p := os.Getenv("FAKEDLV_LOG")
	if p == "" {
		return
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, s)
}
