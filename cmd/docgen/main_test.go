package main

import (
	"strings"
	"testing"
)

// TestDocsUpToDate fails when docs/reference/*.md no longer match the
// code (a key, palette command, setting or config file changed).
func TestDocsUpToDate(t *testing.T) {
	stale, err := run("../..", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) > 0 {
		t.Fatalf("reference docs are out of date: %s\nRun `go run ./cmd/docgen` from the repository root and commit the result.",
			strings.Join(stale, ", "))
	}
}

// TestRenderDeterministic guards against map-order output.
func TestRenderDeterministic(t *testing.T) {
	a, b := render(), render()
	for name := range a {
		if a[name] != b[name] {
			t.Fatalf("%s differs between two renders", name)
		}
		if !strings.HasPrefix(a[name], banner) {
			t.Fatalf("%s does not start with the generated banner", name)
		}
	}
}
