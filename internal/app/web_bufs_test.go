package app

import (
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// Spec (WEB-17): bufs lists the laptop's files inside the workspace
// (others left out), marks the active and dirty ones, and sends the
// whole list again when it changes; the replay golden pins it.
func TestBufsFollowsLaptop(t *testing.T) {
	r := newWebFilesRig(t)
	a, b := filepath.Join(r.root, "a.go"), filepath.Join(r.root, "src", "b.go")
	r.m.bufs = []nvim.BufferInfo{{ID: 1, Path: a}, {ID: 4, Path: "/etc/hosts"}}
	r.m.activeBuf = 1
	c := webReplayRecord(t, r.m, r.h, "bufs", func() {
		r.track()
		r.track() // nothing changed: nothing sent
		r.m.bufs = append(r.m.bufs, nvim.BufferInfo{ID: 7, Path: b, Modified: true})
		r.m.activeBuf = 7
		r.track()
	})
	if len(c.Deltas) != 1 {
		t.Fatalf("deltas: %s", c.Deltas)
	}
	if webCanonJSON(t, c.Deltas[0]) != `{"bufs":[{"active":false,"buf":1,"dirty":false,"path":"a.go"},{"active":true,"buf":7,"dirty":true,"path":"src/b.go"}]}` {
		t.Fatalf("delta: %s", c.Deltas[0])
	}
	webReplayCheck(t, webTopics["bufs"], c)
	webReplayGolden(t, webReplayPath("bufs", "open_and_switch"), c)
}
