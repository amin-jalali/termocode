package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/amin-jalali/termocode/internal/acp"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/app"
)

const acpUsage = `Usage: termocode acp

Runs termocode's AI as an agent for another editor, over the Agent
Client Protocol (JSON-RPC on stdin and stdout). Point the editor's
custom agent setting at "termocode acp". It uses the AI provider set up
in termocode (AI: Configure Provider…). Chat only for now: the agent
does not run tools or change files.
`

// runACP serves ACP on stdio until the editor closes stdin.
func runACP(args []string) int {
	return runACPIn(args, os.Stdin, os.Stdout, os.Stderr, nil)
}

// runACPIn is runACP with its streams; stream nil = the configured provider.
func runACPIn(args []string, in io.Reader, out, errw io.Writer, stream acp.StreamFunc) int {
	if len(args) > 0 {
		if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
			fmt.Fprint(out, acpUsage)
			return 0
		}
		fmt.Fprintf(errw, "termocode acp: unknown argument %q\n\n%s", args[0], acpUsage)
		return 2
	}
	if stream == nil {
		prov, err := app.HeadlessAI()
		if err != nil {
			fmt.Fprintf(errw, "termocode acp: %s\n", ai.Redact(err.Error()))
		}
		if !prov.Configured() {
			fmt.Fprintln(errw, "termocode acp: no AI provider set up; run termocode and AI: Configure Provider…")
		}
		stream = func(ctx context.Context, msgs []ai.Message) <-chan ai.Chunk {
			return prov.Stream(ctx, ai.Request{Messages: msgs})
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	s := &acp.Server{Name: "termocode", Version: versionString(), Stream: stream}
	if err := s.Serve(ctx, in, out); err != nil {
		fmt.Fprintf(errw, "termocode acp: %v\n", err)
		return 1
	}
	return 0
}
