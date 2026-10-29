package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/termimg"
)

func writeMemoConfig(t testing.TB, dir, body string) {
	t.Helper()
	p := filepath.Join(dir, "termocode", "config.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// bigTestConfig is a config.json with many settings, like a used one.
func bigTestConfig(images string) string {
	var b strings.Builder
	b.WriteString(`{"theme": "vscode-dark-plus", "icons": "nerd", "animations": "on", "images": "` + images + `"`)
	for _, k := range []string{"font_delta", "tab_size", "search_max_results", "secret_lock_minutes"} {
		b.WriteString(`, "` + k + `": 3`)
	}
	b.WriteString(`, "mcp_server_auto_approve": {"a": ["x", "y"], "b": ["z"]}`)
	b.WriteString(`, "key_hints": true, "auto_save": true, "word_wrap": false, "secret_store": "auto", "mouse_hover": "on"`)
	b.WriteString(`, "containers_sections": ["containers", "images", "volumes", "networks"]}`)
	return b.String()
}

// imageProtocol runs on every frame: it must not read and decode
// config.json each time, yet follow every change to the file.
func TestImageProtocolFollowsConfigWithoutRereading(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	old := termCaps
	termCaps = termimg.Caps{Protocol: termimg.Kitty}
	t.Cleanup(func() { termCaps = old })

	if got := imageProtocol(); got != termimg.Kitty {
		t.Fatalf("no config: %v, want kitty (auto)", got)
	}
	writeMemoConfig(t, dir, bigTestConfig("off"))
	if got := imageProtocol(); got != termimg.None {
		t.Fatalf("images=off: %v, want none", got)
	}
	if a := testing.AllocsPerRun(50, func() { _ = imageProtocol() }); a > 3 {
		t.Fatalf("unchanged config: %v allocs per call, want <= 3 (file read again)", a)
	}
	writeMemoConfig(t, dir, bigTestConfig("blocks"))
	if got := imageProtocol(); got != termimg.Blocks {
		t.Fatalf("images=blocks: %v, want blocks", got)
	}
	// The same size, another value.
	writeMemoConfig(t, dir, bigTestConfig("kitty"))
	writeMemoConfig(t, dir, bigTestConfig("sixel"))
	if got, want := imageProtocol(), termimg.Resolve(termimg.ModeSixel, termCaps, true); got != want {
		t.Fatalf("images=sixel: %v, want %v", got, want)
	}
	if err := os.Remove(filepath.Join(dir, "termocode", "config.json")); err != nil {
		t.Fatal(err)
	}
	if got := imageProtocol(); got != termimg.Kitty {
		t.Fatalf("config removed: %v, want kitty (auto)", got)
	}
	other := t.TempDir()
	writeMemoConfig(t, other, bigTestConfig("off"))
	t.Setenv("XDG_CONFIG_HOME", other)
	if got := imageProtocol(); got != termimg.None {
		t.Fatalf("other config dir: %v, want none", got)
	}
}

// BenchmarkImageProtocol is the per-frame image check with a config file.
func BenchmarkImageProtocol(b *testing.B) {
	dir := b.TempDir()
	b.Setenv("XDG_CONFIG_HOME", dir)
	writeMemoConfig(b, dir, bigTestConfig("auto"))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = imageProtocol()
	}
}
