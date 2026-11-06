// Package expr is the small, safe expression language of workflow files
// (plan §3.4): `when`, `sort`, `dedupe` and the `{{…}}` templates.
//
// It has no assignment, no loops, no lambdas, no user functions and no
// access to the environment. The only names are the scope roots (item,
// items, steps, vars, run, workflow) and a fixed list of functions.
// Missing paths are null, never an error. Patterns use Go's regexp
// (RE2: linear time, no backtracking) and are capped at 1 KB, like the
// expression itself. Eval never panics.
//
// The package is pure: it imports only the standard library.
package expr
