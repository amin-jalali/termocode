// Package agentctl is the small control socket of a running termocode
// (OMX-33, OMX-34): scripts and outside agents in its terminal tabs list
// the tabs and their agent states, type a prompt into an agent's tab,
// wait for an agent to finish, and report their own state (a hook of
// the agent). One JSON request per line, one JSON reply per line. The
// socket lives in the private session folder (0700, socket 0600) and only
// processes of the same user get in.
package agentctl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// Environment variables of a terminal tab.
const (
	EnvMarker = "TERMOCODE_ENV"    // "1" inside a termocode terminal
	EnvTab    = "TERMOCODE_TAB_ID" // the tab's id ("t3")
	EnvSocket = "TERMOCODE_SOCKET" // this socket's path
)

// Ops of a request.
const (
	OpList   = "list"
	OpPrompt = "prompt"
	OpWait   = "wait"
	OpReport = "report"
)

// States an agent reports or a wait waits for.
var States = []string{"working", "waiting", "done"}

// ValidState reports whether s is one of States.
func ValidState(s string) bool {
	for _, x := range States {
		if x == s {
			return true
		}
	}
	return false
}

// Request is one call.
type Request struct {
	Op       string `json:"op"`
	Tab      string `json:"tab,omitempty"`
	Text     string `json:"text,omitempty"`      // prompt
	Enter    *bool  `json:"enter,omitempty"`     // prompt: press Enter (default true)
	Until    string `json:"until,omitempty"`     // wait: a state (default done)
	TimeoutS int    `json:"timeout_s,omitempty"` // wait
	Agent    string `json:"agent,omitempty"`     // report: claude, codex, …
	State    string `json:"state,omitempty"`     // report
}

// Tab is one terminal tab in a list.
type Tab struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Agent  string `json:"agent,omitempty"`
	State  string `json:"state,omitempty"`
	Source string `json:"source,omitempty"` // "hook" when the agent reports itself, else "screen"
	Active bool   `json:"active,omitempty"`
}

// Reply is one answer.
type Reply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Tabs  []Tab  `json:"tabs,omitempty"`
	State string `json:"state,omitempty"` // wait: the state reached
}

// MaxLine caps one request line.
const MaxLine = 64 << 10

// Serve answers the connections of ln until it is closed. peer (nil:
// none) checks a connection first; handle answers one request and may
// block (a wait).
func Serve(ln net.Listener, peer func(net.Conn) error, handle func(Request) Reply) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go serveConn(c, peer, handle)
	}
}

func serveConn(c net.Conn, peer func(net.Conn) error, handle func(Request) Reply) {
	defer c.Close()
	if peer != nil {
		if err := peer(c); err != nil {
			return
		}
	}
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 4096), MaxLine)
	enc := json.NewEncoder(c)
	for sc.Scan() {
		var req Request
		var rep Reply
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			rep = Reply{Error: "bad request: " + err.Error()}
		} else {
			rep = handle(req)
		}
		if enc.Encode(rep) != nil {
			return
		}
	}
}

// Call sends one request to the socket at path and reads the reply.
// timeout bounds the whole call (a wait needs its own timeout plus a
// margin).
func Call(path string, req Request, timeout time.Duration) (Reply, error) {
	if path == "" {
		return Reply{}, errors.New("no termocode socket: run this inside a termocode terminal or in its workspace")
	}
	c, err := net.DialTimeout("unix", path, 5*time.Second)
	if err != nil {
		return Reply{}, fmt.Errorf("termocode is not running here (%v)", strings.TrimPrefix(err.Error(), "dial unix "+path+": "))
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return Reply{}, err
	}
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 4096), 4<<20)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return Reply{}, err
		}
		return Reply{}, errors.New("termocode closed the connection")
	}
	var rep Reply
	if err := json.Unmarshal(sc.Bytes(), &rep); err != nil {
		return Reply{}, err
	}
	if !rep.OK && rep.Error != "" {
		return rep, errors.New(rep.Error)
	}
	return rep, nil
}
