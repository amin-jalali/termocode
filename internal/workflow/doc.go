// Package workflow reads and checks workflow files (ADR 0021).
//
// A workflow is a JSONC file, .termocode/workflows/<name>.jsonc in the
// repository or ~/.config/termocode/workflows/<name>.jsonc, with steps in
// a line: a source reads items from an outside system, a plan step lets
// the AI order them, filters keep some, an each step runs work per item,
// and write steps send results back. Any step may have when / else.
//
// This package is pure: Parse takes the file bytes and returns a File
// (the typed schema) plus warnings, or an error that names the line and
// the field. It does no I/O, starts nothing and resolves no secret. The
// other parts live in their own packages and build on these types:
//
//   - internal/workflow/expr: the expression and {{…}} template language
//   - internal/workflow/source: the source adapters
//   - internal/workflow/ledger: the write ledger and the write gate
//
// Text read from outside systems (items) is untrusted data, never
// instructions. A file from the repository is untrusted until the user
// accepts it on the trust card.
package workflow

// Defaults of the workflows_* settings in config.json (plan §5.8).
const (
	DefaultEnabled        = true
	DefaultParallelMax    = 2
	DefaultDayBudgetUSD   = 0.0 // 0: no day budget
	DefaultMissedGraceMin = 30
	DefaultHistoryDays    = 30
	DefaultWaitHours      = 12
	DefaultAgentSandbox   = SandboxWorktree
	SandboxWorktree       = "worktree"
	SandboxReadOnly       = "read-only"
)

// Sandboxes are the allowed agent sandbox values, the default first.
var Sandboxes = []string{SandboxWorktree, SandboxReadOnly}
