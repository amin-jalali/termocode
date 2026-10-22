//go:build !unix

package app

import (
	"errors"
	"net"
)

var errMCPServeOS = errors.New("serving MCP needs unix sockets")

func mcpServeSocketPath(string) (string, error) { return "", errMCPServeOS }

var mcpServeListen = func(string) (net.Listener, error) { return nil, errMCPServeOS }

var mcpServePeer = func(net.Conn) error { return errMCPServeOS }
