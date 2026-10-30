//go:build !unix

package main

import (
	"fmt"
	"os"
)

// The web panel needs unix sockets.
func runWeb([]string) int {
	fmt.Fprintln(os.Stderr, "termocode web: not supported on this system")
	return 2
}

const webGatewayArg = "__web-gateway"

func runWebGateway([]string) int {
	fmt.Fprintln(os.Stderr, "termocode: the web gateway is not supported on this system")
	return 2
}
