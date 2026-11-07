package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func screen(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(b), "\n")
}

// Spec (CMP-14): Choices reads the numbered answers of the question an
// agent shows at the bottom of its screen, in order, with the one under
// the cursor marked. Option descriptions under a row are not choices; box
// borders and key hints ("(esc)", "(y)") are cut off the label.
func TestChoicesOfWaitingScreens(t *testing.T) {
	for _, tc := range []struct {
		id, file string
		want     []Choice
	}{
		{"claude", "claude-waiting-bash.txt", []Choice{
			{N: 1, Label: "Yes", Cursor: true},
			{N: 2, Label: "Yes, and don't ask again for rm commands in /home/u/proj"},
			{N: 3, Label: "No, and tell Claude what to do differently"},
		}},
		{"claude", "claude-waiting-question.txt", []Choice{
			{N: 1, Label: "SQLite", Cursor: true},
			{N: 2, Label: "Postgres"},
			{N: 3, Label: "Type something."},
		}},
		{"codex", "codex-waiting-command.txt", []Choice{
			{N: 1, Label: "Yes, proceed", Cursor: true},
			{N: 2, Label: "Yes, and don't ask again for this command"},
			{N: 3, Label: "No, and tell Codex what to do differently"},
		}},
	} {
		got := Choices(tc.id, screen(t, tc.file))
		if len(got) != len(tc.want) {
			t.Errorf("%s: %d choices %+v, want %+v", tc.file, len(got), got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: choice %d = %+v, want %+v", tc.file, i, got[i], tc.want[i])
			}
		}
	}
}

// Spec (CMP-14): a screen that does not ask (working, done) has no
// choices, even when the answer text holds a numbered list; a list that
// does not start at 1 is not a question.
func TestChoicesNoneWhenNotAsking(t *testing.T) {
	for _, f := range []string{"claude-working.txt", "claude-done.txt", "codex-done.txt", "codex-working.txt"} {
		id := strings.SplitN(f, "-", 2)[0]
		if got := Choices(id, screen(t, f)); got != nil {
			t.Errorf("%s: choices %+v", f, got)
		}
	}
	lines := []string{"Steps:", "  2. build", "  3. test", "", " Enter to select · Esc to cancel"}
	if got := Choices("claude", lines); got != nil {
		t.Errorf("list from 2: %+v", got)
	}
	if got := Choices("aider", []string{"1. a", "2. b", "(Y)es/(N)o"}); got != nil {
		t.Errorf("aider: %+v", got)
	}
}
