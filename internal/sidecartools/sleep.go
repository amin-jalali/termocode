// Package sidecartools holds the small programs termocode puts in the
// tools volume of a container workspace's sidecar (plan RC, phase 6).
// They are built static (CGO_ENABLED=0) for Linux by
// scripts/sidecar-tools/build.sh and never run on the user's machine.
package sidecartools

import "context"

// Sleep blocks until ctx ends (the engine's SIGTERM in cmd/tc-sleep) and
// returns the exit code 0.
func Sleep(ctx context.Context) int {
	<-ctx.Done()
	return 0
}
