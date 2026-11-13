package assets

import (
	"os"
	"strings"
	"testing"
)

// Spec (CMP-47): a tap on a push opens the panel on the cards (a closed
// page by ?open=cards, an open one by a message of the worker), and the
// phone's page says when it cannot get pushes.
func TestPushOpenAndUnavailableNote(t *testing.T) {
	read := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	push, mainTsx, phone := read("src/lib/push.ts"), read("src/main.tsx"), read("src/views/phone/ThisPhone.tsx")
	for _, want := range []string{"export function watchOpen", `"message"`, "open=cards"} {
		if !strings.Contains(push, want) {
			t.Errorf("lib/push.ts lacks %s", want)
		}
	}
	for _, want := range []string{"watchOpen(", "openCards()"} {
		if !strings.Contains(mainTsx, want) {
			t.Errorf("main.tsx lacks %s", want)
		}
	}
	if !strings.Contains(phone, "canPush()") || !strings.Contains(phone, "ph.alerts_nopush") {
		t.Error("This phone does not say when push is not possible")
	}
	if !strings.Contains(read("src/strings.phone.ts"), `"ph.alerts_nopush"`) {
		t.Error("strings.phone.ts lacks ph.alerts_nopush")
	}
}
