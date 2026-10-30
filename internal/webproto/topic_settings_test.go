package webproto

import (
	"strings"
	"testing"
)

// Spec (WEB-46): the settings topic is typed for the page: a read-only
// list of allow-listed keys; its delta is the whole list again.
func TestSettingsDataGenerated(t *testing.T) {
	b, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	ts := string(b)
	for _, want := range []string{
		`  "settings": { snap: SettingsSnap; delta: SettingsSnap };`,
		`export interface SettingItem {`,
		`  type: "bool" | "int" | "number" | "enum" | "string";`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
}
