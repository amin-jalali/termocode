//go:build !unix

package main

import (
	"fmt"
	"os"
)

// Sessions without a terminal need unix ptys.
func runServe([]string) int {
	fmt.Fprintln(os.Stderr, "termocode serve: not supported on this system")
	return 2
}
