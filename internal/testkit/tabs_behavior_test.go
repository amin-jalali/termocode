package testkit

import (
	"testing"
	"time"
)

// Spec: Given three files,
// when the user opens them one after another quickly (faster than the
// app's 100 ms state refresh),
// then after each open the tab bar shows that file as the active tab —
// the last change of a burst is never lost.
func TestTabsFollowQuickOpens(t *testing.T) {
	d := editorDriver(t, nil)
	seq := []string{"a.txt", "b.txt", "c.txt", "a.txt", "c.txt", "b.txt", "a.txt", "b.txt", "c.txt", "a.txt"}
	for i, rel := range seq {
		d.Action("open:" + d.Dir + "/" + rel)
		if !d.WaitFor(func() bool { _, a := tabs(d); return a == rel }, 2*time.Second) {
			got, a := tabs(d)
			t.Fatalf("open #%d %s: tab bar shows active %q (tabs %v)", i, rel, a, got)
		}
	}
}
