package explorer

// Drop target of a drag in the tree (SCN-18). The host runs the drag and
// the move; the explorer only lights the folder row the drop would go
// into, with the same wash as the selection.

// SetDropTarget marks the folder row a drag would drop into ("" = none).
func (m *Model) SetDropTarget(path string) { m.dropTarget = path }

// DropTarget is the folder row marked by SetDropTarget ("" = none).
func (m Model) DropTarget() string { return m.dropTarget }
