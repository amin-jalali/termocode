package webproto

import (
	"slices"
	"testing"
)

// Edit rights for a share (G-COL): the editor role, which the owner can
// give a shared guest on the laptop's card, may read what a viewer reads
// and edit the files it opened (text, save, undo, close, its caret), and
// nothing else: no terminal, no git, no chat, no cards.
func TestEditorMayEditFilesOnly(t *testing.T) {
	if !ValidRole(RoleEditor) {
		t.Fatal("editor is no valid role")
	}
	for _, op := range []string{OpBufSetText, OpBufSave, OpBufUndo, OpBufClose, OpBufCursor, OpBufOpen, OpTreeList} {
		if !Allowed(RoleEditor, op) {
			t.Errorf("an editor may not %s", op)
		}
		if !Allowed(RoleController, op) {
			t.Errorf("a controller may not %s", op)
		}
	}
	for _, op := range []string{OpTermSend, OpGitCommit, OpChatSend, OpCardsAnswer, OpTasksRun, OpTreeRevealLaptop, OpWebPairOpen} {
		if Allowed(RoleEditor, op) {
			t.Errorf("an editor may %s", op)
		}
	}
	for _, op := range []string{OpBufSetText, OpBufSave, OpBufUndo, OpBufCursor} {
		if Allowed(RoleViewer, op) {
			t.Errorf("a viewer may %s", op)
		}
	}
	caps := CapsFor(RoleEditor)
	if !slices.Contains(caps, OpBufSetText) || slices.Contains(caps, OpTermSend) {
		t.Fatalf("editor caps: %v", caps)
	}
	for _, d := range ViewsFor(RoleEditor) {
		if d.Role == RoleController {
			t.Errorf("an editor sees the controller view %s", d.ID)
		}
	}
}

// The page never gives edit rights: its pairing answer is viewer or
// control (control is refused for a share); edit is the laptop's card
// alone.
func TestPageMayNotAnswerEdit(t *testing.T) {
	if err := (&PairAnswerArgs{Role: "edit"}).validate(0); err == nil {
		t.Fatal("the page may answer edit")
	}
}
