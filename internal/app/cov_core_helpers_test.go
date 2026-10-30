package app

import "testing"

// covNew is New without the nvim it would start: tests that need one
// attach their own (covNvimModel) and close it.
func covNew(t testing.TB) Model {
	t.Helper()
	t.Setenv("TERMOCODE_NO_NVIM", "1")
	return New()
}
