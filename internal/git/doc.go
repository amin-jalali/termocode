// Package git wraps the git CLI for status, branch, and per-file diff queries.
// All functions are sync; callers should run them inside a tea.Cmd goroutine.
package git
