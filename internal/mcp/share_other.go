//go:build !unix

package mcp

import "context"

// Sharing needs unix sockets: every session runs its own servers.

func startShared(context.Context, *ShareOptions, Server, []string, Options) (*Process, error) {
	return nil, errShareUnavailable
}

// RunShareIfHelper does nothing here.
func RunShareIfHelper() {}
