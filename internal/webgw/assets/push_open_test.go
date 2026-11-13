package assets

import (
	"os"
	"strings"
	"testing"
)

// Spec (CMP-47): a tap on a push opens the panel on the cards (a closed
// page by ?open=cards, an open one by a message of the worker), and a
// page that cannot get pushes says so, in both languages.
func TestPushOpenAndUnavailableNote(t *testing.T) {
	read := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	push, mainTsx, field := read("src/lib/push.ts"), read("src/main.tsx"), read("src/ui/NotifyField.tsx")
	for _, want := range []string{"export function watchOpen", `"message"`, `"open"`} {
		if !strings.Contains(push, want) {
			t.Errorf("lib/push.ts lacks %s", want)
		}
	}
	for _, want := range []string{"watchOpen(", `"open"`, "openCards()"} {
		if !strings.Contains(mainTsx, want) {
			t.Errorf("main.tsx lacks %s", want)
		}
	}
	if !strings.Contains(field, "canPush()") || !strings.Contains(field, "notify.nopush") {
		t.Error("the notifications field does not say when push is not possible")
	}
	for _, f := range []string{"src/strings.ts", "src/strings.fa.ts"} {
		if !strings.Contains(read(f), `"notify.nopush"`) {
			t.Errorf("%s lacks notify.nopush", f)
		}
	}
}
