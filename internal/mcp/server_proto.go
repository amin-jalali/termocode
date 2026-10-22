package mcp

import "errors"

const MaxServerResult = 16 << 10

var ErrToolSourceUnavailable = errors.New("termocode is not running")

type toolSourceUnavailable interface{ Unavailable() bool }

// Error codes a ToolSource may return through CallError.
const (
	CodeDeclined      = -32000 // the user said no
	CodeTimeout       = -32002 // nobody answered in time
	CodeInvalidParams = -32602 // unknown tool or bad arguments
)

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
