// Command tc-dap-bridge runs a TCP-only debug adapter and relays it to
// stdin/stdout: `tc-dap-bridge -- dlv dap -l 127.0.0.1:0`. It lives in
// termocode's sidecar tools volume.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/amin-jalali/termocode/internal/sidecartools/dapbridge"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	if err := dapbridge.Run(ctx, args, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
