package app

import (
	"slices"
	"testing"

	"github.com/amin-jalali/termocode/internal/search"
)

// RC-8: Find in Files of a container workspace hands the search the
// container's Exec and the tools its probe found (rg, grep), so it asks
// them which files to read.
func TestContainerSearchOptionsUseTheContainersTools(t *testing.T) {
	ws, h, _, _ := fakeContainerWS(t)
	var opts search.Options
	if !containerSearchOptions(ws, &opts) {
		t.Fatal("a container workspace has search options")
	}
	if opts.FS == nil || opts.Remote == nil {
		t.Fatalf("opts = %+v", opts)
	}
	if !slices.Equal(opts.RemoteTools, h.Caps().Tools) {
		t.Fatalf("tools %v, want %v", opts.RemoteTools, h.Caps().Tools)
	}
	var local search.Options
	if containerSearchOptions(procWorkspace(), &local) || local.Remote != nil {
		t.Fatal("a local workspace gets no remote search")
	}
}
