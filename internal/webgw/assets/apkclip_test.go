package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Clipboard in the APK (MOB-26): inside the termocode Android app the
// page copies through the app's TermocodeClipboard bridge, and keeps the
// browser's clipboard as the fallback. Every copy goes through
// lib/clip.ts writeText; only the pairing page (device.tsx, a laptop
// browser, its own small chunk) calls the browser directly.
func TestCopiesGoThroughTheAPKBridge(t *testing.T) {
	b, err := os.ReadFile("src/lib/clip.ts")
	if err != nil {
		t.Fatal(err)
	}
	clip := string(b)
	for _, want := range []string{"export function writeText", "TermocodeClipboard", "navigator.clipboard.writeText"} {
		if !strings.Contains(clip, want) {
			t.Errorf("lib/clip.ts lacks %q", want)
		}
	}
	err = filepath.WalkDir("src", func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !(strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx")) {
			return err
		}
		switch filepath.ToSlash(p) {
		case "src/lib/clip.ts", "src/device.tsx":
			return nil
		}
		s, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(s), "navigator.clipboard.writeText") {
			t.Errorf("%s copies past the APK bridge: use writeText from lib/clip", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	js, err := os.ReadFile("app.js")
	if err != nil {
		t.Fatal(err)
	}
	all := string(js)
	for _, c := range []string{"app-chunk.js", "app-chunk-2.js", "app-chunk-3.js", "workbench.js"} {
		if b, err := os.ReadFile(c); err == nil {
			all += string(b)
		}
	}
	if !strings.Contains(all, "TermocodeClipboard") {
		t.Error("the built page does not use the APK bridge: run go generate ./internal/webgw/assets")
	}
}
