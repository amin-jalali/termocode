// Command tc-sleep is the entrypoint of termocode's tools sidecar: it
// keeps the container running and does nothing until it is stopped.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/amin-jalali/termocode/internal/sidecartools"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	os.Exit(sidecartools.Sleep(ctx))
}
