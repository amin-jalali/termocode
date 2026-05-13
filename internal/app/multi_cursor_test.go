package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPluginsDirRespectsOverride verifies the env override wins over both
// XDG_DATA_HOME and the user's home directory.
func TestPluginsDirRespectsOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(pluginsDirEnv, tmp)
	t.Setenv("XDG_DATA_HOME", "/should/not/be/used")
	got, err := pluginsDir()
	if err != nil {
		t.Fatalf("pluginsDir: %v", err)
	}
	if got != tmp {
		t.Errorf("pluginsDir = %q, want %q (override should win)", got, tmp)
	}
}

// TestPluginsDirFallsBackToXDG verifies the XDG_DATA_HOME path when there's
// no explicit override.
func TestPluginsDirFallsBackToXDG(t *testing.T) {
	t.Setenv(pluginsDirEnv, "")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-test")
	got, err := pluginsDir()
	if err != nil {
		t.Fatalf("pluginsDir: %v", err)
	}
	want := filepath.Join("/tmp/xdg-test", "termocode", "plugins")
	if got != want {
		t.Errorf("pluginsDir = %q, want %q", got, want)
	}
}

// TestEnsureVimVisualMultiIdempotent makes sure that when the plugin dir
// already contains a .git subdir, the function returns the existing path
// without attempting a clone. We simulate the clone result by pre-creating
// .git ourselves; if ensureVimVisualMulti tried to clone, it would fail
// (we'd see a warn) — but should silently succeed instead.
func TestEnsureVimVisualMultiIdempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(pluginsDirEnv, tmp)

	dest := filepath.Join(tmp, "vim-visual-multi", ".git")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	path, warn := ensureVimVisualMulti()
	if warn != "" {
		t.Errorf("warn = %q, want empty", warn)
	}
	if path != filepath.Join(tmp, "vim-visual-multi") {
		t.Errorf("path = %q, want %s/vim-visual-multi", path, tmp)
	}
}

// TestMultiCursorSetupLuaContainsBindings sanity-checks that the generated
// Lua chunk references the spec'd shortcuts and the plugin path.
func TestMultiCursorSetupLuaContainsBindings(t *testing.T) {
	chunk := multiCursorSetupLua("/some/path/vim-visual-multi")
	for _, want := range []string{
		"<C-A-Up>",
		"<C-A-Down>",
		"<C-d>",
		"['Exit']",
		"/some/path/vim-visual-multi",
		"vim.opt.rtp:prepend",
	} {
		if !strings.Contains(chunk, want) {
			t.Errorf("multiCursorSetupLua missing %q", want)
		}
	}
}

// TestLuaEscape verifies single-quote and backslash escaping.
func TestLuaEscape(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"abc", "abc"},
		{`a\b`, `a\\b`},
		{"a'b", `a\'b`},
		{`a\'b`, `a\\\'b`},
	}
	for _, tc := range cases {
		if got := luaEscape(tc.in); got != tc.want {
			t.Errorf("luaEscape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
