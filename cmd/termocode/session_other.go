//go:build !unix

package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// Persistent sessions need unix sockets and PTYs.
const sessionsSupported = false

func unsupported() int {
	fmt.Fprintln(os.Stderr, "termocode: persistent sessions are not supported on this system")
	return 1
}

func runServer(string) int             { return unsupported() }
func runClient(string, clientOpts) int { return unsupported() }
func cmdSessions() int                 { return unsupported() }
func cmdSSH([]string) int              { return unsupported() }
func cmdKill(string) int               { return unsupported() }
func watchRepaint(*tea.Program)        {}
