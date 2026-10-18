package mcp

import "errors"

const MaxServerResult = 16 << 10

var ErrToolSourceUnavailable = errors.New("termocode is not running")

type toolSourceUnavailable interface{ Unavailable() bool }

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
