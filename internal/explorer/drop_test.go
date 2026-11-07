package explorer

import "testing"

// SCN-18: while a drag is over a folder, that folder's row is lit so the
// user sees where the drop goes; clearing the target puts it back.
func TestDropTargetLightsTheRow(t *testing.T) {
	m, paths := buildModelWithFiles(t, []string{"a.txt", "b.txt"})
	m.cursor = 1 // a.txt, so the root row (the target) is not lit by the cursor
	plain := m.View(true, "", false, nil)
	m.SetDropTarget(paths[0])
	if got := m.DropTarget(); got != paths[0] {
		t.Fatalf("DropTarget() = %q, want %q", got, paths[0])
	}
	if lit := m.View(true, "", false, nil); lit == plain {
		t.Error("the drop target row looks the same as before")
	}
	m.SetDropTarget("")
	if again := m.View(true, "", false, nil); again != plain {
		t.Error("clearing the drop target left the row lit")
	}
}
