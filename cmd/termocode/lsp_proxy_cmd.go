package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/amin-jalali/termocode/internal/lspproxy"
)

// `termocode lsp-proxy --map REMOTE=LOCAL -- server args…` (plan RC,
// phase 5): runs a local language server for files that the editor
// names by their container path. The editor starts it; nobody types it.

const lspProxyUsage = `termocode lsp-proxy: run a local language server for container files

  termocode lsp-proxy --map REMOTE=LOCAL -- SERVER [ARGS…]

REMOTE is the folder inside the container (/app), LOCAL the same files on
this machine (a bind mount or termocode's read-only mirror). Every file
URI is rewritten between the two, both ways. termocode starts this for a
folder opened in a container; see the Language servers guide.
`

// lspProxyArgs reads the command line: the map and the server argv.
func lspProxyArgs(args []string) (lspproxy.Map, []string, error) {
	var m lspproxy.Map
	seen := false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--":
			if !seen {
				return m, nil, fmt.Errorf("--map is missing")
			}
			if i+1 >= len(args) {
				return m, nil, fmt.Errorf("no server command after --")
			}
			return m, args[i+1:], nil
		case "--map":
			if i+1 >= len(args) {
				return m, nil, fmt.Errorf("--map needs REMOTE=LOCAL")
			}
			mm, err := lspproxy.ParseMap(args[i+1])
			if err != nil {
				return m, nil, err
			}
			m, seen = mm, true
			i++
		default:
			return m, nil, fmt.Errorf("unknown argument %q", a)
		}
	}
	return m, nil, fmt.Errorf("no server command (put it after --)")
}

func runLSPProxy(args []string) int {
	return lspProxyMain(args, os.Stdin, os.Stdout, os.Stderr)
}

func lspProxyMain(args []string, in io.Reader, out, errw io.Writer) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		fmt.Fprint(out, lspProxyUsage)
		return 0
	}
	m, argv, err := lspProxyArgs(args)
	if err != nil {
		fmt.Fprintln(errw, "termocode lsp-proxy: "+err.Error())
		fmt.Fprint(errw, lspProxyUsage)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := lspproxy.Run(ctx, m, argv, in, out, errw); err != nil {
		fmt.Fprintln(errw, "termocode lsp-proxy: "+err.Error())
		return 1
	}
	return 0
}
