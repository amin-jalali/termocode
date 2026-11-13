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
	for _, want := range []string{"export async function pushOn"} {
		if !strings.Contains(push, want) {
			t.Errorf("lib/push.ts lacks %s", want)
		}
	}
	for _, want := range []string{`"message"`, "open=cards", "openCards()"} {
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

// Spec (CMP-47): This phone lists the devices that get pushes (name and
// day added) with a Remove button for each, for a controller only; the
// list comes from push.list and a removal is push.remove, both texts in
// strings.phone.ts.
func TestPushDeviceListOnThisPhone(t *testing.T) {
	read := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	phone, strs := read("src/views/phone/ThisPhone.tsx"), read("src/strings.phone.ts")
	for _, want := range []string{`"push.list"`, `"push.remove"`, "ph.push_remove", "ph.push_title", `role === "controller"`} {
		if !strings.Contains(phone, want) {
			t.Errorf("ThisPhone.tsx lacks %s", want)
		}
	}
	for _, key := range []string{"ph.push_title", "ph.push_none", "ph.push_remove", "ph.push_added"} {
		if !strings.Contains(strs, `"`+key+`"`) {
			t.Errorf("strings.phone.ts lacks %s", key)
		}
	}
}
