package containerhost

import (
	"errors"
	"testing"

	"github.com/amin-jalali/termocode/internal/workspace"
)

func TestResolve(t *testing.T) {
	ok := map[string]string{
		"":             "/app",
		".":            "/app",
		"a/b":          "/app/a/b",
		"/app/a/../b":  "/app/b",
		"a/./b/":       "/app/a/b",
		"-rf":          "/app/-rf",
		"$(reboot)":    "/app/$(reboot)",
		"a\nb":         "/app/a\nb",
		"it's \"x\" y": "/app/it's \"x\" y",
		"/app":         "/app",
		"/app/":        "/app",
	}
	for in, want := range ok {
		got, err := resolve("/app", in)
		if err != nil || got != want {
			t.Errorf("resolve(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"..", "../x", "a/../../x", "/etc/passwd", "/application", "/"} {
		if _, err := resolve("/app", in); !errors.Is(err, workspace.ErrOutsideRoot) {
			t.Errorf("resolve(%q) err = %v", in, err)
		}
	}
	if _, err := resolve("/app", "a\x00b"); err == nil {
		t.Error("NUL byte accepted")
	}
	if _, err := resolve("app", "x"); err == nil {
		t.Error("relative root accepted")
	}
	if got, err := resolve("/", "etc/x"); err != nil || got != "/etc/x" {
		t.Errorf("root / = %q, %v", got, err)
	}
}
