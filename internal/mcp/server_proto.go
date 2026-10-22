package mcp

import (
	"encoding/json"
	"errors"

	"github.com/amin-jalali/termocode/internal/extproc"
)

const MaxServerResult = 16 << 10

var ErrToolSourceUnavailable = errors.New("termocode is not running")

type toolSourceUnavailable interface{ Unavailable() bool }

// Error codes a ToolSource may return through CallError.
const (
	CodeDeclined      = -32000 // the user said no
	CodeTimeout       = -32002 // nobody answered in time
	CodeInvalidParams = -32602 // unknown tool or bad arguments
)

// Reasons a tools/call error carries in its data ({"reason": ...}), so a
// client can tell them apart although declined and failed share -32000
// (the MCP spec reserves no code for either: -32000..-32099 are left to
// the server).
const (
	ReasonDeclined  = "declined"   // the user said no
	ReasonToolError = "tool_error" // the tool ran and failed
)

// reasonData is the error data for reason.
func reasonData(reason string) json.RawMessage {
	return json.RawMessage(`{"reason":"` + reason + `"}`)
}

// codedError is the JSON-RPC error for a ToolSource CallError.
func codedError(c *CallError) *extproc.Error {
	e := &extproc.Error{Code: c.Code, Message: c.Message}
	switch c.Code {
	case CodeDeclined:
		e.Data = reasonData(ReasonDeclined)
	}
	return e
}

// toolFailedError is the error of a tool that ran and failed.
func toolFailedError(err error) *extproc.Error {
	return &extproc.Error{Code: serverCodeToolFailed, Message: err.Error(), Data: reasonData(ReasonToolError)}
}

// CallError is a ToolSource error that keeps its JSON-RPC code.
type CallError struct {
	Code    int
	Message string
}

func (e *CallError) Error() string { return e.Message }

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	serverCodeMethodNotFound    = -32601
	serverCodeInvalidParams     = -32602
	serverCodeInternal          = -32603
	serverCodeSourceUnavailable = -32001
	serverCodeToolFailed        = -32000
)
