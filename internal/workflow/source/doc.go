// Package source reads the items of a workflow `source` step (plan
// WFX-4): a shell command, files in the workspace, an HTTP request or a
// read-only MCP tool. Each adapter takes its own small input struct, so
// this package does not depend on the workflow file format; the engine
// maps a parsed step onto one of ShellInput, FilesInput, HTTPInput or
// MCPInput.
//
// Every adapter returns a Result: a list of Item, each with a Key that
// is stable across runs (the seen and write ledgers use it) and the
// item's JSON. Items are untrusted data from an outside system: callers
// must never put them into argv, env, URLs or file paths unescaped, and
// must mark them as data in prompts (plan §6.4).
//
// Security rules kept here (plan §6):
//   - shell runs argv only, never a shell string, through internal/spawn,
//     in a cwd inside the workspace, with a timeout;
//   - files never leave the workspace and never follow a symlink out of it;
//   - http resolves ${secret:NAME} only in headers, and no error this
//     package returns ever holds a secret value;
//   - mcp runs read-only tools only; any other tool gives ErrNeedsConfirm
//     unless the step was confirmed on a card.
package source
