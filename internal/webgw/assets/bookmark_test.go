package assets

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Spec (WEBP-12): the bookmark key of a remembered browser lives in one
// module, net/devicekey.ts: only it names the storage key and the
// device_key reply field, it sends the key only in the body of
// /login/device and /login/forget, and it never touches the URL, the
// history or the console, so the key never lands in a URL, a bookmark,
// a log or a screenshot of the address bar. Two tabs use it one at a
// time (Web Locks), so a rotation never looks like a stolen copy.
func TestBookmarkKeyStaysInItsModule(t *testing.T) {
	b, err := os.ReadFile("src/net/devicekey.ts")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{`"termocode.device"`, "localStorage", `postJSON("/login/device"`, `postJSON("/login/forget"`, "navigator.locks", "device_key"} {
		if !strings.Contains(src, want) {
			t.Errorf("devicekey.ts has no %s", want)
		}
	}
	never := regexp.MustCompile(`\b(location|history|console|URLSearchParams|sessionStorage|document)\b|new URL\(|\?key=`)
	if m := never.FindString(src); m != "" {
		t.Errorf("devicekey.ts uses %s", m)
	}
	err = filepath.WalkDir("src", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == filepath.Join("src", "net", "devicekey.ts") {
			return err
		}
		if ext := filepath.Ext(path); ext != ".ts" && ext != ".tsx" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// auth.ts may list the two paths in postJSON's type only.
		for _, s := range []string{"termocode.device", "device_key", `"/login/device"`, `"/login/forget"`} {
			if strings.Contains(string(b), s) && path != filepath.Join("src", "net", "auth.ts") {
				t.Errorf("%s names %s (only net/devicekey.ts may)", path, s)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The sign-in and pairing screens offer "Remember this browser", off
// until ticked, and send it as remember; the page tries a kept key on
// its own when it opens without a session.
func TestRememberIsOptIn(t *testing.T) {
	for _, f := range []string{"src/ui/SignInScreen.tsx", "src/ui/PairScreen.tsx"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if !strings.Contains(s, "<RememberBox") || !strings.Contains(s, "remember: remember.value") {
			t.Errorf("%s does not offer and send Remember this browser", f)
		}
	}
	b, _ := os.ReadFile("src/ui/RememberBox.tsx")
	if !regexp.MustCompile(`signal\(false\)`).Match(b) {
		t.Error("Remember this browser is not off at first")
	}
	m, _ := os.ReadFile("src/main.tsx")
	if !strings.Contains(string(m), "savedDeviceKey()") || !strings.Contains(string(m), "signInWithBookmark(") {
		t.Error("the page does not try its kept key on open")
	}
}
