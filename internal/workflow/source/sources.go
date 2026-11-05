package source

import (
	"net"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/httpclient"
)

// Sources runs source steps for one workspace. The zero value of each
// optional field is safe; an adapter whose dependency is nil fails with
// a clear error.
type Sources struct {
	// Workspace is the root every path (shell cwd, files glob, .http
	// request file) must stay inside.
	Workspace string
	// Secrets resolves ${secret:NAME} in http headers. A secrets.Store
	// satisfies it.
	Secrets SecretGetter
	// Client sends http sources; nil makes a default client per call.
	Client *httpclient.Client
	// AllowIP, when set, is asked for every IP an http source connects
	// to (see httpclient.Hooks.AllowIP).
	AllowIP func(net.IP) bool
	// Tools calls MCP tools for mcp sources; nil: every mcp source fails with
	// ErrNotConnected.
	Tools ToolCaller
	// GH runs gh for github sources (ghci.DefaultRunner, or a fake); nil:
	// every github source fails. The token is gh's own login; nothing
	// here reads or passes one.
	GH ghci.Runner
	// Databases opens read-only database sessions for db sources; nil: every db
	// source fails.
	Databases DBOpener
}

// SecretGetter returns a named secret (secrets.Store.Get). Its errors
// should not hold the value; the adapters scrub them anyway.
type SecretGetter interface {
	Get(name string) (value string, ok bool, err error)
}
